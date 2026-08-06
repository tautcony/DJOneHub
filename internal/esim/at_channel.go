package esim

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// atCommandTransport 将纯 AT 命令执行器适配为 logicalChannelTransport,
// 通过 AT+CCHO / AT+CGLA / AT+CCHC 访问 eUICC APDU 通道。与具体传输无关:
// command 由调用方注入 (darwin USB bulk 传输或 Linux/Windows 串口 AT 通道)。
// 响应解析正则只编译一次 (包级变量), 不随每个 APDU 重新编译。
type atCommandTransport struct {
	command func(string, time.Duration) (string, error)
}

var (
	atCCHOChannelRe = regexp.MustCompile(`\+CCHO:\s*(\d+)`)
	// +CGLA 响应标准格式为 +CGLA: <length>,"<hex>"（TS 27.007 §8.43，length
	// 为 hex 字符数），与 modem 包 parseCGLA 的预期一致；个别模组会在 length
	// 前附带 channel 数字字段。第一段数字设为可选，两种格式都取引号内的
	// APDU 响应，避免把 length 误当作数据捕获。
	atCGLAResponseRe = regexp.MustCompile(`\+CGLA:\s*(?:\d+\s*,\s*)?\d+\s*,\s*"?([0-9A-Fa-f]+)"?`)
)

func newATCommandTransport(command func(string, time.Duration) (string, error)) *atCommandTransport {
	return &atCommandTransport{command: command}
}

func (t *atCommandTransport) OpenLogicalChannel(aid string) (int, error) {
	response, err := t.command(fmt.Sprintf(`AT+CCHO="%s"`, strings.ToUpper(aid)), 8*time.Second)
	if err != nil {
		return 0, fmt.Errorf("AT+CCHO 打开逻辑通道失败: %w", err)
	}
	match := atCCHOChannelRe.FindStringSubmatch(response)
	if len(match) != 2 {
		return 0, fmt.Errorf("AT+CCHO did not return a logical channel: %s", response)
	}
	var channel int
	if _, err := fmt.Sscanf(match[1], "%d", &channel); err != nil || channel <= 0 || channel > 255 {
		return 0, fmt.Errorf("invalid logical channel %q", match[1])
	}
	return channel, nil
}

func (t *atCommandTransport) TransmitAPDU(channel int, apduHex string) (string, error) {
	commandHex := strings.ToUpper(apduHex)
	response, err := t.command(fmt.Sprintf(`AT+CGLA=%d,%d,"%s"`, channel, len(commandHex), commandHex), 15*time.Second)
	if err != nil {
		return "", fmt.Errorf("AT+CGLA APDU 透传失败: %w", err)
	}
	match := atCGLAResponseRe.FindStringSubmatch(response)
	if len(match) != 2 {
		return "", fmt.Errorf("AT+CGLA did not return an APDU response: %s", response)
	}
	return match[1], nil
}

func (t *atCommandTransport) CloseLogicalChannel(channel int) error {
	_, err := t.command(fmt.Sprintf("AT+CCHC=%d", channel), 8*time.Second)
	return err
}
