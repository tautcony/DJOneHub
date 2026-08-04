//go:build darwin

package startup

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

const launchAgentLabel = "com.jamie.djonehub"

type darwinManager struct {
	executable string
	home       string
}

func New() Manager {
	executable, _ := os.Executable()
	home, _ := os.UserHomeDir()
	return darwinManager{executable: executable, home: home}
}

func (m darwinManager) Status() Status {
	_, err := os.Stat(m.plistPath())
	return Status{Supported: m.isAppBundle(), Enabled: err == nil}
}

func (m darwinManager) SetEnabled(enabled bool) error {
	if !m.isAppBundle() {
		return fmt.Errorf("login startup requires a DJOneHub.app bundle")
	}
	if enabled {
		return m.enable()
	}
	return m.disable()
}

func (m darwinManager) isAppBundle() bool {
	if m.executable == "" {
		return false
	}
	clean := filepath.Clean(m.executable)
	return filepath.Base(filepath.Dir(filepath.Dir(clean))) == "Contents" &&
		filepath.Base(filepath.Dir(clean)) == "MacOS" &&
		filepath.Ext(filepath.Dir(filepath.Dir(filepath.Dir(clean)))) == ".app"
}

func (m darwinManager) plistPath() string {
	return filepath.Join(m.home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

func (m darwinManager) enable() error {
	if err := os.MkdirAll(filepath.Dir(m.plistPath()), 0o755); err != nil {
		return err
	}
	logDir := filepath.Join(m.home, "Library", "Logs", "DJOneHub")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	webDir := filepath.Clean(filepath.Join(filepath.Dir(m.executable), "..", "Resources", "web", "dist"))
	plist := launchAgentPlist{
		Dict: plistDict{Entries: []plistEntry{
			{Key: "Label", Value: plistString(launchAgentLabel)},
			{Key: "ProgramArguments", Value: plistArray{m.executable, "-listen", "127.0.0.1:7575", "-web-dir", webDir}},
			{Key: "RunAtLoad", Value: plistBool(true)},
			{Key: "KeepAlive", Value: plistBool(true)},
			{Key: "ThrottleInterval", Value: plistInteger(5)},
			{Key: "ProcessType", Value: plistString("Interactive")},
			{Key: "StandardOutPath", Value: plistString(filepath.Join(logDir, "djonehub.log"))},
			{Key: "StandardErrorPath", Value: plistString(filepath.Join(logDir, "djonehub.log"))},
			{Key: "WorkingDirectory", Value: plistString(filepath.Dir(filepath.Dir(m.executable)))},
		}},
	}
	data, err := xml.Marshal(plist)
	if err != nil {
		return err
	}
	data = append([]byte(xml.Header), data...)
	tmp, err := os.CreateTemp(filepath.Dir(m.plistPath()), ".djonehub-launch-agent-*.plist")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, m.plistPath()); err != nil {
		return err
	}
	// The file is picked up at the next login. Avoid launching a second copy
	// when the user enables the setting from the already running app.
	return nil
}

func (m darwinManager) disable() error {
	_ = exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid()), launchAgentLabel).Run()
	if err := os.Remove(m.plistPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

type launchAgentPlist struct {
	XMLName xml.Name  `xml:"plist"`
	Version string    `xml:"version,attr"`
	Dict    plistDict `xml:"dict"`
}

type plistDict struct {
	Entries []plistEntry
}

type plistEntry struct {
	Key   string
	Value any
}

type plistArray []string
type plistString string
type plistBool bool
type plistInteger int

func (p launchAgentPlist) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	start.Name.Local = "plist"
	start.Attr = []xml.Attr{{Name: xml.Name{Local: "version"}, Value: "1.0"}}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	if err := e.Encode(p.Dict); err != nil {
		return err
	}
	return e.EncodeToken(start.End())
}

func (d plistDict) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	start.Name.Local = "dict"
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	for _, entry := range d.Entries {
		if err := e.EncodeElement(entry.Key, xml.StartElement{Name: xml.Name{Local: "key"}}); err != nil {
			return err
		}
		if err := encodePlistValue(e, entry.Value); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

func encodePlistValue(e *xml.Encoder, value any) error {
	switch value := value.(type) {
	case plistString:
		return e.EncodeElement(string(value), xml.StartElement{Name: xml.Name{Local: "string"}})
	case plistBool:
		name := "false"
		if value {
			name = "true"
		}
		if err := e.EncodeToken(xml.StartElement{Name: xml.Name{Local: name}}); err != nil {
			return err
		}
		return encodeEnd(e, name)
	case plistInteger:
		return e.EncodeElement(int(value), xml.StartElement{Name: xml.Name{Local: "integer"}})
	case plistArray:
		if err := e.EncodeToken(xml.StartElement{Name: xml.Name{Local: "array"}}); err != nil {
			return err
		}
		for _, item := range value {
			if err := e.EncodeElement(item, xml.StartElement{Name: xml.Name{Local: "string"}}); err != nil {
				return err
			}
		}
		return e.EncodeToken(xml.EndElement{Name: xml.Name{Local: "array"}})
	default:
		return fmt.Errorf("unsupported plist value %T", value)
	}
}

func encodeEnd(e *xml.Encoder, name string) error {
	return e.EncodeToken(xml.EndElement{Name: xml.Name{Local: name}})
}
