package mbim

import (
	"fmt"
	"time"
)

// fixedDoneOffset is where the info buffer begins inside a first
// COMMAND_DONE/INDICATE fragment.
const fixedDoneOffset = headerLen + fragHdrLen + uuidLen + 4 + 4 + 4

// fixedCmdOffset is where the info buffer begins inside a first COMMAND fragment.
const fixedCmdOffset = headerLen + fragHdrLen + uuidLen + 4 + 4 + 4

// 收集器资源上限：设备控制的碎片流不能无限累积内存。
const (
	maxCollectorFragments = 256
	maxCollectorBytes     = 1 << 20 // 1 MiB
)

// collectorStaleTTL 与 collectorSweepInterval 控制不完整收集器的定期清理。
const (
	collectorStaleTTL      = 2 * time.Minute
	collectorSweepInterval = 30 * time.Second
)

type collector struct {
	started  bool
	total    uint32
	next     uint32
	service  UUID
	cid      uint32
	status   uint32
	fullLen  uint32
	info     []byte
	fragments int
	lastSeen time.Time
}

func newCollector() *collector {
	return &collector{lastSeen: time.Now()}
}

func (c *collector) add(b []byte) (bool, error) {
	if len(b) < headerLen+fragHdrLen {
		return false, fmt.Errorf("mbim: fragment shorter than fragment header len=%d", len(b))
	}
	c.lastSeen = time.Now()

	total := le.Uint32(b[12:])
	current := le.Uint32(b[16:])
	if !c.started {
		if current != 0 {
			return false, fmt.Errorf("mbim: first fragment current=%d, want 0", current)
		}
		if len(b) < fixedDoneOffset {
			return false, fmt.Errorf("mbim: first fragment shorter than fixed fields len=%d", len(b))
		}
		if err := c.checkLimits(len(b) - fixedDoneOffset); err != nil {
			return false, err
		}
		copy(c.service[:], b[20:36])
		c.cid = le.Uint32(b[36:])
		c.status = le.Uint32(b[40:])
		c.fullLen = le.Uint32(b[44:])
		c.total = total
		c.next = 1
		c.started = true
		c.info = append(c.info, b[fixedDoneOffset:]...)
		c.fragments++
		return c.next >= c.total, nil
	}

	if current != c.next {
		return false, fmt.Errorf("mbim: fragment out of order got=%d want=%d", current, c.next)
	}
	if err := c.checkLimits(len(b) - (headerLen + fragHdrLen)); err != nil {
		return false, err
	}
	c.next++
	c.info = append(c.info, b[headerLen+fragHdrLen:]...)
	c.fragments++
	return c.next >= c.total, nil
}

// checkLimits 在追加 payload 前校验累计字节数与碎片数上限，设备控制的
// 无终止碎片流不会耗尽内存。
func (c *collector) checkLimits(incoming int) error {
	if c.fragments+1 > maxCollectorFragments {
		return fmt.Errorf("mbim: fragment collector fragment count %d exceeds limit %d", c.fragments+1, maxCollectorFragments)
	}
	if len(c.info)+incoming > maxCollectorBytes {
		return fmt.Errorf("mbim: fragment collector bytes %d exceeds limit %d", len(c.info)+incoming, maxCollectorBytes)
	}
	return nil
}

func (c *collector) commandDone() (CommandDone, error) {
	if !c.started {
		return CommandDone{}, fmt.Errorf("mbim: collector has no data")
	}
	if int(c.fullLen) > len(c.info) {
		return CommandDone{}, fmt.Errorf("mbim: reassembled info shorter than declared length got=%d want=%d", len(c.info), c.fullLen)
	}
	info := c.info
	if int(c.fullLen) <= len(info) {
		info = info[:c.fullLen]
	}
	return CommandDone{
		Service:    c.service,
		CID:        c.cid,
		Status:     c.status,
		InfoBuffer: info,
	}, nil
}

func splitCommand(txID uint32, service UUID, cid uint32, ct CommandType, info []byte, maxControlTransfer uint32) [][]byte {
	max := int(maxControlTransfer)
	firstCap := max - fixedCmdOffset
	contCap := max - (headerLen + fragHdrLen)
	if firstCap <= 0 || contCap <= 0 || len(info) <= firstCap {
		return [][]byte{encodeCommand(txID, service, cid, ct, info)}
	}

	sizes := []int{firstCap}
	for remaining := len(info) - firstCap; remaining > 0; {
		take := contCap
		if take > remaining {
			take = remaining
		}
		sizes = append(sizes, take)
		remaining -= take
	}

	total := uint32(len(sizes))
	frags := make([][]byte, 0, len(sizes))
	pos := 0
	for i, size := range sizes {
		chunk := info[pos : pos+size]
		pos += size
		if i == 0 {
			b := make([]byte, fixedCmdOffset+size)
			putHeader(b, MessageTypeCommand, uint32(len(b)), txID)
			le.PutUint32(b[12:], total)
			le.PutUint32(b[16:], 0)
			copy(b[20:36], service[:])
			le.PutUint32(b[36:], cid)
			le.PutUint32(b[40:], uint32(ct))
			le.PutUint32(b[44:], uint32(len(info)))
			copy(b[fixedCmdOffset:], chunk)
			frags = append(frags, b)
			continue
		}

		b := make([]byte, headerLen+fragHdrLen+size)
		putHeader(b, MessageTypeCommand, uint32(len(b)), txID)
		le.PutUint32(b[12:], total)
		le.PutUint32(b[16:], uint32(i))
		copy(b[headerLen+fragHdrLen:], chunk)
		frags = append(frags, b)
	}
	return frags
}
