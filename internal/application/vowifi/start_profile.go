package vowifi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

// 本文件移植自 vohive-open internal/device（vowifi_start_profile.go）：
// 基于实时 IMSI 构建 VoWiFi 启动画像（identity.Profile）。

// buildVoWiFiStartProfile 基于实时 IMSI 构建 VoWiFi 启动画像。
// db 为协议后端（QMI/MBIM 的 GetIMSI/GetNativeMCCMNC 是实时的），
// m 为 AT 路径的 modem.Manager（用于 SMSC 查询，可为 nil），
// status 为当前设备状态（用于回退已缓存的归属 MCC/MNC）。
func buildVoWiFiStartProfile(db voWiFiBackend, m *modem.Manager, status *modem.DeviceStatus, traceID string) (identity.Profile, error) {
	if db == nil {
		return identity.Profile{}, fmt.Errorf("backend_not_available")
	}

	liveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	imsi, err := db.GetIMSI(liveCtx)
	if err != nil {
		return identity.Profile{}, fmt.Errorf("实时读取 IMSI 失败: %w", err)
	}
	imsi = strings.TrimSpace(imsi)
	if imsi == "" {
		return identity.Profile{}, fmt.Errorf("实时 IMSI 为空")
	}

	mcc, mnc, plmnSource := resolveVoWiFiProfileMCCMNC(liveCtx, db, status, imsi)
	if mcc == "" || mnc == "" {
		return identity.Profile{}, fmt.Errorf("缺少 SIM 归属 MCC/MNC，无法构建 VoWiFi 启动画像: %s", imsi)
	}

	imei := ""
	iccid := ""
	if status != nil {
		imei = strings.TrimSpace(status.IMEI)
		iccid = strings.TrimSpace(status.ICCID)
	}

	smsc := ""
	if m != nil {
		if v, smscErr := m.QuerySMSC(); smscErr == nil {
			smsc = strings.TrimSpace(v)
		}
	}
	switch {
	case smsc != "":
		logger.Info("VoWiFi 启动前获取 SMSC 成功", "trace_id", traceID, "smsc", smsc)
	default:
		logger.Warn("VoWiFi 启动前未获取到 SMSC，将以空 SMSC 继续启动", "trace_id", traceID)
	}

	logger.Info("VoWiFi 启动画像将基于实时 IMSI 构建",
		"trace_id", traceID,
		"source", "live_imsi",
		"plmn_source", plmnSource,
		"iccid", iccid,
		"imsi", imsi,
		"mcc", mcc,
		"mnc", mnc,
		"imei", imei)

	return buildVoWiFiRawProfile(imsi, mcc, mnc, imei, smsc), nil
}

func buildVoWiFiRawProfile(imsi, mcc, mnc, imei, smsc string) identity.Profile {
	return identity.Profile{
		IMSI: strings.TrimSpace(imsi),
		MCC:  strings.TrimSpace(mcc),
		MNC:  strings.TrimSpace(mnc),
		IMEI: strings.TrimSpace(imei),
		SMSC: strings.TrimSpace(smsc),
	}
}

func resolveVoWiFiProfileMCCMNC(ctx context.Context, db voWiFiBackend, status *modem.DeviceStatus, imsi string) (mcc, mnc, source string) {
	imsi = strings.TrimSpace(imsi)
	if db != nil {
		if liveMCC, liveMNC, err := db.GetNativeMCCMNC(ctx); err == nil {
			liveMCC = strings.TrimSpace(liveMCC)
			liveMNC = strings.TrimSpace(liveMNC)
			if liveMCC != "" && liveMNC != "" {
				return liveMCC, liveMNC, "sim_home"
			}
		}
	}

	if status != nil {
		statusIMSI := strings.TrimSpace(status.IMSI)
		if statusIMSI == imsi && strings.TrimSpace(status.NativeMCC) != "" && strings.TrimSpace(status.NativeMNC) != "" {
			return strings.TrimSpace(status.NativeMCC), strings.TrimSpace(status.NativeMNC), "sim_home_cache"
		}
	}

	return "", "", ""
}

func newVoWiFiSIMReadyStartupState(deviceID, dataplaneMode, networkMode string, now time.Time) runtimehost.State {
	return runtimehost.State{
		Phase:         runtimehost.PhaseSIMReady,
		DeviceID:      deviceID,
		DataplaneMode: dataplaneMode,
		NetworkMode:   strings.TrimSpace(networkMode),
		SIMReady:      true,
		LastReason:    "sim_ready",
		UpdatedAt:     now,
	}
}
