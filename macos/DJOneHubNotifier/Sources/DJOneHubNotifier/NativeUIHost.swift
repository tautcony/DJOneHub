import AppKit
import Foundation
import UserNotifications

// The C ABI the Go bridge calls into. These symbols are exported from the
// static library and declared in internal/platform/darwin/native/bridge.h.
// native_ui_start must be called from the process main thread (the Go main
// goroutine pins it with runtime.LockOSThread) and blocks until the UI run
// loop exits.

public typealias NativeUICommandCallback = @convention(c) (UnsafePointer<CChar>?) -> Void
public typealias NativeUIReadyCallback = @convention(c) () -> Void

@_cdecl("native_ui_start")
public func nativeUIStart(
    configJSON: UnsafePointer<CChar>?,
    onCommand: NativeUICommandCallback?,
    onReady: NativeUIReadyCallback?
) {
    // The Go bridge guarantees this runs on the process main thread (the Go
    // main goroutine pins it with runtime.LockOSThread before calling in).
    let config = configJSON.map { String(cString: $0) }
    MainActor.assumeIsolated {
        NativeUIHost.shared.start(configJSON: config, onCommand: onCommand, onReady: onReady)
    }
}

@_cdecl("native_ui_handle_event")
public func nativeUIHandleEvent(eventJSON: UnsafePointer<CChar>?) {
    guard let eventJSON, let json = String(cString: eventJSON) as String? else { return }
    NativeUIHost.shared.handleEvent(json: json)
}

@_cdecl("native_ui_stop")
public func nativeUIStop() {
    NativeUIHost.shared.stop()
}

// NativeUIHost owns the NSApplication lifecycle and routes bridge events to
// the UI coordinator on the main thread. Event and stop entry points are
// nonisolated because they are called from Go worker goroutines; they hop to
// the main thread before touching any UI state.
@MainActor
final class NativeUIHost {
    nonisolated static let shared = NativeUIHost()

    private var coordinator: UIAppDelegate?
    private var onCommand: NativeUICommandCallback?
    private var onReady: NativeUIReadyCallback?
    private var ready = false
    private var pendingEvents: [BridgeEvent] = []

    nonisolated private init() {}

    func start(configJSON: String?, onCommand: NativeUICommandCallback?, onReady: NativeUIReadyCallback?) {
        let delegate = UIAppDelegate()
        coordinator = delegate
        self.onCommand = onCommand
        self.onReady = onReady
        if let configJSON {
            delegate.applyConfig(configJSON)
        }
        let app = NSApplication.shared
        app.delegate = delegate
        app.setActivationPolicy(.accessory)
        app.run()
    }

    nonisolated func handleEvent(json: String) {
        guard let event = BridgeEvent.parse(json) else {
            return
        }
        DispatchQueue.main.async { [weak self] in
            self?.deliver(event)
        }
    }

    nonisolated func stop() {
        DispatchQueue.main.async {
            NSApplication.shared.terminate(nil)
        }
    }

    private func deliver(_ event: BridgeEvent) {
        guard let coordinator else { return }
        guard ready else {
            pendingEvents.append(event)
            return
        }
        coordinator.handleEvent(event)
    }

    fileprivate func markReady() {
        guard let coordinator else { return }
        guard !ready else { return }
        ready = true
        onReady?()
        let events = pendingEvents
        pendingEvents.removeAll()
        for event in events {
            coordinator.handleEvent(event)
        }
    }

    fileprivate func sendCommand(_ name: String, params: [String: String] = [:]) {
        let command = Command(name: name, params: params.isEmpty ? nil : params)
        guard let data = try? JSONEncoder().encode(command),
              let json = String(data: data, encoding: .utf8)
        else {
            return
        }
        json.withCString { pointer in
            onCommand?(pointer)
        }
    }
}

// UIAppDelegate owns system notifications and the cellular menu bar status
// item. It is driven entirely by bridge events: no polling, no HTTP, no dedup.
@MainActor
final class UIAppDelegate: NSObject, NSApplicationDelegate, @MainActor UNUserNotificationCenterDelegate {
    private let notificationService = NativeNotificationService()
    private let panel = NotifierPanel()
    private var notificationPreferences = NotificationPreferences.system
    private var webURL: URL?

    private var cellularStatusItem: NSStatusItem?
    private var activeCallID: String?
    private var activeCallNumber: String?
    private var activeCall: CallEvent?
    private var activeCallUsesCustomPanel = false
    private var rejectingCallID: String?

    func applyConfig(_ json: String) {
        guard let data = json.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else {
            return
        }
        if let urlString = object["web_url"] as? String {
            webURL = URL(string: urlString)
        }
        if let preferencesObject = object["notification_preferences"] as? [String: Any],
           let preferencesData = try? JSONSerialization.data(withJSONObject: preferencesObject),
           let preferences = try? JSONDecoder().decode(NotificationPreferences.self, from: preferencesData) {
            notificationPreferences = preferences
        }
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        notificationService.configure(delegate: self) { status in
            NativeUIHost.shared.sendCommand(
                Command.notificationPermissionStatus,
                params: ["state": status]
            )
        }
        NativeUIHost.shared.markReady()
    }

    func applicationWillTerminate(_ notification: Notification) {
        removeCellularStatusItem()
        panel.hide()
    }

    // MARK: - Bridge event handling

    func handleEvent(_ event: BridgeEvent) {
        switch event.type {
        case BridgeEventType.callIncoming:
            guard let call = event.decode(CallEvent.self) else { return }
            activeCallID = call.id
            activeCallNumber = call.number
            activeCall = call
            activeCallUsesCustomPanel = notificationPreferences.incomingCall == "custom"
            rejectingCallID = nil
            if activeCallUsesCustomPanel {
                showCustomIncoming(call, rejecting: false)
            } else {
                notificationService.showIncomingCall(call)
            }
        case BridgeEventType.callUpdated:
            guard let call = event.decode(CallEvent.self), call.id == activeCallID else { return }
            activeCallNumber = call.number
            activeCall = call
            if activeCallUsesCustomPanel {
                showCustomIncoming(call, rejecting: rejectingCallID == call.id)
            } else {
                notificationService.updateIncomingCall(call)
            }
        case BridgeEventType.callEnded:
            guard let call = event.decode(CallEvent.self) else { return }
            if call.id == activeCallID, activeCallUsesCustomPanel {
                panel.hide()
            } else {
                notificationService.removeCall(callID: call.id)
            }
            if call.id == activeCallID {
                activeCallID = nil
                activeCallNumber = nil
                activeCall = nil
                activeCallUsesCustomPanel = false
                rejectingCallID = nil
            }
        case BridgeEventType.callMissed:
            guard let call = event.decode(CallEvent.self) else { return }
            if notificationPreferences.missedCall == "custom" {
                panel.show(
                    .missed(
                        number: NotificationText.displayNumber(call.number),
                        startedAt: call.startedAt
                    ),
                    onReject: {},
                    onOpen: { [weak self] in self?.openDashboard() }
                )
            } else {
                notificationService.showMissedCall(call)
            }
        case BridgeEventType.smsReceived:
            guard let message = event.decode(SMSMessageEvent.self) else { return }
            if notificationPreferences.sms == "custom" {
                NSSound(named: "Glass")?.play()
                panel.show(
                    .sms(
                        sender: (message.sender?.isEmpty == false) ? message.sender! : "未知发送方",
                        preview: NotificationText.smsPreview(message)
                    ),
                    onReject: {},
                    onOpen: { [weak self] in self?.openDashboard() }
                )
            } else {
                notificationService.showSMS(message, eventID: event.id)
            }
        case BridgeEventType.deviceOffline:
            guard let offline = event.decode(DeviceOfflineEvent.self) else { return }
            if notificationPreferences.deviceOffline == "custom" {
                panel.show(
                    .error(message: NotificationText.offlineDetail(reason: offline.reason, lastError: offline.lastError)),
                    onReject: {},
                    onOpen: { [weak self] in self?.openDashboard() }
                )
            } else {
                notificationService.showOffline(offline)
            }
        case BridgeEventType.networkUpdated:
            guard let state = event.decode(NetworkUpdateEvent.self) else { return }
            applyNetwork(state)
        case BridgeEventType.callRejectSucceeded:
            if let result = event.decode(RejectResult.self), result.callId == rejectingCallID {
                rejectingCallID = nil
                if result.callId == activeCallID {
                    activeCallID = nil
                    activeCallNumber = nil
                    activeCall = nil
                }
                if let callID = result.callId {
                    if activeCallUsesCustomPanel {
                        panel.hide()
                    } else {
                        notificationService.removeCall(callID: callID)
                    }
                }
                activeCallUsesCustomPanel = false
            }
        case BridgeEventType.callRejectFailed:
            if let result = event.decode(RejectResult.self), result.callId == rejectingCallID {
                rejectingCallID = nil
                if let callID = result.callId {
                    let message = result.error ?? "拒接失败"
                    if activeCallUsesCustomPanel {
                        panel.show(
                            .error(message: message),
                            onReject: {},
                            onOpen: { [weak self] in self?.openDashboard() }
                        )
                    } else {
                        notificationService.showRejectFailure(callID: callID, message: message)
                    }
                }
            }
        case BridgeEventType.notificationPreferencesUpdated:
            if let preferences = event.decode(NotificationPreferences.self) {
                applyNotificationPreferences(preferences)
            }
        case BridgeEventType.notificationPermissionRequest:
            notificationService.requestAuthorization()
        case BridgeEventType.notificationPermissionOpenSettings:
            notificationService.openSettings()
        case BridgeEventType.dashboardOpened:
            if let opened = event.decode(DashboardOpened.self), let url = URL(string: opened.url) {
                NSWorkspace.shared.open(url)
            }
        default:
            break
        }
    }

    // MARK: - User actions

    private func rejectCall(callID: String) {
        guard !callID.isEmpty, rejectingCallID == nil else { return }
        if let activeCallID, activeCallID != callID {
            return
        }
        activeCallID = activeCallID ?? callID
        rejectingCallID = callID
        if activeCallUsesCustomPanel {
            panel.show(
                .incoming(
                    number: NotificationText.displayNumber(activeCallNumber),
                    startedAt: activeCall?.startedAt ?? Date(),
                    state: activeCall?.state ?? "incoming",
                    rejecting: true
                ),
                onReject: {},
                onOpen: { [weak self] in self?.openDashboard() }
            )
        }
        NativeUIHost.shared.sendCommand(Command.rejectCall, params: ["call_id": callID])
    }

    private func rejectActiveCall() {
        guard let callID = activeCallID else { return }
        rejectCall(callID: callID)
    }

    private func openDashboard() {
        NativeUIHost.shared.sendCommand(Command.openDashboard)
    }

    private func showCustomIncoming(_ call: CallEvent, rejecting: Bool) {
        panel.show(
            .incoming(
                number: NotificationText.displayNumber(call.number),
                startedAt: call.startedAt,
                state: call.state,
                rejecting: rejecting
            ),
            onReject: { [weak self] in self?.rejectActiveCall() },
            onOpen: { [weak self] in self?.openDashboard() }
        )
    }

    var presentedPanelContent: PanelContent? {
        panel.currentContent
    }

    private func applyNotificationPreferences(_ preferences: NotificationPreferences) {
        let previous = notificationPreferences
        notificationPreferences = preferences

        guard let activeCall,
              previous.incomingCall != preferences.incomingCall
        else {
            return
        }

        if preferences.incomingCall == "custom" {
            notificationService.removeCall(callID: activeCall.id)
            activeCallUsesCustomPanel = true
            showCustomIncoming(activeCall, rejecting: false)
        } else {
            panel.hide()
            activeCallUsesCustomPanel = false
            notificationService.showIncomingCall(activeCall)
        }
    }

    // MARK: - System notification actions

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping @Sendable (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .sound])
    }

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping @Sendable () -> Void
    ) {
        defer { completionHandler() }
        switch response.actionIdentifier {
        case NativeNotificationAction.rejectCall:
            if let callID = response.notification.request.content.userInfo[NativeNotificationUserInfoKey.callID] as? String {
                rejectCall(callID: callID)
            }
        case NativeNotificationAction.openDashboard, UNNotificationDefaultActionIdentifier:
            openDashboard()
        default:
            break
        }
    }

    private func applyNetwork(_ state: NetworkUpdateEvent) {
        guard state.registered,
              Self.isCellularNetwork(state.networkMode),
              let signalDBM = state.signalDBM
        else {
            removeCellularStatusItem()
            return
        }
        showCellularStatusItem(signalLevel: Self.cellularSignalLevel(signalDBM))
    }

    private func showCellularStatusItem(signalLevel: Int) {
        if cellularStatusItem == nil {
            cellularStatusItem = NSStatusBar.system.statusItem(withLength: 42)
            cellularStatusItem?.button?.target = self
            cellularStatusItem?.button?.action = #selector(openDJOneHubFromCellularMenuBar)
        }
        cellularStatusItem?.button?.image = Self.cellularStatusImage(signalLevel: signalLevel)
        cellularStatusItem?.button?.toolTip = "DJOneHub 4G 正在接管网络；点击打开控制面板"
    }

    private func removeCellularStatusItem() {
        guard let cellularStatusItem else { return }
        NSStatusBar.system.removeStatusItem(cellularStatusItem)
        self.cellularStatusItem = nil
    }

    @objc private func openDJOneHubFromCellularMenuBar() {
        openDashboard()
    }

    // MARK: - Drawing helpers (unchanged from the legacy notifier)

    private static func isCellularNetwork(_ mode: String?) -> Bool {
        let normalized = mode?.uppercased() ?? ""
        return normalized.contains("LTE") || normalized.contains("4G")
    }

    private static func cellularSignalLevel(_ dbm: Int) -> Int {
        if dbm >= -60 { return 4 }
        if dbm >= -75 { return 3 }
        if dbm >= -90 { return 2 }
        return 1
    }

    // A compact stepped mobile-signal indicator. Weakening signal fades the
    // highest missing bar instead of removing it, so the level stays legible.
    // The "4G" text is part of a template image and follows the menu-bar theme.
    private static func cellularStatusImage(signalLevel: Int) -> NSImage {
        let image = NSImage(size: NSSize(width: 42, height: 18))
        image.lockFocus()
        let active = NSColor.black
        let inactive = NSColor.black.withAlphaComponent(0.28)
        for (index, height) in [4.2, 7.4, 10.6, 13.8].enumerated() {
            (index < signalLevel ? active : inactive).setFill()
            let bar = NSBezierPath(
                roundedRect: NSRect(x: CGFloat(index) * 5.2, y: 1, width: 3.6, height: height),
                xRadius: 0.9,
                yRadius: 0.9
            )
            bar.fill()
        }
        let label = "4G" as NSString
        label.draw(
            at: NSPoint(x: 24, y: 3),
            withAttributes: [
                .font: NSFont.monospacedSystemFont(ofSize: 10, weight: .semibold),
                .foregroundColor: NSColor.black,
            ]
        )
        image.unlockFocus()
        image.isTemplate = true
        image.accessibilityDescription = "DJOneHub 4G 信号 \(signalLevel) 格"
        return image
    }

}
