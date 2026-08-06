package esim

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestReadAfterWriteCompletionNeverReportsFalseOperationInProgress 压测写完成
// 通知的关闭/替换序列：并发写方不断获取/释放写锁并广播完成，并发读方排队等待。
// 读方只有在锁确实被长期占用时才能返回 ErrOperationInProgress；锁空闲却等到
// 超时（订阅了一个永远不会关闭的完成 channel）即为误报。
func TestReadAfterWriteCompletionNeverReportsFalseOperationInProgress(t *testing.T) {
	mgr := &Manager{
		opDone:              make(chan struct{}),
		readQueueWaitTimeout: 200 * time.Millisecond,
	}

	stop := make(chan struct{})
	var writers sync.WaitGroup
	for w := 0; w < 3; w++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := mgr.acquireOperationLock(); err != nil {
					t.Errorf("writer acquire failed: %v", err)
					return
				}
				time.Sleep(100 * time.Microsecond)
				mgr.opMu.Unlock()
				mgr.notifyWriteDone()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	var falsePositives atomic.Int32
	var readers sync.WaitGroup
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for i := 0; i < 200; i++ {
				select {
				case <-stop:
					return
				default:
				}
				err := mgr.acquireOperationLock()
				if err == nil {
					mgr.opMu.Unlock()
					continue
				}
				// 超时返回时锁必须确实被占用；锁空闲说明读方等待了一个
				// 永远不会关闭的完成 channel，是误报。
				if mgr.opMu.TryLock() {
					mgr.opMu.Unlock()
					falsePositives.Add(1)
				}
			}
		}()
	}
	readers.Wait()
	close(stop)
	writers.Wait()

	if falsePositives.Load() != 0 {
		t.Fatalf("%d false operation-in-progress reports with an idle write lock", falsePositives.Load())
	}
}
