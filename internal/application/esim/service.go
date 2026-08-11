package esim

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/application/simprofiles"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
	"golang.org/x/sync/singleflight"
)

// confirmationCodeTimeout 是确认码请求的等待窗口；超时按用户取消处理。
const confirmationCodeTimeout = 5 * time.Minute

// confirmationReply 是确认码交互的回复。
type confirmationReply struct {
	Code     string
	Declined bool
}

// voWiFiSwitcher 是切卡联动的最小接口，由 vowifi.Service 实现
// （SwitchBegin/SwitchEnd 透传到 vowifihost.Manager）。
type voWiFiSwitcher interface {
	// SwitchBegin 切卡前抢占拆除进行中的 VoWiFi 实例。
	SwitchBegin(context.Context) error
	// SwitchEnd 切卡成功后以 AllowSwitch 恢复 VoWiFi（RestoreRadio=true）。
	SwitchEnd(context.Context, bool) error
}

type Service struct {
	devices *device.Service
	ops     *operation.Manager
	runtime *runtime.Runtime
	// store 为 nil 时通知历史不落库（测试/无存储环境）。
	store    *storage.SQLiteStore
	profiles *simprofiles.Service
	// vowifi 是切卡联动控制器；nil 时切卡不影响 VoWiFi（默认行为）。
	vowifi voWiFiSwitcher

	confirmationMu sync.Mutex
	// operationID -> 确认码回复 channel；下载结束（成功/失败/取消）时删除。
	confirmationRequests map[string]chan confirmationReply

	overviewMu         sync.RWMutex
	overviewCache      map[string]any
	overviewGeneration uint64
	overviewEpoch      uint64
	overviewLoadedAt   time.Time
	overviewFlight     singleflight.Group
}

func (s *Service) SetProfileRegistry(profiles *simprofiles.Service) {
	s.profiles = profiles
}

// SetVoWiFiSwitcher 注入切卡联动控制器（上游 pool_esim_switch.go 模式）。
func (s *Service) SetVoWiFiSwitcher(v voWiFiSwitcher) {
	s.vowifi = v
}

// switchBegin / switchEnd 是切卡联动辅助：失败只记日志不阻断切卡
// （上游 SwitchBegin 失败 warn 后继续的语义）。成功切换后调用
// SwitchEnd 恢复 VoWiFi；切卡失败时保持拆除状态，不自动恢复。
func (s *Service) switchBegin(ctx context.Context, iccid string) {
	if s.vowifi == nil {
		return
	}
	if err := s.vowifi.SwitchBegin(ctx); err != nil {
		log.Printf("esim switch begin (VoWiFi teardown) failed: %v", err)
		return
	}
	log.Printf("esim switch: VoWiFi teardown done before switching profile %s", iccid)
}

func (s *Service) switchEnd(ctx context.Context, iccid string) {
	if s.vowifi == nil {
		return
	}
	if err := s.vowifi.SwitchEnd(ctx, true); err != nil {
		log.Printf("esim switch end (VoWiFi restore) failed: %v", err)
		return
	}
	log.Printf("esim switch: VoWiFi restored after profile %s", iccid)
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

const overviewSnapshotTTL = 10 * time.Second

func cloneOverviewResult(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		if profiles, ok := item.([]backend.Profile); ok {
			out[key] = append([]backend.Profile(nil), profiles...)
			continue
		}
		out[key] = item
	}
	return out
}

func (s *Service) cachedOverview(generation uint64) map[string]any {
	s.overviewMu.RLock()
	defer s.overviewMu.RUnlock()
	if s.overviewCache == nil || s.overviewGeneration != generation || time.Since(s.overviewLoadedAt) >= overviewSnapshotTTL {
		return nil
	}
	return cloneOverviewResult(s.overviewCache)
}

func (s *Service) invalidateOverviewCache() {
	s.overviewMu.Lock()
	s.overviewEpoch++
	s.overviewCache = nil
	s.overviewLoadedAt = time.Time{}
	s.overviewMu.Unlock()
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

// Overview returns the eSIM snapshot. The AT snapshot path uses AT+CSIM to
// probe EF_DIR, AT+CCHO to open each candidate AID, AT+CGLA for eUICC APDUs,
// and AT+CCHC to close channels. The fallback path can perform a lightweight
// profile scan and a second full chip-information scan.
func (s *Service) Overview(ctx context.Context) (map[string]any, error) {
	port, err := s.port("esim_overview")
	if err != nil {
		return nil, err
	}
	if snapshotPort, ok := port.(backend.ESIMSnapshotPort); ok {
		generation := uint64(0)
		if s.runtime != nil {
			generation = s.runtime.Snapshot().Generation
		}
		if cached := s.cachedOverview(generation); cached != nil {
			return cached, nil
		}
		s.overviewMu.RLock()
		epoch := s.overviewEpoch
		s.overviewMu.RUnlock()
		value, snapshotErr, _ := s.overviewFlight.Do(fmt.Sprintf("%d:%d", generation, epoch), func() (any, error) {
			if cached := s.cachedOverview(generation); cached != nil {
				return cached, nil
			}
			snapshot, err := snapshotPort.ESIMSnapshot(ctx)
			if err != nil {
				return nil, err
			}
			result := map[string]any{"card_type": "euicc", "eid": snapshot.EID, "profiles": snapshot.Profiles,
				"free_nvram_bytes": snapshot.Storage.FreeNvramBytes, "free_nvram": snapshot.Storage.FreeNvram,
				"device_info": snapshot.DeviceInfo}
			if s.profiles != nil {
				if observeErr := s.profiles.ObserveESIM(snapshot.Profiles); observeErr != nil {
					log.Printf("observe eSIM profiles: %v", observeErr)
				}
			}
			s.overviewMu.Lock()
			generationCurrent := s.runtime == nil || s.runtime.Snapshot().Generation == generation
			if s.overviewEpoch == epoch && generationCurrent {
				s.overviewCache = cloneOverviewResult(result)
				s.overviewGeneration = generation
				s.overviewLoadedAt = time.Now()
			}
			s.overviewMu.Unlock()
			return result, nil
		})
		if snapshotErr == nil && value != nil {
			return cloneOverviewResult(value.(map[string]any)), nil
		}
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
	if s.profiles != nil {
		if err := s.profiles.ObserveESIM(profiles); err != nil {
			log.Printf("observe eSIM profiles: %v", err)
		}
	}
	result := map[string]any{"card_type": "euicc", "eid": eid, "profiles": profiles}
	if storagePort, ok := port.(backend.ESIMStoragePort); ok {
		if storage, storageErr := storagePort.ESIMStorage(ctx); storageErr == nil {
			result["free_nvram_bytes"] = storage.FreeNvramBytes
			result["free_nvram"] = storage.FreeNvram
		}
	}
	if deviceInfoPort, ok := port.(backend.ESIMDeviceInfoPort); ok {
		if deviceInfo, deviceInfoErr := deviceInfoPort.ESIMDeviceInfo(ctx); deviceInfoErr == nil {
			result["device_info"] = deviceInfo
		}
	}
	return result, nil
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
		s.invalidateOverviewCache()
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
		// 切卡联动：切卡前抢占拆除 VoWiFi 实例（IsSwitching 门控在 op 存活期间
		// 保持拒绝新启用）。锁域说明：本 op 持 ResourceSIM；SwitchBegin/End 是
		// vowifi lifecycle 命令，不获取 runtime 资源，不会死锁。
		s.switchBegin(taskCtx, iccid)
		progress(10, "switching profile")
		if err := port.Enable(taskCtx, iccid); err != nil {
			return err
		}
		s.switchEnd(taskCtx, iccid)
		progress(100, "profile enabled")
		s.invalidateOverviewCache()
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
		// 切卡联动：同 Enable（锁域说明见上）。
		s.switchBegin(taskCtx, iccid)
		progress(10, "disabling profile")
		if err := port.Disable(taskCtx, iccid); err != nil {
			return err
		}
		s.switchEnd(taskCtx, iccid)
		progress(100, "profile disabled")
		s.invalidateOverviewCache()
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
	s.invalidateOverviewCache()
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
		s.invalidateOverviewCache()
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

// ListNotifications discovers the notification eUICC when needed and reads
// its notification records. The AT path uses AT+CSIM for card probing and
// AT+CCHO/AT+CGLA/AT+CCHC for AID selection, APDU reads, and channel cleanup.
// An invalid discovered target can trigger a full static AID rescan.
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
