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

export interface SMSStorageUsage {
  storage: string
  used: number
  total: number
}

export interface SmsThread {
  key: string
  peer: string
  iccid: string
  items: SMSMessage[]
  latest?: SMSMessage
}

export interface EsimOverview {
  card_type?: 'physical_sim' | 'euicc' | 'unknown'
  eid?: string
  free_nvram_bytes?: number
  free_nvram?: string
  probe_error?: string
  message?: string
  device_info?: {
    sku_name?: string
    serial_number?: string
    firmware?: string
  }
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

export interface EsimNotification {
  sequence_number: number
  event?: string
  iccid?: string
  address?: string
  can_retry?: boolean
}

export interface SimProfile {
  iccid: string
  imsi?: string
  msisdn?: string
  name?: string
  local_phone?: string
  notes?: string
  tags?: string
  profile_type: 'unknown' | 'physical' | 'esim'
  first_seen_at?: string
  last_seen_at?: string
}

export interface EsimNotificationHistory {
  sequence_number: number
  event?: string
  iccid?: string
  address?: string
  aid?: string
  state?: 'pending' | 'processed' | 'failed' | 'removed'
  observed_at?: string
  updated_at?: string
}

export interface EsimHealth {
  ok: boolean
  module_iccid?: string
  imsi?: string
  operator?: string
  registration?: boolean
  registered?: boolean
  signal_dbm?: number
  network_mode?: string
  active_profile?: EsimOverview['profiles'][number]
  message?: string
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

export interface DeviceControlStatus {
  available: boolean
  manufacturer?: string
  model?: string
  firmware?: string
  firmware_version_source?: string
  firmware_version_live?: boolean
  firmware_version_reason?: string
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
    reset_available?: boolean
    reason?: string
    command?: string
    script?: string
    default_dir?: string
  }
  entry_methods?: string[]
  entry_method_reasons?: Record<string, string>
  edl?: {
    state: string
    protocol?: string
    source?: string
    serial_number?: string
    hardware_id?: string
    pk_hash?: string
    sbl_version?: string
    observed_at?: string
    reason?: string
    recovery_required?: boolean
  }
  edl_session?: {
    session_id?: string
    observation: DeviceControlStatus['edl']
    lease_held: boolean
    lease_owned: boolean
    lease_expires_at?: string
    active_operation?: string
  }
  settings: DeviceControlSettings
}

export interface DeviceControlSettings {
  adb_command?: string
  edl_path?: string
  edl_runner?: string
  loader_path?: string
  backup_directory?: string
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
  connected_at?: string
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

export interface RuntimeWorkerDiagnostics {
  id: string
  name: string
  kind: string
  state: 'running' | 'idle' | 'stopped' | string
  detail: string
  interval_ms?: number
  event_source?: boolean
  event_types?: string[]
  queue_depth?: number
  queue_capacity?: number
  dropped?: number
  last_activity?: string
}

export interface RuntimeChannelDiagnostics {
  id: string
  name: string
  kind: string
  state: string
  detail: string
  published?: number
  dropped?: number
  subscribers?: number
  queue_depth?: number
  queue_capacity?: number
}

export interface RuntimeDiagnostics {
  generated_at: string
  uptime_seconds: number
  goroutines: number
  workers: RuntimeWorkerDiagnostics[]
  channels: RuntimeChannelDiagnostics[]
  event_bus: {
    published: number
    cumulative_drops: number
    subscribers: Array<{
      id: number
      name: string
      queued: number
      capacity: number
      dropped: number
      since: string
    }>
    event_types: Array<{ type: string; count: number; last_id: number; last_occurred_at: string }>
    recent: Array<{
      id: number
      type: string
      occurred_at: string
      subscribers: number
      delivered: number
      dropped: number
    }>
  }
  flows: Array<{ id: string; from: string; via: string; to: string[]; event_types: string[] }>
  topology: RuntimeTopology
  traces: RuntimeMessageTrace[]
  channel_recovery: Array<{
    channel: string
    attempts: number
    retryable: boolean
    last_error: string
    next_retry?: string
    last_failed: string
  }>
}

export interface RuntimeTopologyNode {
  id: string
  name: string
  kind: 'source' | 'channel' | 'processor' | 'destination' | string
  state: string
  detail?: string
}

export interface RuntimeTopologyEdge {
  id: string
  source: string
  target: string
  event_types?: string[]
}

export interface RuntimeTopology {
  nodes: RuntimeTopologyNode[]
  edges: RuntimeTopologyEdge[]
}

export interface RuntimeTraceHop {
  node_id: string
  from_node_id?: string
  action: string
  state: string
  at: string
  detail?: string
}

export interface RuntimeMessageTrace {
  id: number
  type: string
  started_at: string
  updated_at: string
  status: string
  fields?: Record<string, unknown>
  hops: RuntimeTraceHop[]
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

export interface TelegramChannelSettings {
  enabled: boolean
  bot_token: string
  chat_id: number
  admin_id: number
  base_url: string
  proxy: string
}
export interface FeishuChannelSettings {
  enabled: boolean
  app_id: string
  app_secret: string
  chat_ids: string[]
}
export interface WebhookChannelSettings {
  enabled: boolean
  urls: string[]
  secret: string
  timeout_ms: number
  retry_max: number
  text_template: string
  headers: Record<string, string>
}
export interface BarkChannelSettings {
  enabled: boolean
  urls: string[]
  group: string
  icon: string
  level: string
}
export interface EmailChannelSettings {
  enabled: boolean
  use_ssl: boolean
  smtp_host: string
  smtp_port: number
  username: string
  password: string
  from_address: string
  to_addresses: string[]
}
export interface PushplusChannelSettings {
  enabled: boolean
  token: string
  topic: string
  channel: string
}
// NotificationChannelsSettings 对应后端 internal/notify.Settings。
// 机密字段回显为占位符 "__unchanged__"，原样回传表示不修改。
export interface NotificationChannelsSettings {
  telegram: TelegramChannelSettings
  feishu: FeishuChannelSettings
  webhook: WebhookChannelSettings
  bark: BarkChannelSettings
  email: EmailChannelSettings
  pushplus: PushplusChannelSettings
}
export interface NotificationChannelsResponse {
  channels: NotificationChannelsSettings
}
export interface NotificationChannelTestResponse {
  channel: string
  delivered: boolean
}
