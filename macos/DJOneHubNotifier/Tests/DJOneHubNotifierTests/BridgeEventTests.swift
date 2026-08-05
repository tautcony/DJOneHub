import XCTest
@testable import DJOneHubNotifier

final class BridgeEventTests: XCTestCase {
    func testCallIncomingEventDecodes() {
        let json = """
        {"id": 42, "type": "call.incoming", "version": 1, "occurred_at": "2026-08-02T10:00:00Z", "data": {"id": "1783069200000-1", "direction": "incoming", "state": "incoming", "number": "18900007376", "started_at": "2026-08-02T10:00:00Z", "missed": false}}
        """
        let event = try! XCTUnwrap(BridgeEvent.parse(json))
        XCTAssertEqual(event.type, BridgeEventType.callIncoming)
        XCTAssertEqual(event.version, 1)
        XCTAssertEqual(event.id, 42)
        let call = try! XCTUnwrap(event.decode(CallEvent.self))
        XCTAssertEqual(call.id, "1783069200000-1")
        XCTAssertEqual(call.direction, "incoming")
        XCTAssertEqual(call.state, "incoming")
        XCTAssertEqual(call.number, "18900007376")
        XCTAssertFalse(call.missed)
        XCTAssertNil(call.endedAt)
    }

    func testMissedCallEventWithFractionalDateDecodes() {
        let json = """
        {"id": 45, "type": "call.missed", "version": 1, "occurred_at": "2026-08-02T10:00:45.123456Z", "data": {"id": "1783069200000-2", "direction": "incoming", "state": "incoming", "number": "18900007376", "started_at": "2026-08-02T10:00:30.5Z", "ended_at": "2026-08-02T10:00:45.123456Z", "missed": true}}
        """
        let event = try! XCTUnwrap(BridgeEvent.parse(json))
        let call = try! XCTUnwrap(event.decode(CallEvent.self))
        XCTAssertTrue(call.missed)
        let endedAt = try! XCTUnwrap(call.endedAt)
        // ISO8601DateFormatter resolves fractional seconds to milliseconds.
        XCTAssertEqual(endedAt.timeIntervalSince1970, 1_785_664_845.123456, accuracy: 0.001)
    }

    func testSMSReceivedEventDecodes() {
        let json = """
        {"id": 46, "type": "sms.received", "version": 1, "occurred_at": "2026-08-02T10:00:05Z", "data": {"index": 7, "sender": "10086", "body": "您的验证码是 482913", "received_at": "2026-08-02T10:00:05Z"}}
        """
        let event = try! XCTUnwrap(BridgeEvent.parse(json))
        let message = try! XCTUnwrap(event.decode(SMSMessageEvent.self))
        XCTAssertEqual(message.index, 7)
        XCTAssertEqual(message.sender, "10086")
        XCTAssertEqual(message.body, "您的验证码是 482913")
    }

    func testNetworkUpdatedEventDecodes() {
        let json = """
        {"id": 49, "type": "network.updated", "version": 1, "occurred_at": "2026-08-02T10:00:10Z", "data": {"network_mode": "LTE", "registered": true, "operator": "CHN-UNICOM", "signal_dbm": -83, "sim_inserted": false, "sim_known": true}}
        """
        let event = try! XCTUnwrap(BridgeEvent.parse(json))
        let state = try! XCTUnwrap(event.decode(NetworkUpdateEvent.self))
        XCTAssertTrue(state.registered)
        XCTAssertEqual(state.networkMode, "LTE")
        XCTAssertEqual(state.signalDBM, -83)
        XCTAssertEqual(state.simInserted, false)
        XCTAssertEqual(state.simKnown, true)
    }

    func testDeviceStatusEventDecodes() {
        let json = """
        {"id": 51, "type": "device.status.changed", "version": 1, "occurred_at": "2026-08-02T10:00:10Z", "data": {"state": "ready", "identity": {"stable_id": "usb-1", "manufacturer": "Quectel", "product": "EC25"}, "backend": "at"}}
        """
        let event = try! XCTUnwrap(BridgeEvent.parse(json))
        let status = try! XCTUnwrap(event.decode(DeviceStatusEvent.self))
        XCTAssertEqual(status.state, "ready")
        XCTAssertEqual(status.identity?.manufacturer, "Quectel")
        XCTAssertEqual(status.backend, "at")
    }

    func testDeviceOfflineEventDecodes() {
        let json = """
        {"id": 2, "type": "device.offline", "version": 1, "occurred_at": "2026-08-02T11:00:00Z", "data": {"state": "disconnected", "reason": "no managed device was discovered"}}
        """
        let event = try! XCTUnwrap(BridgeEvent.parse(json))
        let offline = try! XCTUnwrap(event.decode(DeviceOfflineEvent.self))
        XCTAssertEqual(offline.state, "disconnected")
        XCTAssertEqual(offline.reason, "no managed device was discovered")
    }

    func testRejectResultEventsDecode() {
        let started = """
        {"id": 50, "type": "call.reject.started", "version": 1, "occurred_at": "2026-08-02T10:00:01Z", "data": {"call_id": "1783069200000-1"}}
        """
        let startedEvent = try! XCTUnwrap(BridgeEvent.parse(started))
        XCTAssertEqual(startedEvent.decode(RejectResult.self)?.callId, "1783069200000-1")

        let failed = """
        {"id": 52, "type": "call.reject.failed", "version": 1, "occurred_at": "2026-08-02T10:00:01Z", "data": {"call_id": "1783069200000-1", "error": "device is not ready"}}
        """
        let failedEvent = try! XCTUnwrap(BridgeEvent.parse(failed))
        let result = try! XCTUnwrap(failedEvent.decode(RejectResult.self))
        XCTAssertEqual(result.error, "device is not ready")
    }

    func testMalformedEventIsRejected() {
        XCTAssertNil(BridgeEvent.parse("not json"))
        XCTAssertNil(BridgeEvent.parse(#"{"id": 1, "version": 1}"#))
    }

    func testNotificationTextFormatting() {
        XCTAssertEqual(
            NotificationText.offlineDetail(reason: "device_offline", lastError: "device is not ready"),
            "设备暂未就绪，相关服务已暂停。"
        )
        XCTAssertEqual(
            NotificationText.offlineDetail(reason: nil, lastError: nil),
            "设备连接已断开，相关服务已暂停。"
        )

        XCTAssertEqual(NotificationText.displayNumber(nil), "未知号码")
        XCTAssertEqual(NotificationText.displayNumber("  "), "未知号码")
        XCTAssertEqual(NotificationText.displayNumber("10086"), "10086")

        let message = SMSMessageEvent(index: 1, sender: "10086", recipient: nil, body: "您的验证码是 482913", receivedAt: Date())
        XCTAssertEqual(NotificationText.smsPreview(message), "您的验证码是 482913")

        let long = SMSMessageEvent(index: 2, sender: "10086", recipient: nil, body: "第一行\n第二行以及一段很长很长的短信正文", receivedAt: Date())
        XCTAssertEqual(NotificationText.smsPreview(long, limit: 8), "第一行 第二行以…")
    }

    func testCommandEncoding() throws {
        let command = Command(name: Command.rejectCall, params: ["call_id": "1783069200000-1"])
        let data = try JSONEncoder().encode(command)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(object["name"] as? String, "reject_call")
        let params = try XCTUnwrap(object["params"] as? [String: String])
        XCTAssertEqual(params["call_id"], "1783069200000-1")
    }
}
