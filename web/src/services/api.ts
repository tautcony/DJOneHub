import type {
  CallStatus,
  DeviceStatus,
  EsimHealth,
  EsimNotification,
  EsimNotificationHistory,
  EsimOverview,
  NetworkTrafficDaily,
  NetworkTrafficRange,
  NetworkStatus,
  NotificationChannelsResponse,
  NotificationChannelTestResponse,
  NotificationChannelsSettings,
  NotificationDebugInfo,
  NotificationDebugRequest,
  NotificationDebugResponse,
  NotificationPermissionStatus,
  NotificationPreferences,
  NotificationPreferencesResponse,
  SimProfile,
  StartupStatus,
  OperationStatus,
  SMSMessage,
  SMSStorageUsage,
  VowifiStatus,
  DeviceControlStatus,
  DeviceControlSettings,
  RuntimeDiagnostics,
  UpstreamProxy,
  UpstreamProxyCountryRule,
  CardPolicy,
  CountryTableStatus,
} from '../types'
import type { RawATSMSDiagnostic } from './at'

const base = '/api/v1'

export class APIError extends Error {
  readonly code: string
  readonly details?: Record<string, unknown>

  constructor(code: string, message: string, details?: Record<string, unknown>) {
    super(message)
    this.name = 'APIError'
    this.code = code
    this.details = details
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${base}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init?.headers || {}) },
  })
  const body = await response.json().catch(() => ({}))
  if (!response.ok) {
    throw new APIError(
      body?.error?.code || 'internal_error',
      body?.error?.message || `Request failed (${response.status})`,
      body?.error?.details,
    )
  }
  return body as T
}

export const api = {
  runtimeDiagnostics: () => request<RuntimeDiagnostics>('/runtime/diagnostics'),
  status: () => request<DeviceStatus>('/device/status'),
  capabilities: () =>
    request<{ backend?: string; backend_reason?: string; capabilities: Record<string, string> }>(
      '/device/capabilities',
    ),
  rescan: () => request<{ state: unknown }>('/device/actions/rescan', { method: 'POST' }),
  reboot: () => request<{ accepted: boolean }>('/device/actions/reboot', { method: 'POST' }),
  smsRefresh: () =>
    request<{ items: SMSMessage[]; storage?: SMSStorageUsage[] }>('/sms/actions/refresh', { method: 'POST' }),
  smsClear: () => request<{ state: string }>('/sms/actions/clear', { method: 'POST' }),
  sendSMS: (to: string, body: string) =>
    request<{ operation_id: string }>('/sms/actions/send', {
      method: 'POST',
      body: JSON.stringify({ to, body }),
    }),
  esim: () => request<EsimOverview>('/esim'),
  esimDownload: (activation_code: string, confirmation_code: string, matching_id: string) =>
    request<{ operation_id: string }>('/esim/actions/download', {
      method: 'POST',
      body: JSON.stringify({ activation_code, confirmation_code, matching_id }),
    }),
  esimEnable: (iccid: string) =>
    request<{ operation_id: string }>('/esim/actions/enable', {
      method: 'POST',
      body: JSON.stringify({ iccid }),
    }),
  esimRename: (iccid: string, label: string) =>
    request<{ state: string }>('/esim/actions/rename', {
      method: 'POST',
      body: JSON.stringify({ iccid, label }),
    }),
  esimDelete: (iccid: string) =>
    request<{ operation_id: string }>('/esim/actions/delete', {
      method: 'POST',
      body: JSON.stringify({ iccid }),
    }),
  esimDisable: (iccid: string) =>
    request<{ operation_id: string }>('/esim/actions/disable', {
      method: 'POST',
      body: JSON.stringify({ iccid }),
    }),
  esimNotifications: () => request<{ notifications: EsimNotification[] }>('/esim/notifications'),
  esimNotificationHistory: () =>
    request<{ history: EsimNotificationHistory[] }>('/esim/notifications/history'),
  esimProcessNotification: (sequence: number) =>
    request<{ state: string }>(`/esim/notifications/${sequence}/process`, {
      method: 'POST',
      body: '{}',
    }),
  esimRemoveNotification: (sequence: number) =>
    request<{ state: string }>(`/esim/notifications/${sequence}`, { method: 'DELETE' }),
  simProfiles: () => request<{ profiles: SimProfile[] }>('/sim-profiles'),
  simProfileCreate: (profile: Omit<SimProfile, 'first_seen_at' | 'last_seen_at'>) =>
    request<{ state: string }>('/sim-profiles', {
      method: 'POST',
      body: JSON.stringify(profile),
    }),
  simProfileUpdate: (iccid: string, metadata: Pick<SimProfile, 'name' | 'local_phone' | 'notes' | 'tags'>) =>
    request<{ state: string }>(`/sim-profiles/${encodeURIComponent(iccid)}`, {
      method: 'PUT',
      body: JSON.stringify(metadata),
    }),
  simProfileDelete: (iccid: string) =>
    request<{ state: string }>(`/sim-profiles/${encodeURIComponent(iccid)}`, { method: 'DELETE' }),
  esimConfirmationCode: (operationID: string, code: string, declined: boolean) =>
    request<{ state: string }>(`/esim/operations/${encodeURIComponent(operationID)}/confirmation-code`, {
      method: 'POST',
      body: JSON.stringify({ code, declined }),
    }),
  network: () => request<NetworkStatus>('/network'),
  networkTrafficDaily: (date?: string) =>
    request<NetworkTrafficDaily>(`/network/traffic/daily${date ? `?date=${encodeURIComponent(date)}` : ''}`),
  networkTrafficRange: (period: 'day' | 'week' | 'month') =>
    request<NetworkTrafficRange>(`/network/traffic/range?range=${encodeURIComponent(period)}`),
  networkMode: (mode: string) =>
    request<{ operation_id: string }>('/network/actions/mode', {
      method: 'POST',
      body: JSON.stringify({ mode }),
    }),
  networkCheck: () =>
    request<{ ok: boolean; summary: string; detail?: string }>('/network/actions/check', { method: 'POST' }),
  rawAT: (command: string) =>
    request<{ response: string; sms_messages?: RawATSMSDiagnostic[] }>('/device/actions/raw-at', {
      method: 'POST',
      body: JSON.stringify({ command }),
    }),
  deviceControl: () => request<DeviceControlStatus>('/device-control'),
  deviceControlADBUnlock: () =>
    request<{ operation_id: string }>('/device-control/actions/adb-unlock', { method: 'POST' }),
  deviceControlADBMode: (enabled: boolean) =>
    request<{ operation_id: string }>('/device-control/actions/adb-mode', {
      method: 'POST',
      body: JSON.stringify({ enabled }),
    }),
  deviceControlADBReboot: (serial: string) =>
    request<{ operation_id: string }>('/device-control/actions/adb/reboot', {
      method: 'POST',
      body: JSON.stringify({ serial }),
    }),
  deviceControlUSBID: (vid: string, pid: string) =>
    request<{ operation_id: string }>('/device-control/actions/usb-id', {
      method: 'POST',
      body: JSON.stringify({ vid, pid }),
    }),
  deviceControlEDL: (serial: string, method: 'direct' | 'adb' = 'direct') =>
    request<{ operation_id: string }>('/device-control/actions/edl', {
      method: 'POST',
      body: JSON.stringify({ serial, method }),
    }),
  deviceControlReset: () =>
    request<{ operation_id: string }>('/device-control/actions/reset', { method: 'POST' }),
  deviceControlBackup: (
    output_path: string,
    loader_path: string,
    edl_path: string,
    edl_runner: 'python' | 'uv',
  ) =>
    request<{ operation_id: string }>('/device-control/actions/nand-backup', {
      method: 'POST',
      body: JSON.stringify({ output_path, loader_path, edl_path, edl_runner }),
    }),
  selectDeviceControlBackupDirectory: () =>
    request<{ directory: string }>('/device-control/actions/select-backup-directory', { method: 'POST' }),
  selectDeviceControlEDLDirectory: () =>
    request<{ directory: string }>('/device-control/actions/select-edl-directory', { method: 'POST' }),
  selectDeviceControlADBFile: () =>
    request<{ path: string }>('/device-control/actions/select-adb-file', { method: 'POST' }),
  selectDeviceControlLoaderFile: () =>
    request<{ path: string }>('/device-control/actions/select-loader-file', { method: 'POST' }),
  saveDeviceControlSettings: (settings: DeviceControlSettings) =>
    request<DeviceControlSettings>('/device-control/settings', {
      method: 'POST',
      body: JSON.stringify(settings),
    }),
  vowifi: () => request<VowifiStatus>('/vowifi'),
  vowifiEnable: () => request<{ operation_id: string }>('/vowifi/actions/enable', { method: 'POST' }),
  vowifiDisable: () => request<{ operation_id: string }>('/vowifi/actions/disable', { method: 'POST' }),
  vowifiReconnect: () => request<{ operation_id: string }>('/vowifi/actions/reconnect', { method: 'POST' }),
  vowifiProxies: () => request<UpstreamProxy[]>('/vowifi/proxies'),
  vowifiProxyUpsert: (proxy: UpstreamProxy) =>
    request<{ ok: boolean }>('/vowifi/proxies', { method: 'POST', body: JSON.stringify(proxy) }),
  vowifiProxyDelete: (id: string) =>
    request<{ ok: boolean }>(`/vowifi/proxies?id=${encodeURIComponent(id)}`, { method: 'DELETE' }),
  vowifiCountryRules: () => request<UpstreamProxyCountryRule[]>('/vowifi/proxy-country-rules'),
  vowifiCountryRuleUpsert: (rule: UpstreamProxyCountryRule) =>
    request<{ ok: boolean }>('/vowifi/proxy-country-rules', { method: 'POST', body: JSON.stringify(rule) }),
  vowifiCountryRuleDelete: (countryCode: string) =>
    request<{ ok: boolean }>(`/vowifi/proxy-country-rules?country_code=${encodeURIComponent(countryCode)}`, {
      method: 'DELETE',
    }),
  vowifiCountryTable: () => request<CountryTableStatus>('/vowifi/country-table'),
  vowifiCardPolicies: () => request<CardPolicy[]>('/vowifi/card-policies'),
  vowifiCardPolicySet: (iccid: string, vowifiEnabled: boolean) =>
    request<{ ok: boolean }>(`/vowifi/card-policies?iccid=${encodeURIComponent(iccid)}`, {
      method: 'PUT',
      body: JSON.stringify({ vowifi_enabled: vowifiEnabled }),
    }),
  notificationDebugInfo: () => request<NotificationDebugInfo>('/notifications/debug'),
  notificationDebug: (payload: NotificationDebugRequest) =>
    request<NotificationDebugResponse>('/notifications/debug', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  notificationPermissions: () => request<NotificationPermissionStatus>('/notifications/permissions'),
  requestNotificationPermission: () =>
    request<NotificationPermissionStatus>('/notifications/permissions/request', { method: 'POST' }),
  openNotificationSettings: () =>
    request<NotificationPermissionStatus>('/notifications/permissions/open-settings', { method: 'POST' }),
  notificationPreferences: () => request<NotificationPreferencesResponse>('/notifications/preferences'),
  updateNotificationPreferences: (preferences: NotificationPreferences) =>
    request<NotificationPreferencesResponse>('/notifications/preferences', {
      method: 'PUT',
      body: JSON.stringify(preferences),
    }),
  startupSettings: () => request<StartupStatus>('/settings/startup'),
  updateStartupSettings: (enabled: boolean) =>
    request<StartupStatus>('/settings/startup', {
      method: 'PUT',
      body: JSON.stringify({ enabled }),
    }),
  notificationChannels: () => request<NotificationChannelsResponse>('/notifications/channels'),
  updateNotificationChannels: (settings: NotificationChannelsSettings) =>
    request<NotificationChannelsResponse>('/notifications/channels', {
      method: 'PUT',
      body: JSON.stringify(settings),
    }),
  testNotificationChannel: (channel: string, probe: NotificationChannelsSettings) =>
    request<NotificationChannelTestResponse>('/notifications/channels/actions/test', {
      method: 'POST',
      body: JSON.stringify({ channel, probe }),
    }),
  discoverTelegramChatIDs: (settings: NotificationChannelsSettings['telegram']) =>
    request<{ chat_ids: number[] }>('/notifications/channels/telegram/chat-ids', {
      method: 'POST',
      body: JSON.stringify(settings),
    }),
  operation: (id: string) => request<OperationStatus>(`/operations/${encodeURIComponent(id)}`),
  calls: () => request<CallStatus>('/calls'),
  dialCall: (number: string) =>
    request<{ dialed: boolean }>('/calls/actions/dial', { method: 'POST', body: JSON.stringify({ number }) }),
  rejectCall: () => request<{ rejected: boolean }>('/calls/actions/reject', { method: 'POST' }),
  esimHealth: () => request<EsimHealth>('/esim/health'),
}
