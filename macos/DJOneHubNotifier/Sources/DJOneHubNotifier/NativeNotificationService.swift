import AppKit
import Foundation
import UserNotifications

enum NativeNotificationCategory {
    static let incomingCall = "djonehub.incoming-call"
    static let standard = "djonehub.standard"
}

enum NativeNotificationAction {
    static let rejectCall = "djonehub.reject-call"
    static let openDashboard = "djonehub.open-dashboard"
}

enum NativeNotificationUserInfoKey {
    static let callID = "call_id"
}

@MainActor
final class NativeNotificationService {
    let center: UNUserNotificationCenter?
    private var permissionStatusHandler: ((String) -> Void)?
    private lazy var timeFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.dateStyle = .none
        formatter.timeStyle = .short
        return formatter
    }()

    init(center: UNUserNotificationCenter? = nil) {
        if let center {
            self.center = center
        } else if Bundle.main.bundleURL.pathExtension == "app", Bundle.main.bundleIdentifier != nil {
            // UserNotifications requires an application bundle. A raw `go run`
            // executable has no bundle identity, so it must stay in custom UI
            // mode instead of calling UNUserNotificationCenter.current().
            self.center = UNUserNotificationCenter.current()
        } else {
            self.center = nil
        }
    }

    func configure(
        delegate: UNUserNotificationCenterDelegate,
        permissionStatusHandler: @escaping (String) -> Void
    ) {
        self.permissionStatusHandler = permissionStatusHandler
        guard let center else {
            permissionStatusHandler("unsupported")
            return
        }
        center.delegate = delegate
        center.setNotificationCategories([
            UNNotificationCategory(
                identifier: NativeNotificationCategory.incomingCall,
                actions: [
                    UNNotificationAction(
                        identifier: NativeNotificationAction.rejectCall,
                        title: "拒接",
                        options: [.destructive]
                    ),
                    UNNotificationAction(
                        identifier: NativeNotificationAction.openDashboard,
                        title: "详情",
                        options: [.foreground]
                    ),
                ],
                intentIdentifiers: [],
                options: []
            ),
            UNNotificationCategory(
                identifier: NativeNotificationCategory.standard,
                actions: [
                    UNNotificationAction(
                        identifier: NativeNotificationAction.openDashboard,
                        title: "打开 DJOneHub",
                        options: [.foreground]
                    ),
                ],
                intentIdentifiers: [],
                options: []
            ),
        ])

        refreshAuthorizationStatus()
    }

    func requestAuthorization() {
        guard let center else {
            permissionStatusHandler?("unsupported")
            return
        }
        center.requestAuthorization(options: [.alert, .sound]) { [weak self] _, error in
            if let error {
                nativeBridgeLog(NativeLogLevel.error, "DJOneHub notification authorization failed", ["error": error.localizedDescription])
            }
            Task { @MainActor in
                self?.refreshAuthorizationStatus()
            }
        }
    }

    func openSettings() {
        guard center != nil else { return }
        guard let url = URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension") else {
            return
        }
        NSWorkspace.shared.open(url)
    }

    private func refreshAuthorizationStatus() {
        guard let center else {
            permissionStatusHandler?("unsupported")
            return
        }
        center.getNotificationSettings { [weak self] settings in
            let state: String
            switch settings.authorizationStatus {
            case .notDetermined:
                state = "not_determined"
            case .authorized:
                state = "authorized"
            case .denied:
                state = "denied"
            case .provisional:
                state = "provisional"
            case .ephemeral:
                state = "authorized"
            @unknown default:
                state = "unknown"
            }
            Task { @MainActor in
                self?.permissionStatusHandler?(state)
            }
        }
    }

    func showIncomingCall(_ call: CallEvent) {
        presentIncomingCall(call, sound: .default)
    }

    func updateIncomingCall(_ call: CallEvent) {
        presentIncomingCall(call, sound: nil)
    }

    private func presentIncomingCall(_ call: CallEvent, sound: UNNotificationSound?) {
        let number = NotificationText.displayNumber(call.number)
        let content = UNMutableNotificationContent()
        content.title = call.state == "active" ? "DJOneHub 通话中" : "DJOneHub 来电"
        content.subtitle = number
        content.body = call.state == "active"
            ? "通话开始于 \(timeFormatter.string(from: call.startedAt))。点击通知查看详情。"
            : "收到来电，时间 \(timeFormatter.string(from: call.startedAt))。点击通知查看详情。"
        content.sound = sound
        // 来电使用 time-sensitive 中断级别: Focus 模式下仍可打断, 最终呈现
        // 仍受用户的通知授权与 Focus 策略约束。
        content.interruptionLevel = .timeSensitive
        content.categoryIdentifier = NativeNotificationCategory.incomingCall
        content.userInfo = [
            NativeNotificationUserInfoKey.callID: call.id,
        ]
        enqueue(content, identifier: incomingCallIdentifier(call.id))
    }

    func showMissedCall(_ call: CallEvent) {
        removeCall(callID: call.id)

        let content = UNMutableNotificationContent()
        content.title = "未接来电"
        content.subtitle = NotificationText.displayNumber(call.number)
        content.body = "来电时间 \(timeFormatter.string(from: call.startedAt))。点击通知查看详情。"
        content.categoryIdentifier = NativeNotificationCategory.standard
        content.userInfo = [
            NativeNotificationUserInfoKey.callID: call.id,
        ]
        enqueue(content, identifier: missedCallIdentifier(call.id))
    }

    func showSMS(_ message: SMSMessageEvent, eventID: UInt64, senderOnly: Bool) {
        let sender = (message.sender?.isEmpty == false) ? message.sender! : "未知发送方"
        let content = UNMutableNotificationContent()
        content.title = sender
        content.subtitle = "DJOneHub 短信"
        // "仅显示发送方" 偏好开启时通知请求不携带正文, 使 banner/锁屏/通知中心
        // 均不出现短信内容 (含一次性验证码)。
        if !senderOnly {
            content.body = NotificationText.smsPreview(message)
        }
        content.sound = .default
        content.categoryIdentifier = NativeNotificationCategory.standard
        enqueue(content, identifier: "sms-\(eventID)")
    }

    func showOffline(_ event: DeviceOfflineEvent) {
        let content = UNMutableNotificationContent()
        content.title = "DJOneHub 暂时离线"
        content.body = NotificationText.offlineDetail(reason: event.reason, lastError: event.lastError)
        content.categoryIdentifier = NativeNotificationCategory.standard
        enqueue(content, identifier: "device-offline")
    }

    func showRejectFailure(callID: String, message: String) {
        removeCall(callID: callID)

        let content = UNMutableNotificationContent()
        content.title = "拒接失败"
        content.subtitle = "DJOneHub"
        content.body = message
        content.categoryIdentifier = NativeNotificationCategory.standard
        content.userInfo = [
            NativeNotificationUserInfoKey.callID: callID,
        ]
        enqueue(content, identifier: "call-reject-error-\(callID)")
    }

    func removeCall(callID: String) {
        guard let center else { return }
        let identifiers = [incomingCallIdentifier(callID)]
        center.removePendingNotificationRequests(withIdentifiers: identifiers)
        center.removeDeliveredNotifications(withIdentifiers: identifiers)
    }

    private func incomingCallIdentifier(_ callID: String) -> String {
        "incoming-call-\(callID)"
    }

    private func missedCallIdentifier(_ callID: String) -> String {
        "missed-call-\(callID)"
    }

    private func enqueue(_ content: UNMutableNotificationContent, identifier: String) {
        guard let center else {
            nativeBridgeLog(NativeLogLevel.warn, "DJOneHub notification skipped: center unavailable", ["identifier": identifier])
            return
        }
        center.removePendingNotificationRequests(withIdentifiers: [identifier])
        center.removeDeliveredNotifications(withIdentifiers: [identifier])
        let request = UNNotificationRequest(identifier: identifier, content: content, trigger: nil)
        nativeBridgeLog(NativeLogLevel.debug, "DJOneHub notification enqueue", ["identifier": identifier])
        center.add(request) { error in
            if let error {
                nativeBridgeLog(NativeLogLevel.error, "DJOneHub notification delivery failed", ["identifier": identifier, "error": error.localizedDescription])
            } else {
                nativeBridgeLog(NativeLogLevel.info, "DJOneHub notification accepted", ["identifier": identifier])
            }
        }
    }
}
