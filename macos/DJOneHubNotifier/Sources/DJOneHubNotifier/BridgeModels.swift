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

    enum CodingKeys: String, CodingKey {
        case mode, registered
        case networkMode = "network_mode"
        case operatorName = "operator"
        case signalDBM = "signal_dbm"
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

struct DashboardOpened: Codable, Equatable, Sendable {
    let url: String
}

struct NotificationPreferences: Codable, Equatable, Sendable {
    let incomingCall: String
    let missedCall: String
    let sms: String
    let deviceOffline: String

    enum CodingKeys: String, CodingKey {
        case incomingCall = "incoming_call"
        case missedCall = "missed_call"
        case sms
        case deviceOffline = "device_offline"
    }

    static let system = NotificationPreferences(
        incomingCall: "system",
        missedCall: "system",
        sms: "system",
        deviceOffline: "system"
    )
}

// Command is a user action sent back to Go; see the contract doc section 3.
struct Command: Codable, Equatable, Sendable {
    let name: String
    let params: [String: String]?

    static let rejectCall = "reject_call"
    static let openDashboard = "open_dashboard"
    static let notificationPermissionStatus = "notification_permission_status"
}

enum BridgeEventType {
    static let snapshot = "snapshot"
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
        let payload = object["data"] as? [String: Any] ?? [:]
        guard let data = try? JSONSerialization.data(withJSONObject: payload) else {
            return nil
        }
        return BridgeEvent(id: id, type: type, version: version, occurredAt: occurredAt, data: data)
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

    static func date(from value: Any?) -> Date? {
        guard let string = value as? String else { return nil }
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = fractional.date(from: string) {
            return date
        }
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        if let date = plain.date(from: string) {
            return date
        }
        return nil
    }
}
