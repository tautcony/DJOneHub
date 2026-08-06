package modem

import (
	"reflect"
	"testing"
	"time"
)

// TestHandleCMTIEmitsStorageAndIndexToConsumer verifies the +CMTI handler
// forwards the storage and index pair to a registered consumer and performs no
// read/delete itself: reading, durable persistence, and acknowledgement are
// the consumer's job.
func TestHandleCMTIEmitsStorageAndIndexToConsumer(t *testing.T) {
	m := newRunningTestManager(t)

	received := make(chan [2]string, 1)
	m.SetNewSMSHandler(func(storage, index string) {
		received <- [2]string{storage, index}
	})

	m.handleURC(`+CMTI: "ME",7`)
	select {
	case got := <-received:
		if got != [2]string{"ME", "7"} {
			t.Fatalf("consumer ref = %#v, want [ME 7]", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for consumer callback")
	}
}

// TestHandleCMTIRetainsEntryWithoutConsumer verifies that without a registered
// consumer the +CMTI handler issues no AT commands at all: the entry stays in
// modem storage instead of being auto-read and deleted.
func TestHandleCMTIRetainsEntryWithoutConsumer(t *testing.T) {
	m := newRunningTestManager(t)
	m.SetNewSMSHandler(nil)

	m.handleURC(`+CMTI: "SM",12`)

	// No command traffic may result; a brief window covers the async handler.
	time.Sleep(100 * time.Millisecond)
	select {
	case req := <-m.cmdChan:
		t.Fatalf("unexpected command issued without a consumer: %s", req.cmd)
	case req := <-m.cmdChanHigh:
		t.Fatalf("unexpected high-priority command issued without a consumer: %s", req.cmd)
	default:
	}
}

// TestReadAndDeleteSMSFromStorageSwitchAndRestore covers the storage-aware
// read/delete sequence: CPMS query, switch to the indicated storage, the
// operation itself, and restore of the previous selection, serialized by
// smsReadMu.
func TestReadAndDeleteSMSFromStorageSwitchAndRestore(t *testing.T) {
	m := newRunningTestManager(t)
	validPDU := "079144872000302320048102020000625061028204401AD9775D0E72D7DBE2B21C949E8360B75A4E7683D16AB71B"

	done := make(chan []string, 1)
	go func() {
		done <- respondToCommands(t, m, 8, func(req commandRequest) {
			switch req.cmd {
			case "AT+CPMS?":
				req.respChan <- "\r\n+CPMS: \"SM\",0,10,\"SM\",0,10,\"SM\",0,10\r\n\r\nOK\r\n"
			case `AT+CPMS="ME","ME","ME"`:
				req.respChan <- "OK"
			case "AT+CMGR=7":
				req.respChan <- "\r\n+CMGR: 0,,38\r\n" + validPDU + "\r\n\r\nOK\r\n"
			case "AT+CMGD=7":
				req.respChan <- "OK"
			case `AT+CPMS="SM","SM","SM"`:
				req.respChan <- "OK"
			default:
				req.errChan <- nil
			}
		})
	}()

	pdu, err := m.ReadSMSFromStorage("ME", 7)
	if err != nil {
		t.Fatalf("ReadSMSFromStorage: %v", err)
	}
	if pdu != validPDU {
		t.Fatalf("pdu = %q", pdu)
	}
	if err := m.DeleteSMSFromStorage("ME", 7); err != nil {
		t.Fatalf("DeleteSMSFromStorage: %v", err)
	}

	var got []string
	select {
	case got = <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for storage-aware read/delete")
	}
	want := []string{
		"AT+CPMS?",
		`AT+CPMS="ME","ME","ME"`,
		"AT+CMGR=7",
		`AT+CPMS="SM","SM","SM"`,
		"AT+CPMS?",
		`AT+CPMS="ME","ME","ME"`,
		"AT+CMGD=7",
		`AT+CPMS="SM","SM","SM"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands=%#v want %#v", got, want)
	}
}
