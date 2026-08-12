package modem

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/iniwex5/vohive/internal/apduarbiter"
	domainat "github.com/iniwex5/vohive/internal/domain/at"
	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vohive/pkg/smscodec"
	"github.com/warthog618/sms/encoding/gsm7"

	"go.bug.st/serial"
)

// SMSCallback 短信回调函数类型
type SMSCallback func(sender, content string, timestamp time.Time)

// rxMsg 串口接收到的消息包装
type rxMsg struct {
	Data string
	Err  error
}

// commandRequest AT 命令请求结构
type commandRequest struct {
	cmd             string
	enqueuedAt      time.Time
	respChan        chan string
	errChan         chan error
	timeout         time.Duration
	silent          bool
	highPriority    bool
	includeTerminal bool

	// 交互式模式支持
	interactive bool   // 是否为交互式命令 (如发送短信)
	waitPrompt  bool   // 是否等待 "> " 提示符
	followUp    string // 后续指令 (当 waitPrompt=true 且收到提示符时发送)
}

type atCommandDiagnostic struct {
	CommandClass   string `json:"command_class"`
	QueueWaitMS    int64  `json:"queue_wait_ms"`
	ExecMS         int64  `json:"exec_ms"`
	TerminalResult string `json:"terminal_result"`
	TimeoutClass   string `json:"timeout_class"`
}

func commandDiagnostic(req commandRequest, started time.Time, terminalResult, timeoutClass string) atCommandDiagnostic {
	queueWait := time.Duration(0)
	if !req.enqueuedAt.IsZero() && started.After(req.enqueuedAt) {
		queueWait = started.Sub(req.enqueuedAt)
	}
	execTime := time.Since(started)
	if execTime < 0 {
		execTime = 0
	}
	return atCommandDiagnostic{
		CommandClass: domainat.CommandClass(req.cmd), QueueWaitMS: queueWait.Milliseconds(),
		ExecMS: execTime.Milliseconds(), TerminalResult: terminalResult, TimeoutClass: timeoutClass,
	}
}

func (m *Manager) logATCommandDiagnostic(value atCommandDiagnostic) {
	fields := []any{
		"command_class", value.CommandClass,
		"queue_wait_ms", value.QueueWaitMS,
		"exec_ms", value.ExecMS,
		"terminal_result", value.TerminalResult,
		"timeout_class", value.TimeoutClass,
	}
	if value.TerminalResult == "ok" || value.TerminalResult == "prompt" {
		logger.Debug(fmt.Sprintf("[%s] AT 命令完成", m.cfg.ID), fields...)
		return
	}
	logger.Debug(fmt.Sprintf("[%s] AT 命令完成", m.cfg.ID), fields...)
}

// Manager 管理单个 EC20 模块的 AT 指令通信
type Manager struct {
	cfg      Config
	atPort   string
	port     ATTransport
	portMode *serial.Mode

	// 通道驱动的异步架构
	stop        chan struct{}
	stopOnce    sync.Once
	loopWG      sync.WaitGroup
	cmdChan     chan commandRequest // 普通优先级
	cmdChanHigh chan commandRequest // 高优先级 (短信, IP 切换)
	rxChan      chan rxMsg
	triggerChan chan struct{} // 短信触发信号
	ready       chan struct{}
	readyOnce   sync.Once

	// 资源池
	reqPool sync.Pool

	// 状态
	running   bool
	runningMu sync.RWMutex
	busy      bool
	busyMu    sync.Mutex
	healthy   bool
	eofCount  int // readLoop 中连续 EOF 计数，用于检测设备断开

	// overLimitBytes 是 readLoop 中超长行累计丢弃的字节数（仅 readLoop 访问）。
	overLimitBytes int

	atTimeoutMu     sync.Mutex
	atTimeoutStreak int
	// atTimeoutWatchdogThreshold 连续超时触发控制面恢复的阈值；0 表示默认值。
	atTimeoutWatchdogThreshold int

	// atQuarantine* 隔离超时命令的命令流：新命令不被接受，直到超时命令的终结
	// 响应（OK/ERROR）被观测到，或超过传输恢复期限后关闭传输交给运行时重连。
	atQuarantineMu       sync.Mutex
	atQuarantined        bool
	atQuarantineDeadline time.Time

	// 设备信息 (从 AT 指令获取)
	imei             string
	firmware         string
	iccid            string
	imsi             string
	operator         string
	simInserted      bool
	simInsertedKnown bool
	simState         string
	simStateKnown    bool
	signalDBM        int
	signalRSRQ       int
	signalRSRP       int

	// 网络信息
	regStatus          int    // 网络注册状态 (0-5)
	regStatusText      string // 注册状态文本
	registrationStates map[string]int
	lac                string // 位置区代码
	cellID             string // 小区 ID
	apn                string // 接入点
	imsStatus          int    // IMS 注册状态
	networkMode        string // 网络模式 (LTE/WCDMA/GSM等)
	networkDuplex      string // 网络双工方式 (FDD/TDD)
	usbnetMode         int    // USBNET 模式 (0: QMI, 1: ECM)

	infoMu sync.RWMutex

	// 回调
	smsCallback            SMSCallback
	newSMSHandler          func(storage, index string) // +CMTI 消费者回调 (携带存储名与索引)
	disableURCRead         bool                        // 如果启用 QMI，禁用 AT 自动读取
	simStatusHandler       func(inserted *bool, state string)
	onDisconnectWithReason func(reason string)

	// smsReadMu 串行化存储切换/读取/恢复序列，防止并发的 +CMTI 通知交错。
	smsReadMu sync.Mutex

	// CS 来电回调
	ringCallback    func()              // RING URC 回调
	clipCallback    func(number string) // +CLIP URC 回调 (来电号码)
	hangupCallback  func()              // NO CARRIER URC 回调 (对方挂断)
	connectCallback func()              // CONNECT/OK URC 回调 (对方接听外呼)
	qpcmvChan       chan int            // +QPCMV URC 流控通道 (0=忙, 1=就绪)

	reassembler *smscodec.Reassembler

	// SIM 卡低频巡检告警状态
	simFailCount int
	simAlerting  bool

	// USSD 会话通道：当有协程在等待 USSD 响应时，+CUSD URC 会被投递到此通道
	ussdChan chan USSDResult

	// RDY 事件订阅（模组重启后广播）
	rdyMu   sync.Mutex
	rdySubs []chan struct{}

	// APDU 仲裁（设备级全局）
	apduArbiter  *apduarbiter.Arbiter
	apduLeaseMu  sync.Mutex
	apduSessions map[int]apduSessionInfo
}

// Config contains runtime values derived from a discovered modem candidate.
// It is not loaded from a user configuration file.
type Config struct {
	ID                         string
	ATPort                     string
	ManagePort                 string
	ControlDevice              string
	DeviceBackend              string
	ATTimeoutWatchdogThreshold int
}

const (
	// defaultATTimeoutWatchdogThreshold 是连续超时触发控制面恢复的默认阈值。
	defaultATTimeoutWatchdogThreshold = 5
	// longCommandTimeout 超过该超时上限的命令视为长耗时命令，不计入连续超时计数。
	longCommandTimeout = 30 * time.Second
	// atQuarantineRecoveryDeadline 隔离期间等待终结响应的最大时长；超时后
	// 关闭传输并交给运行时重新初始化。
	atQuarantineRecoveryDeadline = 10 * time.Second
	// maxATLineBytes 是单行 AT 输入的最大字节数；超过上限的字节被丢弃。
	maxATLineBytes = 4 * 1024
	// maxATOverLimitBytes 是行缓冲超限时可容忍的累计丢弃字节数；超过后判定
	// 设备异常并触发控制面恢复。
	maxATOverLimitBytes = 64 * 1024
)

type apduSessionInfo struct {
	Channel  int
	Owner    string
	Class    apduarbiter.APDUClass
	OpenedAt time.Time
}

func (m *Manager) DeviceID() string {
	return m.cfg.ID
}

func pureQMIBackendConfig(cfg Config) bool {
	mode := strings.ToLower(strings.TrimSpace(cfg.DeviceBackend))
	return mode == "qmi" || mode == "mbim" || (mode == "" && strings.TrimSpace(cfg.ControlDevice) != "")
}

func (m *Manager) pureQMIBackend() bool {
	return pureQMIBackendConfig(m.cfg)
}

func New(cfg Config) (*Manager, error) {
	return newManager(cfg, nil)
}

// NewWithATTransport creates a manager that owns an already-open AT transport.
// The shared command session uses this path for platform transports that are
// not exposed as operating-system serial ports, such as macOS USB bulk AT.
func NewWithATTransport(cfg Config, transport ATTransport) (*Manager, error) {
	if transport == nil {
		return nil, errors.New("AT transport is nil")
	}
	return newManager(cfg, transport)
}

func newManager(cfg Config, transport ATTransport) (*Manager, error) {
	watchdogThreshold := cfg.ATTimeoutWatchdogThreshold
	if watchdogThreshold <= 0 {
		watchdogThreshold = defaultATTimeoutWatchdogThreshold
	}
	m := &Manager{
		cfg:                        cfg,
		atPort:                     cfg.ATPort,
		port:                       transport,
		stop:                       make(chan struct{}),
		cmdChan:                    make(chan commandRequest, 10),
		cmdChanHigh:                make(chan commandRequest, 5),
		rxChan:                     make(chan rxMsg, 100),
		triggerChan:                make(chan struct{}, 1),
		ready:                      make(chan struct{}),
		healthy:                    true,
		reassembler:                smscodec.NewReassembler(),
		ussdChan:                   make(chan USSDResult, 1),
		apduSessions:               make(map[int]apduSessionInfo),
		registrationStates:         make(map[string]int),
		atTimeoutWatchdogThreshold: watchdogThreshold,
		reqPool: sync.Pool{
			New: func() interface{} {
				return &commandRequest{
					respChan: make(chan string, 1),
					errChan:  make(chan error, 1),
				}
			},
		},
	}

	// 如果未指定 AT 端口，使用 ManagePort
	if m.atPort == "" {
		m.atPort = cfg.ManagePort
	}
	// QMI 后端模式下允许 AT 端口为空（模组不依赖 AT 串口）
	// AT 模式仍然要求 AT 端口非空
	if m.atPort == "" && transport == nil && !pureQMIBackendConfig(cfg) {
		return nil, errors.New("AT port not configured")
	}

	m.portMode = &serial.Mode{
		BaudRate: 115200,
	}

	return m, nil
}

func (m *Manager) markReady() {
	m.readyOnce.Do(func() {
		close(m.ready)
	})
}

func (m *Manager) WaitReady(timeout time.Duration) bool {
	select {
	case <-m.ready:
		return true
	case <-time.After(timeout):
		return false
	case <-m.stop:
		return false
	}
}

// SetSMSCallback 设置短信接收回调
func (m *Manager) SetSMSCallback(cb SMSCallback) {
	m.smsCallback = cb
}

// SetNewSMSHandler 设置新短信引用回调（收到 +CMTI URC 时调用，携带存储名与索引）。
// 回调注册后，管理器不再自动读取/删除短信：读取、持久化与确认删除全部由消费者
// 负责。未注册消费者时，+CMTI 只记录日志，短信条目保留在模组存储中。
func (m *Manager) SetNewSMSHandler(handler func(storage, index string)) {
	m.infoMu.Lock()
	m.newSMSHandler = handler
	m.infoMu.Unlock()
}

// SetDisableURCRead 启用/禁用 URC 自动读取 (当 QMI 接管时应禁用)
func (m *Manager) SetDisableURCRead(disable bool) {
	m.infoMu.Lock()
	m.disableURCRead = disable
	m.infoMu.Unlock()
}

func (m *Manager) SetSIMStatusHandler(handler func(inserted *bool, state string)) {
	m.infoMu.Lock()
	m.simStatusHandler = handler
	m.infoMu.Unlock()
}

// SetRingCallback 设置来电 RING 回调
func (m *Manager) SetRingCallback(cb func()) {
	m.infoMu.Lock()
	m.ringCallback = cb
	m.infoMu.Unlock()
}

// SetClipCallback 设置 +CLIP 来电号码回调
func (m *Manager) SetClipCallback(fn func(number string)) {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	m.clipCallback = fn
}

// SetHangupCallback 设置 NO CARRIER 对方挂断回调
func (m *Manager) SetHangupCallback(fn func()) {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	m.hangupCallback = fn
}

// GetQPCMVChan 获取 +QPCMV URC 流控通道 (0=模块忙, 1=就绪)
func (m *Manager) GetQPCMVChan() <-chan int {
	if m.qpcmvChan == nil {
		m.qpcmvChan = make(chan int, 4)
	}
	return m.qpcmvChan
}

// AnswerCall 接听来电 (ATA)
func (m *Manager) AnswerCall() error {
	_, err := m.ExecuteAT("ATA", 5*time.Second)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] 接听来电失败", m.cfg.ID), "err", err)
		return err
	}
	logger.Info(fmt.Sprintf("[%s] 已接听来电", m.cfg.ID))
	return nil
}

// DialCall 发起语音外呼 (ATD<number>;)
func (m *Manager) DialCall(number string) error {
	cmd := fmt.Sprintf("ATD%s;", number)
	_, err := m.ExecuteAT(cmd, 60*time.Second)
	if err != nil {
		fields := []any{"err", err}
		if LogMessageContent {
			fields = append(fields, "number", number)
		}
		logger.Error(fmt.Sprintf("[%s] 拨号失败", m.cfg.ID), fields...)
		return err
	}
	if LogMessageContent {
		logger.Info(fmt.Sprintf("[%s] 拨号指令已发出", m.cfg.ID), "number", number)
	} else {
		logger.Info(fmt.Sprintf("[%s] 拨号指令已发出", m.cfg.ID))
	}
	return nil
}

// HangupCall 挂断通话 (ATH)
func (m *Manager) HangupCall() error {
	_, err := m.ExecuteAT("ATH", 3*time.Second)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] 挂断通话失败", m.cfg.ID), "err", err)
		return err
	}
	logger.Info(fmt.Sprintf("[%s] 已挂断通话", m.cfg.ID))
	return nil
}

// SetConnectCallback 设置 CONNECT/OK (对方接听外呼) 回调
func (m *Manager) SetConnectCallback(fn func()) {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	m.connectCallback = fn
}

// SetOnDisconnectWithReason 设置带原因的串口/控制面掉线回调。
func (m *Manager) SetOnDisconnectWithReason(cb func(reason string)) {
	m.infoMu.Lock()
	m.onDisconnectWithReason = cb
	m.infoMu.Unlock()
}

func (m *Manager) notifyDisconnect(reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "modem_disconnect"
	}
	m.infoMu.RLock()
	withReason := m.onDisconnectWithReason
	m.infoMu.RUnlock()
	if withReason != nil {
		go withReason(reason)
	}
}

func (m *Manager) resetATTimeoutWatchdog() {
	m.atTimeoutMu.Lock()
	m.atTimeoutStreak = 0
	m.atTimeoutMu.Unlock()
}

func (m *Manager) recordATTimeout(req commandRequest) (int, bool) {
	if req.highPriority {
		return 0, false
	}
	// 长耗时命令（如拨号 60s）超时并不代表设备卡死，不计入连续超时计数。
	if req.timeout > longCommandTimeout {
		return 0, false
	}
	m.atTimeoutMu.Lock()
	defer m.atTimeoutMu.Unlock()
	m.atTimeoutStreak++
	return m.atTimeoutStreak, m.atTimeoutStreak >= m.atTimeoutWatchdogThreshold
}

func (m *Manager) tripATTimeoutWatchdog(commandClass string, failures int) {
	if !m.isRunning() {
		return
	}
	logger.Warn(fmt.Sprintf("[%s] AT 连续超时达到阈值，触发控制面恢复", m.cfg.ID),
		"command_class", commandClass,
		"port", m.atPort,
		"failures", failures,
		"threshold", m.atTimeoutWatchdogThreshold)
	m.healthy = false
	m.Stop()
	m.notifyDisconnect("at_timeout_threshold")
}

// Start 启动 AT 管理器的后台协程
func (m *Manager) Start() error {
	if m.pureQMIBackend() {
		logger.Info(fmt.Sprintf("[%s] 纯 QMI 模式，跳过 AT 管理器启动", m.cfg.ID), "at_port", m.atPort)
		m.setRunning(false)
		m.markReady()
		return nil
	}
	if m.atPort == "" && m.port == nil {
		return errors.New("AT port not configured")
	}

	if m.port == nil {
		// 检查并强制接管被占用的端口
		m.forceReleasePort(m.atPort)

		var err error
		for attempt := 0; attempt < 8; attempt++ {
			m.port, err = serial.Open(m.atPort, m.portMode)
			if err == nil {
				break
			}
			if !isRetryableSerialOpenErr(err) {
				break
			}
			time.Sleep(time.Duration(80*(attempt+1)) * time.Millisecond)
		}
		if err != nil {
			return fmt.Errorf("打开串口 %s 失败: %w", m.atPort, err)
		}
	}

	if setter, ok := m.port.(atReadTimeoutSetter); ok {
		if err := setter.SetReadTimeout(100 * time.Millisecond); err != nil {
			_ = m.port.Close()
			m.port = nil
			return fmt.Errorf("set AT transport read timeout: %w", err)
		}
	}
	m.setRunning(true)

	// 启动读取协程
	m.loopWG.Add(1)
	go func() {
		defer m.loopWG.Done()
		m.readLoop()
	}()

	// 启动主事件循环
	m.loopWG.Add(1)
	go func() {
		defer m.loopWG.Done()
		m.runLoop()
	}()

	// 启动分片清理协程
	m.loopWG.Add(1)
	go func() {
		defer m.loopWG.Done()
		ticker := time.NewTicker(2 * time.Minute)
		for {
			select {
			case <-m.stop:
				return
			case <-ticker.C:
				m.cleanupOldFragments()
			}
		}
	}()

	logger.Info(fmt.Sprintf("[%s] AT 管理器已启动", m.cfg.ID), "port", m.atPort)
	return nil
}

func isRetryableSerialOpenErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "resource busy") ||
		strings.Contains(msg, "device or resource busy") ||
		strings.Contains(msg, "temporarily unavailable")
}

func isFatalSerialRuntimeErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(msg, "timeout") {
		return false
	}
	return strings.Contains(msg, "input/output error") ||
		strings.Contains(msg, "no such device") ||
		strings.Contains(msg, "bad file descriptor") ||
		strings.Contains(msg, "device disconnected") ||
		strings.Contains(msg, "usb bulk") ||
		strings.Contains(msg, "libusb_error_no_device") ||
		strings.Contains(msg, "libusb_error_io") ||
		strings.Contains(msg, "libusb_error_pipe")
}

func (m *Manager) handleFatalSerialRuntimeErr(err error, phase string, commandClass string) {
	if !isFatalSerialRuntimeErr(err) {
		return
	}
	if !m.isRunning() {
		return
	}
	logger.Warn(fmt.Sprintf("[%s] AT 串口运行期失效，触发恢复", m.cfg.ID),
		"phase", phase, "command_class", commandClass, "port", m.atPort, "err", err)
	m.healthy = false
	m.Stop()
	m.notifyDisconnect("serial_runtime_error")
}

// Stop 停止管理器
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		m.releaseAllAPDULeases("stop")
		close(m.stop)
		if m.port != nil {
			m.port.Close()
		}
		m.setRunning(false)
	})
}

func (m *Manager) StopAndWait(timeout time.Duration) bool {
	m.Stop()
	done := make(chan struct{})
	go func() {
		m.loopWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// runLoop 主事件循环，处理命令和 URC
func (m *Manager) runLoop() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(fmt.Sprintf("[%s] runLoop panic recovered", m.cfg.ID), "err", r)
		}
	}()

	// 异步初始化模组，避免阻塞主循环导致命令执行死锁
	go m.initModem()

	for {
		// 若上一个命令超时后进入隔离，先等待其终结响应或传输恢复，
		// 期间 URC 照常分发；未解除隔离前不接受任何新命令。
		m.waitForQuarantineRecovery()

		// 优先级调度逻辑
		select {
		case <-m.stop:
			logger.Info(fmt.Sprintf("[%s] AT 管理器已停止", m.cfg.ID))
			return
		case req := <-m.cmdChanHigh:
			// 优先处理高优先级命令
			m.handleCommand(req)
			continue
		default:
			// 如果没有高优先级命令，则检查普通命令
		}

		select {
		case <-m.stop:
			logger.Info(fmt.Sprintf("[%s] AT 管理器已停止", m.cfg.ID))
			return
		case req := <-m.cmdChanHigh: // 再次检查高优先级，防止饿死
			m.handleCommand(req)
		case req := <-m.cmdChan:
			m.handleCommand(req)
		case msg := <-m.rxChan:
			// 空闲状态下的数据处理（主要是 URC）
			if msg.Err != nil {
				logger.Error(fmt.Sprintf("[%s] 串口读取错误，模块可能已掉线", m.cfg.ID), "err", msg.Err)
				m.Stop()
				m.notifyDisconnect("serial_read_error")
				return
			}
			if m.isURC(msg.Data) {
				m.handleURC(msg.Data)
			}
		}
	}
}

// handleCommand 处理单个 AT 命令
func (m *Manager) handleCommand(req commandRequest) {
	startTime := time.Now()
	terminalResult := "unknown"
	timeoutClass := "none"
	defer func() {
		m.logATCommandDiagnostic(commandDiagnostic(req, startTime, terminalResult, timeoutClass))
	}()

	// 发送命令
	if _, err := m.port.Write([]byte(req.cmd + "\r\n")); err != nil {
		terminalResult = "write_error"
		req.errChan <- err
		m.handleFatalSerialRuntimeErr(err, "write", domainat.CommandClass(req.cmd))
		return
	}

	// 等待响应
	fullResponse := []string{}
	timeoutTimer := time.NewTimer(req.timeout)
	defer timeoutTimer.Stop()

RespLoop:
	for {
		select {
		case <-m.stop:
			terminalResult = "stopped"
			req.errChan <- errors.New("manager stopped")
			return
		case <-timeoutTimer.C:
			terminalResult = "timeout"
			timeoutClass = "execution"
			// 超时：先尽力排空已排队的数据（URC 照常分发），随后将命令流隔离，
			// 直到本命令的终结响应（OK/ERROR）被观测到，或传输恢复期限触发
			// 关闭/重连。迟到的 OK 或残余数据因此永远不会被下一个命令消费。
			m.drainResidualLines()
			// 超时时尝试发送 ESC (0x1B) 以取消可能的挂起操作（如短信输入）
			m.port.Write([]byte{0x1B})
			req.errChan <- errors.New("命令执行超时")
			if failures, tripped := m.recordATTimeout(req); tripped {
				m.tripATTimeoutWatchdog(domainat.CommandClass(req.cmd), failures)
			}
			m.enterQuarantine()
			return

		case msg := <-m.rxChan:
			if msg.Err != nil {
				terminalResult = "read_error"
				req.errChan <- msg.Err
				m.handleFatalSerialRuntimeErr(msg.Err, "read", domainat.CommandClass(req.cmd))
				return
			}

			line := msg.Data

			if line == "OK" {
				terminalResult = "ok"
				m.resetATTimeoutWatchdog()
				if req.includeTerminal {
					fullResponse = append(fullResponse, line)
				}
				req.respChan <- strings.Join(fullResponse, "\n")
				break RespLoop
			} else if strings.Contains(line, "ERROR") {
				terminalResult = "device_error"
				m.resetATTimeoutWatchdog()
				fullResponse = append(fullResponse, line)
				req.errChan <- fmt.Errorf("设备返回错误: %s", strings.Join(fullResponse, "\n"))
				break RespLoop
			} else if isResponseLineForCommand(req.cmd, line) {
				fullResponse = append(fullResponse, line)
			} else if m.isURC(line) {
				// URC 判定先于提示符分支：包含 > 的 URC/USSD 行被分发为 URC，
				// 绝不会终止当前命令。
				m.handleURC(line)

				// 但仅当它是某些明确的、完全异步的事件（如短信通知、来电、USSD、插拔卡）时，才将其从当前命令的 fullResponse 中剔除，以免污染解析器
				// 其余如 +CIMI:, +QCCID:, +CSQ: 实际上既是 URC 也是命令回显，必须被当前命令捕获！
				isPureAsyncURC := func(s string) bool {
					key := urcKey(s)
					switch key {
					case "+CUSD", "+CMTI", "RING", "+CLIP", "+QSIMSTAT", "+QSTKURC", "+QPCMV":
						return true
					}
					return false
				}

				if !isPureAsyncURC(line) {
					fullResponse = append(fullResponse, line)
				}
			} else if req.interactive && req.waitPrompt && isBarePrompt(line) {
				// 提示符仅在交互命令正在等待时识别，且必须是裸 "> "/">" 行。
				m.resetATTimeoutWatchdog()
				if req.followUp != "" {
					// 收到提示符，立即发送后续指令
					m.port.Write([]byte(req.followUp))
					// 继续等待最终响应 (OK/ERROR)
					// 重置 waitPrompt 防止重复触发
					req.waitPrompt = false
					continue
				}

				terminalResult = "prompt"
				req.respChan <- "> "
				break RespLoop
			} else {
				fullResponse = append(fullResponse, line)
			}
		}
	}
}

// isBarePrompt 报告 line 是否为裸提示符行（"> " 或 ">"）。
func isBarePrompt(line string) bool {
	return strings.TrimSpace(line) == ">"
}

func isResponseLineForCommand(command, line string) bool {
	command = strings.ToUpper(strings.TrimSpace(command))
	line = strings.ToUpper(strings.TrimSpace(line))
	prefix := ""
	switch command {
	case "AT+CEREG?":
		prefix = "+CEREG:"
	case "AT+CGREG?":
		prefix = "+CGREG:"
	case "AT+CREG?":
		prefix = "+CREG:"
	case "AT+QSIMSTAT?":
		prefix = "+QSIMSTAT:"
	case "AT+CPIN?":
		prefix = "+CPIN:"
	}
	return prefix != "" && strings.HasPrefix(line, prefix)
}

// enterQuarantine 将命令流隔离：新命令不被接受，直到被隔离命令的终结响应
// 被观测到或超过传输恢复期限。
func (m *Manager) enterQuarantine() {
	m.atQuarantineMu.Lock()
	defer m.atQuarantineMu.Unlock()
	m.atQuarantined = true
	m.atQuarantineDeadline = time.Now().Add(atQuarantineRecoveryDeadline)
}

// clearQuarantine 解除命令流隔离。
func (m *Manager) clearQuarantine() {
	m.atQuarantineMu.Lock()
	defer m.atQuarantineMu.Unlock()
	m.atQuarantined = false
	m.atQuarantineDeadline = time.Time{}
}

// isQuarantined 报告命令流当前是否处于隔离状态。
func (m *Manager) isQuarantined() bool {
	m.atQuarantineMu.Lock()
	defer m.atQuarantineMu.Unlock()
	return m.atQuarantined
}

// drainResidualLines 尽力排空 rxChan 中已排队但未消费的行；URC 照常分发。
// 这只是隔离正确性之外的优化，不构成超时后残余数据的隔离保证。
func (m *Manager) drainResidualLines() {
	for {
		select {
		case msg := <-m.rxChan:
			if msg.Err != nil {
				continue
			}
			if m.isURC(msg.Data) {
				m.handleURC(msg.Data)
			}
		default:
			return
		}
	}
}

// waitForQuarantineRecovery 在隔离期间消费 rxChan：观测到被隔离命令的终结响应
// （OK/ERROR）即解除隔离；URC 照常分发（隔离窗口内的 +CMTI/+CUSD 被投递而不
// 是被丢弃或归因于被隔离命令）；超过传输恢复期限则关闭传输并交给运行时重新
// 初始化，之后才能接受新命令。
func (m *Manager) waitForQuarantineRecovery() {
	m.atQuarantineMu.Lock()
	if !m.atQuarantined {
		m.atQuarantineMu.Unlock()
		return
	}
	deadline := m.atQuarantineDeadline
	m.atQuarantineMu.Unlock()
	for {
		select {
		case <-m.stop:
			return
		case msg := <-m.rxChan:
			if msg.Err != nil {
				m.handleFatalSerialRuntimeErr(msg.Err, "quarantine", "other")
				return
			}
			line := msg.Data
			if line == "OK" || strings.Contains(line, "ERROR") {
				m.clearQuarantine()
				logger.Debug(fmt.Sprintf("[%s] 隔离解除：观测到超时命令的终结响应", m.cfg.ID), "terminal_result", "observed")
				return
			}
			if m.isURC(line) {
				m.handleURC(line)
			}
			// 其余行归因于被隔离的命令，丢弃。
		case <-time.After(time.Until(deadline)):
			logger.Warn(fmt.Sprintf("[%s] 隔离期限内未观测到终结响应，关闭传输触发重连", m.cfg.ID),
				"deadline", atQuarantineRecoveryDeadline.String())
			m.clearQuarantine()
			m.healthy = false
			m.Stop()
			m.notifyDisconnect("at_quarantine_recovery")
			return
		}
	}
}

// readLoop 专用读取协程
func (m *Manager) readLoop() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(fmt.Sprintf("[%s] readLoop panic recovered", m.cfg.ID), "err", r)
		}
	}()

	buf := make([]byte, 1024)
	var lineBuf bytes.Buffer

	for {
		select {
		case <-m.stop:
			return
		default:
		}

		n, err := m.port.Read(buf)
		if err != nil {
			errMsg := err.Error()
			// 忽略超时错误和多次读取无数据错误
			if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "multiple Read calls return no data") {
				continue
			}

			// EOF 处理：连续 EOF 超过阈值则判定为设备已断开
			if err == io.EOF {
				m.eofCount++
				if m.eofCount >= 30 { // 连续 30 次 EOF（约 3 秒）
					select {
					case <-m.stop:
						return
					default:
						logger.Warn(fmt.Sprintf("[%s] 串口连续 %d 次 EOF，判定设备已断开", m.cfg.ID, m.eofCount))
						m.rxChan <- rxMsg{Err: fmt.Errorf("连续 %d 次 EOF，判定设备已断开", m.eofCount)}
						return
					}
				}
				time.Sleep(100 * time.Millisecond)
				continue
			}
			m.eofCount = 0 // 非 EOF 错误时重置计数

			select {
			case <-m.stop:
				return
			default:
				m.rxChan <- rxMsg{Err: err}
				return
			}
		}

		if n > 0 {
			m.eofCount = 0 // 成功读取数据，重置 EOF 计数
			for i := 0; i < n; i++ {
				b := buf[i]
				if b == '\n' {
					// 行结束：TrimSpace 后的非空行上抛；含提示符的 "\r\n> " 行
					// 在此被规范化为 ">"。
					line := strings.TrimSpace(lineBuf.String())
					lineBuf.Reset()
					if line != "" {
						select {
						case m.rxChan <- rxMsg{Data: line}:
						case <-m.stop:
							return
						}
					}
					m.overLimitBytes = 0
					continue
				}
				// 行缓冲上限：超过 maxATLineBytes 的字节被丢弃，绝不进入任何
				// 命令的 fullResponse；持续的超限输入视为设备异常，触发恢复。
				if lineBuf.Len() >= maxATLineBytes {
					m.overLimitBytes++
					if m.overLimitBytes >= maxATOverLimitBytes {
						logger.Warn(fmt.Sprintf("[%s] 串口超长行持续超限，判定设备异常", m.cfg.ID),
							"port", m.atPort, "dropped_bytes", m.overLimitBytes)
						m.healthy = false
						m.Stop()
						m.notifyDisconnect("at_over_limit_input")
						return
					}
					continue
				}
				lineBuf.WriteByte(b)
			}

			// 只把独立的提示符行（"> " 或 ">"，如 "\r\n> "）作为提示符上抛；
			// 不再使用包含 > 的半行启发式判定，避免把 URC/数据中的 > 误判为
			// 提示符。其余半行保留在缓冲区等待换行。
			if strings.TrimSpace(lineBuf.String()) == ">" {
				select {
				case m.rxChan <- rxMsg{Data: "> "}:
				case <-m.stop:
					return
				}
				// 清空缓冲区，因为已经消费了提示符
				lineBuf.Reset()
			}
		}
	}
}

// initModem 初始化模组
func (m *Manager) initModem() {
	if m.pureQMIBackend() {
		m.markReady()
		return
	}

	time.Sleep(150 * time.Millisecond)

	// 1. 探测连通性
	_, err := m.ExecuteATSilent("AT", 2*time.Second)
	if err != nil {
		logger.Warn(fmt.Sprintf("[%s] AT 探测失败", m.cfg.ID), "err", err)
		m.markReady()
		return
	}

	// 2. 初始化命令序列
	initCmds := []string{
		"ATE0",              // 关闭回显
		"AT+CMGF=0",         // PDU 模式
		"AT+CNMI=2,1,0,0,0", // 新短信上报 +CMTI
		"AT+CLIP=1",         // 启用来电号码显示 (+CLIP URC)
		"AT+QPCMV=1,2",      // 开启 UAC 语音模式 (PCM → ALSA 桥接必须)
	}

	for _, cmd := range initCmds {
		// 这些初始化命令使用 ExecuteATSilent 降低日志噪音，避免用户误解全在走 AT
		m.ExecuteATSilent(cmd, 2*time.Second)
		time.Sleep(100 * time.Millisecond)
	}

	m.markReady()

	// 3. 采集设备信息
	m.collectDeviceInfo()
	// Info 级别只记录掩码身份; 完整 IMEI/ICCID 仅在 Debug 级别输出。
	logger.Info(fmt.Sprintf("[%s] 模组初始化完成", m.cfg.ID), "imei", maskIdentity(m.imei), "iccid", maskIdentity(m.iccid))
	logger.Debug(fmt.Sprintf("[%s] 模组初始化完成 (完整身份)", m.cfg.ID), "imei", m.imei, "iccid", m.iccid)
}

// RefreshDeviceInfo 重新采集设备信息（切卡后需要更新缓存）
func (m *Manager) RefreshDeviceInfo() {
	m.collectDeviceInfo()
}

// collectDeviceInfo 采集设备信息 (IMEI, ICCID, IMSI, 运营商, 信号等)
func (m *Manager) collectDeviceInfo() {
	// 1. 无锁阶段：执行所有 AT 命令
	var imei, firmware, iccid, imsi, operator, apn, networkMode, networkDuplex string
	var simInserted bool
	var regStatus, imsStatus int
	var regStatusText, lac, cellID string
	var signalDBM, signalRSRQ, signalRSRP int = -999, 0, 0
	var usbnetMode int = -1

	if v, err := m.QueryIMEI(); err == nil {
		imei = v
	}
	if v, err := m.QueryFirmware(); err == nil {
		firmware = v
	}
	if v, err := m.QuerySIMInserted(); err == nil {
		simInserted = v
	}
	if simInserted {
		if v, err := m.QueryIMSI(); err == nil {
			imsi = v
		}
	}
	if v, err := m.QueryICCID(); err == nil {
		iccid = v
	}
	if v, err := m.QueryOperator(); err == nil {
		operator = v
	}
	if st, text, lacV, cellV, err := m.QueryRegistration(); err == nil {
		regStatus = st
		regStatusText = text
		lac = lacV
		cellID = cellV
	}
	if _, dbm, err := m.QueryCSQ(); err == nil && dbm != -999 {
		signalDBM = dbm
	}
	if rsrp, rsrq, err := m.QueryServingCellLTE(); err == nil {
		signalRSRP = rsrp
		signalRSRQ = rsrq
	}
	if v, err := m.QueryAPN(); err == nil {
		apn = v
	}
	if v, err := m.QueryIMSStatus(); err == nil {
		imsStatus = v
	}
	if mode, duplex, err := m.QueryNetworkModeAndDuplex(); err == nil {
		networkMode = mode
		networkDuplex = duplex
	}
	if v, err := m.QueryUSBNetMode(); err == nil {
		usbnetMode = v
	}

	// 2. 有锁阶段：统一更新状态
	m.infoMu.Lock()
	defer m.infoMu.Unlock()

	if imei != "" {
		m.imei = imei
	}
	if firmware != "" {
		m.firmware = firmware
	}
	if iccid != "" {
		m.iccid = iccid
	}
	if imsi != "" {
		m.imsi = imsi
	}
	if operator != "" {
		m.operator = operator
	}
	m.simInserted = simInserted
	if signalDBM != -999 {
		m.signalDBM = signalDBM
	}
	if signalRSRP != 0 {
		m.signalRSRP = signalRSRP
	}
	if signalRSRQ != 0 {
		m.signalRSRQ = signalRSRQ
	}
	m.regStatus = regStatus
	m.regStatusText = regStatusText
	m.lac = lac
	m.cellID = cellID
	m.apn = apn
	m.imsStatus = imsStatus
	m.networkMode = networkMode
	m.networkDuplex = networkDuplex
	m.usbnetMode = usbnetMode
}

// getRegStatusText 返回网络注册状态文本
func (m *Manager) getRegStatusText(status int) string {
	switch status {
	case 0:
		return "未注册"
	case 1:
		return "已注册(本地)"
	case 2:
		return "搜索中"
	case 3:
		return "注册被拒"
	case 4:
		return "未知"
	case 5:
		return "已注册(漫游)"
	default:
		return "未知"
	}
}

// GetIMEI 返回设备 IMEI
func (m *Manager) GetIMEI() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.imei
}

// GetICCID 返回当前 SIM 卡 ICCID
func (m *Manager) GetICCID() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.iccid
}

// GetIMSI 返回当前 SIM 卡 IMSI
func (m *Manager) GetIMSI() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.imsi
}

// GetOperator 返回运营商名称
func (m *Manager) GetOperator() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.operator
}

// GetSignalDBM 返回信号强度 (dBm)
func (m *Manager) GetSignalDBM() int {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.signalDBM
}

// GetFirmware 返回固件版本
func (m *Manager) GetFirmware() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.firmware
}

// IsSimInserted 返回是否插入 SIM 卡
func (m *Manager) IsSimInserted() bool {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.simInserted
}

// GetRegStatus 返回网络注册状态
func (m *Manager) GetRegStatus() (int, string) {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.regStatus, m.regStatusText
}

// GetCellInfo 返回小区信息 (LAC, CellID)
func (m *Manager) GetCellInfo() (string, string) {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.lac, m.cellID
}

// GetSignalLTE 返回 LTE 详细信号 (RSRP, RSRQ)
func (m *Manager) GetSignalLTE() (int, int) {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.signalRSRP, m.signalRSRQ
}

// GetAPN 返回当前 APN
func (m *Manager) GetAPN() string {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.apn
}

// GetIMSStatus 返回 IMS 注册状态
func (m *Manager) GetIMSStatus() int {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return m.imsStatus
}

type PNNRecord struct {
	Record    int    `json:"record"`
	FullName  string `json:"full_name,omitempty"`
	ShortName string `json:"short_name,omitempty"`
	RawHex    string `json:"raw_hex,omitempty"`
}

type OPLRecord struct {
	Record    int    `json:"record"`
	PLMN      string `json:"plmn,omitempty"`
	LACStart  uint16 `json:"lac_start,omitempty"`
	LACEnd    uint16 `json:"lac_end,omitempty"`
	PNNRecord int    `json:"pnn_record,omitempty"`
	RawHex    string `json:"raw_hex,omitempty"`
}

type SIMServiceTable struct {
	Kind            string `json:"kind,omitempty"`
	RawHex          string `json:"raw_hex,omitempty"`
	EnabledServices []int  `json:"enabled_services,omitempty"`
}

// GetFullStatus 返回完整状态信息
type DeviceStatus struct {
	IMEI            string           `json:"imei"`
	Firmware        string           `json:"firmware"`
	ICCID           string           `json:"iccid"`
	IMSI            string           `json:"imsi"`
	NativeSPN       string           `json:"native_spn,omitempty"`
	NativeMCC       string           `json:"native_mcc,omitempty"`
	NativeMNC       string           `json:"native_mnc,omitempty"`
	GID1            string           `json:"gid1,omitempty"`
	GID2            string           `json:"gid2,omitempty"`
	PNN             []PNNRecord      `json:"pnn,omitempty"`
	OPL             []OPLRecord      `json:"opl,omitempty"`
	SIMServiceTable *SIMServiceTable `json:"sim_service_table,omitempty"`
	Operator        string           `json:"operator"`
	SimInserted     bool             `json:"sim_inserted"`
	SignalDBM       int              `json:"signal_dbm"`
	SignalRSRP      int              `json:"signal_rsrp"`
	SignalRSRQ      int              `json:"signal_rsrq"`
	SignalSINR      int              `json:"signal_sinr,omitempty"`
	NR5GSignalSINR  int              `json:"nr5g_signal_sinr,omitempty"`
	RadioBand       string           `json:"radio_band,omitempty"`
	RadioChannel    uint32           `json:"radio_channel,omitempty"`
	RegStatus       int              `json:"reg_status"`
	RegStatusText   string           `json:"reg_status_text"`
	PSAttached      bool             `json:"ps_attached"`
	LAC             string           `json:"lac"`
	CellID          string           `json:"cell_id"`
	APN             string           `json:"apn"`
	IMSStatus       int              `json:"ims_status"`
	NetworkMode     string           `json:"network_mode"`
	NetworkDuplex   string           `json:"network_duplex"`
	USBNetMode      int              `json:"usbnet_mode"`
	OperatingMode   *int             `json:"operating_mode,omitempty"`
}

func (m *Manager) GetFullStatus() DeviceStatus {
	m.infoMu.RLock()
	defer m.infoMu.RUnlock()
	return DeviceStatus{
		IMEI:          m.imei,
		Firmware:      m.firmware,
		ICCID:         m.iccid,
		IMSI:          m.imsi,
		Operator:      m.operator,
		SimInserted:   m.simInserted,
		SignalDBM:     m.signalDBM,
		SignalRSRP:    m.signalRSRP,
		SignalRSRQ:    m.signalRSRQ,
		RegStatus:     m.regStatus,
		RegStatusText: m.regStatusText,
		LAC:           m.lac,
		CellID:        m.cellID,
		APN:           m.apn,
		IMSStatus:     m.imsStatus,
		NetworkMode:   m.networkMode,
		NetworkDuplex: m.networkDuplex,
		USBNetMode:    m.usbnetMode,
		OperatingMode: nil,
	}
}

// RefreshStatus 刷新设备状态 (信号、运营商、SIM)，并在发现 SIM 卡掉线时触发告警
func (m *Manager) RefreshStatus(onAlert func(msg string), onRecover func(msg string)) {
	// 1. 在锁外执行耗时的 AT 命令
	var operator, networkMode, networkDuplex string
	var signalDBM, signalRSRP, signalRSRQ int = -999, 0, 0

	// 检查 SIM 卡是否存活
	simInserted, simErr := m.QuerySIMInserted()

	if v, err := m.QueryOperator(); err == nil {
		operator = v
	}
	if _, dbm, err := m.QueryCSQ(); err == nil && dbm != -999 {
		signalDBM = dbm
	}
	if mode, duplex, err := m.QueryNetworkModeFallbackAndDuplex(); err == nil {
		networkMode = mode
		networkDuplex = duplex
	}

	// 2. 获取锁并更新状态
	m.infoMu.Lock()
	defer m.infoMu.Unlock()

	if operator != "" {
		m.operator = operator
	}
	if signalDBM != -999 {
		m.signalDBM = signalDBM
	}
	if networkMode != "" {
		m.networkMode = networkMode
		m.networkDuplex = networkDuplex
	}
	if signalRSRP != 0 {
		m.signalRSRP = signalRSRP
	}
	if signalRSRQ != 0 {
		m.signalRSRQ = signalRSRQ
	}

	// 处理 SIM 卡告警逻辑 (连续 3 次探测失败)
	if simErr != nil || !simInserted {
		m.simFailCount++
		if m.simFailCount >= 3 && !m.simAlerting {
			m.simAlerting = true
			errDetail := "SIM 卡未插入或状态异常"
			if simErr != nil {
				errDetail = fmt.Sprintf("AT+CPIN 查询失败: %v", simErr)
			}
			logger.Warn(fmt.Sprintf("[%s] 定时巡检发现 SIM 卡掉线", m.cfg.ID), "err", errDetail)
			if onAlert != nil {
				// 异步发送告警避免阻塞锁内时间
				go onAlert(fmt.Sprintf("⚠️ 设备 %s SIM 卡掉线: %s", m.cfg.ID, errDetail))
			}
		}
	} else {
		if m.simAlerting {
			logger.Info(fmt.Sprintf("[%s] 定时巡检发现 SIM 卡已恢复", m.cfg.ID))
			if onRecover != nil {
				go onRecover(fmt.Sprintf("✅ 设备 %s SIM 卡已恢复正常", m.cfg.ID))
			}
		}
		m.simFailCount = 0
		m.simAlerting = false
	}
}

// isURC 判断是否为 URC
func (m *Manager) isURC(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return false
	}
	// 排除确认为同步命令的异步回显，避免被 URC 处理函数拦截并报“未分类”
	if strings.HasPrefix(s, "+CSIM:") || strings.HasPrefix(s, "+CGLA:") || strings.HasPrefix(s, "+CCHO:") || strings.HasPrefix(s, "+CMGR:") || strings.HasPrefix(s, "+CMGS:") || strings.HasPrefix(s, "+QENG:") {
		return false
	}
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "^") || strings.HasPrefix(s, "$") {
		return true
	}
	switch s {
	case "RING", "RDY", "SMS Ready", "Call Ready", "NORMAL POWER DOWN", "NO CARRIER", "BUSY", "NO ANSWER":
		return true
	default:
		return false
	}
}

// SubscribeRDY 订阅一次性 RDY 事件。
// 调用方应在发出会触发模组重启的操作 *之前* 先调用本方法，然后等待返回的 channel。
// 模组重启并发出 RDY URC 后，channel 会被关闭（可通过 `<-ch` 或 `select` 接收）。
func (m *Manager) SubscribeRDY() <-chan struct{} {
	ch := make(chan struct{})
	m.rdyMu.Lock()
	m.rdySubs = append(m.rdySubs, ch)
	m.rdyMu.Unlock()
	return ch
}

// dispatchRDY 内部调用：广播 RDY 事件并清空订阅列表
func (m *Manager) dispatchRDY() {
	m.rdyMu.Lock()
	subs := m.rdySubs
	m.rdySubs = nil
	m.rdyMu.Unlock()
	for _, ch := range subs {
		close(ch)
	}
}

func (m *Manager) dispatchSIMStatusURC(inserted *bool, state string) {
	m.infoMu.RLock()
	handler := m.simStatusHandler
	m.infoMu.RUnlock()
	if handler != nil {
		go handler(inserted, state)
	}
}

func (m *Manager) observeRegistration(domain string, stat int) bool {
	domain = strings.ToUpper(strings.TrimSpace(domain))
	if domain == "" || stat < 0 {
		return false
	}
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	if m.registrationStates == nil {
		m.registrationStates = make(map[string]int)
	}
	if previous, known := m.registrationStates[domain]; known && previous == stat {
		return false
	}
	m.registrationStates[domain] = stat
	m.regStatus = stat
	m.regStatusText = m.getRegStatusText(stat)
	return true
}

func (m *Manager) observeSIMInserted(inserted bool) bool {
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	if m.simInsertedKnown && m.simInserted == inserted {
		return false
	}
	m.simInserted = inserted
	m.simInsertedKnown = true
	return true
}

func (m *Manager) observeSIMState(state string) bool {
	state = strings.ToUpper(strings.Trim(strings.TrimSpace(state), "\""))
	if state == "" {
		return false
	}
	m.infoMu.Lock()
	defer m.infoMu.Unlock()
	if m.simStateKnown && m.simState == state {
		return false
	}
	m.simState = state
	m.simStateKnown = true
	return true
}

func urcIntField(fields []any, name string) (int, bool) {
	for i := 0; i+1 < len(fields); i += 2 {
		if key, _ := fields[i].(string); key == name {
			value, ok := fields[i+1].(int)
			return value, ok
		}
	}
	return 0, false
}

func urcStringField(fields []any, name string) (string, bool) {
	for i := 0; i+1 < len(fields); i += 2 {
		if key, _ := fields[i].(string); key == name {
			value, ok := fields[i+1].(string)
			return value, ok
		}
	}
	return "", false
}

func (m *Manager) acceptStateURC(fr urcFormatResult) bool {
	switch fr.Key {
	case "+CREG", "+CGREG", "+CEREG":
		stat, ok := urcIntField(fr.Fields, "stat")
		return ok && stat >= 0 && m.observeRegistration(fr.Key, stat)
	case "+CPIN":
		state, ok := urcStringField(fr.Fields, "state")
		return ok && m.observeSIMState(state)
	case "+QSIMSTAT":
		inserted, ok := urcIntField(fr.Fields, "inserted")
		return ok && inserted >= 0 && m.observeSIMInserted(inserted == 1)
	default:
		return true
	}
}

// handleURC 处理 URC
func (m *Manager) handleURC(line string) {
	s := strings.TrimSpace(line)
	if s == "" {
		return
	}

	fr := m.formatURC(s)
	if !m.acceptStateURC(fr) {
		return
	}
	msg := fmt.Sprintf("[%s] %s", m.cfg.ID, fr.Msg)
	logFields := filterSensitiveLogFields(fr.Fields)
	switch fr.Level {
	case urcLogWarn:
		logger.Warn(msg, logFields...)
	case urcLogInfo:
		logger.Info(msg, logFields...)
	default:
		logger.Debug(msg, logFields...)
	}

	// 模组重启信号：广播给所有 SubscribeRDY() 的等待方。
	// 部分 EC20 固件重启后不发 RDY，而是直接发 +CPIN: READY，两者都作为就绪信号处理。
	if fr.Key == "RDY" {
		m.dispatchRDY()
	}
	if fr.Key == "+CPIN" {
		state := ""
		for i := 0; i+1 < len(fr.Fields); i += 2 {
			if k, _ := fr.Fields[i].(string); k == "state" {
				state, _ = fr.Fields[i+1].(string)
				if state == "READY" {
					m.dispatchRDY()
				}
				break
			}
		}
		m.dispatchSIMStatusURC(nil, state)
	}

	if fr.Key == "+QSIMSTAT" {
		var inserted *bool
		for i := 0; i+1 < len(fr.Fields); i += 2 {
			if k, _ := fr.Fields[i].(string); k == "inserted" {
				if v, ok := fr.Fields[i+1].(int); ok && v >= 0 {
					b := v == 1
					inserted = &b
				}
				break
			}
		}
		m.dispatchSIMStatusURC(inserted, "")
	}

	// 分发 +CUSD USSD 响应到等待通道
	if fr.Key == "+CUSD" {
		var n, dcs int
		var text string
		for i := 0; i < len(fr.Fields)-1; i += 2 {
			key, _ := fr.Fields[i].(string)
			switch key {
			case "n":
				n, _ = fr.Fields[i+1].(int)
			case "dcs":
				dcs, _ = fr.Fields[i+1].(int)
			case "text":
				text, _ = fr.Fields[i+1].(string)
			}
		}
		result := USSDResult{Status: n, RawText: text, DCS: dcs}
		result.Text = m.decodeUSSDText(text, dcs)
		select {
		case m.ussdChan <- result:
		default:
			// 没有人在等待，丢弃
			if LogMessageContent {
				logger.Debug(fmt.Sprintf("[%s] USSD 响应无人等待，已丢弃", m.cfg.ID), "text", result.Text)
			} else {
				logger.Debug(fmt.Sprintf("[%s] USSD 响应无人等待，已丢弃", m.cfg.ID))
			}
		}
	}

	if fr.Key == "+CMTI" && fr.CMTIIndex != "" {
		m.infoMu.RLock()
		handler := m.newSMSHandler
		m.infoMu.RUnlock()
		if handler != nil {
			// 消费者已注册：仅上抛存储引用，读取/持久化/确认删除全部由消费者负责。
			go handler(fr.CMTIStorage, fr.CMTIIndex)
			return
		}
		// 无消费者：不自动读取也不删除，条目保留在模组存储中。
		logger.Debug(fmt.Sprintf("[%s] 收到 +CMTI 但无消费者注册，保留短信条目", m.cfg.ID),
			"index", fr.CMTIIndex, "storage", fr.CMTIStorage)
	}

	// 分发 RING 来电事件
	if fr.Key == "RING" {
		m.infoMu.RLock()
		cb := m.ringCallback
		m.infoMu.RUnlock()
		if cb != nil {
			go cb()
		}
	}

	// 分发对方挂断事件 NO CARRIER
	if fr.Key == "NO CARRIER" {
		m.infoMu.RLock()
		cb := m.hangupCallback
		m.infoMu.RUnlock()
		if cb != nil {
			go cb()
		}
	}

	// 分发对方接听外呼事件 (CONNECT)
	if fr.Key == "CONNECT" || fr.Key == "MO CONNECTED" {
		m.infoMu.RLock()
		cb := m.connectCallback
		m.infoMu.RUnlock()
		if cb != nil {
			go cb()
		}
	}

	// 分发 +CLIP 来电号码
	if fr.Key == "+CLIP" {
		for i := 0; i+1 < len(fr.Fields); i += 2 {
			if k, _ := fr.Fields[i].(string); k == "number" {
				if number, _ := fr.Fields[i+1].(string); number != "" {
					m.infoMu.RLock()
					cb := m.clipCallback
					m.infoMu.RUnlock()
					if cb != nil {
						go cb(number)
					}
				}
				break
			}
		}
	}

	// 分发 +QPCMV 流控事件 (0=模块忙, 1=就绪)
	if fr.Key == "+QPCMV" {
		rest := parseURCAfterColon(s)
		if v, ok := parseInt(strings.TrimSpace(rest)); ok {
			if m.qpcmvChan != nil {
				select {
				case m.qpcmvChan <- v:
				default:
				}
			}
		}
	}
}

// readAndProcessSMS 读取并处理短信
func (m *Manager) readAndProcessSMS(index string) {
	// 公开给外部调用的封装 (如果需要)
	m.ReadAndProcessSMS(index)
}

// ReadAndProcessSMS 公开方法：读取并处理短信
func (m *Manager) ReadAndProcessSMS(index string) {
	m.readAndProcessSMSFromStorage("", index)
}

// ReadAndProcessSMSFromStorage 读取指定 AT 短信存储中的短信。
func (m *Manager) ReadAndProcessSMSFromStorage(storage, index string) {
	m.readAndProcessSMSFromStorage(storage, index)
}

func (m *Manager) readAndProcessSMSFromStorage(storage, index string) {
	index, ok := normalizeSMSIndex(index)
	if !ok {
		logger.Warn(fmt.Sprintf("[%s] 短信索引非法，跳过读取", m.cfg.ID), "index", index)
		return
	}
	idx, err := strconv.ParseUint(index, 10, 32)
	if err != nil {
		logger.Warn(fmt.Sprintf("[%s] 短信索引非法，跳过读取", m.cfg.ID), "index", index)
		return
	}
	normalizedStorage, _ := normalizeSMSStorage(storage)

	logger.Info(fmt.Sprintf("[%s] 读取短信 (AT)", m.cfg.ID), "index", index, "storage", normalizedStorage)

	pduHex, err := m.ReadSMSFromStorage(normalizedStorage, uint32(idx))
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] 读取短信失败", m.cfg.ID), "index", index, "storage", normalizedStorage, "err", err)
		return
	}

	sender, content, timestamp := m.decodePDU(pduHex)

	// 分片未完成时不做回调，也不删除条目，等待后续分片或刷新重试。
	if content == "" {
		logger.Debug(fmt.Sprintf("[%s] 短信分片未完成，保留条目", m.cfg.ID), "index", index, "storage", normalizedStorage)
		return
	}

	if LogMessageContent {
		logger.Debug(fmt.Sprintf("[%s] 短信内容", m.cfg.ID), "sender", sender, "content", content)
	}

	if m.smsCallback != nil {
		m.smsCallback(sender, content, timestamp)
	}

	if err := m.DeleteSMSFromStorage(normalizedStorage, uint32(idx)); err != nil {
		logger.Warn(fmt.Sprintf("[%s] 删除已读短信失败", m.cfg.ID), "index", index, "err", err)
	}
}

// ReadSMSFromStorage 从指定 AT 存储读取索引对应的短信 PDU（十六进制）。
// 整个切换存储/读取/恢复序列由 smsReadMu 串行化，并发的 +CMTI 通知不会交错。
func (m *Manager) ReadSMSFromStorage(storage string, index uint32) (string, error) {
	m.smsReadMu.Lock()
	defer m.smsReadMu.Unlock()
	restore, ok := m.switchSMSStorageForRead(storage)
	if !ok {
		return "", fmt.Errorf("切换短信存储失败: %s", storage)
	}
	if restore != nil {
		defer restore()
	}
	resp, err := m.ExecuteAT(fmt.Sprintf("AT+CMGR=%d", index), 5*time.Second)
	if err != nil {
		return "", err
	}
	pduHex, _ := extractSMSPDUAfterPrefix(resp, "+CMGR:")
	if pduHex == "" || pduHex == "OK" {
		return "", fmt.Errorf("短信 %d 不存在或为空", index)
	}
	return pduHex, nil
}

// DeleteSMSFromStorage 从指定 AT 存储删除索引对应的短信条目，与读取共享
// smsReadMu 串行化。
func (m *Manager) DeleteSMSFromStorage(storage string, index uint32) error {
	m.smsReadMu.Lock()
	defer m.smsReadMu.Unlock()
	normalized, hasStorage := normalizeSMSStorage(storage)
	if !hasStorage {
		_, err := m.ExecuteAT(fmt.Sprintf("AT+CMGD=%d", index), 3*time.Second)
		return err
	}
	restore, ok := m.switchSMSStorageForRead(normalized)
	if !ok {
		return fmt.Errorf("切换短信存储失败: %s", normalized)
	}
	if restore != nil {
		defer restore()
	}
	_, err := m.ExecuteAT(fmt.Sprintf("AT+CMGD=%d", index), 3*time.Second)
	return err
}

func normalizeSMSIndex(index string) (string, bool) {
	index = strings.TrimSpace(index)
	if index == "" {
		return "", false
	}
	for _, ch := range index {
		if ch < '0' || ch > '9' {
			return "", false
		}
	}
	return index, true
}

func normalizeSMSStorage(storage string) (string, bool) {
	storage = strings.ToUpper(strings.Trim(strings.TrimSpace(storage), `"`))
	if storage == "" || len(storage) > 8 {
		return "", false
	}
	for _, ch := range storage {
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			continue
		}
		return "", false
	}
	return storage, true
}

func parseCPMSStorages(resp string) []string {
	for _, line := range strings.Split(resp, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+CPMS:") {
			continue
		}
		fields := parseCommaFields(parseURCAfterColon(line))
		storages := make([]string, 0, 3)
		for i := 0; i < len(fields); i += 3 {
			if storage, ok := normalizeSMSStorage(fields[i]); ok {
				storages = append(storages, storage)
			}
		}
		return storages
	}
	return nil
}

func cpmsSetCommand(storages ...string) string {
	normalized := make([]string, 0, len(storages))
	for _, storage := range storages {
		if s, ok := normalizeSMSStorage(storage); ok {
			normalized = append(normalized, s)
		}
	}
	if len(normalized) == 0 {
		return ""
	}
	if len(normalized) == 1 {
		normalized = []string{normalized[0], normalized[0], normalized[0]}
	}
	if len(normalized) > 3 {
		normalized = normalized[:3]
	}

	quoted := make([]string, 0, len(normalized))
	for _, storage := range normalized {
		quoted = append(quoted, fmt.Sprintf("%q", storage))
	}
	return "AT+CPMS=" + strings.Join(quoted, ",")
}

func (m *Manager) switchSMSStorageForRead(storage string) (func(), bool) {
	targetCmd := cpmsSetCommand(storage)
	if targetCmd == "" {
		return nil, true
	}

	var previous []string
	if resp, err := m.ExecuteAT("AT+CPMS?", 3*time.Second); err == nil {
		previous = parseCPMSStorages(resp)
		if len(previous) > 0 && strings.EqualFold(previous[0], storage) {
			return nil, true
		}
	} else {
		logger.Warn(fmt.Sprintf("[%s] 查询短信存储失败，将直接尝试切换", m.cfg.ID), "storage", storage, "err", err)
	}

	if _, err := m.ExecuteAT(targetCmd, 5*time.Second); err != nil {
		logger.Warn(fmt.Sprintf("[%s] 切换短信存储失败", m.cfg.ID), "storage", storage, "err", err)
		return nil, false
	}

	restoreCmd := cpmsSetCommand(previous...)
	if restoreCmd == "" || restoreCmd == targetCmd {
		return nil, true
	}
	return func() {
		if _, err := m.ExecuteAT(restoreCmd, 5*time.Second); err != nil {
			logger.Warn(fmt.Sprintf("[%s] 恢复短信存储失败", m.cfg.ID), "storage", previous, "err", err)
		}
	}, true
}

// Reboot 重启模组 (AT+CFUN=1,1)
func (m *Manager) Reboot() error {
	logger.Warn(fmt.Sprintf("[%s] 正在重启模组...", m.cfg.ID))
	_, err := m.ExecuteAT("AT+CFUN=1,1", 5*time.Second)
	return err
}

// cleanupOldFragments 清理过期的短信分片
func (m *Manager) cleanupOldFragments() {
	m.reassembler.Cleanup(10 * time.Minute)
}

// decodePDU 解码 PDU
func (m *Manager) decodePDU(raw string) (sender, content string, timestamp time.Time) {
	timestamp = time.Now()

	sender, content, msgTime, concat, err := smscodec.DecodeDeliverPDUHex(raw)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] PDU 解析失败", m.cfg.ID), "err", err)
		content = fmt.Sprintf("[PDU 解析失败] %s", raw)
		return
	}
	if !msgTime.IsZero() {
		timestamp = msgTime
	}

	if concat.IsConcat {
		logger.Debug(fmt.Sprintf("[%s] 收到短信分片", m.cfg.ID), "ref", concat.Ref, "seq", concat.Seq, "total", concat.Total)
		complete, full := m.reassembler.Add(sender, concat, content)
		if !complete {
			return "", "", time.Time{}
		}
		content = full
		logger.Info(fmt.Sprintf("[%s] 长短信重组完成", m.cfg.ID), "total", concat.Total)
		return sender, content, timestamp
	}

	return sender, content, timestamp
}

// ExecuteAT 执行 AT 命令 (普通优先级)
func (m *Manager) ExecuteAT(cmd string, timeout time.Duration) (string, error) {
	return m.executeAT(cmd, timeout, false, false, false)
}

// ExecuteATSilent 静默执行 AT 命令 (普通优先级)
func (m *Manager) ExecuteATSilent(cmd string, timeout time.Duration) (string, error) {
	return m.executeAT(cmd, timeout, true, false, false)
}

// ExecuteATHigh 执行 AT 命令 (高优先级)
func (m *Manager) ExecuteATHigh(cmd string, timeout time.Duration) (string, error) {
	return m.executeAT(cmd, timeout, false, true, false)
}

// ExecuteATRaw preserves the terminal response line for raw command output.
// Regular protocol queries continue to receive payload-only responses so
// their existing parsers do not change behavior.
func (m *Manager) ExecuteATRaw(cmd string, timeout time.Duration) (string, error) {
	return m.executeAT(cmd, timeout, false, false, true)
}

// executeAT 内部通用的 AT 命令执行逻辑
func (m *Manager) executeAT(cmd string, timeout time.Duration, silent, highPriority, includeTerminal bool) (string, error) {
	if !m.HasATPort() {
		return "", errors.New("当前设备没有可用 AT 端口")
	}
	if !m.CanExecuteAT() {
		return "", errors.New("AT 管理器未启动或不可用")
	}
	if !m.healthy {
		return "", errors.New("设备异常")
	}

	// 从池中获取请求对象
	req := m.reqPool.Get().(*commandRequest)
	// 重置字段
	req.cmd = cmd
	req.enqueuedAt = time.Now()
	req.timeout = timeout
	req.silent = silent
	req.highPriority = highPriority
	req.includeTerminal = includeTerminal
	req.interactive = false
	req.waitPrompt = false
	req.followUp = ""

	// 确保回收资源
	defer func() {
		// 清空通道以防万一
		select {
		case <-req.respChan:
		default:
		}
		select {
		case <-req.errChan:
		default:
		}
		m.reqPool.Put(req)
	}()

	// 根据优先级选择通道
	targetChan := m.cmdChan
	if highPriority {
		targetChan = m.cmdChanHigh
	}

	select {
	case targetChan <- *req: // 注意：这里发送的是值拷贝，但这不影响 respChan/errChan 的引用
		select {
		case resp := <-req.respChan:
			return resp, nil
		case err := <-req.errChan:
			return "", err
		case <-m.stop:
			select {
			case err := <-req.errChan:
				return "", err
			case resp := <-req.respChan:
				return resp, nil
			default:
			}
			return "", errors.New("manager stopped")
		}
	case <-time.After(5 * time.Second): // 通道写入超时 (队列满)
		m.logATCommandDiagnostic(atCommandDiagnostic{
			CommandClass: domainat.CommandClass(cmd), QueueWaitMS: time.Since(req.enqueuedAt).Milliseconds(),
			TerminalResult: "queue_timeout", TimeoutClass: "queue",
		})
		return "", errors.New("command queue full")
	case <-m.stop:
		return "", errors.New("manager stopped")
	}
}

// SetBusy 设置忙碌状态
func (m *Manager) SetBusy(busy bool) {
	m.busyMu.Lock()
	m.busy = busy
	m.busyMu.Unlock()
}

// IsBusy 查询忙碌状态
func (m *Manager) IsBusy() bool {
	m.busyMu.Lock()
	defer m.busyMu.Unlock()
	return m.busy
}

// IsHealthy 返回健康状态
func (m *Manager) IsHealthy() bool {
	return m.healthy && m.isRunning()
}

// HasATPort reports whether the manager has a configured or injected AT
// transport. The name is retained for existing backend callers.
func (m *Manager) HasATPort() bool {
	return m.port != nil || strings.TrimSpace(m.atPort) != ""
}

// ATPort 返回配置中的 AT 端口路径。纯 QMI 模式会保留该值供人工 AT 终端使用。
func (m *Manager) ATPort() string {
	return strings.TrimSpace(m.atPort)
}

// CanExecuteAT 返回当前管理器是否已启动，可接受 AT 命令。
func (m *Manager) CanExecuteAT() bool {
	return !m.pureQMIBackend() && m.HasATPort() && m.isRunning()
}

func (m *Manager) setRunning(value bool) {
	m.runningMu.Lock()
	m.running = value
	m.runningMu.Unlock()
}

func (m *Manager) isRunning() bool {
	m.runningMu.RLock()
	defer m.runningMu.RUnlock()
	return m.running
}

func (m *Manager) SetAPDUArbiter(arbiter *apduarbiter.Arbiter) {
	m.apduLeaseMu.Lock()
	defer m.apduLeaseMu.Unlock()
	if m.apduSessions == nil {
		m.apduSessions = make(map[int]apduSessionInfo)
	}
	if m.apduArbiter == arbiter {
		return
	}
	clear(m.apduSessions)
	m.apduArbiter = arbiter
	if arbiter == nil {
		return
	}
	// 本管理器是 AT 传输的所有者：接收隔离上报，并在（重）注册时确认恢复，
	// 因为重新注册意味着新传输已就绪。
	arbiter.SetTransportOwner(m)
	arbiter.ConfirmTransportRecovery()
}

// OnTransportQuarantine 由 APDU 仲裁器在传输恢复期限到期时调用：关闭 AT 传输，
// 运行时重连即完成关闭/重新初始化序列。仲裁器从不直接关闭它不拥有的传输。
func (m *Manager) OnTransportQuarantine(reason string) {
	logger.Warn(fmt.Sprintf("[%s] APDU 传输被仲裁器隔离，触发控制面恢复", m.cfg.ID), "reason", reason, "port", m.atPort)
	m.healthy = false
	m.Stop()
	m.notifyDisconnect("apdu_quarantine")
}

func (m *Manager) acquireAPDUTransportLease(timeout time.Duration, owner string, class apduarbiter.APDUClass, channel int) (*apduarbiter.Lease, error) {
	m.apduLeaseMu.Lock()
	arbiter := m.apduArbiter
	m.apduLeaseMu.Unlock()
	if arbiter == nil {
		return nil, nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return arbiter.AcquireTransport(ctx, apduarbiter.Request{
		Owner:   owner,
		Mode:    "AT",
		Class:   class,
		Channel: channel,
	})
}

func (m *Manager) bindAPDUSession(channel int, owner string, class ...apduarbiter.APDUClass) {
	m.apduLeaseMu.Lock()
	defer m.apduLeaseMu.Unlock()
	if m.apduSessions == nil {
		m.apduSessions = make(map[int]apduSessionInfo)
	}
	sessionClass := apduarbiter.APDUClassEUICCWrite
	if len(class) > 0 && class[0] != "" {
		sessionClass = class[0]
	}
	m.apduSessions[channel] = apduSessionInfo{
		Channel:  channel,
		Owner:    strings.TrimSpace(owner),
		Class:    sessionClass,
		OpenedAt: time.Now(),
	}
}

func (m *Manager) getAPDUSession(channel int) (apduSessionInfo, bool) {
	m.apduLeaseMu.Lock()
	defer m.apduLeaseMu.Unlock()
	session, ok := m.apduSessions[channel]
	return session, ok
}

func (m *Manager) hasAPDUSession(channel int) bool {
	m.apduLeaseMu.Lock()
	defer m.apduLeaseMu.Unlock()
	_, ok := m.apduSessions[channel]
	return ok
}

func (m *Manager) takeAPDUSession(channel int) (apduSessionInfo, bool) {
	m.apduLeaseMu.Lock()
	defer m.apduLeaseMu.Unlock()
	session, ok := m.apduSessions[channel]
	delete(m.apduSessions, channel)
	return session, ok
}

func (m *Manager) releaseAllAPDULeases(reason string) {
	m.apduLeaseMu.Lock()
	count := len(m.apduSessions)
	clear(m.apduSessions)
	m.apduLeaseMu.Unlock()

	if count > 0 {
		logger.Warn(fmt.Sprintf("[%s] APDU logical session registry 已清理", m.cfg.ID), "reason", reason, "session_count", count)
	}
}

// Rotate 执行 IP 切换
func (m *Manager) Rotate() error {
	m.SetBusy(true)
	defer m.SetBusy(false)

	logger.Info(fmt.Sprintf("[%s] 开始 IP 切换", m.cfg.ID))

	if err := m.SetAttach(false); err != nil {
		return fmt.Errorf("PS 域脱附失败: %w", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := m.SetAttach(true); err != nil {
		return fmt.Errorf("PS 域附着失败: %w", err)
	}

	logger.Info(fmt.Sprintf("[%s] IP 切换完成", m.cfg.ID))
	return nil
}

// CheckSignal 检查信号强度
func (m *Manager) CheckSignal() (int, error) {
	resp, err := m.ExecuteAT("AT+CSQ", 3*time.Second)
	if err != nil {
		return 0, err
	}

	rssi, _, ok := parseCSQ(resp)
	if ok {
		return rssi, nil
	}

	return 0, errors.New("无法解析信号强度")
}

// Close 关闭管理器
func (m *Manager) Close() error {
	m.Stop()
	return nil
}

// CheckAllSMS 检查所有短信（轮询模式）
func (m *Manager) CheckAllSMS() {
	if m.IsBusy() {
		return
	}

	pdus, err := m.SMSListAllPDU()
	if err != nil {
		logger.Warn(fmt.Sprintf("[%s] 检查短信失败", m.cfg.ID), "err", err)
		return
	}

	if len(pdus) == 0 {
		return
	}

	for _, entry := range pdus {
		sender, content, timestamp := m.decodePDU(entry.PDU)
		if m.smsCallback != nil && content != "" {
			m.smsCallback(sender, content, timestamp)
		}
	}

	// 删除所有短信
	_ = m.SMSDeleteAll()
}

// DeleteSMS 删除指定索引的短信
func (m *Manager) DeleteSMS(index uint32) error {
	_, err := m.ExecuteAT(fmt.Sprintf("AT+CMGD=%d", index), 3*time.Second)
	return err
}

// SendSMS 使用 PDU 模式发送短信
func (m *Manager) SendSMS(phone, message string) error {
	return m.SendSMSWithOptions(phone, message, smscodec.SubmitOptions{})
}

// SendSMSWithOptions 使用 PDU 模式发送短信，并允许调用方指定文本编码策略。
func (m *Manager) SendSMSWithOptions(phone, message string, opts smscodec.SubmitOptions) error {
	m.SetBusy(true)
	defer m.SetBusy(false)

	logger.Info(fmt.Sprintf("[%s] 准备发送短信 (PDU)", m.cfg.ID), "to", phone)

	// 确保处于 PDU 模式
	if _, err := m.ExecuteATHigh("AT+CMGF=0", 3*time.Second); err != nil {
		return fmt.Errorf("设置 PDU 模式失败: %w", err)
	}

	// 构建 PDUs
	pduHexList, tpduLenList, err := m.buildSMSPDUsWithOptions(phone, message, opts)
	if err != nil {
		return fmt.Errorf("构建 PDU 失败: %w", err)
	}

	for i, pduHex := range pduHexList {
		tpduLen := tpduLenList[i]
		if LogMessageContent {
			logger.Debug(fmt.Sprintf("[%s] PDU 编码完成 (分片 %d/%d)", m.cfg.ID, i+1, len(pduHexList)), "pdu", pduHex, "tpdu_len", tpduLen)
		} else {
			logger.Debug(fmt.Sprintf("[%s] PDU 编码完成 (分片 %d/%d)", m.cfg.ID, i+1, len(pduHexList)), "tpdu_len", tpduLen)
		}

		req := commandRequest{
			cmd:          fmt.Sprintf("AT+CMGS=%d", tpduLen), // PDU 长度 (不含 SMSC)
			enqueuedAt:   time.Now(),
			respChan:     make(chan string, 1),
			errChan:      make(chan error, 1),
			timeout:      20 * time.Second, // 增加超时时间，因为包含两步
			highPriority: true,
			interactive:  true,
			waitPrompt:   true,
			followUp:     pduHex + "\x1A", // PDU + Ctrl+Z
		}

		// 使用高优先级通道原子执行
		select {
		case m.cmdChanHigh <- req:
		case <-time.After(5 * time.Second):
			return errors.New("command queue full")
		}

		// 等待最终响应 (OK)
		select {
		case resp := <-req.respChan:
			if !strings.Contains(resp, "OK") && !strings.Contains(resp, "+CMGS:") {
				return fmt.Errorf("发送分片 %d 失败: %s", i+1, resp)
			}
		case err := <-req.errChan:
			return fmt.Errorf("发送分片 %d 失败: %w", i+1, err)
		case <-time.After(20 * time.Second):
			return errors.New("发送超时")
		}

		// 稍微等待下一段发信，防止模组队列溢出
		if i < len(pduHexList)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	logger.Info(fmt.Sprintf("[%s] 短信已发送", m.cfg.ID))
	return nil
}

// buildSMSPDUs 构建多段 SMS-SUBMIT PDU
// 返回: PDU 十六进制字符串列表, TPDU 长度列表 (不含 SMSC), 错误
func (m *Manager) buildSMSPDUs(phone, message string) ([]string, []int, error) {
	return m.buildSMSPDUsWithOptions(phone, message, smscodec.SubmitOptions{})
}

func (m *Manager) buildSMSPDUsWithOptions(phone, message string, opts smscodec.SubmitOptions) ([]string, []int, error) {
	tpduBytesList, tpduLenList, err := smscodec.BuildSubmitTPDUsWithOptions(phone, message, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("PDU 编码失败: %w", err)
	}

	// SMSC 使用默认 (长度字节为 00)
	smsc := []byte{0x00}
	var pduHexList []string

	for _, tpduBytes := range tpduBytesList {
		// 完整 PDU = SMSC + TPDU
		fullPDU := append(smsc, tpduBytes...)

		// 转换为十六进制
		pduHex := strings.ToUpper(hex.EncodeToString(fullPDU))
		pduHexList = append(pduHexList, pduHex)
	}

	return pduHexList, tpduLenList, nil
}

// USSDResult USSD 会话响应结果
type USSDResult struct {
	Status  int    `json:"status"`   // 0=无需操作, 1=需要用户回复, 2=会话结束, 5=网络超时
	Text    string `json:"text"`     // 解码后的可读文本
	RawText string `json:"raw_text"` // 原始文本（调试用）
	DCS     int    `json:"dcs"`      // 数据编码方案
}

// decodeUSSDText 根据 DCS (Data Coding Scheme) 解码 USSD 文本
// 参考 3GPP TS 23.038 的编码方案
func (m *Manager) decodeUSSDText(raw string, dcs int) string {
	if raw == "" {
		return ""
	}

	// 判断是否为 Hex 字符串（偶数长度、全 hex 字符）
	isHex := smscodec.IsHexString(raw)

	// DCS 高 4 位判断编码类型
	// 0x00-0x03 (0-3): GSM 7-bit
	// 0x04-0x07 (4-7): 8-bit data
	// 0x08-0x0B (8-11): UCS2
	// 0x0F (15): GSM 7-bit (unspecified)
	// 0x48 (72): UCS2
	codingGroup := (dcs >> 4) & 0x0F
	alphabet := (dcs >> 2) & 0x03

	isUCS2 := false
	if codingGroup == 0x00 || codingGroup == 0x01 {
		// 一般编码组
		isUCS2 = alphabet == 2 // bit3-2 = 10 -> UCS2
	} else if dcs == 72 {
		isUCS2 = true
	} else if dcs >= 0x40 && dcs <= 0x7F {
		// 消息类编码组
		isUCS2 = alphabet == 2
	}

	if isUCS2 && isHex {
		// UCS2: Hex 字符串 -> UTF-16BE -> UTF-8
		b, err := hex.DecodeString(raw)
		if err != nil {
			if LogMessageContent {
				logger.Debug(fmt.Sprintf("[%s] USSD UCS2 hex 解码失败", m.cfg.ID), "err", err, "raw", raw)
			} else {
				logger.Debug(fmt.Sprintf("[%s] USSD UCS2 hex 解码失败", m.cfg.ID), "err", err)
			}
			return raw
		}
		if len(b)%2 != 0 {
			return raw
		}
		u16 := make([]uint16, len(b)/2)
		for i := 0; i < len(b); i += 2 {
			u16[i/2] = uint16(b[i])<<8 | uint16(b[i+1])
		}
		return string(utf16.Decode(u16))
	}

	if isHex {
		// 可能是 GSM 7-bit packed 的 hex 表示
		b, err := hex.DecodeString(raw)
		if err != nil {
			return raw
		}
		unpacked := gsm7.Unpack7BitUSSD(b, 0)
		decoded, err := gsm7.Decode(unpacked)
		if err != nil {
			return raw
		}
		return string(decoded)
	}

	// 非 Hex 字符串，直接返回原文（某些 Modem 已经做了解码）
	return raw
}

// ExecuteUSSD 发送 USSD 指令并等待网络返回结果
// command: USSD 代码，如 "*100#", "*135#"
// timeout: 等待 URC 响应的超时时间
func (m *Manager) ExecuteUSSD(command string, timeout time.Duration) (*USSDResult, error) {
	// 清空可能残留的旧结果
	select {
	case <-m.ussdChan:
	default:
	}

	logger.Info(fmt.Sprintf("[%s] 开始执行 USSD: %s", m.cfg.ID, command), "timeout", timeout.String())

	// 设置字符集，避免部分模组因使用非 GSM 的短信格式导致发不出去 USSD
	m.ExecuteATSilent(`AT+CSCS="GSM"`, 2*time.Second)

	// 发送 AT+CUSD=1,"command",15
	cmd := fmt.Sprintf(`AT+CUSD=1,"%s",15`, command)
	_, err := m.ExecuteAT(cmd, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("发送 USSD 指令失败: %w", err)
	}

	logger.Debug(fmt.Sprintf("[%s] USSD 发送成功，等待网络回包 URC (+CUSD)...", m.cfg.ID))

	// 阻塞等待 +CUSD URC 回调
	select {
	case result := <-m.ussdChan:
		if LogMessageContent {
			logger.Info(fmt.Sprintf("[%s] 收到 USSD 返回", m.cfg.ID), "status", result.Status, "text", result.Text)
		} else {
			logger.Info(fmt.Sprintf("[%s] 收到 USSD 返回", m.cfg.ID), "status", result.Status)
		}
		return &result, nil
	case <-time.After(timeout):
		logger.Warn(fmt.Sprintf("[%s] USSD 响应网络超时（无回调），正在自动取消网络等待", m.cfg.ID), "timeout", timeout.String())
		// 超时后取消 USSD 会话
		m.CancelUSSD()
		return nil, errors.New("USSD 响应网络超时（无回调）")
	case <-m.stop:
		return nil, errors.New("设备已停止")
	}
}

// CancelUSSD 取消当前 USSD 会话
func (m *Manager) CancelUSSD() {
	_, err := m.ExecuteATSilent(`AT+CUSD=2`, 3*time.Second)
	if err != nil {
		logger.Debug(fmt.Sprintf("[%s] 取消 USSD 会话(AT+CUSD=2)失败", m.cfg.ID), "err", err)
	} else {
		logger.Debug(fmt.Sprintf("[%s] 已发送 USSD 取消指令 (AT+CUSD=2)", m.cfg.ID))
	}
}

// CheckAndEnableUAC 查询并确保开启 USB Audio Class (UAC) 接口
// 许多 Quectel 模块需要 AT+QCFG="USBCFG" 最后一位为 1 才能在系统枚举出声卡
// 返回 modified(bool) 表示是否发生了配置更改，如果发生了更改，必须重启才能生效
func (m *Manager) CheckAndEnableUAC() (bool, error) {
	resp, err := m.ExecuteAT(`AT+QCFG="USBCFG"?`, 3*time.Second)
	if err != nil {
		return false, err
	}

	// 查找 +QCFG: "usbcfg" 或 +QCFG: "USBCFG"
	idx := strings.Index(strings.ToLower(resp), `+qcfg: "usbcfg",`)
	if idx == -1 {
		return false, nil // 可能不支持该指令或格式不匹配
	}

	start := idx + 7 // Skip "+QCFG: " (7 chars)
	line := resp[start:]
	if end := strings.IndexAny(line, "\r\n"); end != -1 {
		line = line[:end]
	}
	line = strings.TrimSpace(line)

	// line 例: "usbcfg",0x2C7C,0x0125,1,1,1,1,1,0,0
	parts := strings.Split(line, ",")
	if len(parts) < 8 {
		return false, nil // 参数过少跳过
	}

	lastIdx := len(parts) - 1
	lastVal := strings.TrimSpace(parts[lastIdx])

	if lastVal == "0" {
		parts[lastIdx] = "1"
		newArgs := strings.Join(parts, ",")
		newCmd := fmt.Sprintf(`AT+QCFG=%s`, newArgs)
		logger.Info(fmt.Sprintf("[%s] 检测到 UAC 接口未开启，正在通过 %s 执行开启", m.cfg.ID, newCmd))
		_, err := m.ExecuteAT(newCmd, 3*time.Second)
		if err != nil {
			return false, fmt.Errorf("动态开启 UAC 失败: %w", err)
		}
		return true, nil
	} else {
		logger.Debug(fmt.Sprintf("[%s] UAC 接口已处于开启状态 (%s)，无需重启", m.cfg.ID, lastVal))
	}
	return false, nil
}

// EnableUSBAudio 开启 USB Audio UAC模式 (AT+QPCMV=1,2)
// 注意：每次模块重启此设置都会失效，需要在开机后初始化流程或业务需要前调用
func (m *Manager) EnableUSBAudio() error {
	// 查询当前 QPCMV 状态避免重复发送
	enabled, _, err := m.QueryUSBAudioMode()
	if err == nil && enabled {
		logger.Debug(fmt.Sprintf("[%s] USB Audio 此时已经处于开启状态，无需重复下发指令", m.cfg.ID))
		return nil
	}

	_, err = m.ExecuteAT("AT+QPCMV=1,2", 2*time.Second)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] 开启 USB Audio (QPCMV) 失败", m.cfg.ID), "err", err)
		return err
	}
	logger.Info(fmt.Sprintf("[%s] USB Audio (QPCMV) 已配置开启", m.cfg.ID))
	return nil
}

// DisableUSBAudio 关闭 USB Audio 模式 (AT+QPCMV=0)
func (m *Manager) DisableUSBAudio() error {
	_, err := m.ExecuteAT("AT+QPCMV=0", 2*time.Second)
	if err != nil {
		logger.Error(fmt.Sprintf("[%s] 关闭 USB Audio 失败", m.cfg.ID), "err", err)
		return err
	}
	logger.Info(fmt.Sprintf("[%s] USB Audio 已配置关闭", m.cfg.ID))
	return nil
}

// QueryUSBAudioMode 查询当前 USB Audio 状态
func (m *Manager) QueryUSBAudioMode() (bool, int, error) {
	resp, err := m.ExecuteAT("AT+QPCMV?", 2*time.Second)
	if err != nil {
		return false, 0, err
	}
	idx := strings.Index(resp, "+QPCMV:")
	if idx == -1 {
		return false, 0, errors.New("查询失败: 响应未包含 +QPCMV")
	}
	parts := strings.Split(strings.TrimSpace(resp[idx+7:]), ",")
	enabled := strings.TrimSpace(parts[0]) == "1"
	mode := 0
	if len(parts) > 1 {
		fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &mode)
	}
	return enabled, mode, nil
}
