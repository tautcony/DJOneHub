import type {
  CallStatus,
  DeviceStatus,
  EsimOverview,
  NetworkTrafficDaily,
  NetworkTrafficRange,
  NetworkStatus,
  NotificationDebugInfo,
  NotificationDebugRequest,
  NotificationDebugResponse,
  NotificationPermissionStatus,
  NotificationPreferences,
  NotificationPreferencesResponse,
  StartupStatus,
  OperationStatus,
  SMSMessage,
  VowifiStatus,
} from '../types'

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
    headers: { 'Content-Type': 'application/json', ...(init?.headers || {}) },
    ...init,
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
  status: () => request<DeviceStatus>('/device/status'),
  capabilities: () =>
    request<{ backend?: string; backend_reason?: string; capabilities: Record<string, string> }>(
      '/device/capabilities',
    ),
  rescan: () => request<{ state: unknown }>('/device/actions/rescan', { method: 'POST' }),
  reboot: () => request<{ accepted: boolean }>('/device/actions/reboot', { method: 'POST' }),
  smsRefresh: () => request<{ items: SMSMessage[] }>('/sms/actions/refresh', { method: 'POST' }),
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
    request<{ response: string }>('/device/actions/raw-at', {
      method: 'POST',
      body: JSON.stringify({ command }),
    }),
  vowifi: () => request<VowifiStatus>('/vowifi'),
  vowifiEnable: () => request<{ operation_id: string }>('/vowifi/actions/enable', { method: 'POST' }),
  vowifiDisable: () => request<{ operation_id: string }>('/vowifi/actions/disable', { method: 'POST' }),
  vowifiReconnect: () => request<{ operation_id: string }>('/vowifi/actions/reconnect', { method: 'POST' }),
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
  operation: (id: string) => request<OperationStatus>(`/operations/${encodeURIComponent(id)}`),
  calls: () => request<CallStatus>('/calls'),
  rejectCall: () => request<{ rejected: boolean }>('/calls/actions/reject', { method: 'POST' }),
  esimHealth: () => request<Record<string, unknown>>('/esim/health'),
  esimNotes: () =>
    request<{ notes: Record<string, { label: string; phone: string; tags: string }> }>('/esim/notes'),
  saveEsimNote: (iccid: string, note: { label: string; phone: string; tags: string }) =>
    request<{ state: string }>('/esim/notes', { method: 'PUT', body: JSON.stringify({ iccid, ...note }) }),
}
