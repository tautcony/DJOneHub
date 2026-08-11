package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

const maxEDLSessions = 8

type edlSession struct {
	id               string
	physicalLocation string
	observation      device.EDLObservation
	activeOperation  string
	original         device.Candidate
	edl              device.Candidate
	updatedAt        time.Time
}

// EDLSessionManager 是进程内 EDL 会话状态的唯一写入者。互斥只表达设备忙:
// 同时刻至多一个进行中的 device-control 操作 (含打开的 ADB shell), 没有
// 客户端侧租约或 token。状态读路径 (探测/观察) 始终允许, 与操作互斥无关。
type EDLSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*edlSession
	now      func() time.Time
	bus      *EventBus
}

func NewEDLSessionManager(bus *EventBus) *EDLSessionManager {
	return &EDLSessionManager{sessions: make(map[string]*edlSession), now: time.Now, bus: bus}
}

func (m *EDLSessionManager) Observe(location string, observation device.EDLObservation) (device.EDLSessionSnapshot, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return device.EDLSessionSnapshot{}, derrors.New(derrors.InvalidRequest, "physical location is required", false, nil)
	}
	m.mu.Lock()
	session, err := m.sessionLocked(location)
	if err != nil {
		m.mu.Unlock()
		return device.EDLSessionSnapshot{}, err
	}
	observation.ObservedAt = m.now().UTC()
	session.observation = observation
	session.updatedAt = m.now()
	snapshot := m.snapshotLocked(session)
	m.mu.Unlock()
	m.publish(snapshot)
	return snapshot, nil
}

func (m *EDLSessionManager) Correlate(original, edl device.Candidate) (device.EDLSessionSnapshot, error) {
	location := strings.TrimSpace(original.Identity.PhysicalLocation)
	if location == "" || location != strings.TrimSpace(edl.Identity.PhysicalLocation) {
		if location != "" {
			m.MarkRecoveryRequired(location, "EDL physical location does not match the managed device")
		}
		return device.EDLSessionSnapshot{}, derrors.New(derrors.DeviceOffline, "EDL physical location does not match the managed device", true, map[string]any{"phase": "await_edl"})
	}
	m.mu.Lock()
	session, err := m.sessionLocked(location)
	if err != nil {
		m.mu.Unlock()
		return device.EDLSessionSnapshot{}, err
	}
	session.original = original
	session.edl = edl
	session.updatedAt = m.now()
	snapshot := m.snapshotLocked(session)
	m.mu.Unlock()
	m.publish(snapshot)
	return snapshot, nil
}

// BeginOperation 申请设备互斥: 同时刻至多一个操作 (含 shell 连接) 可持有。
// 会话不存在时按物理位置创建, 互斥不依赖任何先前的 EDL 观察。
func (m *EDLSessionManager) BeginOperation(location, operation string) error {
	m.mu.Lock()
	session, err := m.sessionLocked(strings.TrimSpace(location))
	if err != nil {
		m.mu.Unlock()
		return operationBusy(device.EDLSessionSnapshot{})
	}
	if session.activeOperation != "" {
		snapshot := m.snapshotLocked(session)
		m.mu.Unlock()
		return operationBusy(snapshot)
	}
	session.activeOperation = strings.TrimSpace(operation)
	session.updatedAt = m.now()
	snapshot := m.snapshotLocked(session)
	m.mu.Unlock()
	m.publish(snapshot)
	return nil
}

func (m *EDLSessionManager) EndOperation(location string) {
	m.mu.Lock()
	session := m.sessions[strings.TrimSpace(location)]
	if session != nil {
		session.activeOperation = ""
		session.updatedAt = m.now()
	}
	var snapshot device.EDLSessionSnapshot
	if session != nil {
		snapshot = m.snapshotLocked(session)
	}
	m.mu.Unlock()
	if session != nil {
		m.publish(snapshot)
	}
}

func (m *EDLSessionManager) ClearObservation(location string) {
	m.mu.Lock()
	session := m.sessions[strings.TrimSpace(location)]
	if session != nil {
		session.observation = device.EDLObservation{}
		session.updatedAt = m.now()
	}
	var snapshot device.EDLSessionSnapshot
	if session != nil {
		snapshot = m.snapshotLocked(session)
	}
	m.mu.Unlock()
	if session != nil {
		m.publish(snapshot)
	}
}

func (m *EDLSessionManager) MarkRecoveryRequired(location, reason string) {
	location = strings.TrimSpace(location)
	if location == "" {
		return
	}
	_, _ = m.Observe(location, device.EDLObservation{
		State:          device.EDLStateRecoveryRequired,
		Protocol:       "sahara",
		Source:         "usb",
		Reason:         strings.TrimSpace(reason),
		RecoveryNeeded: true,
	})
}

func (m *EDLSessionManager) Snapshot(location string) (device.EDLSessionSnapshot, bool) {
	m.mu.Lock()
	session := m.sessions[strings.TrimSpace(location)]
	if session == nil {
		m.mu.Unlock()
		return device.EDLSessionSnapshot{}, false
	}
	snapshot := m.snapshotLocked(session)
	m.mu.Unlock()
	return snapshot, true
}

func (m *EDLSessionManager) sessionLocked(location string) (*edlSession, error) {
	if session := m.sessions[location]; session != nil {
		return session, nil
	}
	now := m.now()
	if len(m.sessions) >= maxEDLSessions {
		var oldestLocation string
		var oldest time.Time
		for candidateLocation, candidate := range m.sessions {
			if candidate.activeOperation != "" {
				continue
			}
			if oldestLocation == "" || candidate.updatedAt.Before(oldest) {
				oldestLocation = candidateLocation
				oldest = candidate.updatedAt
			}
		}
		if oldestLocation == "" {
			return nil, derrors.New(derrors.OperationConflict, "EDL session capacity is occupied by active operations", true, nil)
		}
		delete(m.sessions, oldestLocation)
	}
	id, err := randomSessionID()
	if err != nil {
		id = location
	}
	session := &edlSession{id: id, physicalLocation: location, updatedAt: now}
	m.sessions[location] = session
	return session, nil
}

func (m *EDLSessionManager) snapshotLocked(session *edlSession) device.EDLSessionSnapshot {
	return device.EDLSessionSnapshot{SessionID: session.id, PhysicalLocation: session.physicalLocation, Observation: session.observation, ActiveOperation: session.activeOperation}
}

func (m *EDLSessionManager) publish(snapshot device.EDLSessionSnapshot) {
	if m.bus != nil {
		m.bus.Publish("device_control.edl_session_changed", snapshot)
	}
}

func operationBusy(snapshot device.EDLSessionSnapshot) error {
	details := map[string]any{}
	if snapshot.SessionID != "" {
		details["session_id"] = snapshot.SessionID
	}
	if snapshot.ActiveOperation != "" {
		details["active_operation"] = snapshot.ActiveOperation
	}
	return derrors.New(derrors.DeviceSessionConflict, "the device is busy with an in-flight operation", true, details)
}

func randomSessionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
