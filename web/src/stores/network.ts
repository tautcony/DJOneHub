import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '../services/api'
import type { NetworkStatus, NetworkTrafficRange, NetworkTrafficUpdate } from '../types'

export type TrafficRange = 'day' | 'week' | 'month'

export interface TrafficSample {
  rxRate: number
  txRate: number
  rxBytes: number
  txBytes: number
  dailyAvailable: boolean
  sampledAt: string
  date: string
}

// network 域状态: 网络状态、流量采样与历史。
// 视图经 typed ViewContext 读取本 store 暴露的 refs/actions。
export const useNetworkStore = defineStore('network', () => {
  const status = ref<NetworkStatus | null>(null)
  const overviewStatus = ref<NetworkStatus | null>(null)
  const mode = ref('')
  const traffic = ref<TrafficSample>({
    rxRate: 0,
    txRate: 0,
    rxBytes: 0,
    txBytes: 0,
    dailyAvailable: false,
    sampledAt: '',
    date: '',
  })
  const trafficHistory = ref<Array<{ at: number; rxRate: number; txRate: number }>>([])
  const trafficRangeData = ref<NetworkTrafficRange | null>(null)
  let trafficRangeRequest = 0
  let previousTrafficSample: { rx: number; tx: number; at: number } | undefined

  async function load(): Promise<void> {
    status.value = await api.network()
    mode.value = status.value.mode || ''
  }

  async function loadOverview(): Promise<void> {
    try {
      overviewStatus.value = await api.network()
    } catch {
      overviewStatus.value = null
    }
  }

  async function loadTrafficDaily(): Promise<void> {
    try {
      const result = await api.networkTrafficDaily()
      traffic.value = {
        ...traffic.value,
        rxBytes: result.rx_bytes,
        txBytes: result.tx_bytes,
        dailyAvailable: result.available,
        sampledAt: result.sampled_at || '',
        date: result.date,
      }
    } catch {
      traffic.value = { ...traffic.value, dailyAvailable: false }
    }
  }

  async function loadTrafficRange(period: TrafficRange): Promise<void> {
    const requestID = ++trafficRangeRequest
    try {
      const result = await api.networkTrafficRange(period)
      if (requestID === trafficRangeRequest) trafficRangeData.value = result
    } catch {
      if (requestID === trafficRangeRequest) trafficRangeData.value = null
    }
  }

  function applyTraffic(data: unknown) {
    if (!data || typeof data !== 'object') return
    const sample = data as Partial<NetworkTrafficUpdate>
    if (typeof sample.rx_bytes !== 'number' || typeof sample.tx_bytes !== 'number') return
    const parsedAt = typeof sample.sampled_at === 'string' ? Date.parse(sample.sampled_at) : NaN
    const at = Number.isFinite(parsedAt) ? parsedAt : Date.now()
    const previous = previousTrafficSample
    const seconds = previous ? Math.max((at - previous.at) / 1000, 0.1) : 0
    const dailyAvailable = sample.daily_available === true
    const dailyRX = typeof sample.daily_rx_bytes === 'number' ? sample.daily_rx_bytes : sample.rx_bytes
    const dailyTX = typeof sample.daily_tx_bytes === 'number' ? sample.daily_tx_bytes : sample.tx_bytes
    traffic.value = {
      rxBytes: dailyAvailable ? dailyRX : sample.rx_bytes,
      txBytes: dailyAvailable ? dailyTX : sample.tx_bytes,
      rxRate: previous ? Math.max(0, sample.rx_bytes - previous.rx) / seconds : 0,
      txRate: previous ? Math.max(0, sample.tx_bytes - previous.tx) / seconds : 0,
      dailyAvailable,
      sampledAt: sample.sampled_at || '',
      date: dailyAvailable ? new Date(at).toLocaleDateString() : '',
    }
    const rxRate = previous ? Math.max(0, sample.rx_bytes - previous.rx) / seconds : 0
    const txRate = previous ? Math.max(0, sample.tx_bytes - previous.tx) / seconds : 0
    trafficHistory.value = [...trafficHistory.value, { at, rxRate, txRate }].filter(
      (point) => point.at >= at - 30_000,
    )
    previousTrafficSample = { rx: sample.rx_bytes, tx: sample.tx_bytes, at }
  }

  async function setMode(): Promise<{ operation_id: string }> {
    return api.networkMode(mode.value)
  }

  async function check(): Promise<{ ok: boolean; summary: string; detail?: string }> {
    return api.networkCheck()
  }

  async function reboot(): Promise<{ accepted: boolean }> {
    return api.reboot()
  }

  return {
    status,
    overviewStatus,
    mode,
    traffic,
    trafficHistory,
    trafficRangeData,
    load,
    loadOverview,
    loadTrafficDaily,
    loadTrafficRange,
    applyTraffic,
    setMode,
    check,
    reboot,
  }
})

export type NetworkStore = ReturnType<typeof useNetworkStore>
