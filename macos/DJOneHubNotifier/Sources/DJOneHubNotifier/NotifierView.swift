import SwiftUI

struct NotifierView: View {
    let content: PanelContent
    let onReject: () -> Void
    let onOpen: () -> Void

    var body: some View {
        ZStack {
            switch content {
            case let .incoming(number, startedAt, state, rejecting):
                incomingView(number: number, startedAt: startedAt, state: state, rejecting: rejecting)
            case let .sms(sender, preview):
                smsView(sender: sender, preview: preview)
            case let .missed(number, startedAt):
                missedView(number: number, startedAt: startedAt)
            case let .error(message):
                messageView(title: "DJOneHub 暂时离线", detail: message, symbol: "exclamationmark.triangle.fill")
            case .idle:
                EmptyView()
            }
        }
    }

    private func incomingView(number: String, startedAt: Date, state: String, rejecting: Bool) -> some View {
        VStack(spacing: 8) {
            VStack(spacing: 2) {
                Text(callTitle(for: state))
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(.secondary)
                Text(number)
                    .font(.system(size: 20, weight: .semibold, design: .rounded))
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                Text(startedAt, style: state == "active" || state == "held" ? .timer : .time)
                    .font(.system(size: 10, weight: .medium))
                    .foregroundStyle(.secondary)
            }

            HStack(spacing: state == "incoming" || state == "waiting" ? 38 : 0) {
                if state == "incoming" || state == "waiting" {
                    callAction(
                        title: rejecting ? "拒接中" : "拒接",
                        symbol: "phone.down.fill",
                        color: .red,
                        action: onReject
                    )
                    .disabled(rejecting)
                }
                callAction(
                    title: "详情",
                    symbol: "arrow.up.forward.app.fill",
                    color: .green,
                    action: onOpen
                )
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
    }

    private func callTitle(for state: String) -> String {
        switch state {
        case "active":
            return "DJOneHub 通话中"
        case "held":
            return "DJOneHub 通话保持"
        case "waiting":
            return "DJOneHub 等待接听"
        default:
            return "DJOneHub 来电"
        }
    }

    private func callAction(
        title: String,
        symbol: String,
        color: Color,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            VStack(spacing: 3) {
                Image(systemName: symbol)
                    .font(.system(size: 18, weight: .semibold))
                    .frame(width: 40, height: 40)
                    .background(color)
                    .foregroundStyle(.white)
                    .clipShape(Circle())
                Text(title)
                    .font(.system(size: 10, weight: .medium))
                    .foregroundStyle(.primary)
            }
        }
        .buttonStyle(.plain)
    }

    private func smsView(sender: String, preview: String) -> some View {
        Button(action: onOpen) {
            HStack(spacing: 9) {
                Image(systemName: "message.fill")
                    .font(.system(size: 17, weight: .semibold))
                    .frame(width: 34, height: 34)
                    .background(Color.green.opacity(0.16))
                    .foregroundStyle(.green)
                    .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                VStack(alignment: .leading, spacing: 2) {
                    HStack {
                        Text(sender)
                            .font(.system(size: 12, weight: .semibold))
                            .lineLimit(1)
                        Spacer()
                        Text("现在")
                            .font(.system(size: 9))
                            .foregroundStyle(.secondary)
                    }
                    Text(preview)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .multilineTextAlignment(.leading)
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 7)
        }
        .buttonStyle(.plain)
    }

    private func missedView(number: String, startedAt: Date) -> some View {
        Button(action: onOpen) {
            HStack(spacing: 10) {
                Image(systemName: "phone.down.circle.fill")
                    .font(.system(size: 24))
                    .foregroundStyle(.red)
                VStack(alignment: .leading, spacing: 4) {
                    Text("未接来电")
                        .font(.system(size: 12, weight: .semibold))
                    Text(number)
                        .font(.system(size: 11))
                    Text(startedAt, style: .time)
                        .font(.system(size: 9))
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }
            .padding(12)
        }
        .buttonStyle(.plain)
    }

    private func messageView(title: String, detail: String, symbol: String) -> some View {
        HStack(spacing: 10) {
            Image(systemName: symbol)
                .font(.system(size: 20))
                .foregroundStyle(.orange)
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(.headline)
                Text(detail).font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
        }
        .padding(12)
    }
}
