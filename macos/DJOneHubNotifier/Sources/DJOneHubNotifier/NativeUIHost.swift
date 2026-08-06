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
            nativeBridgeLog(NativeLogLevel.warn, "DJOneHub native bridge rejected an invalid event")
            return
        }
        if event.type.hasPrefix("call.") {
            nativeBridgeLog(NativeLogLevel.debug, "DJOneHub native bridge received event", ["type": event.type, "id": String(event.id)])
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
        guard ready, let coordinator else {
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

// nativeBridgeLog routes a UI-layer trace back to the Go process so it lands
// in the same structured log pipeline as Go-side output instead of NSLog.
// The message must be a constant: dynamic values travel as structured fields
// and are only formatted by the Go logger after its level filter passes, so a
// filtered level costs nothing but the transport. Callable from any thread;
// delivery hops to the main actor where the command callback lives. Lines
// emitted before the bridge starts are dropped.
func nativeBridgeLog(_ level: String, _ message: String, _ fields: [String: String] = [:]) {
    Task { @MainActor in
        var params = fields
        params["level"] = level
        params["message"] = message
        NativeUIHost.shared.sendCommand(Command.log, params: params)
    }
}

// UIAppDelegate owns system notifications and the persistent DJOneHub menu bar
// status item. It is driven entirely by bridge events: no polling, no HTTP,
// no dedup.
@MainActor
final class UIAppDelegate: NSObject, NSApplicationDelegate, @MainActor UNUserNotificationCenterDelegate {
    private let notificationService = NativeNotificationService()
    private let panel = NotifierPanel()
    private var notificationPreferences = NotificationPreferences.system
    private var webURL: URL?

    private var statusItem: NSStatusItem?
    private var deviceStatus = DeviceStatusEvent(state: "absent", identity: nil, backend: nil, lastError: nil)
    private var networkStatus = NetworkUpdateEvent(mode: nil, networkMode: nil, registered: false, operatorName: nil, signalDBM: nil, simInserted: nil, simKnown: nil)
    private var activeCallID: String?
    private var activeCallNumber: String?
    private var activeCall: CallEvent?
    private var activeCallUsesCustomPanel = false
    private var rejectingCallID: String?
    private var rejectTimeoutTask: Task<Void, Never>?

    // rejectStateTimeout bounds the in-progress reject state: if no
    // callRejectSucceeded / callRejectFailed arrives within this window, the
    // state is cleared so a lost command or unresponsive device can never
    // strand the panel in "rejecting". Tests shorten it.
    static var rejectStateTimeout: TimeInterval = 8

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
        installStatusItem()
        notificationService.configure(delegate: self) { status in
            NativeUIHost.shared.sendCommand(
                Command.notificationPermissionStatus,
                params: ["state": status]
            )
        }
        NativeUIHost.shared.markReady()
    }

    func applicationWillTerminate(_ notification: Notification) {
        removeStatusItem()
        panel.hide()
    }

    // MARK: - Bridge event handling

    func handleEvent(_ event: BridgeEvent) {
        switch event.type {
        case BridgeEventType.deviceStatusChanged:
            if let status = event.decode(DeviceStatusEvent.self) {
                applyDeviceStatus(status)
            }
        case BridgeEventType.callIncoming:
            guard let call = event.decode(CallEvent.self) else { return }
            nativeBridgeLog(NativeLogLevel.debug, "DJOneHub native UI handling call.incoming", ["id": call.id, "state": call.state])
            activeCallID = call.id
            activeCallNumber = call.number
            activeCall = call
            activeCallUsesCustomPanel = notificationPreferences.incomingCall == "custom"
            clearRejectingState()
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
                clearRejectingState()
            }
        case BridgeEventType.callMissed:
            guard let call = event.decode(CallEvent.self) else { return }
            nativeBridgeLog(NativeLogLevel.debug, "DJOneHub native UI handling call.missed", ["id": call.id])
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
                clearRejectingState()
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
            // The rejecting state is cleared regardless of the call-ID match:
            // a failure (or a result for a stale call) must never leave the
            // panel stuck in "rejecting".
            guard let result = event.decode(RejectResult.self) else { return }
            clearRejectingState()
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
        case BridgeEventType.commandDropped:
            guard let dropped = event.decode(CommandDropped.self) else { return }
            if dropped.command == Command.rejectCall, rejectingCallID != nil {
                // The reject command never left the device: restore the
                // actionable buttons so the user can retry.
                clearRejectingState()
                if activeCallUsesCustomPanel, let activeCall {
                    showCustomIncoming(activeCall, rejecting: false)
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

    func rejectCall(callID: String) {
        guard !callID.isEmpty, rejectingCallID == nil else { return }
        if let activeCallID, activeCallID != callID {
            return
        }
        activeCallID = activeCallID ?? callID
        rejectingCallID = callID
        // Bound the rejecting state: if no result arrives in time, the state
        // is cleared and the buttons restored instead of staying stuck.
        rejectTimeoutTask?.cancel()
        rejectTimeoutTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(Self.rejectStateTimeout * 1_000_000_000))
            guard !Task.isCancelled else { return }
            self?.recoverRejectState()
        }
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

    // clearRejectingState cancels the reject timeout and resets the rejecting
    // state; the buttons are re-enabled by the caller for retry.
    private func clearRejectingState() {
        rejectTimeoutTask?.cancel()
        rejectTimeoutTask = nil
        rejectingCallID = nil
    }

    // recoverRejectState runs when the reject result never arrived within the
    // bounded timeout: the rejecting state is cleared and the actionable
    // buttons are restored so the user can retry.
    private func recoverRejectState() {
        guard rejectingCallID != nil else { return }
        clearRejectingState()
        if activeCallUsesCustomPanel, let activeCall {
            showCustomIncoming(activeCall, rejecting: false)
        }
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

    // 通知点击冷启动时，系统可能在后台队列调用这些回调。Swift 6 下若保持
    // @MainActor 隔离，动态隔离检查会中止进程；改为 nonisolated，状态访问
    // 切到主 actor，完成处理器在回调内同步调用（UNUserNotificationCenter
    // 要求）。
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping @Sendable (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .sound])
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping @Sendable () -> Void
    ) {
        completionHandler()
        // 先把非 Sendable 的 response 中需要的值提取为 Sendable 值，再切到
        // 主 actor 访问状态，避免把 response 送入 task 造成数据竞争。
        let actionIdentifier = response.actionIdentifier
        let callID = response.notification.request.content.userInfo[NativeNotificationUserInfoKey.callID] as? String
        Task { @MainActor in
            switch actionIdentifier {
            case NativeNotificationAction.rejectCall:
                if let callID {
                    rejectCall(callID: callID)
                }
            case NativeNotificationAction.openDashboard, UNNotificationDefaultActionIdentifier:
                openDashboard()
            default:
                break
            }
        }
    }

    private func applyNetwork(_ state: NetworkUpdateEvent) {
        networkStatus = state
        updateStatusItem()
    }

    private func applyDeviceStatus(_ status: DeviceStatusEvent) {
        deviceStatus = status
        updateStatusItem()
    }

    private func installStatusItem() {
        guard statusItem == nil else { return }
        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.button?.target = self
        item.button?.action = #selector(handleStatusItemClick)
        item.button?.sendAction(on: [.leftMouseUp, .rightMouseUp])
        statusItem = item
        updateStatusItem()
    }

    private func updateStatusItem() {
        guard let button = statusItem?.button else { return }
        button.image = Self.statusImage(device: deviceStatus, network: networkStatus)
        button.toolTip = statusSummary()
        button.setAccessibilityLabel("DJOneHub：\(statusSummary())")
    }

    private func statusSummary() -> String {
        switch deviceStatus.state {
        case "ready":
            if networkStatus.simKnown == true, networkStatus.simInserted != true {
                return "设备已连接，未插入 SIM 卡"
            }
            if networkStatus.registered {
                let mode = networkStatus.networkMode?.isEmpty == false ? networkStatus.networkMode! : "移动网络"
                let operatorName = networkStatus.operatorName?.isEmpty == false ? " · \(networkStatus.operatorName!)" : ""
                return "\(mode) 已注册\(operatorName)"
            }
            return "设备已连接，等待网络注册"
        case "connecting", "initializing", "discovered":
            return "正在连接设备"
        case "degraded":
            return "设备连接异常"
        case "disconnected", "absent":
            return "未检测到设备"
        default:
            return "设备状态未知"
        }
    }

    private func removeStatusItem() {
        guard let statusItem else { return }
        NSStatusBar.system.removeStatusItem(statusItem)
        self.statusItem = nil
    }

    @objc private func handleStatusItemClick(_ sender: NSStatusBarButton) {
        if NSApp.currentEvent?.type == .rightMouseUp {
            showStatusMenu(from: sender)
        } else {
            openDashboard()
        }
    }

    private func showStatusMenu(from button: NSStatusBarButton) {
        let menu = NSMenu()
        let status = NSMenuItem(title: statusSummary(), action: nil, keyEquivalent: "")
        status.isEnabled = false
        menu.addItem(status)

        let card = NSMenuItem(title: simSummary(), action: nil, keyEquivalent: "")
        card.isEnabled = false
        menu.addItem(card)
        menu.addItem(.separator())

        let open = NSMenuItem(title: "打开 DJOneHub", action: #selector(openDashboardFromMenu), keyEquivalent: "")
        open.target = self
        menu.addItem(open)

        let quit = NSMenuItem(title: "退出 DJOneHub", action: #selector(quitFromMenu), keyEquivalent: "q")
        quit.target = self
        menu.addItem(quit)

        menu.popUp(positioning: nil, at: NSPoint(x: button.bounds.midX, y: button.bounds.minY), in: button)
    }

    private func simSummary() -> String {
        guard deviceStatus.state == "ready" else { return "SIM 卡：设备不可用" }
        guard networkStatus.simKnown == true else { return "SIM 卡：状态未知" }
        return networkStatus.simInserted == true ? "SIM 卡：已插入" : "SIM 卡：未插入"
    }

    @objc private func openDashboardFromMenu() {
        openDashboard()
    }

    @objc private func quitFromMenu() {
        NativeUIHost.shared.stop()
    }

    // MARK: - Drawing helpers (unchanged from the legacy notifier)

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

    private static func statusImage(device: DeviceStatusEvent, network: NetworkUpdateEvent) -> NSImage {
        if device.state == "ready", network.simKnown == true, network.simInserted != true {
            return simStatusImage(inserted: false)
        }
        if device.state == "ready" {
            let signalLevel = network.registered && network.signalDBM != nil
                ? cellularSignalLevel(network.signalDBM!)
                : 0
            return cellularStatusImage(signalLevel: signalLevel)
        }
        return deviceStatusImage(state: device.state)
    }

    private static func deviceStatusImage(state: String) -> NSImage {
        let image = NSImage(size: NSSize(width: 22, height: 18))
        image.lockFocus()
        let color = NSColor.black
        color.setStroke()
        let body = NSBezierPath(roundedRect: NSRect(x: 3, y: 2, width: 16, height: 14), xRadius: 2, yRadius: 2)
        body.lineWidth = 1.6
        body.stroke()
        NSBezierPath(roundedRect: NSRect(x: 6, y: 5, width: 10, height: 2), xRadius: 1, yRadius: 1).fill()
        NSBezierPath(roundedRect: NSRect(x: 6, y: 9, width: 7, height: 2), xRadius: 1, yRadius: 1).fill()
        if state != "connecting" && state != "initializing" && state != "discovered" {
            let slash = NSBezierPath()
            slash.move(to: NSPoint(x: 3, y: 2))
            slash.line(to: NSPoint(x: 19, y: 16))
            slash.lineWidth = 1.8
            slash.stroke()
        }
        image.unlockFocus()
        image.isTemplate = true
        image.accessibilityDescription = state == "degraded" ? "设备连接异常" : "未检测到设备"
        return image
    }

    private static func simStatusImage(inserted: Bool) -> NSImage {
        let image = NSImage(size: NSSize(width: 22, height: 18))
        image.lockFocus()
        NSColor.black.setStroke()
        let body = NSBezierPath()
        body.move(to: NSPoint(x: 5, y: 2))
        body.line(to: NSPoint(x: 15, y: 2))
        body.line(to: NSPoint(x: 19, y: 6))
        body.line(to: NSPoint(x: 19, y: 16))
        body.line(to: NSPoint(x: 5, y: 16))
        body.close()
        body.lineWidth = 1.6
        body.stroke()
        if !inserted {
            let slash = NSBezierPath()
            slash.move(to: NSPoint(x: 4, y: 2))
            slash.line(to: NSPoint(x: 19, y: 17))
            slash.lineWidth = 1.8
            slash.stroke()
        }
        image.unlockFocus()
        image.isTemplate = true
        image.accessibilityDescription = inserted ? "SIM 卡已插入" : "未插入 SIM 卡"
        return image
    }

}
