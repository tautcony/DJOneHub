package vowifi

import (
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost"
)

func TestBuildIMSRegistrarDefaults(t *testing.T) {
	t.Setenv("DJONEHUB_VOWIFI_IMS_REGISTRAR", "")
	t.Setenv("DJONEHUB_VOWIFI_IMS_SERVER", "")

	reg := buildIMSRegistrar()
	if reg.Network != "udp" {
		t.Fatalf("Network = %q, want udp", reg.Network)
	}
	if reg.Expires != 3600 {
		t.Fatalf("Expires = %d, want 3600", reg.Expires)
	}
	if reg.Timeout != 10*time.Second {
		t.Fatalf("Timeout = %v, want 10s", reg.Timeout)
	}
	if reg.UserAgent == "" {
		t.Fatal("UserAgent should not be empty")
	}
	// 默认走引擎解析路径：不指定 registrar URI / server 地址。
	if reg.RegistrarURI != "" || reg.ServerAddr != "" || reg.Resolver != nil {
		t.Fatalf("default registrar should leave URI/server/resolver empty, got %+v", reg)
	}
	var _ runtimehost.IMSRegistrar = reg
}

func TestBuildIMSRegistrarEnvOverrides(t *testing.T) {
	t.Setenv("DJONEHUB_VOWIFI_IMS_REGISTRAR", "sip:ims.example.com")
	t.Setenv("DJONEHUB_VOWIFI_IMS_SERVER", "10.0.0.1:5060")

	reg := buildIMSRegistrar()
	if reg.RegistrarURI != "sip:ims.example.com" {
		t.Fatalf("RegistrarURI = %q, want sip:ims.example.com", reg.RegistrarURI)
	}
	if reg.ServerAddr != "10.0.0.1:5060" {
		t.Fatalf("ServerAddr = %q, want 10.0.0.1:5060", reg.ServerAddr)
	}
}

func TestBuildIMSRegistrarEnvOverrideTrims(t *testing.T) {
	t.Setenv("DJONEHUB_VOWIFI_IMS_REGISTRAR", "  sip:ims.example.com  ")
	t.Setenv("DJONEHUB_VOWIFI_IMS_SERVER", " 10.0.0.1:5060 ")

	reg := buildIMSRegistrar()
	if reg.RegistrarURI != "sip:ims.example.com" {
		t.Fatalf("RegistrarURI = %q, want trimmed", reg.RegistrarURI)
	}
	if reg.ServerAddr != "10.0.0.1:5060" {
		t.Fatalf("ServerAddr = %q, want trimmed", reg.ServerAddr)
	}
}
