package operation

import (
	"context"
	stdErrors "errors"
	"log"
	"strings"
	"sync"
	"time"

	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
)

type State string

const (
	Pending   State = "pending"
	Running   State = "running"
	Succeeded State = "succeeded"
	Failed    State = "failed"
	Cancelled State = "cancelled"
)

type Status struct {
	ID         string         `json:"operation_id"`
	Type       string         `json:"type"`
	State      State          `json:"state"`
	Progress   int            `json:"progress"`
	Message    string         `json:"message,omitempty"`
	Error      *derrors.Error `json:"error,omitempty"`
	StartedAt  time.Time      `json:"started_at,omitempty"`
	FinishedAt time.Time      `json:"finished_at,omitempty"`
}

type Log struct {
	OperationID string `json:"operation_id"`
	Type        string `json:"type"`
	Message     string `json:"message"`
}

// Task 执行异步操作。第二个参数是 operation_id，任务内需要发布带 operation
// 身份的事件（如下载确认码请求）时使用；第一个参数是操作上下文。
type Task func(context.Context, string, func(int, string)) error

// ErrShutdown is returned by Start once shutdown admission has closed; no
// operation is launched and no ID is returned.
var ErrShutdown = derrors.New(derrors.Unavailable, "the application is shutting down", false, nil)

type Manager struct {
	mu      sync.RWMutex
	seq     uint64
	items   map[string]*Status
	cancels map[string]context.CancelFunc
	bus     *runtime.EventBus

	closed bool
	runWG  sync.WaitGroup
	// shutdownDone is closed once every tracked run goroutine has joined. It
	// is shared by all Shutdown callers: an early caller timeout does not
	// poison later callers, who wait with their own contexts.
	shutdownDone chan struct{}
}

func NewManager(bus *runtime.EventBus) *Manager {
	if bus == nil {
		bus = runtime.NewEventBus()
	}
	return &Manager{
		items:        make(map[string]*Status),
		cancels:      make(map[string]context.CancelFunc),
		bus:          bus,
		shutdownDone: make(chan struct{}),
	}
}

// Start launches an asynchronous operation. After Shutdown closes admission it
// returns the structured ErrShutdown and launches nothing.
func (m *Manager) Start(ctx context.Context, kind string, task Task) (string, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", ErrShutdown
	}
	m.seq++
	id := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + formatSequence(m.seq)
	status := &Status{ID: id, Type: kind, State: Pending}
	m.items[id] = status
	// REST handlers return immediately for asynchronous operations. Detach the
	// worker from the request context so the operation remains alive after the
	// HTTP response; callers can cancel it explicitly through the operation API.
	child, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.cancels[id] = cancel
	m.runWG.Add(1)
	m.mu.Unlock()
	m.publish(*status)
	log.Printf("operation started id=%s type=%s", id, kind)
	go m.run(child, status, task)
	return id, nil
}

// Shutdown closes admission for new work, cancels every tracked operation, and
// waits for their run goroutines within the bounded context. Repeated calls
// share one close signal: each caller waits with its own context, so an early
// caller timeout does not become the result for later callers. Workers already
// report the Cancelled terminal state when their context is cancelled.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		for _, cancel := range m.cancels {
			cancel()
		}
		go func() {
			m.runWG.Wait()
			close(m.shutdownDone)
		}()
	}
	done := m.shutdownDone
	m.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) run(ctx context.Context, status *Status, task Task) {
	defer m.runWG.Done()
	m.update(status.ID, func(s *Status) { s.State = Running; s.StartedAt = time.Now().UTC() })
	err := task(ctx, status.ID, func(progress int, message string) {
		if progress < 0 {
			progress = 0
		}
		if progress > 100 {
			progress = 100
		}
		m.update(status.ID, func(s *Status) { s.Progress = progress; s.Message = message })
		m.mu.RLock()
		current := clone(*m.items[status.ID])
		m.mu.RUnlock()
		m.bus.Publish("operation.progress", current)
	})
	m.mu.Lock()
	delete(m.cancels, status.ID)
	m.mu.Unlock()
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("operation cancelled id=%s type=%s error=%v", status.ID, status.Type, err)
			m.update(status.ID, func(s *Status) {
				s.State = Cancelled
				s.Message = "operation cancelled"
				// 取消是显式分类错误 (design D15): 客户端可据 error.code 判断,
				// 而不是把取消误报为通用失败。
				details := map[string]any(nil)
				var structured *derrors.Error
				if stdErrors.As(err, &structured) && len(structured.Details) > 0 {
					details = make(map[string]any, len(structured.Details))
					for key, value := range structured.Details {
						details[key] = value
					}
				}
				s.Error = derrors.New(derrors.OperationCancelled, "operation cancelled", false, details)
				s.FinishedAt = time.Now().UTC()
			})
		} else {
			log.Printf("operation failed id=%s type=%s error=%v", status.ID, status.Type, err)
			m.update(status.ID, func(s *Status) { s.State = Failed; s.Error = normalizeError(err); s.FinishedAt = time.Now().UTC() })
		}
	} else {
		log.Printf("operation succeeded id=%s type=%s", status.ID, status.Type)
		m.update(status.ID, func(s *Status) { s.State = Succeeded; s.Progress = 100; s.FinishedAt = time.Now().UTC() })
	}
	m.mu.RLock()
	current := clone(*m.items[status.ID])
	m.mu.RUnlock()
	m.bus.Publish("operation.completed", current)
}

// HasActiveKind reports whether any in-flight operation (pending or running)
// matches one of the given kinds. Used by the VoWiFi host adapter to gate
// lifecycle changes while an eSIM enable/disable switch is underway.
func (m *Manager) HasActiveKind(kinds ...string) bool {
	if len(kinds) == 0 {
		return false
	}
	want := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		if kind = strings.TrimSpace(kind); kind != "" {
			want[kind] = struct{}{}
		}
	}
	if len(want) == 0 {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, status := range m.items {
		if status.State != Pending && status.State != Running {
			continue
		}
		if _, ok := want[status.Type]; ok {
			return true
		}
	}
	return false
}

func (m *Manager) Get(id string) (Status, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status, ok := m.items[id]
	if !ok {
		return Status{}, false
	}
	return clone(*status), true
}

func (m *Manager) Events() *runtime.EventBus { return m.bus }

func (m *Manager) Publish(eventType string, data any) {
	m.bus.Publish(eventType, data)
}

func (m *Manager) Log(id, message string) {
	if message == "" {
		return
	}
	status, ok := m.Get(id)
	if !ok {
		return
	}
	m.bus.Publish("operation.log", Log{OperationID: id, Type: status.Type, Message: message})
}

func (m *Manager) Cancel(id string) bool {
	m.mu.RLock()
	cancel, ok := m.cancels[id]
	m.mu.RUnlock()
	if ok {
		cancel()
	}
	return ok
}

type Diagnostics struct {
	Accepting bool          `json:"accepting"`
	Active    int           `json:"active"`
	ByState   map[State]int `json:"by_state"`
}

func (m *Manager) Diagnostics() Diagnostics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := Diagnostics{Accepting: !m.closed, Active: len(m.cancels), ByState: make(map[State]int)}
	for _, status := range m.items {
		out.ByState[status.State]++
	}
	return out
}

func (m *Manager) update(id string, update func(*Status)) {
	m.mu.Lock()
	status, ok := m.items[id]
	if ok {
		update(status)
		current := clone(*status)
		m.mu.Unlock()
		m.publish(current)
		return
	}
	m.mu.Unlock()
}

func (m *Manager) publish(status Status) { m.bus.Publish("operation.changed", status) }

func normalizeError(err error) *derrors.Error {
	var structured *derrors.Error
	if stdErrors.As(err, &structured) {
		copy := *structured
		copy.Message = derrors.PublicMessage(copy.Code)
		return &copy
	}
	return &derrors.Error{Code: derrors.Internal, Message: derrors.PublicMessage(derrors.Internal), Retryable: true, Cause: err}
}

func clone(status Status) Status {
	if status.Error != nil {
		copied := *status.Error
		status.Error = &copied
	}
	return status
}

func formatSequence(value uint64) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if value == 0 {
		return "0"
	}
	var out [13]byte
	i := len(out)
	for value > 0 {
		i--
		out[i] = digits[value%36]
		value /= 36
	}
	return string(out[i:])
}
