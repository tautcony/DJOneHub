package smscodec

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestReassemblerPassesThroughNonConcat(t *testing.T) {
	r := NewReassembler()

	complete, full := r.Add("10086", ConcatInfo{}, "hello")
	if !complete || full != "hello" {
		t.Fatalf("Add(non-concat) = (%v, %q), want (true, hello)", complete, full)
	}
}

func TestReassemblerReassemblesOutOfOrder(t *testing.T) {
	r := NewReassembler()
	concat2 := ConcatInfo{IsConcat: true, Ref: 7, Total: 2, Seq: 2}
	concat1 := ConcatInfo{IsConcat: true, Ref: 7, Total: 2, Seq: 1}

	if complete, full := r.Add("10086", concat2, "world"); complete || full != "" {
		t.Fatalf("first Add = (%v, %q), want (false, \"\")", complete, full)
	}
	complete, full := r.Add("10086", concat1, "hello ")
	if !complete || full != "hello world" {
		t.Fatalf("second Add = (%v, %q), want (true, \"hello world\")", complete, full)
	}
}

func TestReassemblerDeduplicatesSequence(t *testing.T) {
	r := NewReassembler()
	concat1 := ConcatInfo{IsConcat: true, Ref: 9, Total: 2, Seq: 1}
	concat2 := ConcatInfo{IsConcat: true, Ref: 9, Total: 2, Seq: 2}

	if complete, _ := r.Add("10010", concat1, "foo"); complete {
		t.Fatal("first seq should not complete")
	}
	if complete, _ := r.Add("10010", concat1, "foo-dup"); complete {
		t.Fatal("duplicate seq should not complete")
	}
	complete, full := r.Add("10010", concat2, "bar")
	if !complete || full != "foobar" {
		t.Fatalf("final Add = (%v, %q), want (true, \"foobar\")", complete, full)
	}
}

func TestReassemblerCleanupRemovesExpiredGroup(t *testing.T) {
	r := NewReassembler()
	concat1 := ConcatInfo{IsConcat: true, Ref: 11, Total: 2, Seq: 1}
	concat2 := ConcatInfo{IsConcat: true, Ref: 11, Total: 2, Seq: 2}

	if complete, _ := r.Add("alice", concat1, "part1"); complete {
		t.Fatal("first seq should not complete")
	}
	r.Cleanup(0)
	if complete, full := r.Add("alice", concat2, "part2"); complete || full != "" {
		t.Fatalf("Add after Cleanup(0) = (%v, %q), want (false, \"\")", complete, full)
	}
}

func TestReassemblerConcurrentAddDifferentSenders(t *testing.T) {
	r := NewReassembler()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sender := fmt.Sprintf("sender-%d", i)
			concat1 := ConcatInfo{IsConcat: true, Ref: i, Total: 2, Seq: 1}
			concat2 := ConcatInfo{IsConcat: true, Ref: i, Total: 2, Seq: 2}
			if complete, _ := r.Add(sender, concat1, "foo"); complete {
				t.Errorf("%s first Add unexpectedly completed", sender)
			}
			if complete, full := r.Add(sender, concat2, "bar"); !complete || full != "foobar" {
				t.Errorf("%s second Add = (%v, %q), want (true, \"foobar\")", sender, complete, full)
			}
		}()
	}
	wg.Wait()
}

func TestReassemblerCleanupExpiresByLatestFragmentAge(t *testing.T) {
	r := NewReassembler()
	r.cache["bob_5_2"] = []Fragment{{Ref: 5, Total: 2, Seq: 1, Content: "x", Time: time.Now().Add(-time.Minute)}}
	r.Cleanup(30 * time.Second)
	if _, ok := r.cache["bob_5_2"]; ok {
		t.Fatal("expected expired group to be removed")
	}
}

// TestReassemblerKeySurvivesReferenceWraparound: 8 位引用号回绕后，同一发送方的
// 两条不同长短信可能复用同一 (sender, ref)；总分片数进入缓存键后两者互不污染。
func TestReassemblerKeySurvivesReferenceWraparound(t *testing.T) {
	r := NewReassembler()
	// 消息 A：ref=255，3 段
	msgA1 := ConcatInfo{IsConcat: true, Ref: 255, Total: 3, Seq: 1}
	msgA2 := ConcatInfo{IsConcat: true, Ref: 255, Total: 3, Seq: 2}
	// 消息 B：回绕后 ref=255，2 段
	msgB1 := ConcatInfo{IsConcat: true, Ref: 255, Total: 2, Seq: 1}

	if complete, _ := r.Add("10086", msgA1, "A1"); complete {
		t.Fatal("msgA seq1 should not complete")
	}
	// B 的分片不能补全 A 的组。
	if complete, _ := r.Add("10086", msgB1, "B1"); complete {
		t.Fatal("msgB seq1 must not complete msgA")
	}
	complete, full := r.Add("10086", msgA2, "A2")
	if complete || full != "" {
		t.Fatalf("msgA with only 2/3 segments = (%v, %q), want (false, \"\")", complete, full)
	}
	complete, full = r.Add("10086", ConcatInfo{IsConcat: true, Ref: 255, Total: 2, Seq: 2}, "B2")
	if !complete || full != "B1B2" {
		t.Fatalf("msgB completion = (%v, %q), want (true, \"B1B2\")", complete, full)
	}
	// A 的第三段仍独立补全。
	complete, full = r.Add("10086", ConcatInfo{IsConcat: true, Ref: 255, Total: 3, Seq: 3}, "A3")
	if !complete || full != "A1A2A3" {
		t.Fatalf("msgA completion = (%v, %q), want (true, \"A1A2A3\")", complete, full)
	}
}

func TestReassemblerRejectsInconsistentSegments(t *testing.T) {
	r := NewReassembler()
	// 序号超出总分片数或为零：拒绝，不进入缓存。
	if complete, _ := r.Add("10086", ConcatInfo{IsConcat: true, Ref: 1, Total: 2, Seq: 0}, "bad"); complete {
		t.Fatal("seq 0 must be rejected")
	}
	if complete, _ := r.Add("10086", ConcatInfo{IsConcat: true, Ref: 1, Total: 2, Seq: 3}, "bad"); complete {
		t.Fatal("seq beyond total must be rejected")
	}
	if complete, _ := r.Add("10086", ConcatInfo{IsConcat: true, Ref: 1, Total: 0, Seq: 1}, "bad"); complete {
		t.Fatal("total 0 must be rejected")
	}
	if complete, full := r.Add("10086", ConcatInfo{IsConcat: true, Ref: 1, Total: 2, Seq: 1}, "ok"); complete || full != "" {
		t.Fatalf("valid seq 1 = (%v, %q), want (false, \"\")", complete, full)
	}
}
