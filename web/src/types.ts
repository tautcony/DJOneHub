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

export interface SMSMessage {
  index: number
  sender?: string
  recipient?: string
  body: string
  code?: string
  received_at?: string
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
  interface?: string
  addresses?: string[]
  default_route?: string
  rx_bytes: number
  tx_bytes: number
}

export interface VowifiStatus {
  available?: boolean
  state?: string
  reason?: string
  [key: string]: unknown
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
}
export interface CallStatus {
  active?: CallRecord | null
  history: CallRecord[] | null
  polling: boolean
  poll_interval_s: number
  last_poll: string
  last_poll_error?: string
}
export interface GPSFix {
  utc: string
  latitude: string
  longitude: string
  hdop: string
  altitude: string
  fix: string
  satellites: string
  timestamp: string
}
export interface GPSStatus {
  enabled: boolean
  last_fix?: GPSFix
  last_checked: string
  last_error?: string
  poll_interval_s: number
}
export interface CellularPolicy {
  force_off: boolean
  services?: string[]
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
  code?: string
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
}
export interface NotificationPreferencesResponse {
  native_ui: boolean
  preferences: NotificationPreferences
}
