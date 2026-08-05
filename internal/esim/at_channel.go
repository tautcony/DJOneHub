package esim

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ATSmartCardChannel 实现 euicc-go 的 driver.SmartCardChannel 接口，
// 通过纯 AT 命令（AT+CCHO / AT+CGLA / AT+CCHC）访问 eUICC APDU 通道。
// 与具体传输无关：command 由调用方注入（darwin USB bulk 传输或
// Linux/Windows 串口 modem.Manager 的 AT 通道）。
type ATSmartCardChannel struct {
	command func(string, time.Duration) (string, error)
	channel byte
	mu      sync.Mutex
}

// NewATSmartCardChannel 创建基于 AT 命令的 eUICC APDU 通道。
func NewATSmartCardChannel(command func(string, time.Duration) (string, error)) *ATSmartCardChannel {
	return &ATSmartCardChannel{command: command}
}

func (c *ATSmartCardChannel) Connect() error       { return nil }
func (c *ATSmartCardChannel) Disconnect() error    { return nil }
func (c *ATSmartCardChannel) CurrentChannel() byte { return c.channel }

func (c *ATSmartCardChannel) OpenLogicalChannel(aid []byte) (byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	response, err := c.command(fmt.Sprintf(`AT+CCHO="%s"`, strings.ToUpper(hex.EncodeToString(aid))), 8*time.Second)
	if err != nil {
		return 0, fmt.Errorf("AT+CCHO 打开逻辑通道失败: %w", err)
	}
	match := regexp.MustCompile(`\+CCHO:\s*(\d+)`).FindStringSubmatch(response)
	if len(match) != 2 {
		return 0, fmt.Errorf("AT+CCHO did not return a logical channel: %s", response)
	}
	var channel int
	if _, err := fmt.Sscanf(match[1], "%d", &channel); err != nil || channel <= 0 || channel > 255 {
		return 0, fmt.Errorf("invalid logical channel %q", match[1])
	}
	c.channel = byte(channel)
	return c.channel, nil
}

func (c *ATSmartCardChannel) Transmit(command []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.channel == 0 {
		return nil, fmt.Errorf("eUICC logical channel is not open")
	}
	commandHex := strings.ToUpper(hex.EncodeToString(command))
	response, err := c.command(fmt.Sprintf(`AT+CGLA=%d,%d,"%s"`, c.channel, len(commandHex), commandHex), 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("AT+CGLA APDU 透传失败: %w", err)
	}
	match := regexp.MustCompile(`\+CGLA:\s*\d+\s*,\s*"?([0-9A-Fa-f]+)"?`).FindStringSubmatch(response)
	if len(match) != 2 {
		return nil, fmt.Errorf("AT+CGLA did not return an APDU response: %s", response)
	}
	return hex.DecodeString(match[1])
}

func (c *ATSmartCardChannel) CloseLogicalChannel(channel byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.command(fmt.Sprintf("AT+CCHC=%d", channel), 8*time.Second)
	if c.channel == channel {
		c.channel = 0
	}
	return err
}
