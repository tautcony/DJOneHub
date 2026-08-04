package darwin

import (
	"context"
	"os"
	"testing"

	"github.com/iniwex5/vohive/internal/domain/device"
)

func TestLiveECMNetworkStatus(t *testing.T) {
	if os.Getenv("DJONEHUB_TEST_LIVE_NETWORK") != "1" {
		t.Skip("set DJONEHUB_TEST_LIVE_NETWORK=1 to inspect connected hardware")
	}
	adapter := New()
	candidates, err := adapter.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) == 0 {
		t.Fatal("no supported USB modem was discovered")
	}
	status, err := adapter.Status(context.Background(), candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if status.Interface == "" || len(status.Addresses) == 0 || status.DefaultRoute == "" || status.SystemDefaultRoute == "" {
		t.Fatalf("incomplete live status: %+v", status)
	}
	connectivity, err := adapter.CheckConnectivity(context.Background(), candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if !connectivity.OK {
		t.Fatalf("live connectivity failed: %+v", connectivity)
	}
	rxBytes, txBytes, err := adapter.NetworkTraffic(context.Background(), candidates[0])
	if err != nil || rxBytes == 0 || txBytes == 0 {
		t.Fatalf("live traffic failed: rx=%d tx=%d err=%v", rxBytes, txBytes, err)
	}
	t.Logf("status=%+v connectivity=%+v traffic=%d/%d", status, connectivity, rxBytes, txBytes)
}

func TestParseIfconfigHandlesAdjacentInterfaceBlocks(t *testing.T) {
	output := `lo0: flags=8049<UP,LOOPBACK,RUNNING,MULTICAST> mtu 16384
	inet 127.0.0.1 netmask 0xff000000
en0: flags=8863<UP,BROADCAST,RUNNING> mtu 1500
	inet 192.168.0.111 netmask 0xffffff00 broadcast 192.168.0.255
	status: active
en13: flags=8863<UP,BROADCAST,RUNNING> mtu 1500
	inet6 fe80::1%en13 prefixlen 64
	inet 192.168.225.29 netmask 0xffffff00 broadcast 192.168.225.255
	status: active
utun0: flags=8051<UP,POINTOPOINT,RUNNING> mtu 1500
	inet6 fe80::2%utun0 prefixlen 64`

	items := parseIfconfig(output)
	if len(items) != 2 {
		t.Fatalf("interfaces = %+v", items)
	}
	if items[0].Name != "en0" || items[0].IPv4 != "192.168.0.111" || items[1].Name != "en13" || items[1].IPv4 != "192.168.225.29" {
		t.Fatalf("interfaces = %+v", items)
	}
}

func TestParseECMInterfaceNamesMatchesUSBIdentityAndLocation(t *testing.T) {
	output := `+-o CDC Ethernet Control Model (ECM)@4 <class IOUSBHostInterface>
  | {
  |   "idProduct" = 16390
  |   "locationID" = 1048576
  |   "bInterfaceClass" = 2
  |   "bInterfaceSubClass" = 6
  |   "idVendor" = 11427
  | }
  +-o AppleUserECM <class IOUserNetworkEthernet>
      {
        "IOInterfaceName" = "en13"
      }
`
	candidate := device.Candidate{Identity: device.Identity{
		VendorID: "2ca3", ProductID: "4006", PhysicalLocation: "0x100000",
	}}
	names := parseECMInterfaceNames(output, candidate)
	if len(names) != 1 || names[0] != "en13" {
		t.Fatalf("ECM interfaces = %v", names)
	}
	candidate.Identity.PhysicalLocation = "0x200000"
	if names := parseECMInterfaceNames(output, candidate); len(names) != 0 {
		t.Fatalf("mismatched location returned %v", names)
	}
}

func TestParseInterfaceCountersUsesLinkRow(t *testing.T) {
	output := `Name Mtu Network Address Ipkts Ierrs Ibytes Opkts Oerrs Obytes Coll
en13 1500 <Link#46> 86:33:1a:d1:aa:45 281 0 38756 462 0 95136 0
en13 1500 192.168.225 192.168.225.29 281 - 38756 462 - 95136 -`
	rxBytes, txBytes := parseInterfaceCounters(output, "en13")
	if rxBytes != 38756 || txBytes != 95136 {
		t.Fatalf("counters = %d/%d", rxBytes, txBytes)
	}
}

func TestParseAndFormatDefaultRoute(t *testing.T) {
	route := parseRoute("gateway: 192.168.0.1\ninterface: en0\n")
	if got := formatRoute(route); got != "en0 via 192.168.0.1" {
		t.Fatalf("route = %q", got)
	}
}
