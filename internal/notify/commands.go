package notify

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// commandTimeout 限制单条命令的同步执行时间。异步操作（发短信、切卡）由
// AwaitOperation 在独立 goroutine 里继续等待，不受此限制。
const commandTimeout = 20 * time.Second

// asyncTimeout 限制等待异步 operation 完成的时间。
const asyncTimeout = 3 * time.Minute

// commandHandler 处理一条命令并返回同步回复。
type commandHandler func(ctx context.Context, cmd Command) string

// commandSpec 描述一条命令的用法，用于 /help 与参数错误提示。
type commandSpec struct {
	name    string
	usage   string
	example string
	summary string
	handle  commandHandler
}

// Commands 把 Ports 暴露的业务能力包装成聊天命令。它实现 Dispatcher。
//
// 与上游版本的区别：本工程是单设备运行时，所有命令都不再接收 deviceID 参数。
type Commands struct {
	ports Ports
	specs map[string]commandSpec
	order []string
}

// NewCommands 构造命令分发器。
func NewCommands(ports Ports) *Commands {
	c := &Commands{ports: ports, specs: map[string]commandSpec{}}
	c.register(commandSpec{
		name: "help", summary: "查看全部命令",
		usage: "/help", example: "/help",
		handle: c.handleHelp,
	})
	c.register(commandSpec{
		name: "status", summary: "查看设备状态",
		usage: "/status", example: "/status",
		handle: c.handleStatus,
	})
	c.register(commandSpec{
		name: "send", summary: "发送短信",
		usage: "/send [号码] [内容]", example: "/send +8613800138000 测试短信",
		handle: c.handleSend,
	})
	c.register(commandSpec{
		name: "sms", summary: "查看最近短信",
		usage: "/sms [条数]", example: "/sms 5",
		handle: c.handleSMS,
	})
	c.register(commandSpec{
		name: "esim", summary: "查看 eSIM 配置",
		usage: "/esim", example: "/esim",
		handle: c.handleESIM,
	})
	c.register(commandSpec{
		name: "switch", summary: "切换 eSIM 配置",
		usage: "/switch [序号或ICCID]", example: "/switch 2",
		handle: c.handleSwitch,
	})
	c.register(commandSpec{
		name: "call", summary: "查看通话状态",
		usage: "/call", example: "/call",
		handle: c.handleCall,
	})
	c.register(commandSpec{
		name: "dial", summary: "发起呼叫",
		usage: "/dial [号码]", example: "/dial 10086",
		handle: c.handleDial,
	})
	c.register(commandSpec{
		name: "hangup", summary: "挂断当前通话",
		usage: "/hangup", example: "/hangup",
		handle: c.handleHangup,
	})
	c.register(commandSpec{
		name: "network", summary: "查看或切换联网模式",
		usage: "/network [模式]", example: "/network",
		handle: c.handleNetwork,
	})
	return c
}

func (c *Commands) register(spec commandSpec) {
	c.specs[spec.name] = spec
	c.order = append(c.order, spec.name)
}

// Dispatch 执行一条命令并返回同步回复文本。
func (c *Commands) Dispatch(ctx context.Context, cmd Command) string {
	spec, ok := c.specs[cmd.Name]
	if !ok {
		return fmt.Sprintf("未知命令 /%s\n提示  发送 /help 查看全部命令", cmd.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	return spec.handle(ctx, cmd)
}

func (c *Commands) handleHelp(context.Context, Command) string {
	var sb strings.Builder
	sb.WriteString("可用命令")
	for _, name := range c.order {
		spec := c.specs[name]
		sb.WriteString(fmt.Sprintf("\n%-24s %s", spec.usage, spec.summary))
	}
	return sb.String()
}

func (c *Commands) handleStatus(ctx context.Context, _ Command) string {
	if c.ports.DeviceStatus == nil {
		return unavailable("设备状态")
	}
	status, err := c.ports.DeviceStatus(ctx)
	if err != nil {
		return failure("设备状态", err)
	}

	health := "异常"
	if status.Healthy {
		health = "正常"
	}
	sim := "未插入"
	if status.SIMInserted {
		sim = "已插入"
	}
	registered := "未注册"
	if status.Registered {
		registered = "已注册"
	}

	msg := Message{
		Title: "设备状态",
		Fields: []Field{
			{Key: "状态", Value: fmt.Sprintf("%s / %s", orDash(status.State), health)},
			{Key: "IMEI", Value: orDash(status.IMEI)},
			{Key: "ICCID", Value: orDash(status.ICCID)},
			{Key: "本机号码", Value: orDash(status.LocalPhone)},
			{Key: "固件", Value: orDash(status.Firmware)},
			{Key: "SIM", Value: sim},
			{Key: "运营商", Value: orDash(status.Operator)},
			{Key: "网络", Value: fmt.Sprintf("%s / %s", orDash(status.NetworkMode), registered)},
			{Key: "信号", Value: formatSignal(status)},
		},
	}
	return msg.Text()
}

func (c *Commands) handleSend(ctx context.Context, cmd Command) string {
	if c.ports.SendSMS == nil {
		return unavailable("发送短信")
	}
	if len(cmd.Args) < 2 {
		return c.usage("send")
	}
	recipient := cmd.Arg(0)
	body := cmd.Rest(1)

	operationID, err := c.ports.SendSMS(ctx, recipient, body)
	if err != nil {
		return failure("发送短信", err)
	}
	c.awaitAsync(cmd, operationID, func(ok bool, message string) string {
		if ok {
			return fmt.Sprintf("发送短信 / 成功\n号码  %s\n内容  %s", recipient, body)
		}
		return fmt.Sprintf("发送短信 / 失败\n号码  %s\n原因  %s", recipient, message)
	})
	return fmt.Sprintf("发送短信 / 已受理\n号码  %s\n内容  %s", recipient, body)
}

func (c *Commands) handleSMS(ctx context.Context, cmd Command) string {
	if c.ports.ListSMS == nil {
		return unavailable("短信列表")
	}
	limit := 5
	if raw := cmd.Arg(0); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return fmt.Sprintf("短信列表 / 参数错误\n条数  %s\n要求  正整数", raw)
		}
		limit = parsed
	}

	records, err := c.ports.ListSMS(ctx, limit)
	if err != nil {
		return failure("短信列表", err)
	}
	if len(records) == 0 {
		return "短信列表 / 空\n结果  暂无短信"
	}

	var sb strings.Builder
	sb.WriteString("短信列表")
	for i, record := range records {
		direction := "来信"
		if record.Outbound {
			direction = "去信"
		}
		sb.WriteString(fmt.Sprintf("\n\n%d. %s / %s", i+1, direction, orDash(record.Peer)))
		sb.WriteString(fmt.Sprintf("\n内容  %s", strings.TrimSpace(record.Body)))
		if !record.At.IsZero() {
			sb.WriteString(fmt.Sprintf("\n时间  %s", record.At.Format("2006-01-02 15:04:05")))
		}
	}
	return sb.String()
}

func (c *Commands) handleESIM(ctx context.Context, _ Command) string {
	if c.ports.ListESIM == nil {
		return unavailable("eSIM 列表")
	}
	profiles, err := c.ports.ListESIM(ctx)
	if err != nil {
		return failure("eSIM 列表", err)
	}
	if len(profiles) == 0 {
		return "eSIM 列表 / 空\n结果  未发现可用配置"
	}

	var sb strings.Builder
	sb.WriteString("eSIM 列表")
	for i, profile := range profiles {
		state := "未启用"
		if profile.Active {
			state = "已启用"
		}
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			name = "未命名"
		}
		sb.WriteString(fmt.Sprintf("\n\n%d. %s / %s", i+1, state, name))
		sb.WriteString(fmt.Sprintf("\nICCID  %s", maskICCID(profile.ICCID)))
		if provider := strings.TrimSpace(profile.Provider); provider != "" {
			sb.WriteString(fmt.Sprintf("\n运营商  %s", provider))
		}
	}
	sb.WriteString("\n\n切换  /switch [序号或ICCID]")
	return sb.String()
}

func (c *Commands) handleSwitch(ctx context.Context, cmd Command) string {
	if c.ports.ListESIM == nil || c.ports.EnableESIM == nil {
		return unavailable("切换 eSIM")
	}
	target := cmd.Arg(0)
	if target == "" {
		return c.usage("switch")
	}

	profiles, err := c.ports.ListESIM(ctx)
	if err != nil {
		return failure("切换 eSIM", err)
	}
	if len(profiles) == 0 {
		return "切换 eSIM / 失败\n原因  没有可用的 eSIM 配置"
	}

	profile, ok := resolveProfile(profiles, target)
	if !ok {
		return fmt.Sprintf("切换 eSIM / 失败\n目标  %s\n原因  未找到匹配的配置，可用 /esim 查看列表", target)
	}
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name = "未命名"
	}

	operationID, err := c.ports.EnableESIM(ctx, profile.ICCID)
	if err != nil {
		return failure("切换 eSIM", err)
	}
	c.awaitAsync(cmd, operationID, func(ok bool, message string) string {
		if ok {
			return fmt.Sprintf("切换 eSIM / 成功\n配置  %s\nICCID  %s", name, maskICCID(profile.ICCID))
		}
		return fmt.Sprintf("切换 eSIM / 失败\n配置  %s\n原因  %s", name, message)
	})
	return fmt.Sprintf("切换 eSIM / 已受理\n配置  %s", name)
}

func (c *Commands) handleCall(ctx context.Context, _ Command) string {
	if c.ports.Calls == nil {
		return unavailable("通话状态")
	}
	summary, err := c.ports.Calls(ctx)
	if err != nil {
		return failure("通话状态", err)
	}
	if !summary.Active {
		return "通话状态 / 空闲\n结果  当前没有通话"
	}
	msg := Message{
		Title: "通话状态 / 进行中",
		Fields: []Field{
			{Key: "方向", Value: orDash(summary.Direction)},
			{Key: "状态", Value: orDash(summary.State)},
			{Key: "号码", Value: orDash(summary.Number)},
			{Key: "开始", Value: formatTime(summary.StartedAt)},
		},
	}
	return msg.Text()
}

func (c *Commands) handleDial(ctx context.Context, cmd Command) string {
	if c.ports.Dial == nil {
		return unavailable("发起呼叫")
	}
	number := cmd.Arg(0)
	if number == "" {
		return c.usage("dial")
	}
	if err := c.ports.Dial(ctx, number); err != nil {
		return failure("发起呼叫", err)
	}
	return fmt.Sprintf("发起呼叫 / 已受理\n号码  %s", number)
}

func (c *Commands) handleHangup(ctx context.Context, _ Command) string {
	if c.ports.Reject == nil {
		return unavailable("挂断通话")
	}
	if err := c.ports.Reject(ctx); err != nil {
		return failure("挂断通话", err)
	}
	return "挂断通话 / 完成"
}

func (c *Commands) handleNetwork(ctx context.Context, cmd Command) string {
	mode := cmd.Arg(0)
	if mode == "" {
		if c.ports.NetworkMode == nil {
			return unavailable("联网模式")
		}
		current, err := c.ports.NetworkMode(ctx)
		if err != nil {
			return failure("联网模式", err)
		}
		return fmt.Sprintf("联网模式\n当前  %s\n切换  /network [模式]", orDash(current))
	}

	if c.ports.SetNetworkMode == nil {
		return unavailable("切换联网模式")
	}
	operationID, err := c.ports.SetNetworkMode(ctx, mode)
	if err != nil {
		return failure("切换联网模式", err)
	}
	c.awaitAsync(cmd, operationID, func(ok bool, message string) string {
		if ok {
			return fmt.Sprintf("切换联网模式 / 成功\n模式  %s", mode)
		}
		return fmt.Sprintf("切换联网模式 / 失败\n模式  %s\n原因  %s", mode, message)
	})
	return fmt.Sprintf("切换联网模式 / 已受理\n模式  %s", mode)
}

// awaitAsync 在后台等待 operation 结束并把结果回送给发起命令的会话。
// operationID 为空或缺少 AwaitOperation 能力时静默跳过，此时用户只会看到
// "已受理"，不会收到一条无意义的失败回执。
func (c *Commands) awaitAsync(cmd Command, operationID string, render func(ok bool, message string) string) {
	if operationID == "" || c.ports.AwaitOperation == nil || cmd.Reply == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), asyncTimeout)
		defer cancel()
		ok, message := c.ports.AwaitOperation(ctx, operationID)
		cmd.Reply(render(ok, message))
	}()
}

func (c *Commands) usage(name string) string {
	spec, ok := c.specs[name]
	if !ok {
		return fmt.Sprintf("未知命令 /%s", name)
	}
	return fmt.Sprintf("%s / 参数错误\n用法  %s\n示例  %s", spec.summary, spec.usage, spec.example)
}

// resolveProfile 按 ICCID（全量或后缀）或列表序号（1 起）匹配一个 eSIM 配置。
//
// 必须先试 ICCID：ICCID 是纯数字且长度在 int64 范围内，先按序号解析会把一个
// 完整的 19 位 ICCID 误判成越界序号，导致切卡命令永远失败。
func resolveProfile(profiles []ESIMProfile, target string) (ESIMProfile, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return ESIMProfile{}, false
	}
	for _, profile := range profiles {
		if profile.ICCID == target {
			return profile, true
		}
	}
	// 后缀至少 4 位，避免 "2" 这类序号被当成 ICCID 尾号误切卡。
	if len(target) >= 4 {
		for _, profile := range profiles {
			if strings.HasSuffix(profile.ICCID, target) {
				return profile, true
			}
		}
	}
	if index, err := strconv.Atoi(target); err == nil && index >= 1 && index <= len(profiles) {
		return profiles[index-1], true
	}
	return ESIMProfile{}, false
}

// SortSMSByTimeDesc 按时间倒序排列短信，供端口实现复用。
func SortSMSByTimeDesc(records []SMSRecord) {
	sort.SliceStable(records, func(i, j int) bool { return records[i].At.After(records[j].At) })
}

func formatSignal(status DeviceStatus) string {
	if status.SignalDBM == 0 && status.SignalRSRP == 0 && status.SignalRSRQ == 0 {
		return "--"
	}
	return fmt.Sprintf("%d dBm (RSRP %d / RSRQ %d)", status.SignalDBM, status.SignalRSRP, status.SignalRSRQ)
}

func formatTime(at time.Time) string {
	if at.IsZero() {
		return "--"
	}
	return at.Format("2006-01-02 15:04:05")
}

func maskICCID(iccid string) string {
	iccid = strings.TrimSpace(iccid)
	if len(iccid) <= 10 {
		return orDash(iccid)
	}
	return iccid[:6] + "..." + iccid[len(iccid)-4:]
}

func orDash(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return "--"
}

func unavailable(title string) string {
	return fmt.Sprintf("%s / 不可用\n原因  当前构建或设备不支持该能力", title)
}

func failure(title string, err error) string {
	return fmt.Sprintf("%s / 失败\n原因  %v", title, err)
}
