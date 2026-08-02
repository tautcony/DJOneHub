package runtime

import (
	"context"
	"sync"

	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

type Resource string

const (
	ResourceAT      Resource = "at"
	ResourceQMI     Resource = "qmi"
	ResourceMBIM    Resource = "mbim"
	ResourceSIM     Resource = "sim"
	ResourceNetwork Resource = "network"
	ResourceVoWiFi  Resource = "vowifi"
	ResourceDevice  Resource = "device"
)

type ResourceLocks struct {
	mu    sync.Mutex
	locks map[Resource]chan struct{}
}

func NewResourceLocks() *ResourceLocks {
	return &ResourceLocks{locks: make(map[Resource]chan struct{})}
}

func (l *ResourceLocks) lock(resource Resource) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locks[resource] == nil {
		l.locks[resource] = make(chan struct{}, 1)
		l.locks[resource] <- struct{}{}
	}
	return l.locks[resource]
}

func (l *ResourceLocks) Acquire(ctx context.Context, resource Resource) (func(), error) {
	select {
	case <-l.lock(resource):
		return func() { l.lock(resource) <- struct{}{} }, nil
	case <-ctx.Done():
		return nil, derrors.New(derrors.OperationConflict, "device resource is busy", true, map[string]any{"resource": resource})
	}
}
