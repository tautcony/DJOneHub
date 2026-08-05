//go:build !linux

package modem

import (
	"os"
	"strconv"
	"strings"
)

// forceReleasePort 在非 Linux 平台无 fuser 命令与 /proc 接口，不做端口抢占释放。
func (m *Manager) forceReleasePort(portPath string) {}

func parseFuserPIDs(raw string) []int {
	// fuser 输出通常形如: "/dev/ttyUSB2: 1234 5678"
	// 只解析冒号后的 PID，避免把设备名中的数字(如 ttyUSB2)误当作 PID。
	if idx := strings.Index(raw, ":"); idx >= 0 && idx+1 < len(raw) {
		raw = raw[idx+1:]
	}
	seen := make(map[int]struct{})
	out := make([]int, 0)
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r < '0' || r > '9'
	})
	for _, f := range fields {
		pid, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || pid <= 0 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		out = append(out, pid)
	}
	return out
}

func currentProcessTaskPIDSet() map[int]struct{} {
	return map[int]struct{}{
		os.Getpid(): {},
	}
}
