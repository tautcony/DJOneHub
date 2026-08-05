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

type Task func(context.Context, func(int, string)) error

type Manager struct {
	mu      sync.RWMutex
	seq     uint64
	items   map[string]*Status
	cancels map[string]context.CancelFunc
	bus     *runtime.EventBus
}

func NewManager(bus *runtime.EventBus) *Manager {
	if bus == nil {
		bus = runtime.NewEventBus()
	}
	return &Manager{items: make(map[string]*Status), cancels: make(map[string]context.CancelFunc), bus: bus}
}

func (m *Manager) Start(ctx context.Context, kind string, task Task) string {
	m.mu.Lock()
	m.seq++
	id := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + formatSequence(m.seq)
	status := &Status{ID: id, Type: kind, State: Pending}
	m.items[id] = status
	// REST handlers return immediately for asynchronous operations. Detach the
	// worker from the request context so the operation remains alive after the
	// HTTP response; callers can cancel it explicitly through the operation API.
	child, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.cancels[id] = cancel
	m.mu.Unlock()
	m.publish(*status)
	log.Printf("operation started id=%s type=%s", id, kind)
	go m.run(child, status, task)
	return id
}

func (m *Manager) run(ctx context.Context, status *Status, task Task) {
	m.update(status.ID, func(s *Status) { s.State = Running; s.StartedAt = time.Now().UTC() })
	err := task(ctx, func(progress int, message string) {
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
	if strings.TrimSpace(message) == "" {
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
