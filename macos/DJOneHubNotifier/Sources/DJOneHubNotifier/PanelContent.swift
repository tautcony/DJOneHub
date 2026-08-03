import Foundation

public enum PanelContent: Equatable {
    case idle
    case incoming(number: String, startedAt: Date, state: String, rejecting: Bool)
    case sms(sender: String, preview: String, code: String?)
    case missed(number: String, startedAt: Date)
    case error(message: String)

    var isCall: Bool {
        if case .incoming = self {
            return true
        }
        return false
    }
}

public enum NotificationText {
    public static func offlineDetail(reason: String?, lastError: String?) -> String {
        let detail = [reason, lastError]
            .compactMap { $0?.trimmingCharacters(in: .whitespacesAndNewlines) }
            .first { !$0.isEmpty }

        guard detail != nil else {
            return "设备连接已断开，相关服务已暂停。"
        }
        return "设备暂未就绪，相关服务已暂停。"
    }

    public static func displayNumber(_ number: String?) -> String {
        let trimmed = number?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmed.isEmpty ? "未知号码" : trimmed
    }

    public static func smsPreview(_ message: SMSMessageEvent, limit: Int = 48) -> String {
        if let code = message.code, !code.isEmpty {
            return "验证码 \(code)"
        }
        let singleLine = message.body
            .replacingOccurrences(of: "\r", with: " ")
            .replacingOccurrences(of: "\n", with: " ")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard singleLine.count > limit else {
            return singleLine
        }
        return String(singleLine.prefix(limit)) + "…"
    }
}
