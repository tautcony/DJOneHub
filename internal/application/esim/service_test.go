package esim

import (
	"context"
	"testing"

	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

func TestResolveActivationCode(t *testing.T) {
	cases := []struct {
		name           string
		activationCode string
		matchingID     string
		wantSMDP       string
		wantMatchingID string
	}{
		{
			name:           "plain SM-DP+ host is passed through",
			activationCode: "smdp.example.com",
			matchingID:     "1-abc",
			wantSMDP:       "smdp.example.com",
			wantMatchingID: "1-abc",
		},
		{
			name:           "full LPA:1$ code extracts SM-DP+ and matching ID",
			activationCode: "LPA:1$smdp.example.com$1-abc",
			matchingID:     "user-typed",
			wantSMDP:       "smdp.example.com",
			wantMatchingID: "1-abc",
		},
		{
			name:           "embedded matching ID wins over typed field",
			activationCode: "LPA:1$smdp.example.com$embedded",
			matchingID:     "typed",
			wantSMDP:       "smdp.example.com",
			wantMatchingID: "embedded",
		},
		{
			name:           "LPA:1$ code with OID keeps matching ID",
			activationCode: "LPA:1$smdp.example.com$1-abc$1.2.3.4",
			matchingID:     "",
			wantSMDP:       "smdp.example.com",
			wantMatchingID: "1-abc",
		},
		{
			name:           "malformed LPA:1$ code falls back to raw string",
			activationCode: "LPA:1$",
			matchingID:     "typed",
			wantSMDP:       "LPA:1$",
			wantMatchingID: "typed",
		},
		{
			name:           "LPA:1$ code without matching ID keeps typed field",
			activationCode: "LPA:1$smdp.example.com",
			matchingID:     "typed",
			wantSMDP:       "smdp.example.com",
			wantMatchingID: "typed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSMDP, gotMatchingID := resolveActivationCode(tc.activationCode, tc.matchingID)
			if gotSMDP != tc.wantSMDP || gotMatchingID != tc.wantMatchingID {
				t.Fatalf("resolveActivationCode(%q, %q) = (%q, %q), want (%q, %q)",
					tc.activationCode, tc.matchingID, gotSMDP, gotMatchingID, tc.wantSMDP, tc.wantMatchingID)
			}
		})
	}
}

func TestSubmitConfirmationCodeRejectsUnknownOperation(t *testing.T) {
	service := NewService(nil, nil, nil)
	err := service.SubmitConfirmationCode("no-such-operation", "123456", false)
	if err == nil {
		t.Fatal("SubmitConfirmationCode() error=nil, want not-found error")
	}
	var target *derrors.Error
	if !errorsAs(err, &target) || target.Code != derrors.NotFound {
		t.Fatalf("err = %v, want not_found", err)
	}
}

func errorsAs(err error, target any) bool {
	switch v := target.(type) {
	case **derrors.Error:
		typed, ok := err.(*derrors.Error)
		if !ok {
			return false
		}
		*v = typed
		return true
	default:
		return false
	}
}

type fakeVoWiFiSwitcher struct {
	begin    int
	end      int
	endRadio []bool
}

func (f *fakeVoWiFiSwitcher) SwitchBegin(context.Context) error {
	f.begin++
	return nil
}

func (f *fakeVoWiFiSwitcher) SwitchEnd(_ context.Context, restoreRadio bool) error {
	f.end++
	f.endRadio = append(f.endRadio, restoreRadio)
	return nil
}

func TestServiceSwitchLinkageHelpers(t *testing.T) {
	fake := &fakeVoWiFiSwitcher{}
	svc := &Service{vowifi: fake}

	svc.switchBegin(context.Background(), "iccid-1")
	svc.switchEnd(context.Background(), "iccid-1")
	if fake.begin != 1 || fake.end != 1 {
		t.Fatalf("switch helpers = begin %d end %d, want 1/1", fake.begin, fake.end)
	}
	if len(fake.endRadio) != 1 || !fake.endRadio[0] {
		t.Fatalf("SwitchEnd restoreRadio = %v, want [true]", fake.endRadio)
	}

	// nil 联动时安全空转。
	nilSvc := &Service{}
	nilSvc.switchBegin(context.Background(), "iccid-2")
	nilSvc.switchEnd(context.Background(), "iccid-2")
}
