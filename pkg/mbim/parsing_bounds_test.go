package mbim

import (
	"strings"
	"testing"
	"time"
)

// TestHostileDeviceCountsRejectedBeforePreallocation: 设备控制的 count 超过
// 剩余缓冲区容量时必须被拒绝，而不是按恶意计数预分配内存。
func TestHostileDeviceCountsRejectedBeforePreallocation(t *testing.T) {
	// 8 字节的 SMS_READ 头 + 恶意 count。
	smsInfo := make([]byte, 8)
	le.PutUint32(smsInfo[4:], 0xFFFFFFFF)
	if _, err := parseSMSRead(smsInfo); err == nil || !strings.Contains(err.Error(), "exceeds buffer capacity") {
		t.Fatalf("parseSMSRead hostile count err = %v, want capacity rejection", err)
	}

	providersInfo := make([]byte, 4)
	le.PutUint32(providersInfo[0:], 0x7FFFFFFF)
	if _, err := parseVisibleProviders(providersInfo); err == nil || !strings.Contains(err.Error(), "exceeds buffer capacity") {
		t.Fatalf("parseVisibleProviders hostile count err = %v, want capacity rejection", err)
	}

	hostileCount := make([]byte, 4)
	le.PutUint32(hostileCount[0:], 0xFFFFFFFF)
	if _, err := newInfoReader(hostileCount).stringArrayCountAt(0); err == nil {
		t.Fatal("stringArrayCountAt with hostile count must be rejected")
	}

	appsInfo := make([]byte, 8)
	le.PutUint32(appsInfo[4:], 0xFFFFFFFF)
	if _, err := parseUICCApplicationList(appsInfo); err == nil || !strings.Contains(err.Error(), "exceeds buffer capacity") {
		t.Fatalf("parseUICCApplicationList hostile count err = %v, want capacity rejection", err)
	}
}

// TestHostileCountsWithRoomForSomeEntries: 缓冲区只够放少量条目时，count 超出
// 容量仍被拒绝。
func TestHostileCountsWithRoomForSomeEntries(t *testing.T) {
	smsInfo := make([]byte, 8+2*8)
	le.PutUint32(smsInfo[4:], 100) // 只够 2 个 (offset,size) 对
	if _, err := parseSMSRead(smsInfo); err == nil || !strings.Contains(err.Error(), "exceeds buffer capacity") {
		t.Fatalf("parseSMSRead count=100 in 24-byte buffer err = %v, want capacity rejection", err)
	}
}

// TestCollectorCapsNeverCompletingFragmentStream: 永不完成的碎片流被收集器的
// 字节/碎片数上限终止。
func TestCollectorCapsNeverCompletingFragmentStream(t *testing.T) {
	c := newCollector()
	// 第一片声明 total 非常大，后续持续投递续片。
	first := make([]byte, fixedDoneOffset+16)
	putHeader(first, MessageTypeCommandDone, uint32(len(first)), 1)
	le.PutUint32(first[12:], 0xFFFFFFF0)
	le.PutUint32(first[16:], 0)
	copy(first[20:36], UUIDSMS[:])
	le.PutUint32(first[36:], CIDSMSSend)
	le.PutUint32(first[40:], 0)
	le.PutUint32(first[44:], 0xFFFFFFFF)
	copy(first[fixedDoneOffset:], make([]byte, 16))

	done, err := c.add(first)
	if err != nil || done {
		t.Fatalf("first fragment add = (%v, %v), want (false, nil)", done, err)
	}

	cont := make([]byte, headerLen+fragHdrLen+512)
	putHeader(cont, MessageTypeCommandDone, uint32(len(cont)), 1)
	le.PutUint32(cont[12:], 0xFFFFFFF0)
	for i := uint32(1); i < maxCollectorFragments; i++ {
		le.PutUint32(cont[16:], i)
		if _, err := c.add(cont); err != nil {
			t.Fatalf("collector rejected fragment %d early: %v", i, err)
		}
	}
	// 首片 + 255 续片已占满上限；下一片超过碎片数上限。
	le.PutUint32(cont[16:], maxCollectorFragments)
	if _, err := c.add(cont); err == nil || !strings.Contains(err.Error(), "fragment count") {
		t.Fatalf("overflow add err = %v, want fragment count rejection", err)
	}
}

// TestCollectorByteCap: 累计字节数超过上限时被拒绝。
func TestCollectorByteCap(t *testing.T) {
	c := newCollector()
	chunk := make([]byte, headerLen+fragHdrLen+64)
	putHeader(chunk, MessageTypeCommandDone, uint32(len(chunk)), 1)
	le.PutUint32(chunk[12:], 0xFFFFFFF0)
	first := make([]byte, fixedDoneOffset+16)
	putHeader(first, MessageTypeCommandDone, uint32(len(first)), 1)
	le.PutUint32(first[12:], 0xFFFFFFF0)
	le.PutUint32(first[16:], 0)
	copy(first[20:36], UUIDSMS[:])
	copy(first[fixedDoneOffset:], make([]byte, 16))
	if _, err := c.add(first); err != nil {
		t.Fatal(err)
	}
	// 16 字节首片后追加 64 字节续片直到超过 1 MiB 上限。
	for i := uint32(1); i < maxCollectorBytes/64; i++ {
		le.PutUint32(chunk[16:], i)
		if _, err := c.add(chunk); err != nil {
			if strings.Contains(err.Error(), "exceeds limit") {
				return
			}
			t.Fatalf("fragment %d add err = %v", i, err)
		}
	}
	t.Fatal("byte cap was never enforced")
}

// TestStaleCollectorSwept: 长时间无碎片的不完整收集器被定期清理。
func TestStaleCollectorSwept(t *testing.T) {
	d := newDevice(nil)
	c := newCollector()
	c.lastSeen = time.Now().Add(-collectorStaleTTL - time.Minute)
	d.mu.Lock()
	d.collector[7] = c
	d.mu.Unlock()

	d.sweepStaleCollectors()
	d.mu.Lock()
	_, ok := d.collector[7]
	d.mu.Unlock()
	if ok {
		t.Fatal("stale collector was not swept")
	}
}
