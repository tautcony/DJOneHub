//go:build windows

package native

import (
	"encoding/json"
	"fmt"
	"os/exec"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/pkg/logger"
)

const windowsToastGroup = "djonehub"

// windowsDriver owns a hidden Win32 window and the tray-icon lifecycle used to
// present notifications directly from the DJOneHub process.
type windowsDriver struct {
	stopOnce    sync.Once
	stopCh      chan struct{}
	available   bool
	windowReady chan struct{}
	windowDone  chan struct{}

	mu          sync.RWMutex
	hwnd        uintptr
	threadID    uint32
	iconAdded   bool
	webURL      string
	preferences notification.NotificationPreferences
}

func newDriver() uiDriver {
	return &windowsDriver{
		stopCh:      make(chan struct{}),
		available:   true,
		windowReady: make(chan struct{}),
		windowDone:  make(chan struct{}),
		preferences: notification.DefaultNotificationPreferences(),
	}
}

func (d *windowsDriver) start(configJSON string, bridge *Bridge) {
	d.applyConfig(configJSON)
	go d.runWindowHost()
	<-d.windowReady
	logger.Info("windows native notification UI started", "available", d.available, "driver", "win32-tray")
	bridge.markReady()
	<-d.stopCh
}

func (d *windowsDriver) stop() {
	d.stopOnce.Do(func() {
		d.removeTrayIcon()
		d.mu.RLock()
		threadID := d.threadID
		d.mu.RUnlock()
		if threadID != 0 {
			_, _, _ = postThreadMessage.Call(uintptr(threadID), wmQuit, 0, 0)
			<-d.windowDone
		}
		close(d.stopCh)
	})
}

func (d *windowsDriver) hasUI() bool { return d.available }

func (d *windowsDriver) permissionStatus() string {
	if !d.available {
		return notification.NotificationPermissionUnsupported
	}
	// Windows desktop Toasts do not expose a first-run authorization callback
	// equivalent to UNUserNotificationCenter. The user's Windows notification
	// settings remain the authority, so the bridge reports the native path as
	// available and opens those settings on demand.
	return notification.NotificationPermissionAuthorized
}

func (d *windowsDriver) requestPermission() bool { return false }

func (d *windowsDriver) openSettings() bool {
	if !d.available {
		return false
	}
	if err := exec.Command("explorer.exe", "ms-settings:notifications").Start(); err != nil {
		logger.Warn("windows notification settings could not be opened", "error", err)
		return false
	}
	return true
}

func (d *windowsDriver) applyConfig(configJSON string) {
	var config struct {
		WebURL      string                               `json:"web_url"`
		Preferences notification.NotificationPreferences `json:"notification_preferences"`
	}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return
	}
	d.mu.Lock()
	d.webURL = strings.TrimSpace(config.WebURL)
	d.preferences = config.Preferences.Normalize()
	d.mu.Unlock()
}

func (d *windowsDriver) handleEvent(eventJSON string) {
	var event struct {
		ID   uint64          `json:"id"`
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
		logger.Warn("windows native notification event decode failed", "error", err)
		return
	}

	switch event.Type {
	case notification.EventCallIncoming, notification.EventCallUpdated:
		var call notification.CallEvent
		if json.Unmarshal(event.Data, &call) == nil {
			d.showCall(call, event.Type == notification.EventCallIncoming)
		}
	case notification.EventCallEnded:
		var call notification.CallEvent
		if json.Unmarshal(event.Data, &call) == nil {
			d.removeToast(incomingCallTag(call.ID))
		}
	case notification.EventCallMissed:
		var call notification.CallEvent
		if json.Unmarshal(event.Data, &call) == nil {
			d.showMissedCall(call)
		}
	case notification.EventSMSReceived:
		var message notification.SMSMessageEvent
		if json.Unmarshal(event.Data, &message) == nil {
			d.showSMS(message, event.ID)
		}
	case notification.EventDeviceOffline:
		var offline notification.DeviceOfflineEvent
		if json.Unmarshal(event.Data, &offline) == nil {
			d.showOffline(offline)
		}
	case notification.EventNotificationPreferencesUpdated:
		var preferences notification.NotificationPreferences
		if json.Unmarshal(event.Data, &preferences) == nil {
			d.mu.Lock()
			d.preferences = preferences.Normalize()
			d.mu.Unlock()
		}
	}
}

func (d *windowsDriver) showCall(call notification.CallEvent, sound bool) {
	state := "来电"
	if call.State == "active" {
		state = "通话中"
	}
	content := windowsToast{
		title:  "DJOneHub " + state,
		body:   displayWindowsNumber(call.Number) + "，时间 " + call.StartedAt.Local().Format("15:04") + "。点击通知查看详情。",
		launch: d.dashboardURL(),
		tag:    incomingCallTag(call.ID),
		group:  windowsToastGroup,
		sound:  sound,
	}
	d.show(content)
}

func (d *windowsDriver) showMissedCall(call notification.CallEvent) {
	d.show(windowsToast{
		title:  "未接来电",
		body:   "来自 " + displayWindowsNumber(call.Number) + "，时间 " + call.StartedAt.Local().Format("15:04") + "。点击通知查看详情。",
		launch: d.dashboardURL(), tag: missedCallTag(call.ID), group: windowsToastGroup, sound: true,
	})
}

func (d *windowsDriver) showSMS(message notification.SMSMessageEvent, eventID uint64) {
	sender := strings.TrimSpace(message.Sender)
	if sender == "" {
		sender = "未知发送方"
	}
	d.mu.RLock()
	senderOnly := d.preferences.SenderOnly == nil || *d.preferences.SenderOnly
	d.mu.RUnlock()
	body := "收到一条短信"
	if !senderOnly {
		body = trimWindowsNotificationText(message.Body, 48)
	}
	d.show(windowsToast{
		title: sender, body: body, launch: d.dashboardURL(),
		tag: "sms-" + strconv.FormatUint(eventID, 10), group: windowsToastGroup, sound: true,
	})
}

func (d *windowsDriver) showOffline(_ notification.DeviceOfflineEvent) {
	d.show(windowsToast{
		title:  "DJOneHub 暂时离线",
		body:   "设备连接已断开，相关服务已暂停。点击通知查看详情。",
		launch: d.dashboardURL(), tag: "device-offline", group: windowsToastGroup, sound: true,
	})
}

func (d *windowsDriver) dashboardURL() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.webURL
}

type windowsToast struct {
	title    string
	body     string
	launch   string
	tag      string
	group    string
	scenario string
	sound    bool
}

func (d *windowsDriver) show(content windowsToast) {
	if !d.available {
		return
	}
	if err := d.showWin32Balloon(content); err != nil {
		logger.Warn("windows native notification delivery failed", "error", err, "tag", content.tag)
		return
	}
	logger.Info("windows native notification delivered", "tag", content.tag)
}

var (
	shellNotifyIcon    = syscall.NewLazyDLL("shell32.dll").NewProc("Shell_NotifyIconW")
	user32             = syscall.NewLazyDLL("user32.dll")
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	createWindowEx     = user32.NewProc("CreateWindowExW")
	destroyWindow      = user32.NewProc("DestroyWindow")
	getMessage         = user32.NewProc("GetMessageW")
	translateMessage   = user32.NewProc("TranslateMessage")
	dispatchMessage    = user32.NewProc("DispatchMessageW")
	postThreadMessage  = user32.NewProc("PostThreadMessageW")
	loadIcon           = user32.NewProc("LoadIconW")
	getCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
)

const (
	nimAdd         = 0x00000000
	nimModify      = 0x00000001
	nimDelete      = 0x00000002
	nifMessage     = 0x00000001
	nifIcon        = 0x00000002
	nifTip         = 0x00000004
	nifInfo        = 0x00000010
	niifInfo       = 0x00000001
	wmApp          = 0x8000
	wmQuit         = 0x0012
	idiApplication = 32512
)

type windowMessage struct {
	hWnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	point   struct{ x, y int32 }
	private uint32
}

type notifyIconData struct {
	cbSize            uint32
	hWnd              uintptr
	uID               uint32
	uFlags            uint32
	uCallbackMessage  uint32
	hIcon             uintptr
	szTip             [128]uint16
	dwState           uint32
	dwStateMask       uint32
	szInfo            [256]uint16
	uTimeoutOrVersion uint32
	szInfoTitle       [64]uint16
	dwInfoFlags       uint32
	guid              [16]byte
	hBalloonIcon      uintptr
}

func (d *windowsDriver) runWindowHost() {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	className, _ := syscall.UTF16PtrFromString("STATIC")
	windowName, _ := syscall.UTF16PtrFromString("DJOneHub Notification Host")
	hwnd, _, _ := createWindowEx.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowName)), 0, 0, 0, 0, 0, 0, 0, 0, 0)
	threadID, _, _ := getCurrentThreadID.Call()
	d.mu.Lock()
	d.hwnd = hwnd
	d.threadID = uint32(threadID)
	d.mu.Unlock()
	close(d.windowReady)
	if hwnd == 0 {
		close(d.windowDone)
		return
	}
	var message windowMessage
	for {
		ret, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		_, _, _ = translateMessage.Call(uintptr(unsafe.Pointer(&message)))
		_, _, _ = dispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
	}
	_, _, _ = destroyWindow.Call(hwnd)
	close(d.windowDone)
}

func (d *windowsDriver) showWin32Balloon(content windowsToast) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.hwnd == 0 {
		return fmt.Errorf("Windows notification host window is unavailable")
	}
	hIcon, _, _ := loadIcon.Call(0, idiApplication)
	data := notifyIconData{cbSize: uint32(unsafe.Sizeof(notifyIconData{})), hWnd: d.hwnd, uID: 1, uFlags: nifMessage | nifIcon | nifTip | nifInfo, uCallbackMessage: wmApp + 1, hIcon: hIcon, dwInfoFlags: niifInfo, uTimeoutOrVersion: 5000}
	copy(data.szTip[:], utf16.Encode([]rune("DJOneHub")))
	copy(data.szInfo[:], utf16.Encode([]rune(trimWindowsNotificationText(content.body, 255))))
	copy(data.szInfoTitle[:], utf16.Encode([]rune(trimWindowsNotificationText(content.title, 63))))
	action := uintptr(nimAdd)
	if d.iconAdded {
		action = nimModify
	}
	ret, _, err := shellNotifyIcon.Call(action, uintptr(unsafe.Pointer(&data)))
	if ret == 0 {
		return err
	}
	d.iconAdded = true
	return nil
}

func (d *windowsDriver) removeToast(tag string) {
	d.removeTrayIcon()
}

func (d *windowsDriver) removeTrayIcon() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.iconAdded || d.hwnd == 0 {
		return
	}
	data := notifyIconData{cbSize: uint32(unsafe.Sizeof(notifyIconData{})), hWnd: d.hwnd, uID: 1}
	_, _, _ = shellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
	d.iconAdded = false
}

func trimWindowsNotificationText(value string, limit int) string {
	value = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(value))
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

func displayWindowsNumber(number string) string {
	if strings.TrimSpace(number) == "" {
		return "未知号码"
	}
	return strings.TrimSpace(number)
}

func incomingCallTag(callID string) string { return "incoming-call-" + strings.TrimSpace(callID) }
func missedCallTag(callID string) string   { return "missed-call-" + strings.TrimSpace(callID) }

var _ uiDriver = (*windowsDriver)(nil)
var _ permissionDriver = (*windowsDriver)(nil)
