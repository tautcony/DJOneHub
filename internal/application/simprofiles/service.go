package simprofiles

import (
	"context"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/backend"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/storage"
	"github.com/iniwex5/vohive/pkg/logger"
)

// Profile is the API-facing canonical SIM Profile DTO.
type Profile struct {
	ICCID       string                 `json:"iccid"`
	IMSI        string                 `json:"imsi,omitempty"`
	MSISDN      string                 `json:"msisdn,omitempty"`
	Name        string                 `json:"name,omitempty"`
	LocalPhone  string                 `json:"local_phone,omitempty"`
	Notes       string                 `json:"notes,omitempty"`
	Tags        string                 `json:"tags,omitempty"`
	ProfileType storage.SimProfileType `json:"profile_type"`
	FirstSeen   time.Time              `json:"first_seen_at,omitempty"`
	LastSeen    time.Time              `json:"last_seen_at,omitempty"`
}

type Service struct {
	store *storage.SQLiteStore
}

func NewService(store *storage.SQLiteStore) *Service {
	return &Service{store: store}
}

// Attach records the active subscription observed by device polling. EID is
// authoritative when present; otherwise the inserted card is physical.
func (s *Service) Attach(devices *device.Service) {
	devices.SetSimObserver(func(sim backend.SIMState, identity backend.Identity) {
		profileType := storage.SimProfilePhysical
		if strings.TrimSpace(sim.EID) != "" {
			profileType = storage.SimProfileESIM
		}
		if err := s.Observe(storage.SimProfileRecord{
			ICCID: sim.ICCID, IMSI: sim.IMSI, MSISDN: identity.MSISDN, ProfileType: profileType,
		}); err != nil {
			logger.Warn("sim profile observation failed", "iccid", sim.ICCID, "err", err)
		}
	})
}

func (s *Service) Observe(record storage.SimProfileRecord) error {
	if strings.TrimSpace(record.ICCID) == "" {
		return nil
	}
	return s.store.UpsertSimProfileObserved(record)
}

func (s *Service) ObserveESIM(profiles []backend.Profile) error {
	for _, profile := range profiles {
		if err := s.Observe(storage.SimProfileRecord{
			ICCID: profile.ICCID, MSISDN: profile.Phone, ProfileType: storage.SimProfileESIM,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) List(context.Context) ([]Profile, error) {
	records, err := s.store.ListSimProfiles()
	if err != nil {
		return nil, err
	}
	out := make([]Profile, 0, len(records))
	for _, record := range records {
		out = append(out, profileFromStorage(record))
	}
	return out, nil
}

func (s *Service) Create(_ context.Context, profile Profile) error {
	if err := validateProfile(profile.ICCID, profile.Name, profile.LocalPhone, profile.Notes, profile.Tags); err != nil {
		return err
	}
	if err := s.store.InsertSimProfile(storage.SimProfileRecord{
		ICCID: profile.ICCID, IMSI: strings.TrimSpace(profile.IMSI), MSISDN: strings.TrimSpace(profile.MSISDN),
		Name: strings.TrimSpace(profile.Name), LocalPhone: strings.TrimSpace(profile.LocalPhone),
		Notes: strings.TrimSpace(profile.Notes), Tags: strings.TrimSpace(profile.Tags), ProfileType: profile.ProfileType,
	}); err != nil {
		return derrors.New(derrors.OperationConflict, "sim profile already exists", false, map[string]any{"iccid": profile.ICCID})
	}
	return nil
}

func (s *Service) UpdateMeta(_ context.Context, iccid, name, localPhone, notes, tags string) error {
	if err := validateProfile(iccid, name, localPhone, notes, tags); err != nil {
		return err
	}
	updated, err := s.store.UpdateSimProfileMeta(iccid, name, localPhone, notes, tags)
	if err != nil {
		return err
	}
	if !updated {
		return derrors.New(derrors.NotFound, "sim profile not found", false, map[string]any{"iccid": strings.TrimSpace(iccid)})
	}
	return nil
}

func (s *Service) Delete(_ context.Context, iccid string) error {
	iccid = strings.TrimSpace(iccid)
	if iccid == "" || len(iccid) > 22 {
		return derrors.New(derrors.InvalidRequest, "iccid is invalid", false, nil)
	}
	deleted, err := s.store.DeleteSimProfile(iccid)
	if err != nil {
		return err
	}
	if !deleted {
		return derrors.New(derrors.NotFound, "sim profile not found", false, map[string]any{"iccid": iccid})
	}
	return nil
}

func validateProfile(iccid, name, localPhone, notes, tags string) error {
	iccid = strings.TrimSpace(iccid)
	if iccid == "" || len(iccid) > 22 {
		return derrors.New(derrors.InvalidRequest, "iccid is invalid", false, nil)
	}
	if len(strings.TrimSpace(name)) > 80 || len(strings.TrimSpace(localPhone)) > 80 ||
		len(strings.TrimSpace(notes)) > 1000 || len(strings.TrimSpace(tags)) > 200 {
		return derrors.New(derrors.InvalidRequest, "sim profile metadata is too long", false, nil)
	}
	return nil
}

func profileFromStorage(record storage.SimProfileRecord) Profile {
	return Profile{
		ICCID: record.ICCID, IMSI: record.IMSI, MSISDN: record.MSISDN, Name: record.Name,
		LocalPhone: record.LocalPhone, Notes: record.Notes, Tags: record.Tags,
		ProfileType: record.ProfileType, FirstSeen: record.FirstSeen, LastSeen: record.LastSeen,
	}
}
