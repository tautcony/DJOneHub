import Foundation
import DJOneHubNotifier

// Dev-only CLI for the bridge contract self-test. The distribution builds the
// native UI as a static library linked into the Go process instead (see
// docs/MACOS_GO_NATIVE_BRIDGE_PLAN.md phase 4); this executable is not
// installed by any release script.

@main
enum DJOneHubNotifierCLI {
    static func main() {
        guard CommandLine.arguments.dropFirst().contains("--self-test") else {
            print("use --self-test")
            return
        }

        // NotificationText preconditions mirror the legacy notifier.
        precondition(NotificationText.displayNumber("  ") == "未知号码")
        let message = SMSMessageEvent(
            index: 7,
            sender: "10086",
            recipient: nil,
            body: "您的验证码是 482913",
            receivedAt: Date()
        )
        precondition(NotificationText.smsPreview(message) == "您的验证码是 482913")
        let longMessage = SMSMessageEvent(
            index: 8,
            sender: "10086",
            recipient: nil,
            body: "第一行\n第二行以及一段很长很长的短信正文",
            receivedAt: Date()
        )
        precondition(NotificationText.smsPreview(longMessage, limit: 8) == "第一行 第二行以…")
        print("DJOneHubNotifier self-test passed")
    }
}
