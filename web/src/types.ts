export type DeviceState = 'absent' | 'discovered' | 'connecting' | 'initializing' | 'ready' | 'degraded' | 'disconnected'

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
  radio: { registered: boolean; operator?: string; network_mode?: string; signal_dbm?: number; signal_rsrp?: number; signal_rsrq?: number; signal_sinr?: number }
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

export interface Envelope { id: number; type: string; version: number; occurred_at: string; data: unknown }
