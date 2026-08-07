package esim

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
)

// confirmationCodeTimeout 是确认码请求的等待窗口；超时按用户取消处理。
const confirmationCodeTimeout = 5 * time.Minute

// confirmationReply 是确认码交互的回复。
type confirmationReply struct {
	Code     string
	Declined bool
}

type Service struct {
	devices *device.Service
	ops     *operation.Manager
	runtime *runtime.Runtime
	// store 为 nil 时通知历史不落库（测试/无存储环境）。
	store *storage.SQLiteStore

	confirmationMu sync.Mutex
	// operationID -> 确认码回复 channel；下载结束（成功/失败/取消）时删除。
	confirmationRequests map[string]chan confirmationReply
}

func NewService(devices *device.Service, ops *operation.Manager, runtime *runtime.Runtime, store ...*storage.SQLiteStore) *Service {
	var historyStore *storage.SQLiteStore
	if len(store) > 0 {
		historyStore = store[0]
	}
	return &Service{
		devices:              devices,
		ops:                  ops,
		runtime:              runtime,
		store:                historyStore,
		confirmationRequests: make(map[string]chan confirmationReply),
	}
}

func (s *Service) port(operationName string) (backend.ESIMPort, error) {
	b, err := s.devices.RequireCapability(domain.CapabilityESIM, operationName)
	if err != nil {
		return nil, err
	}
	port, ok := b.(backend.ESIMPort)
	if !ok {
		return nil, derrors.CapabilityMissing("esim", operationName, "the selected backend has no eSIM service port")
	}
	return port, nil
}

func (s *Service) Overview(ctx context.Context) (map[string]any, error) {
	port, err := s.port("esim_overview")
	if err != nil {
		return nil, err
	}
	eid, err := port.EID(ctx)
	if err != nil {
		if isEUICCUnavailableProbeError(err) {
			return map[string]any{
				"card_type":   "unknown",
				"profiles":    []backend.Profile{},
				"probe_error": err.Error(),
				"message":     "eUICC profile service was not readable",
			}, nil
		}
		return nil, err
	}
	profiles, err := port.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"card_type": "euicc", "eid": eid, "profiles": profiles}, nil
}

func isEUICCUnavailableProbeError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no euicc") ||
		(strings.Contains(message, "未发现任何 euicc") && strings.Contains(message, "at+ccho"))
}

// resolveActivationCode 归一化下载激活码输入：完整的 LPA:1$ 格式（二维码/粘贴）拆出
// SM-DP+ 地址与 matchingID；纯地址输入原样返回。协议解析保持在本层，不透传给端口。
func resolveActivationCode(activationCode, matchingID string) (smdp, resolvedMatchingID string) {
	smdp = strings.TrimSpace(activationCode)
	resolvedMatchingID = strings.TrimSpace(matchingID)
	if !strings.HasPrefix(smdp, "LPA:1") {
		return smdp, resolvedMatchingID
	}
	parts := strings.Split(smdp, "$")
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		return smdp, resolvedMatchingID
	}
	smdp = strings.TrimSpace(parts[1])
	if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
		// 激活码内嵌的 matchingID 是权威值，显式输入仅在激活码不含时生效。
		resolvedMatchingID = strings.TrimSpace(parts[2])
	}
	return smdp, resolvedMatchingID
}

func (s *Service) Download(ctx context.Context, activationCode, confirmationCode, matchingID string) (string, error) {
	port, err := s.port("esim_download")
	if err != nil {
		return "", err
	}
	smdp, resolvedMatchingID := resolveActivationCode(activationCode, matchingID)
	return s.ops.Start(ctx, "esim.download", func(taskCtx context.Context, operationID string, progress func(int, string)) error {
		release, err := s.runtime.Acquire(taskCtx, runtime.ResourceSIM)
		if err != nil {
			return err
		}
		defer release()
		progress(5, "preparing")
		replyCh := make(chan confirmationReply, 1)
		s.confirmationMu.Lock()
		s.confirmationRequests[operationID] = replyCh
		s.confirmationMu.Unlock()
		defer func() {
			s.confirmationMu.Lock()
			delete(s.confirmationRequests, operationID)
			s.confirmationMu.Unlock()
		}()
		opts := &backend.ESIMDownloadOptions{
			Progress: func(_ string, pct int, msg string) {
				progress(pct, msg)
			},
			ConfirmationCodeRequest: func() (string, bool, error) {
				s.ops.Publish("esim.confirmation_code_request", map[string]any{
					"operation_id": operationID,
					"message":      "SM-DP+ 要求输入确认码",
				})
				select {
				case reply := <-replyCh:
					if reply.Declined {
						return "", true, nil
					}
					return reply.Code, false, nil
				case <-taskCtx.Done():
					return "", true, nil
				case <-time.After(confirmationCodeTimeout):
					return "", true, nil
				}
			},
		}
		if err := port.Download(taskCtx, smdp, confirmationCode, resolvedMatchingID, opts); err != nil {
			return err
		}
		progress(100, "downloaded")
		s.ops.Publish("esim.updated", map[string]any{"operation": "download"})
		return nil
	})
}

// SubmitConfirmationCode 向运行中的下载操作提交确认码回复；declined 为 true 表示用户取消。
func (s *Service) SubmitConfirmationCode(operationID, code string, declined bool) error {
	s.confirmationMu.Lock()
	replyCh, ok := s.confirmationRequests[operationID]
	s.confirmationMu.Unlock()
	if !ok {
		return derrors.New(derrors.NotFound, "confirmation request is not waiting for input", false, nil)
	}
	select {
	case replyCh <- confirmationReply{Code: code, Declined: declined}:
		return nil
	default:
		return derrors.New(derrors.OperationConflict, "confirmation code was already submitted", false, nil)
	}
}

func (s *Service) Enable(ctx context.Context, iccid string) (string, error) {
	port, err := s.port("esim_enable")
	if err != nil {
		return "", err
	}
	return s.ops.Start(ctx, "esim.enable", func(taskCtx context.Context, _ string, progress func(int, string)) error {
		release, err := s.runtime.Acquire(taskCtx, runtime.ResourceSIM)
		if err != nil {
			return err
		}
		defer release()
		progress(10, "switching profile")
		if err := port.Enable(taskCtx, iccid); err != nil {
			return err
		}
		progress(100, "profile enabled")
		s.ops.Publish("esim.updated", map[string]any{"operation": "enable", "iccid": iccid})
		return nil
	})
}

func (s *Service) Disable(ctx context.Context, iccid string) (string, error) {
	port, err := s.port("esim_disable")
	if err != nil {
		return "", err
	}
	return s.ops.Start(ctx, "esim.disable", func(taskCtx context.Context, _ string, progress func(int, string)) error {
		release, err := s.runtime.Acquire(taskCtx, runtime.ResourceSIM)
		if err != nil {
			return err
		}
		defer release()
		progress(10, "disabling profile")
		if err := port.Disable(taskCtx, iccid); err != nil {
			return err
		}
		progress(100, "profile disabled")
		s.ops.Publish("esim.updated", map[string]any{"operation": "disable", "iccid": iccid})
		return nil
	})
}

func (s *Service) Rename(ctx context.Context, iccid, label string) error {
	port, err := s.port("esim_rename")
	if err != nil {
		return err
	}
	if err := port.Rename(ctx, iccid, label); err != nil {
		return err
	}
	s.ops.Publish("esim.updated", map[string]any{"operation": "rename", "iccid": iccid})
	return nil
}

func (s *Service) Delete(ctx context.Context, iccid string) (string, error) {
	port, err := s.port("esim_delete")
	if err != nil {
		return "", err
	}
	profiles, err := port.Profiles(ctx)
	if err != nil {
		return "", err
	}
	for _, profile := range profiles {
		if profile.ICCID == iccid && isBootstrapProfile(profile) {
			return "", derrors.New(derrors.InvalidRequest, "bootstrap profile cannot be deleted", false, map[string]any{
				"profile_class": profile.ProfileClass,
			})
		}
	}
	return s.ops.Start(ctx, "esim.delete", func(taskCtx context.Context, _ string, progress func(int, string)) error {
		release, err := s.runtime.Acquire(taskCtx, runtime.ResourceSIM)
		if err != nil {
			return err
		}
		defer release()
		progress(10, "deleting profile")
		if err := port.Delete(taskCtx, iccid); err != nil {
			return err
		}
		progress(100, "profile deleted")
		s.ops.Publish("esim.updated", map[string]any{"operation": "delete", "iccid": iccid})
		return nil
	})
}

func isBootstrapProfile(profile backend.Profile) bool {
	for _, value := range []string{profile.ProfileClass, profile.Label, profile.ServiceProviderName} {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "bootstrap" || normalized == "provisioning" || normalized == "bootstrap profile" {
			return true
		}
	}
	return false
}

func (s *Service) ListNotifications(ctx context.Context) ([]backend.NotificationItem, error) {
	port, err := s.port("esim_notifications")
	if err != nil {
		return nil, err
	}
	items, err := port.ListNotifications(ctx)
	if err != nil {
		return nil, err
	}
	s.syncNotificationHistory(items)
	return items, nil
}

func (s *Service) ProcessNotification(ctx context.Context, sequenceNumber int64) error {
	port, err := s.port("esim_notifications")
	if err != nil {
		return err
	}
	if err := port.ProcessNotification(ctx, sequenceNumber); err != nil {
		s.recordNotificationState(sequenceNumber, storage.NotificationStateFailed)
		return err
	}
	s.recordNotificationState(sequenceNumber, storage.NotificationStateProcessed)
	return nil
}

func (s *Service) RemoveNotification(ctx context.Context, sequenceNumber int64) error {
	port, err := s.port("esim_notifications")
	if err != nil {
		return err
	}
	if err := port.RemoveNotification(ctx, sequenceNumber); err != nil {
		s.recordNotificationState(sequenceNumber, storage.NotificationStateFailed)
		return err
	}
	s.recordNotificationState(sequenceNumber, storage.NotificationStateRemoved)
	return nil
}

// NotificationHistory 返回本地持久化的通知处置历史（含已被清理的记录）。
func (s *Service) NotificationHistory(ctx context.Context, limit int) ([]storage.NotificationHistoryRecord, error) {
	if s.store == nil {
		return []storage.NotificationHistoryRecord{}, nil
	}
	return s.store.ListNotificationHistory(limit)
}

// syncNotificationHistory 以一次卡片快照驱动历史落库：快照内的通知 upsert 为
// pending（首次观察），之前 pending 但已不在卡片上的记录标记为 processed
// （自动清理已处置）。
func (s *Service) syncNotificationHistory(items []backend.NotificationItem) {
	if s.store == nil {
		return
	}
	current := make([]storage.NotificationHistoryRecord, 0, len(items))
	for _, item := range items {
		record := storage.NotificationHistoryRecord{
			SequenceNumber: item.SequenceNumber,
			Event:          item.Event,
			ICCID:          item.ICCID,
			Address:        item.Address,
			State:          storage.NotificationStatePending,
		}
		current = append(current, record)
		if err := s.store.UpsertNotificationHistory(record); err != nil {
			log.Printf("esim notification history upsert failed: %v", err)
		}
	}
	if err := s.store.MarkNotificationHistoryAbsent(current); err != nil {
		log.Printf("esim notification history sync failed: %v", err)
	}
}

// recordNotificationState 按卡片序号把已观察的历史记录标记为指定状态。通知
// 元数据只来自待处理快照，因此不能用缺少 event 的记录创建新的历史条目。
func (s *Service) recordNotificationState(sequenceNumber int64, state storage.NotificationHistoryState) {
	if s.store == nil || sequenceNumber <= 0 {
		return
	}
	if err := s.store.UpdateNotificationHistoryState(sequenceNumber, state); err != nil {
		log.Printf("esim notification history state update failed: %v", err)
	}
}
