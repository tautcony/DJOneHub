package simcards

import (
	"context"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/application/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/storage"
	"github.com/iniwex5/vohive/pkg/logger"
)

// Card 是面向 API 的 SIM 卡档案 DTO。
type Card struct {
	ICCID     string    `json:"iccid"`
	IMSI      string    `json:"imsi,omitempty"`
	MSISDN    string    `json:"msisdn,omitempty"`
	Name      string    `json:"name,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	FirstSeen time.Time `json:"first_seen_at,omitempty"`
	LastSeen  time.Time `json:"last_seen_at,omitempty"`
}

// Service 以 ICCID 为主键维护 SIM 卡档案。主体是用户对卡片名称/备注/号码的
// 增删改查；插卡自动记录（first/last_seen_at、IMSI/MSISDN 补录）是附属能力，
// 由 device 状态轮询经观察者回调驱动。
type Service struct {
	store *storage.SQLiteStore
}

func NewService(store *storage.SQLiteStore) *Service {
	return &Service{store: store}
}

// Attach 把插卡自动记录挂到设备状态轮询上：每次读到 ICCID 非空的 SIM 状态
// 即建档/更新最近见到时间，并补录 IMSI 与 MSISDN。
func (s *Service) Attach(devices *device.Service) {
	devices.SetSimObserver(func(sim backend.SIMState, identity backend.Identity) {
		iccid := strings.TrimSpace(sim.ICCID)
		if iccid == "" {
			return
		}
		if err := s.store.UpsertSimCardSeen(storage.SimCardRecord{
			ICCID:  iccid,
			IMSI:   sim.IMSI,
			MSISDN: identity.MSISDN,
		}); err != nil {
			logger.Warn("sim card auto-record failed", "iccid", iccid, "err", err)
		}
	})
}

func (s *Service) List(ctx context.Context) ([]Card, error) {
	records, err := s.store.ListSimCards()
	if err != nil {
		return nil, err
	}
	out := make([]Card, 0, len(records))
	for _, record := range records {
		out = append(out, cardFromStorage(record))
	}
	return out, nil
}

// Create 手动建档一张卡。ICCID 已存在时返回冲突错误（插卡已自动建档的卡
// 应走更新路径）。
func (s *Service) Create(ctx context.Context, iccid, imsi, msisdn, name, notes string) error {
	iccid = strings.TrimSpace(iccid)
	if iccid == "" {
		return derrors.New(derrors.InvalidRequest, "iccid is required", false, nil)
	}
	if err := s.store.InsertSimCard(storage.SimCardRecord{
		ICCID: iccid, IMSI: imsi, MSISDN: msisdn, Name: name, Notes: notes,
	}); err != nil {
		return derrors.New(derrors.OperationConflict, "sim card already exists", false, map[string]any{"iccid": iccid})
	}
	return nil
}

func (s *Service) UpdateMeta(ctx context.Context, iccid, name, notes, msisdn string) error {
	iccid = strings.TrimSpace(iccid)
	if iccid == "" {
		return derrors.New(derrors.InvalidRequest, "iccid is required", false, nil)
	}
	return s.store.UpdateSimCardMeta(iccid, name, notes, msisdn)
}

func (s *Service) Delete(ctx context.Context, iccid string) error {
	iccid = strings.TrimSpace(iccid)
	if iccid == "" {
		return derrors.New(derrors.InvalidRequest, "iccid is required", false, nil)
	}
	deleted, err := s.store.DeleteSimCard(iccid)
	if err != nil {
		return err
	}
	if !deleted {
		return derrors.New(derrors.NotFound, "sim card not found", false, map[string]any{"iccid": iccid})
	}
	return nil
}

func cardFromStorage(record storage.SimCardRecord) Card {
	return Card{
		ICCID:     record.ICCID,
		IMSI:      record.IMSI,
		MSISDN:    record.MSISDN,
		Name:      record.Name,
		Notes:     record.Notes,
		FirstSeen: record.FirstSeen,
		LastSeen:  record.LastSeen,
	}
}
