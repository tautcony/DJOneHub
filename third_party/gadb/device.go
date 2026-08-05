package gadb

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

type DeviceFileInfo struct {
	Name         string
	Mode         os.FileMode
	Size         uint32
	LastModified time.Time
}

func (info DeviceFileInfo) IsDir() bool {
	return (info.Mode & (1 << 14)) == (1 << 14)
}

const DefaultFileMode = os.FileMode(0664)

type DeviceState string

const (
	StateUnknown      DeviceState = "UNKNOWN"
	StateOnline       DeviceState = "online"
	StateOffline      DeviceState = "offline"
	StateDisconnected DeviceState = "disconnected"
)

var deviceStateStrings = map[string]DeviceState{
	"":        StateDisconnected,
	"offline": StateOffline,
	"device":  StateOnline,
}

func deviceStateConv(k string) (deviceState DeviceState) {
	var ok bool
	if deviceState, ok = deviceStateStrings[k]; !ok {
		return StateUnknown
	}
	return
}

type DeviceForward struct {
	Serial string
	Local  string
	Remote string
	// LocalProtocol string
	// RemoteProtocol string
}

type Device struct {
	adbClient Client
	serial    string
	attrs     map[string]string
}

type ShellSession struct {
	conn net.Conn
}

func (s *ShellSession) Read(p []byte) (int, error) {
	return s.conn.Read(p)
}

func (s *ShellSession) Write(p []byte) (int, error) {
	return s.conn.Write(p)
}

func (s *ShellSession) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (d Device) selector() (string, error) {
	if d.serial != "" {
		return fmt.Sprintf("host-serial:%s", d.serial), nil
	}
	if transportID, ok := d.attrs["transport_id"]; ok && transportID != "" {
		return fmt.Sprintf("host-transport-id:%s", transportID), nil
	}
	return "", errors.New("ADB device has neither a serial number nor a transport ID")
}

func (d Device) transportSelector() (string, error) {
	if d.serial != "" {
		return fmt.Sprintf("host:transport:%s", d.serial), nil
	}
	if transportID, ok := d.attrs["transport_id"]; ok && transportID != "" {
		return fmt.Sprintf("host:transport-id:%s", transportID), nil
	}
	return "", errors.New("ADB device has neither a serial number nor a transport ID")
}

func (d Device) HasAttribute(key string) bool {
	_, ok := d.attrs[key]
	return ok
}

func (d Device) Product() (string, error) {
	if d.HasAttribute("product") {
		return d.attrs["product"], nil
	}
	return "", errors.New("does not have attribute: product")
}

func (d Device) Model() (string, error) {
	if d.HasAttribute("model") {
		return d.attrs["model"], nil
	}
	return "", errors.New("does not have attribute: model")
}

func (d Device) Usb() (string, error) {
	if d.HasAttribute("usb") {
		return d.attrs["usb"], nil
	}
	return "", errors.New("does not have attribute: usb")
}

func (d Device) transportId() (string, error) {
	if d.HasAttribute("transport_id") {
		return d.attrs["transport_id"], nil
	}
	return "", errors.New("does not have attribute: transport_id")
}

func (d Device) DeviceInfo() map[string]string {
	return d.attrs
}

func (d Device) Serial() string {
	// 	resp, err := d.adbClient.executeCommand(fmt.Sprintf("host-serial:%s:get-serialno", d.serial))
	if d.serial != "" {
		return d.serial
	}
	if transportID, ok := d.attrs["transport_id"]; ok && transportID != "" {
		return "transport-id:" + transportID
	}
	return ""
}

func (d Device) IsUsb() (bool, error) {
	usb, err := d.Usb()
	if err != nil {
		return false, err
	}

	return usb != "", nil
}

func (d Device) State() (DeviceState, error) {
	selector, err := d.selector()
	if err != nil {
		return StateUnknown, err
	}
	resp, err := d.adbClient.executeCommand(selector + ":get-state")
	return deviceStateConv(resp), err
}

func (d Device) DevicePath() (string, error) {
	selector, err := d.selector()
	if err != nil {
		return "", err
	}
	resp, err := d.adbClient.executeCommand(selector + ":get-devpath")
	return resp, err
}

func (d Device) Forward(localPort, remotePort int, noRebind ...bool) (err error) {
	command := ""
	local := fmt.Sprintf("tcp:%d", localPort)
	remote := fmt.Sprintf("tcp:%d", remotePort)
	selector, err := d.selector()
	if err != nil {
		return err
	}

	if len(noRebind) != 0 && noRebind[0] {
		command = fmt.Sprintf("%s:forward:norebind:%s;%s", selector, local, remote)
	} else {
		command = fmt.Sprintf("%s:forward:%s;%s", selector, local, remote)
	}

	_, err = d.adbClient.executeCommand(command, true)
	return
}

func (d Device) ForwardList() (deviceForwardList []DeviceForward, err error) {
	var forwardList []DeviceForward
	if forwardList, err = d.adbClient.ForwardList(); err != nil {
		return nil, err
	}

	deviceForwardList = make([]DeviceForward, 0, len(deviceForwardList))
	for i := range forwardList {
		if forwardList[i].Serial == d.serial {
			deviceForwardList = append(deviceForwardList, forwardList[i])
		}
	}
	// resp, err := d.adbClient.executeCommand(fmt.Sprintf("host-serial:%s:list-forward", d.serial))
	return
}

func (d Device) ForwardKill(localPort int) (err error) {
	local := fmt.Sprintf("tcp:%d", localPort)
	selector, selectorErr := d.selector()
	if selectorErr != nil {
		return selectorErr
	}
	_, err = d.adbClient.executeCommand(fmt.Sprintf("%s:killforward:%s", selector, local), true)
	return
}

func (d Device) RunShellCommand(cmd string, args ...string) (string, error) {
	raw, err := d.RunShellCommandWithBytes(cmd, args...)
	return string(raw), err
}

// Reboot requests a reboot through the ADB transport service. This matches
// `adb reboot [mode]`; it is intentionally not executed through adb shell.
func (d Device) Reboot(mode string) error {
	service := "reboot:"
	if mode = strings.TrimSpace(mode); mode != "" {
		service += mode
	}
	_, err := d.executeCommand(service, true)
	return err
}

func (d Device) OpenShell() (io.ReadWriteCloser, error) {
	tp, err := newTransport(fmt.Sprintf("%s:%d", d.adbClient.host, d.adbClient.port))
	if err != nil {
		return nil, err
	}
	selector, selectorErr := d.transportSelector()
	if selectorErr != nil {
		_ = tp.Close()
		return nil, selectorErr
	}
	if err := tp.Send(selector); err != nil {
		_ = tp.Close()
		return nil, err
	}
	if err := tp.VerifyResponse(); err != nil {
		_ = tp.Close()
		return nil, err
	}
	if err := tp.Send("shell:"); err != nil {
		_ = tp.Close()
		return nil, err
	}
	if err := tp.VerifyResponse(); err != nil {
		_ = tp.Close()
		return nil, err
	}
	return &ShellSession{conn: tp.sock}, nil
}

func (d Device) RunShellCommandWithBytes(cmd string, args ...string) ([]byte, error) {
	if len(args) > 0 {
		cmd = fmt.Sprintf("%s %s", cmd, strings.Join(args, " "))
	}
	if strings.TrimSpace(cmd) == "" {
		return nil, errors.New("adb shell: command cannot be empty")
	}
	raw, err := d.executeCommand(fmt.Sprintf("shell:%s", cmd))
	return raw, err
}

func (d Device) EnableAdbOverTCP(port ...int) (err error) {
	if len(port) == 0 {
		port = []int{AdbDaemonPort}
	}

	_, err = d.executeCommand(fmt.Sprintf("tcpip:%d", port[0]), true)
	return
}

func (d Device) createDeviceTransport() (tp transport, err error) {
	if tp, err = newTransport(fmt.Sprintf("%s:%d", d.adbClient.host, d.adbClient.port)); err != nil {
		return transport{}, err
	}

	selector, selectorErr := d.transportSelector()
	if selectorErr != nil {
		return transport{}, selectorErr
	}
	if err = tp.Send(selector); err != nil {
		return transport{}, err
	}
	err = tp.VerifyResponse()
	return
}

func (d Device) executeCommand(command string, onlyVerifyResponse ...bool) (raw []byte, err error) {
	if len(onlyVerifyResponse) == 0 {
		onlyVerifyResponse = []bool{false}
	}

	var tp transport
	if tp, err = d.createDeviceTransport(); err != nil {
		return nil, err
	}
	defer func() { _ = tp.Close() }()

	if err = tp.Send(command); err != nil {
		return nil, err
	}

	if err = tp.VerifyResponse(); err != nil {
		return nil, err
	}

	if onlyVerifyResponse[0] {
		return
	}

	raw, err = tp.ReadBytesAll()
	return
}

func (d Device) List(remotePath string) (devFileInfos []DeviceFileInfo, err error) {
	var tp transport
	if tp, err = d.createDeviceTransport(); err != nil {
		return nil, err
	}
	defer func() { _ = tp.Close() }()

	var sync syncTransport
	if sync, err = tp.CreateSyncTransport(); err != nil {
		return nil, err
	}
	defer func() { _ = sync.Close() }()

	if err = sync.Send("LIST", remotePath); err != nil {
		return nil, err
	}

	devFileInfos = make([]DeviceFileInfo, 0)

	var entry DeviceFileInfo
	for entry, err = sync.ReadDirectoryEntry(); err == nil; entry, err = sync.ReadDirectoryEntry() {
		if entry == (DeviceFileInfo{}) {
			break
		}
		devFileInfos = append(devFileInfos, entry)
	}

	return
}

func (d Device) PushFile(local *os.File, remotePath string, modification ...time.Time) (err error) {
	if len(modification) == 0 {
		var stat os.FileInfo
		if stat, err = local.Stat(); err != nil {
			return err
		}
		modification = []time.Time{stat.ModTime()}
	}

	return d.Push(local, remotePath, modification[0], DefaultFileMode)
}

func (d Device) Push(source io.Reader, remotePath string, modification time.Time, mode ...os.FileMode) (err error) {
	if len(mode) == 0 {
		mode = []os.FileMode{DefaultFileMode}
	}

	var tp transport
	if tp, err = d.createDeviceTransport(); err != nil {
		return err
	}
	defer func() { _ = tp.Close() }()

	var sync syncTransport
	if sync, err = tp.CreateSyncTransport(); err != nil {
		return err
	}
	defer func() { _ = sync.Close() }()

	data := fmt.Sprintf("%s,%d", remotePath, mode[0])
	if err = sync.Send("SEND", data); err != nil {
		return err
	}

	if err = sync.SendStream(source); err != nil {
		return
	}

	if err = sync.SendStatus("DONE", uint32(modification.Unix())); err != nil {
		return
	}

	if err = sync.VerifyStatus(); err != nil {
		return
	}
	return
}

func (d Device) Pull(remotePath string, dest io.Writer) (err error) {
	var tp transport
	if tp, err = d.createDeviceTransport(); err != nil {
		return err
	}
	defer func() { _ = tp.Close() }()

	var sync syncTransport
	if sync, err = tp.CreateSyncTransport(); err != nil {
		return err
	}
	defer func() { _ = sync.Close() }()

	if err = sync.Send("RECV", remotePath); err != nil {
		return err
	}

	err = sync.WriteStream(dest)
	return
}
