package modem

import "testing"

// TestDecodeEFIMSIPinsStandardLayout 钉住 3GPP TS 31.102 标准布局：
// 首个字节为数据长度，后续为 swapped-BCD。首位数字属于 MCC，绝不因
// "parity 位"假设被截断（docs/code-review-report.md 3.2 H1）。
func TestDecodeEFIMSIPinsStandardLayout(t *testing.T) {
	// IMSI 460009300011111 (15 位, 末尾填充 F) 的 EF_IMSI 内容:
	// 长度 0x08, 之后每字节为两位 swapped-BCD 数字 (46 00 90 03 00 11 11 1F)。
	efContent := []byte{0x08, 0x64, 0x00, 0x90, 0x03, 0x00, 0x11, 0x11, 0xF1}

	imsi, err := decodeEFIMSI(efContent)
	if err != nil {
		t.Fatalf("decodeEFIMSI() error = %v", err)
	}
	if imsi != "460009300011111" {
		t.Fatalf("decodeEFIMSI() = %q, want full IMSI 460009300011111 (MCC 460, not 600)", imsi)
	}
	if len(imsi) < 5 || imsi[:3] != "460" {
		t.Fatalf("decodeEFIMSI() MCC = %q, want 460 (first digit must not be truncated)", imsi[:3])
	}
}

// TestDecodeEFIMSIRejectsShortContent 保护长度分支的边界:
// 数据太短或长度字节越界时返回错误而不是静默坏数据。
func TestDecodeEFIMSIRejectsShortContent(t *testing.T) {
	for name, content := range map[string][]byte{
		"single byte":   {0x08},
		"empty":         {},
		"len truncated": {0x08, 0x64, 0x00},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeEFIMSI(content); err == nil {
				t.Fatalf("decodeEFIMSI(%v) succeeded, want error", content)
			}
		})
	}
}

// TestEFIMSICrossPathConsistency 断言 AT 路径与 MBIM 路径对同一 EF_IMSI
// 内容解码出相同的完整 IMSI: MBIM simfile 读取路径 (mbim_backend_simfiles.go)
// 对相同 swapped-BCD 内容直接 DecodeSwappedBCD、从不截断首位。
func TestEFIMSICrossPathConsistency(t *testing.T) {
	efContent := []byte{0x08, 0x64, 0x00, 0x90, 0x03, 0x00, 0x11, 0x11, 0xF1}

	// AT 路径: 完整解码 (含长度字节处理), 不截断。
	atIMSI, err := decodeEFIMSI(efContent)
	if err != nil {
		t.Fatalf("decodeEFIMSI() error = %v", err)
	}

	// MBIM 路径参考: 跳过长度字节后直接 swapped-BCD 解码 (与
	// mbim_backend_simfiles.go 读取 EF 内容的方式一致)。
	mbimIMSI := DecodeSwappedBCD(efContent[1:])

	if atIMSI != mbimIMSI {
		t.Fatalf("AT path IMSI = %q, MBIM reference = %q; paths must decode identically", atIMSI, mbimIMSI)
	}
	if len(mbimIMSI) != 15 || mbimIMSI[:3] != "460" {
		t.Fatalf("MBIM reference IMSI = %q, want full 15-digit 460009300011111", mbimIMSI)
	}
}
