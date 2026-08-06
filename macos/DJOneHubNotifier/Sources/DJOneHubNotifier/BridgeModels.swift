import Foundation

// Bridge DTOs mirror internal/application/notification/model.go and
// docs/native-bridge-contract.md. Swift never fetches these over HTTP; they
// arrive as JSON events through the native bridge.

struct CallEvent: Codable, Equatable, Sendable {
    let id: String
    let direction: String
    let state: String
    let number: String?
    let startedAt: Date
    let endedAt: Date?
    let missed: Bool

    enum CodingKeys: String, CodingKey {
        case id, direction, state, number, missed
        case startedAt = "started_at"
        case endedAt = "ended_at"
    }
}

public struct SMSMessageEvent: Codable, Equatable, Sendable {
    public let index: Int
    public let sender: String?
    public let recipient: String?
    public let body: String
    public let receivedAt: Date

    public init(index: Int, sender: String?, recipient: String?, body: String, receivedAt: Date) {
        self.index = index
        self.sender = sender
        self.recipient = recipient
        self.body = body
        self.receivedAt = receivedAt
    }

    enum CodingKeys: String, CodingKey {
        case index, sender, recipient, body
        case receivedAt = "received_at"
    }
}

struct DeviceOfflineEvent: Codable, Equatable, Sendable {
    let state: String
    let reason: String?
    let lastError: String?

    enum CodingKeys: String, CodingKey {
        case state, reason
        case lastError = "last_error"
    }
}

struct NetworkUpdateEvent: Codable, Equatable, Sendable {
	let mode: String?
	let networkMode: String?
	let registered: Bool
	let operatorName: String?
	let signalDBM: Int?
	let simInserted: Bool?
	let simKnown: Bool?

	enum CodingKeys: String, CodingKey {
		case mode, registered, simInserted = "sim_inserted", simKnown = "sim_known"
		case networkMode = "network_mode"
		case operatorName = "operator"
		case signalDBM = "signal_dbm"
	}
}

struct DeviceStatusEvent: Codable, Equatable, Sendable {
	let state: String
	let identity: DeviceIdentityEvent?
	let backend: String?
	let lastError: String?
}

struct DeviceIdentityEvent: Codable, Equatable, Sendable {
	let stableID: String?
	let manufacturer: String?
	let product: String?
	let serialNumber: String?

	enum CodingKeys: String, CodingKey {
		case stableID = "stable_id"
		case manufacturer, product
		case serialNumber = "serial_number"
	}
}

struct RejectResult: Codable, Equatable, Sendable {
    let callId: String?
    let error: String?

    enum CodingKeys: String, CodingKey {
        case callId = "call_id"
        case error
    }
}

// CommandDropped reports a Swift-to-Go command that could not be enqueued.
// The UI uses it to recover the pending action (e.g. restoring the reject
// buttons) instead of remaining stuck.
struct CommandDropped: Codable, Equatable, Sendable {
    let command: String
    let reason: String?
}

struct DashboardOpened: Codable, Equatable, Sendable {
    let url: String
}

struct NotificationPreferences: Codable, Equatable, Sendable {
    let incomingCall: String
    let missedCall: String
    let sms: String
    let deviceOffline: String
    // senderOnly 是存在性感知的 "仅显示发送方" 偏好: 字段缺失时默认开启
    // (短信正文常含一次性验证码, 不进入通知请求), 显式 false 表示用户
    // 选择显示正文。decodeIfPresent 区分缺失与显式 false。
    let senderOnly: Bool?

    enum CodingKeys: String, CodingKey {
        case incomingCall = "incoming_call"
        case missedCall = "missed_call"
        case sms
        case deviceOffline = "device_offline"
        case senderOnly = "sender_only"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        incomingCall = try container.decode(String.self, forKey: .incomingCall)
        missedCall = try container.decode(String.self, forKey: .missedCall)
        sms = try container.decode(String.self, forKey: .sms)
        deviceOffline = try container.decode(String.self, forKey: .deviceOffline)
        senderOnly = try container.decodeIfPresent(Bool.self, forKey: .senderOnly)
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(incomingCall, forKey: .incomingCall)
        try container.encode(missedCall, forKey: .missedCall)
        try container.encode(sms, forKey: .sms)
        try container.encode(deviceOffline, forKey: .deviceOffline)
        try container.encodeIfPresent(senderOnly, forKey: .senderOnly)
    }

    /// senderOnlyEnabled 报告 "仅显示发送方" 是否生效: 缺失字段按 true 处理。
    var senderOnlyEnabled: Bool { senderOnly ?? true }

    init(incomingCall: String, missedCall: String, sms: String, deviceOffline: String, senderOnly: Bool?) {
        self.incomingCall = incomingCall
        self.missedCall = missedCall
        self.sms = sms
        self.deviceOffline = deviceOffline
        self.senderOnly = senderOnly
    }

    static let system = NotificationPreferences(
        incomingCall: "system",
        missedCall: "system",
        sms: "system",
        deviceOffline: "system",
        senderOnly: nil as Bool?
    )
}

// Command is a user action sent back to Go; see the contract doc section 3.
struct Command: Codable, Equatable, Sendable {
    let name: String
    let params: [String: String]?

    static let rejectCall = "reject_call"
    static let openDashboard = "open_dashboard"
    static let notificationPermissionStatus = "notification_permission_status"
    static let log = "log"
}

// Native log levels for the internal log command; mirror the Go constants in
// internal/application/notification/model.go.
enum NativeLogLevel {
    static let debug = "debug"
    static let info = "info"
    static let warn = "warn"
    static let error = "error"
}

enum BridgeEventType {
	static let snapshot = "snapshot"
	static let deviceStatusChanged = "device.status.changed"
    static let deviceOffline = "device.offline"
    static let callIncoming = "call.incoming"
    static let callUpdated = "call.updated"
    static let callEnded = "call.ended"
    static let callMissed = "call.missed"
    static let smsReceived = "sms.received"
    static let networkUpdated = "network.updated"
    static let callRejectStarted = "call.reject.started"
    static let callRejectSucceeded = "call.reject.succeeded"
    static let callRejectFailed = "call.reject.failed"
    static let commandDropped = "command.dropped"
    static let dashboardOpened = "dashboard.opened"
    static let notificationPermissionRequest = "notification.permission.request"
    static let notificationPermissionOpenSettings = "notification.permission.open_settings"
    static let notificationPreferencesUpdated = "notification.preferences.updated"
}

// BridgeEvent is the runtime.Event envelope {id, type, version, occurred_at,
// data}; data is kept as raw JSON so each event decodes into its own DTO.
struct BridgeEvent: Equatable, Sendable {
    let id: UInt64
    let type: String
    let version: Int
    let occurredAt: Date
    let data: Data

    static func parse(_ json: String) -> BridgeEvent? {
        guard let bytes = json.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: bytes) as? [String: Any]
        else {
            return nil
        }
        guard let type = object["type"] as? String,
              let id = object["id"] as? UInt64,
              let version = object["version"] as? Int
        else {
            return nil
        }
        let occurredAt = Self.date(from: object["occurred_at"]) ?? Date.distantPast
        // data 已是原始 JSON 的一部分: 直接切出 "data" 成员的原始字节,
        // 不再经 JSONSerialization 重新编码 (3.9 L1)。
        let payload = Self.rawDataValue(in: json) ?? Data()
        return BridgeEvent(id: id, type: type, version: version, occurredAt: occurredAt, data: payload)
    }

    func decode<T: Decodable>(_ type: T.Type) -> T? {
        try? Self.decoder.decode(T.self, from: data)
    }

    static let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let string = try container.decode(String.self)
            guard let date = Self.date(from: string) else {
                throw DecodingError.dataCorruptedError(in: container, debugDescription: "invalid date \(string)")
            }
            return date
        }
        return decoder
    }()

    // 缓存两个 ISO8601DateFormatter: 每次事件解析都新建的旧实现 (3.9 L1)。
    // ISO8601DateFormatter 非 Sendable, 仅在主执行器 (事件解析路径) 使用。
    nonisolated(unsafe) private static let fractionalDateFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()
    nonisolated(unsafe) private static let plainDateFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    static func date(from value: Any?) -> Date? {
        guard let string = value as? String else { return nil }
        if let date = Self.fractionalDateFormatter.date(from: string) {
            return date
        }
        return Self.plainDateFormatter.date(from: string)
    }

    // rawDataValue 切出事件 JSON 中 "data" 成员的原始字节, 不做重新编码。
    // 事件由 Go 端生成 (无多余空白、字符串内转义), 对 { [ 平衡计数并跳过
    // 字符串即可正确定界。
    private static func rawDataValue(in json: String) -> Data? {
        let utf8 = Array(json.utf8)
        var index = 0
        while index < utf8.count {
            // 找下一个字符串 key。
            while index < utf8.count && utf8[index] != 0x22 { index += 1 } // "
            if index >= utf8.count { return nil }
            index += 1
            var keyBytes: [UInt8] = []
            while index < utf8.count && utf8[index] != 0x22 {
                if utf8[index] == 0x5C { // 转义字符, 跳过下一字节
                    index += 2
                    continue
                }
                keyBytes.append(utf8[index])
                index += 1
            }
            if index >= utf8.count { return nil }
            index += 1
            if String(decoding: keyBytes, as: UTF8.self) != "data" { continue }
            while index < utf8.count && (utf8[index] == 0x20 || utf8[index] == 0x09 || utf8[index] == 0x0A || utf8[index] == 0x0D) { index += 1 }
            guard index < utf8.count, utf8[index] == 0x3A else { continue } // ':'
            index += 1
            while index < utf8.count && (utf8[index] == 0x20 || utf8[index] == 0x09 || utf8[index] == 0x0A || utf8[index] == 0x0D) { index += 1 }
            let valueStart = index
            guard index < utf8.count, utf8[index] == 0x7B || utf8[index] == 0x5B else { continue } // { [
            var depth = 1
            index += 1
            while index < utf8.count && depth > 0 {
                let byte = utf8[index]
                if byte == 0x22 { // 字符串: 跳到结束引号 (跳过转义)
                    index += 1
                    while index < utf8.count {
                        if utf8[index] == 0x5C {
                            index += 2
                            continue
                        }
                        if utf8[index] == 0x22 { break }
                        index += 1
                    }
                } else if byte == 0x7B || byte == 0x5B {
                    depth += 1
                } else if byte == 0x7D || byte == 0x5D {
                    depth -= 1
                }
                index += 1
            }
            guard depth == 0 else { return nil }
            let valueEnd = index
            return Data(json.utf8[json.utf8.index(json.utf8.startIndex, offsetBy: valueStart)..<json.utf8.index(json.utf8.startIndex, offsetBy: valueEnd)])
        }
        return nil
    }
}
