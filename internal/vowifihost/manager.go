package vowifihost

import (
	"context"

	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

type Manager struct {
	runtimeStore  RuntimeStore
	stateHub      *StateHub
	recoverStore  *DesiredRecoverStore
	lifecycle     *LifecycleController
	runtimeStart  runtimeStartFunc
	adapter       Adapter
	voiceGateway  *voicehost.Gateway
	deliveryStore messaging.DeliveryStore
	dispatcher    eventhost.Dispatcher
	// imsRegistrar 是本仓库新增接线字段（非上游代码）：由宿主在启动前注入，
	// StartRuntime 时透传给引擎，使 IMSReady 反映真实 SIP 注册结果而非占位值。
	imsRegistrar runtimehost.IMSRegistrar
}

func NewManager() *Manager {
	return NewManagerWithRuntimeStore(NewRuntimeStore())
}

func NewManagerWithRuntimeStore(store RuntimeStore) *Manager {
	if store == nil {
		store = NewRuntimeStore()
	}
	m := &Manager{
		runtimeStore: store,
		stateHub:     NewStateHub(),
		recoverStore: NewDesiredRecoverStore(),
	}
	m.lifecycle = NewLifecycleController(LifecycleControllerOptions{
		IsActive: m.Active,
		Run:      m.runLifecycleCommand,
	})
	return m
}

func (m *Manager) RuntimeStore() RuntimeStore {
	if m == nil || m.runtimeStore == nil {
		return NewRuntimeStore()
	}
	return m.runtimeStore
}

func (m *Manager) SubscribeState(deviceID string) (<-chan struct{}, func()) {
	return m.stateNotifications().Subscribe(deviceID)
}

func (m *Manager) BroadcastState(deviceID string) {
	m.stateNotifications().Broadcast(deviceID)
}

func (m *Manager) SubscriberCount(deviceID string) int {
	return m.stateNotifications().SubscriberCount(deviceID)
}

func (m *Manager) RecordStartupState(deviceID string, state runtimehost.State) bool {
	if !m.RuntimeStore().RecordStartupState(deviceID, state) {
		return false
	}
	m.BroadcastState(deviceID)
	return true
}

func (m *Manager) ClearStartupState(deviceID string) bool {
	return m.RuntimeStore().ClearStartupState(deviceID)
}

func (m *Manager) ConfigureRuntimeDependencies(vg *voicehost.Gateway, ds messaging.DeliveryStore, ed eventhost.Dispatcher) {
	if m == nil {
		return
	}
	m.voiceGateway = vg
	m.deliveryStore = ds
	m.dispatcher = ed
}

// ConfigureIMSRegistrar 注入 IMS 注册器（本仓库新增接线点，非上游代码）。
// 注册器在每次 StartRuntime 时透传给引擎；引擎非 nil 时执行真实 SIP REGISTER
// 并将结果反映到 State().IMSReady（nil 时沿用引擎的 IMSReady=true 占位值）。
func (m *Manager) ConfigureIMSRegistrar(reg runtimehost.IMSRegistrar) {
	if m == nil {
		return
	}
	m.imsRegistrar = reg
}

func (m *Manager) ClearStartupStateAndBroadcast(deviceID string) {
	m.ClearStartupState(deviceID)
	m.BroadcastState(deviceID)
}

func (m *Manager) stateNotifications() *StateHub {
	if m == nil || m.stateHub == nil {
		return NewStateHub()
	}
	return m.stateHub
}

func (m *Manager) desiredRecoverStore() *DesiredRecoverStore {
	if m == nil || m.recoverStore == nil {
		return NewDesiredRecoverStore()
	}
	return m.recoverStore
}

func (m *Manager) ConfigureLifecycle(options LifecycleControllerOptions) {
	if m == nil {
		return
	}
	if options.IsActive == nil {
		options.IsActive = m.Active
	}
	if options.Run == nil {
		options.Run = m.runLifecycleCommand
	}
	m.lifecycle = NewLifecycleController(options)
}

func (m *Manager) SubmitLifecycle(ctx context.Context, cmd LifecycleCommand) error {
	return m.lifecycleController().Submit(ctx, cmd)
}

func (m *Manager) NextLifecycleGeneration(deviceID string) uint64 {
	return m.lifecycleController().NextGeneration(deviceID)
}

func (m *Manager) CurrentLifecycleGeneration(deviceID string) uint64 {
	return m.lifecycleController().CurrentGeneration(deviceID)
}

func (m *Manager) SetLifecycleRunForTest(fn func(context.Context, LifecycleCommand) error) {
	m.lifecycleController().SetRunForTest(fn)
}

func (m *Manager) SetLifecycleRecoverRunForTest(fn func(context.Context, string, string, string) error) {
	m.lifecycleController().SetRecoverRunForTest(fn)
}

func (m *Manager) LifecycleControllerForTest() *LifecycleController {
	return m.lifecycleController()
}

func (m *Manager) lifecycleController() *LifecycleController {
	if m == nil || m.lifecycle == nil {
		if m == nil {
			return NewLifecycleController()
		}
		m.ConfigureLifecycle(LifecycleControllerOptions{})
	}
	return m.lifecycle
}
