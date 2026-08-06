package smscodec

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Fragment struct {
	Ref     int
	Total   int
	Seq     int
	Content string
	Time    time.Time
}

type Reassembler struct {
	mu    sync.Mutex
	cache map[string][]Fragment
}

func NewReassembler() *Reassembler {
	return &Reassembler{cache: make(map[string][]Fragment)}
}

func (r *Reassembler) Add(sender string, concat ConcatInfo, content string) (complete bool, fullContent string) {
	if !concat.IsConcat {
		return true, content
	}
	// 8 位引用号回绕后不同长短信可能复用同一 (sender, ref)；总分片数加入缓存
	// 键并在添加时校验序号范围，避免互相污染。
	if concat.Total <= 0 || concat.Seq < 1 || concat.Seq > concat.Total {
		return false, ""
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s_%d_%d", sender, concat.Ref, concat.Total)
	fragments := r.cache[key]
	for _, f := range fragments {
		if f.Seq == concat.Seq {
			return false, ""
		}
	}
	fragments = append(fragments, Fragment{
		Ref:     concat.Ref,
		Total:   concat.Total,
		Seq:     concat.Seq,
		Content: content,
		Time:    time.Now(),
	})
	r.cache[key] = fragments

	if len(fragments) != concat.Total {
		return false, ""
	}

	sort.Slice(fragments, func(i, j int) bool { return fragments[i].Seq < fragments[j].Seq })
	var full strings.Builder
	for _, f := range fragments {
		full.WriteString(f.Content)
	}
	delete(r.cache, key)
	return true, full.String()
}

func (r *Reassembler) Cleanup(ttl time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-ttl)
	for key, fragments := range r.cache {
		var latest time.Time
		for _, f := range fragments {
			if f.Time.After(latest) {
				latest = f.Time
			}
		}
		if latest.IsZero() || !latest.After(cutoff) {
			delete(r.cache, key)
		}
	}
}
