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

const (
	defaultEDLLeaseTTL = 30 * time.Second
	maxEDLSessions     = 8
)

type edlSession struct {
	id               string
	physicalLocation string
	observation      device.EDLObservation
	leaseToken       string
	leaseExpiresAt   time.Time
	activeOperation  string
	original         device.Candidate
	edl              device.Candidate
	updatedAt        time.Time
}

// EDLSessionManager is the single writer for process-local EDL session state.
// Browser connections observe snapshots and use opaque leases for mutations.
type EDLSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*edlSession
	ttl      time.Duration
	now      func() time.Time
	bus      *EventBus
}

func NewEDLSessionManager(bus *EventBus, ttl time.Duration) *EDLSessionManager {
	if ttl <= 0 {
		ttl = defaultEDLLeaseTTL
	}
	return &EDLSessionManager{sessions: make(map[string]*edlSession), ttl: ttl, now: time.Now, bus: bus}
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

func (m *EDLSessionManager) BeginOperation(location, token, operation string) error {
	m.mu.Lock()
	session := m.sessions[strings.TrimSpace(location)]
	if session == nil {
		m.mu.Unlock()
		return sessionConflict(device.EDLSessionSnapshot{})
	}
	expired := m.expireLocked(session, m.now())
	if token == "" || token != session.leaseToken || session.activeOperation != "" {
		snapshot := m.snapshotLocked(session)
		m.mu.Unlock()
		if expired {
			m.publish(snapshot)
		}
		return sessionConflict(snapshot)
	}
	session.activeOperation = strings.TrimSpace(operation)
	session.updatedAt = m.now()
	snapshot := m.snapshotLocked(session)
	m.mu.Unlock()
	m.publish(snapshot)
	return nil
}

func (m *EDLSessionManager) EndOperation(location, token string) {
	m.mu.Lock()
	session := m.sessions[strings.TrimSpace(location)]
	if session != nil && token == session.leaseToken {
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

func (m *EDLSessionManager) Acquire(location string) (string, device.EDLSessionSnapshot, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return "", device.EDLSessionSnapshot{}, derrors.New(derrors.InvalidRequest, "physical location is required", false, nil)
	}
	m.mu.Lock()
	session, sessionErr := m.sessionLocked(location)
	if sessionErr != nil {
		m.mu.Unlock()
		return "", device.EDLSessionSnapshot{}, sessionErr
	}
	now := m.now()
	m.expireLocked(session, now)
	if session.leaseToken != "" {
		snapshot := m.snapshotLocked(session)
		m.mu.Unlock()
		return "", snapshot, sessionConflict(snapshot)
	}
	token, err := randomSessionID()
	if err != nil {
		m.mu.Unlock()
		return "", device.EDLSessionSnapshot{}, err
	}
	session.leaseToken = token
	session.leaseExpiresAt = now.Add(m.ttl)
	session.updatedAt = now
	snapshot := m.snapshotLocked(session)
	m.mu.Unlock()
	m.publish(snapshot)
	return token, snapshot, nil
}

func (m *EDLSessionManager) Renew(location, token string) (device.EDLSessionSnapshot, error) {
	m.mu.Lock()
	session := m.sessions[strings.TrimSpace(location)]
	now := m.now()
	if session == nil {
		m.mu.Unlock()
		return device.EDLSessionSnapshot{}, sessionConflict(device.EDLSessionSnapshot{})
	}
	expired := m.expireLocked(session, now)
	if token == "" || token != session.leaseToken {
		snapshot := m.snapshotLocked(session)
		m.mu.Unlock()
		if expired {
			m.publish(snapshot)
		}
		return snapshot, sessionConflict(snapshot)
	}
	session.leaseExpiresAt = now.Add(m.ttl)
	session.updatedAt = now
	snapshot := m.snapshotLocked(session)
	m.mu.Unlock()
	m.publish(snapshot)
	return snapshot, nil
}

func (m *EDLSessionManager) Release(location, token string) error {
	m.mu.Lock()
	session := m.sessions[strings.TrimSpace(location)]
	if session == nil || token == "" || token != session.leaseToken || session.activeOperation != "" {
		var snapshot device.EDLSessionSnapshot
		if session != nil {
			snapshot = m.snapshotLocked(session)
		}
		m.mu.Unlock()
		return sessionConflict(snapshot)
	}
	session.leaseToken = ""
	session.leaseExpiresAt = time.Time{}
	session.updatedAt = m.now()
	snapshot := m.snapshotLocked(session)
	m.mu.Unlock()
	m.publish(snapshot)
	return nil
}

func (m *EDLSessionManager) Owns(location, token string) bool {
	m.mu.Lock()
	session := m.sessions[strings.TrimSpace(location)]
	if session == nil {
		m.mu.Unlock()
		return false
	}
	expired := m.expireLocked(session, m.now())
	owned := token != "" && token == session.leaseToken
	snapshot := m.snapshotLocked(session)
	m.mu.Unlock()
	if expired {
		m.publish(snapshot)
	}
	return owned
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
	expired := m.expireLocked(session, m.now())
	snapshot := m.snapshotLocked(session)
	m.mu.Unlock()
	if expired {
		m.publish(snapshot)
	}
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
			m.expireLocked(candidate, now)
			if candidate.leaseToken != "" || candidate.activeOperation != "" {
				continue
			}
			if oldestLocation == "" || candidate.updatedAt.Before(oldest) {
				oldestLocation = candidateLocation
				oldest = candidate.updatedAt
			}
		}
		if oldestLocation == "" {
			return nil, derrors.New(derrors.OperationConflict, "EDL session capacity is occupied by active leases", true, nil)
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

func (m *EDLSessionManager) expireLocked(session *edlSession, now time.Time) bool {
	if session.leaseToken != "" && session.activeOperation == "" && !session.leaseExpiresAt.After(now) {
		session.leaseToken = ""
		session.leaseExpiresAt = time.Time{}
		session.activeOperation = ""
		return true
	}
	return false
}

func (m *EDLSessionManager) snapshotLocked(session *edlSession) device.EDLSessionSnapshot {
	return device.EDLSessionSnapshot{SessionID: session.id, PhysicalLocation: session.physicalLocation, Observation: session.observation, LeaseHeld: session.leaseToken != "", LeaseExpiresAt: session.leaseExpiresAt, ActiveOperation: session.activeOperation}
}

func (m *EDLSessionManager) publish(snapshot device.EDLSessionSnapshot) {
	if m.bus != nil {
		m.bus.Publish("device_control.edl_session_changed", snapshot)
	}
}

func sessionConflict(snapshot device.EDLSessionSnapshot) error {
	details := map[string]any{}
	if snapshot.SessionID != "" {
		details["session_id"] = snapshot.SessionID
	}
	return derrors.New(derrors.DeviceSessionConflict, "another client controls the device session", true, details)
}

func randomSessionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
