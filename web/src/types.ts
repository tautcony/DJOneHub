export type DeviceState =
  'absent' | 'discovered' | 'connecting' | 'initializing' | 'ready' | 'degraded' | 'disconnected'

export type Capability = string

export interface Snapshot {
  state: DeviceState
  identity: { stable_id: string; imei?: string; serial_number?: string; product?: string }
  backend?: string
  backend_reason?: string
  capabilities: Record<Capability, string>
  last_error?: string
  generation: number
}

export interface DeviceStatus {
  snapshot: Snapshot
  identity: { imei?: string; imsi?: string; iccid?: string; msisdn?: string; firmware?: string }
  radio: {
    registered: boolean
    operator?: string
    network_mode?: string
    radio_band?: string
    signal_dbm?: number
    signal_rsrp?: number
    signal_rsrq?: number
    signal_sinr?: number
  }
  sim: { inserted: boolean; imsi?: string; iccid?: string; eid?: string }
}

export interface OperationStatus {
  operation_id: string
  type: string
  state: 'pending' | 'running' | 'succeeded' | 'failed' | 'cancelled'
  progress: number
  message?: string
  error?: { code: string; message: string; retryable: boolean; details?: Record<string, unknown> }
}

export interface OperationLog {
  operation_id: string
  type: string
  message: string
}

export interface SMSMessage {
  index: number
  sender?: string
  recipient?: string
  body: string
  received_at?: string
  recorded_at?: string
  iccid?: string
  concat_ref?: number
  part_number?: number
  total_parts?: number
}

export interface EsimOverview {
  card_type?: 'physical_sim' | 'euicc' | 'unknown'
  eid?: string
  probe_error?: string
  message?: string
  profiles: Array<{
    iccid?: string
    state?: 'enabled' | 'disabled' | 'unknown'
    state_code?: number
    state_known?: boolean
    label?: string
    phone?: string
    eid?: string
    aid?: string
    service_provider_name?: string
    profile_class?: string
  }>
}

export interface NetworkStatus {
  mode?: string
  network_mode?: string
  radio_band?: string
  interface?: string
  addresses?: string[]
  default_route?: string
  system_default_route?: string
  rx_bytes: number
  tx_bytes: number
}

export interface NetworkTrafficUpdate {
  rx_bytes: number
  tx_bytes: number
  daily_rx_bytes: number
  daily_tx_bytes: number
  daily_available: boolean
  sampled_at: string
}

export interface NetworkTrafficDaily {
  date: string
  rx_bytes: number
  tx_bytes: number
  sampled_at?: string
  available: boolean
}

export interface NetworkTrafficRange {
  range: 'day' | 'week' | 'month'
  start_date: string
  end_date: string
  items: Array<{ date: string; rx_bytes: number; tx_bytes: number }>
  available: boolean
}

export interface VowifiStatus {
  available?: boolean
  state?: string
  reason?: string
  [key: string]: unknown
}

export interface FirmwareStatus {
  available: boolean
  manufacturer?: string
  model?: string
  firmware?: string
  adb_key_serial?: string
  usb_config?: string
  usb_config_fields?: Array<{ index: number; key: string; value: string }>
  usb_id?: string
  usb_vid?: string
  usb_pid?: string
  mode: 'normal' | 'adb' | 'edl' | 'unknown' | string
  mode_reason?: string
  adb: {
    enabled: boolean
    enabled_known: boolean
    server_available: boolean
    connected: boolean
    serial?: string
    state?: string
    error?: string
    command?: string
    command_source?: 'env' | 'saved' | 'default' | string
    devices?: Array<{ serial: string; state: string; online: boolean }>
  }
  backup: {
    available: boolean
    command?: string
    script?: string
    default_dir?: string
  }
}

export interface CallRecord {
  id: string
  index: number
  direction: string
  state: string
  number?: string
  started_at: string
  updated_at: string
  ended_at?: string
  missed: boolean
  iccid?: string
}
export interface CallStatus {
  active?: CallRecord | null
  history: CallRecord[] | null
  polling: boolean
  poll_interval_s: number
  last_poll: string
  last_poll_error?: string
}
export interface Envelope {
  id: number
  type: string
  version: number
  occurred_at: string
  data: unknown
}

export interface NotificationDebugAction {
  action: string
  event: string
  count?: number
}
export interface NotificationDebugEvent extends Envelope {}
export interface NotificationDebugInfo {
  native_ui: boolean
  actions: NotificationDebugAction[]
}
export interface NotificationDebugRequest {
  action: string
  call_id?: string
  number?: string
  sender?: string
  recipient?: string
  body?: string
}
export interface NotificationDebugResponse {
  action: string
  native_ui: boolean
  events: NotificationDebugEvent[]
}

export type NotificationPermissionState =
  'unknown' | 'not_determined' | 'authorized' | 'denied' | 'provisional' | 'unsupported'
export interface NotificationPermissionStatus {
  native_ui: boolean
  state: NotificationPermissionState
  can_request: boolean
  can_open_settings: boolean
  accepted?: boolean
}

export type NotificationPresentationMode = 'system' | 'custom'
export interface NotificationPreferences {
  incoming_call: NotificationPresentationMode
  missed_call: NotificationPresentationMode
  sms: NotificationPresentationMode
  device_offline: NotificationPresentationMode
  show_debug: boolean
  // sender_only 缺失时 macOS 端默认开启 (仅显示发送方); 显式 false 显示正文。
  sender_only?: boolean
}
export interface NotificationPreferencesResponse {
  native_ui: boolean
  preferences: NotificationPreferences
}

export interface StartupStatus {
  supported: boolean
  enabled: boolean
}
