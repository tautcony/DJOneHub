package modem

import (
	"fmt"
	"strings"
)

type urcLogLevel int

const (
	urcLogDebug urcLogLevel = iota
	urcLogInfo
	urcLogWarn
)

// LogMessageContent 控制是否记录短信内容、USSD 文本、PDU 与通话号码等敏感
// 内容 (openspec 变更 cleanup-architectural-debt D7)。默认关闭; 只有显式
// 开启时这些字段才会出现在默认日志输出中。身份标识 (IMEI/ICCID) 不受此
// 开关影响, 始终按 maskIdentity 掩码记录。
var LogMessageContent = false

// maskIdentity 掩码 IMEI/ICCID 等身份标识: 仅保留前 3 位与后 4 位, 其余以
// 星号填充。完整值只在 Debug 级别日志中记录。
func maskIdentity(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 8 {
		return "****"
	}
	return value[:3] + strings.Repeat("*", len(value)-7) + value[len(value)-4:]
}

// sensitiveLogFieldKeys 是日志输出时统一过滤的敏感字段: 短信内容/USSD 文本/
// PDU/通话号码及其原始行。URC 分发器仍从完整 Fields 读取数据, 只有写入日志
// 前经过 filterSensitiveLogFields。
var sensitiveLogFieldKeys = map[string]bool{
	"text":    true,
	"number":  true,
	"content": true,
	"sender":  true,
	"pdu":     true,
	"raw":     true,
}

// filterSensitiveLogFields 在 LogMessageContent 关闭时移除敏感字段。
func filterSensitiveLogFields(fields []any) []any {
	if LogMessageContent {
		return fields
	}
	out := make([]any, 0, len(fields))
	for i := 0; i+1 < len(fields); i += 2 {
		if key, ok := fields[i].(string); ok && sensitiveLogFieldKeys[key] {
			continue
		}
		out = append(out, fields[i], fields[i+1])
	}
	return out
}

type urcFormatResult struct {
	Level       urcLogLevel
	Key         string
	Msg         string
	Fields      []any
	CMTIIndex   string
	CMTIStorage string
}

func urcKey(line string) string {
	s := strings.TrimSpace(line)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "^") || strings.HasPrefix(s, "$") {
		if i := strings.IndexByte(s, ':'); i > 0 {
			return s[:i]
		}
		if j := strings.IndexAny(s, " ,"); j > 0 {
			return s[:j]
		}
		return s
	}
	// 无前缀但含空格的标准 URC，需要返回完整字符串作为 Key
	switch s {
	case "NO CARRIER", "NO ANSWER", "SMS Ready", "Call Ready", "NORMAL POWER DOWN":
		return s
	}
	if j := strings.IndexByte(s, ' '); j > 0 {
		return s[:j]
	}
	return s
}

func parseCMTI(line string) (string, string, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "+CMTI:") {
		return "", "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(s, "+CMTI:"))
	parts := strings.Split(rest, ",")
	if len(parts) < 2 {
		return "", "", false
	}
	storage := strings.Trim(strings.TrimSpace(parts[0]), "\"")
	index := strings.TrimSpace(parts[1])
	if storage == "" || index == "" {
		return "", "", false
	}
	return storage, index, true
}

func parseURCAfterColon(line string) string {
	if i := strings.IndexByte(line, ':'); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}

func parseCommaFields(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(parts[i]), "\"")
	}
	return parts
}

func (m *Manager) formatURC(line string) urcFormatResult {
	s := strings.TrimSpace(line)
	if s == "" {
		return urcFormatResult{Level: urcLogDebug, Key: "", Msg: "URC"}
	}

	key := urcKey(s)
	out := urcFormatResult{
		Level:  urcLogDebug,
		Key:    key,
		Msg:    "URC",
		Fields: []any{"type", key},
	}

	switch key {
	case "+CMTI":
		st, idx, ok := parseCMTI(s)
		if ok {
			out.Level = urcLogInfo
			out.Msg = "URC: 新短信通知"
			out.Fields = append(out.Fields, "storage", st, "index", idx)
			out.CMTIIndex = idx
			out.CMTIStorage = st
			return out
		}
		out.Level = urcLogInfo
		out.Msg = "URC: 新短信通知"
		out.Fields = append(out.Fields, "raw", s)
		return out

	case "+CREG", "+CGREG", "+CEREG":
		rest := parseURCAfterColon(s)
		fields := parseCommaFields(rest)
		stat := -1
		if len(fields) >= 2 {
			if v, ok := parseInt(fields[1]); ok {
				stat = v
			}
		}
		out.Level = urcLogInfo
		out.Msg = "URC: 注册状态变更"
		out.Fields = append(out.Fields, "domain", strings.TrimPrefix(key, "+"), "stat", stat)
		if stat >= 0 && key == "+CREG" {
			out.Fields = append(out.Fields, "stat_text", m.getRegStatusText(stat))
		}
		if len(fields) >= 4 {
			out.Fields = append(out.Fields, "lac", fields[2], "cell_id", fields[3])
		}
		if len(fields) >= 5 {
			out.Fields = append(out.Fields, "act", fields[4])
		}
		return out

	case "+CPIN":
		rest := parseURCAfterColon(s)
		out.Level = urcLogInfo
		out.Msg = "URC: SIM 状态"
		out.Fields = append(out.Fields, "state", strings.Trim(strings.TrimSpace(rest), "\""))
		return out

	case "+QSIMSTAT":
		rest := parseURCAfterColon(s)
		fields := parseCommaFields(rest)
		inserted := -1
		if len(fields) >= 2 {
			if v, ok := parseInt(fields[1]); ok {
				inserted = v
			}
		}
		out.Level = urcLogInfo
		out.Msg = "URC: SIM 插拔"
		out.Fields = append(out.Fields, "inserted", inserted)
		return out

	case "+QIURC", "+QIND":
		rest := parseURCAfterColon(s)
		fields := extractQuotedFields(rest)
		name := ""
		if len(fields) > 0 {
			name = fields[0]
		}
		out.Level = urcLogInfo
		out.Msg = "URC: Quectel 事件"
		if name != "" {
			out.Fields = append(out.Fields, "event", name)
		}
		out.Fields = append(out.Fields, "raw", s)
		return out

	case "+CUSD":
		rest := parseURCAfterColon(s)
		fields := parseCommaFields(rest)
		n := -1
		dcs := -1
		text := ""
		if len(fields) >= 1 {
			n, _ = parseInt(fields[0])
		}
		if len(fields) >= 2 {
			text = fields[1]
		}
		if len(fields) >= 3 {
			dcs, _ = parseInt(fields[2])
		}
		out.Level = urcLogInfo
		out.Msg = "URC: USSD"
		// text 字段同时被 USSD 分发器消费 (manager.go dispatchURC), 不能在此
		// 过滤; 敏感字段在日志输出点统一过滤 (filterSensitiveLogFields)。
		out.Fields = append(out.Fields, "n", n, "dcs", dcs, "text", text)
		return out

	case "+CLIP":
		rest := parseURCAfterColon(s)
		fields := extractQuotedFields(rest)
		number := ""
		if len(fields) > 0 {
			number = fields[0]
		}
		out.Level = urcLogInfo
		out.Msg = "URC: 来电显示"
		if number != "" {
			out.Fields = append(out.Fields, "number", number)
		}
		out.Fields = append(out.Fields, "raw", s)
		return out

	case "+QPCMV":
		rest := parseURCAfterColon(s)
		out.Level = urcLogInfo
		out.Msg = "URC: PCM 流控"
		out.Fields = append(out.Fields, "state", strings.TrimSpace(rest))
		return out

	default:
		switch s {
		case "RING", "RDY", "SMS Ready", "Call Ready", "NORMAL POWER DOWN", "NO CARRIER", "BUSY", "NO ANSWER":
			out.Level = urcLogInfo
			out.Msg = "URC: 事件"
			out.Fields = append(out.Fields, "raw", s)
			return out
		}
		out.Level = urcLogDebug
		out.Msg = "URC: 未分类"
		out.Fields = append(out.Fields, "raw", s)
		return out
	}
}

func parseInt(s string) (int, bool) {
	v := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &v); err != nil {
		return 0, false
	}
	return v, true
}
