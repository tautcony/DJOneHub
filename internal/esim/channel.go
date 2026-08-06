package esim

import (
	"encoding/hex"
	"fmt"
	"sync"
)

// logicalChannelTransport 是共享 AT APDU 通道的最小传输面: 打开/透传/关闭
// logical channel, 命令以 hex 字符串交换。modem.Manager 原生实现该接口
// (内部经设备级 APDU 仲裁器取 transport lease); 纯 AT 命令传输由
// atCommandTransport 适配。
type logicalChannelTransport interface {
	OpenLogicalChannel(aid string) (int, error)
	TransmitAPDU(channel int, apduHex string) (string, error)
	CloseLogicalChannel(channel int) error
}

// SmartCardChannel 实现 euicc-go 的 driver.SmartCardChannel 接口, 将 eUICC
// APDU 请求桥接到任意 AT logical-channel 传输。底层命令执行器由调用方注入,
// 所有 AT APDU 消费者共用这一份实现与统一防护 (channel 0 拒绝透传)。
type SmartCardChannel struct {
	transport logicalChannelTransport
	channel   byte // 当前打开的 logical channel 号
	mu        sync.Mutex
}

// NewSmartCardChannel 创建基于 AT 传输的 eUICC APDU 通道。
func NewSmartCardChannel(transport logicalChannelTransport) *SmartCardChannel {
	return &SmartCardChannel{transport: transport}
}

func (c *SmartCardChannel) CurrentChannel() byte {
	return c.channel
}

// Connect 连接到 APDU 接口（底层传输已由外部管理，此处为空操作）
func (c *SmartCardChannel) Connect() error {
	return nil
}

// Disconnect 断开 APDU 接口连接（底层传输由外部管理，此处为空操作）
func (c *SmartCardChannel) Disconnect() error {
	return nil
}

// OpenLogicalChannel 通过 AT+CCHO 打开 logical channel 并选择指定 AID
// 返回 channel 号
// 注意：不在此处做 ClearLogicalChannels，由上层 Manager 在遍历开始前统一预清理，
// 避免对 SIM 卡频繁发送通道指令导致卡片进入保护状态。
func (c *SmartCardChannel) OpenLogicalChannel(AID []byte) (byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	aidHex := fmt.Sprintf("%X", AID)
	ch, err := c.transport.OpenLogicalChannel(aidHex)
	if err != nil {
		return 0, fmt.Errorf("打开 logical channel 失败 (AID=%s): %w", aidHex, err)
	}
	if ch <= 0 || ch > 255 {
		return 0, fmt.Errorf("invalid logical channel %d", ch)
	}
	c.channel = byte(ch)
	return c.channel, nil
}

// Transmit 通过 AT+CGLA 在 logical channel 上透传 APDU 命令
// 输入原始二进制 APDU 命令，返回原始二进制 APDU 响应
func (c *SmartCardChannel) Transmit(command []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.channel == 0 {
		return nil, fmt.Errorf("eUICC logical channel is not open")
	}

	// 将二进制 APDU 编码为 hex 字符串
	cmdHex := fmt.Sprintf("%X", command)

	// 通过 AT+CGLA 发送
	respHex, err := c.transport.TransmitAPDU(int(c.channel), cmdHex)
	if err != nil {
		return nil, fmt.Errorf("APDU 透传失败: %w", err)
	}

	// 将 hex 响应解码为二进制
	respBytes, err := hex.DecodeString(respHex)
	if err != nil {
		return nil, fmt.Errorf("解析 APDU 响应 hex 失败: %w", err)
	}

	return respBytes, nil
}

// CloseLogicalChannel 通过 AT+CCHC 关闭 logical channel
func (c *SmartCardChannel) CloseLogicalChannel(channel byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.transport.CloseLogicalChannel(int(channel)); err != nil {
		return fmt.Errorf("关闭 logical channel %d 失败: %w", channel, err)
	}
	if c.channel == channel {
		c.channel = 0
	}
	return nil
}
