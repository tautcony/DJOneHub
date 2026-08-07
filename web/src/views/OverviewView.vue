<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { EyeInvisibleOutlined, EyeOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import FieldRow from '../components/FieldRow.vue'
import Panel from '../components/Panel.vue'
import StatusLight from '../components/StatusLight.vue'
import { useViewContext } from './context'
import { formatBytes, formatRate } from '../utils/format'

const { t } = useI18n()
const {
  device,
  deviceCapabilities,
  esim,
  loadView,
  loadTrafficRange,
  maskSensitive,
  networkTraffic,
  overviewNetwork,
  showSensitive,
  stateLabel,
  stateTone,
  trafficHistory,
  trafficRangeData,
} = useViewContext()

type TrafficRange = 'day' | 'week' | 'month'
const trafficRange = ref<TrafficRange>('day')
const trafficRangeLoading = ref(false)
const trafficTablePage = ref(1)
const trafficTablePageSize = 10
const trafficPeriods: TrafficRange[] = ['day', 'week', 'month']
const smoothRate = ref({ rxRate: 0, txRate: 0 })
const chartNow = ref(Date.now())
let chartAnimationFrame: number | undefined
let lastChartFrameAt = Date.now()
let smoothRateTarget = { rxRate: 0, txRate: 0 }

const radio = computed(() => device.status?.radio)
const networkMode = computed(() => radio.value?.network_mode || t('common.unknown'))
const addresses = computed<string[]>(() => overviewNetwork.value?.addresses || [])
const ipv4 = computed(() => addresses.value.find((address) => !address.includes(':')))
const ipv6 = computed(() => addresses.value.find((address) => address.includes(':')))
const signalLevel = computed(() => {
  if (!radio.value?.registered) return 0
  const dbm = radio.value?.signal_dbm
  if (typeof dbm !== 'number' || !Number.isFinite(dbm) || dbm === 0 || dbm === -999) return 0
  if (dbm >= -60) return 4
  if (dbm >= -75) return 3
  if (dbm >= -90) return 2
  return 1
})
const signalTone = computed<'success' | 'warning' | 'danger' | 'neutral'>(() => {
  if (!radio.value?.registered) return 'neutral'
  const dbm = radio.value?.signal_dbm
  if (typeof dbm !== 'number' || !Number.isFinite(dbm) || dbm === 0 || dbm === -999) return 'neutral'
  if (dbm >= -85) return 'success'
  if (dbm >= -100) return 'warning'
  return 'danger'
})

function display(value?: unknown) {
  return value === undefined || value === null || value === '' ? t('common.empty') : String(value)
}

const trafficRateMax = computed(() => {
  const max = trafficHistory.value.reduce(
    (value: number, point: { rxRate: number; txRate: number }) => Math.max(value, point.rxRate, point.txRate),
    Math.max(smoothRate.value.rxRate, smoothRate.value.txRate),
  )
  return Math.max(max, 1)
})

const trafficPlotPoints = computed(() => {
  if (!trafficHistory.value.length) return []
  const now = chartNow.value
  const cutoff = now - 30_000
  return [
    ...trafficHistory.value.filter((point: { at: number }) => point.at >= cutoff),
    { at: now, rxRate: smoothRate.value.rxRate, txRate: smoothRate.value.txRate },
  ]
})

function trafficPath(key: 'rxRate' | 'txRate') {
  const width = 760
  const height = 190
  const now = chartNow.value
  const start = now - 30_000
  const points = trafficPlotPoints.value
  if (!points.length) return ''
  return points
    .map((point: { at: number; rxRate: number; txRate: number }, index: number) => {
      const x = Math.max(0, Math.min(width, ((point.at - start) / 30_000) * width))
      const y = height - 14 - (point[key] / trafficRateMax.value) * (height - 28)
      return `${index === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${y.toFixed(2)}`
    })
    .join(' ')
}

const trafficDownloadPath = computed(() => trafficPath('rxRate'))
const trafficUploadPath = computed(() => trafficPath('txRate'))
const activeTrafficRangeData = computed(() =>
  trafficRangeData.value?.range === trafficRange.value ? trafficRangeData.value : null,
)
const trafficTableRows = computed(() => [...(activeTrafficRangeData.value?.items || [])].reverse())
const trafficTablePageRows = computed(() => {
  const start = (trafficTablePage.value - 1) * trafficTablePageSize
  return trafficTableRows.value.slice(start, start + trafficTablePageSize)
})

async function refreshTrafficRange(showLoading = false) {
  if (trafficRange.value === 'day') return
  if (showLoading) trafficRangeLoading.value = true
  try {
    await loadTrafficRange(trafficRange.value)
  } finally {
    if (showLoading) trafficRangeLoading.value = false
  }
}

async function refreshOverviewTraffic() {
  await Promise.all([loadView('overview'), refreshTrafficRange(true)])
}

function animateTrafficChart() {
  const frameAt = Date.now()
  const elapsed = Math.max(0, frameAt - lastChartFrameAt)
  lastChartFrameAt = frameAt
  const blend = Math.min(1, (elapsed / 1000) * 8)
  smoothRate.value = {
    rxRate: smoothRate.value.rxRate + (smoothRateTarget.rxRate - smoothRate.value.rxRate) * blend,
    txRate: smoothRate.value.txRate + (smoothRateTarget.txRate - smoothRate.value.txRate) * blend,
  }
  chartNow.value = Date.now()
  chartAnimationFrame = window.requestAnimationFrame(animateTrafficChart)
}

watch(
  trafficHistory,
  (history: Array<{ rxRate: number; txRate: number }>) => {
    const latest = history[history.length - 1]
    if (latest) smoothRateTarget = { rxRate: latest.rxRate, txRate: latest.txRate }
  },
  { deep: true, immediate: true },
)
watch(
  trafficRange,
  (period) => {
    trafficTablePage.value = 1
    if (period !== 'day') void refreshTrafficRange(true)
  },
  { immediate: true },
)

onMounted(() => {
  lastChartFrameAt = Date.now()
  chartAnimationFrame = window.requestAnimationFrame(animateTrafficChart)
})

onBeforeUnmount(() => {
  if (chartAnimationFrame !== undefined) window.cancelAnimationFrame(chartAnimationFrame)
})
</script>

<template>
  <section class="overview-view">
    <header class="overview-device-header">
      <div class="overview-device-identity">
        <span class="overview-device-mark" aria-hidden="true">V</span>
        <div>
          <span class="eyebrow">{{ t('overview.deviceStatus') }}</span>
          <h2>
            {{
              device.snapshot?.identity.stable_id ||
              device.snapshot?.identity.product ||
              t('overview.noModem')
            }}
          </h2>
          <p>
            {{ device.snapshot?.identity.product || t('overview.genericModem') }} ·
            {{ device.snapshot?.backend || t('common.unknown') }}
          </p>
        </div>
      </div>
      <div class="overview-device-status">
        <StatusLight :tone="stateTone" :label="stateLabel" :pulse="stateTone === 'info'" />
        <span class="overview-device-generation"
          >{{ t('overview.generation') }} {{ device.snapshot?.generation ?? '—' }}</span
        >
      </div>
    </header>

    <div class="overview-panels">
      <article class="overview-card">
        <div class="overview-card-heading">
          <div>
            <span class="eyebrow">{{ t('overview.radioEyebrow') }}</span>
            <h2>{{ t('overview.radioNetwork') }}</h2>
          </div>
          <StatusLight
            :tone="radio?.registered ? 'success' : 'warning'"
            :label="radio?.registered ? t('status.registered') : t('status.notRegistered')"
          />
        </div>
        <div class="radio-hero" :class="`radio-${signalTone}`">
          <div class="radio-hero-copy">
            <strong>{{ display(radio?.operator) }}</strong
            ><span>{{ networkMode }}</span>
          </div>
          <div class="signal-bars" :aria-label="`${t('overview.signal')} ${display(radio?.signal_dbm)}`">
            <i
              v-for="bar in 4"
              :key="bar"
              :class="{ active: bar <= signalLevel }"
              :style="{ height: `${bar * 18 + 10}%` }"
            />
          </div>
        </div>
        <div class="signal-reading">
          <span>{{ t('overview.signal') }}</span
          ><strong>{{ radio?.signal_dbm ? `${radio.signal_dbm} dBm` : t('common.empty') }}</strong
          ><StatusLight
            :tone="signalTone"
            :label="signalLevel ? t('overview.signalQuality') : t('common.unknown')"
          />
        </div>
		<div class="detail-list overview-detail-list">
			<FieldRow :label="t('overview.networkMode')" :value="networkMode" monospace /><FieldRow
				:label="t('overview.band')"
				:value="radio?.radio_band"
				monospace
			/><FieldRow
				:label="t('overview.rsrp')"
            :value="radio?.signal_rsrp ? `${radio.signal_rsrp} dBm` : undefined"
            monospace
          /><FieldRow
            :label="t('overview.rsrq')"
            :value="radio?.signal_rsrq ? `${radio.signal_rsrq} dB` : undefined"
            monospace
          /><FieldRow
            :label="t('overview.sinr')"
            :value="radio?.signal_sinr ? `${radio.signal_sinr} dB` : undefined"
            monospace
          />
        </div>
      </article>

      <article class="overview-card">
        <div class="overview-card-heading">
          <div>
            <span class="eyebrow">{{ t('overview.identityEyebrow') }}</span>
            <h2>{{ t('overview.simDevice') }}</h2>
          </div>
          <button
            type="button"
            class="overview-icon-button"
            :aria-label="showSensitive ? t('overview.hideSensitive') : t('overview.showSensitive')"
            @click="showSensitive = !showSensitive"
          >
            <EyeOutlined v-if="showSensitive" /><EyeInvisibleOutlined v-else />
          </button>
        </div>
        <div class="overview-detail-stack">
          <FieldRow
            :label="t('overview.imei')"
            :value="maskSensitive(device.status?.identity.imei)"
            monospace
          /><FieldRow
            :label="t('overview.iccid')"
            :value="maskSensitive(device.status?.identity.iccid || device.status?.sim.iccid)"
            monospace
          /><FieldRow
            :label="t('overview.imsi')"
            :value="maskSensitive(device.status?.identity.imsi || device.status?.sim.imsi)"
            monospace
          /><FieldRow
            :label="t('overview.phoneNumber')"
            :value="maskSensitive(device.status?.identity.msisdn)"
            monospace
          /><FieldRow
            :label="t('overview.simState')"
            :value="device.status?.sim.inserted ? t('status.inserted') : t('common.unavailable')"
          /><FieldRow
            :label="t('overview.eid')"
            :value="maskSensitive(esim?.eid || device.status?.sim.eid)"
            monospace
          /><FieldRow :label="t('overview.firmware')" :value="device.status?.identity.firmware" monospace />
        </div>
      </article>

      <article class="overview-card">
        <div class="overview-card-heading">
          <div>
            <span class="eyebrow">{{ t('overview.networkEyebrow') }}</span>
            <h2>{{ t('overview.network') }}</h2>
          </div>
          <StatusLight
            :tone="overviewNetwork?.interface ? 'success' : 'neutral'"
            :label="overviewNetwork?.interface || t('common.unknown')"
          />
        </div>
        <div class="overview-detail-stack">
          <FieldRow :label="t('overview.ipv4')" :value="ipv4" monospace /><FieldRow
            :label="t('overview.ipv6')"
            :value="ipv6"
            monospace
          /><FieldRow
            :label="t('overview.interface')"
            :value="overviewNetwork?.interface"
            monospace
          /><FieldRow
            :label="t('overview.defaultRoute')"
            :value="overviewNetwork?.default_route"
            monospace
          /><FieldRow
            :label="t('overview.received')"
            :value="formatBytes(networkTraffic.rxBytes)"
            monospace
          /><FieldRow :label="t('overview.sent')" :value="formatBytes(networkTraffic.txBytes)" monospace /><FieldRow
            :label="t('overview.downloadRate')"
            :value="formatRate(networkTraffic.rxRate)"
            monospace
          /><FieldRow :label="t('overview.uploadRate')" :value="formatRate(networkTraffic.txRate)" monospace />
        </div>
      </article>
    </div>

    <section class="overview-traffic-panel">
      <div class="overview-traffic-heading">
        <div>
          <span class="eyebrow">{{ t('overview.trafficEyebrow') }}</span>
          <h2>{{ t('overview.trafficTitle') }}</h2>
        </div>
        <div class="overview-traffic-actions">
          <div class="traffic-range-segments" role="group" :aria-label="t('overview.trafficRange')">
            <button
              v-for="period in trafficPeriods"
              :key="period"
              type="button"
              :class="{ active: trafficRange === period }"
              @click="trafficRange = period"
            >
              {{ t(`overview.${period}`) }}
            </button>
          </div>
          <a-button
            class="overview-traffic-refresh"
            size="small"
            :loading="trafficRangeLoading"
            @click="refreshOverviewTraffic"
            ><ReloadOutlined />{{ t('common.refresh') }}</a-button
          >
        </div>
      </div>
      <div class="traffic-live-chart">
        <div class="traffic-rate-stats">
          <div class="traffic-rate-stat traffic-download">
            <span>{{ t('overview.download') }}</span>
            <strong>{{ formatRate(smoothRate.rxRate) }}</strong>
          </div>
          <div class="traffic-rate-stat traffic-upload">
            <span>{{ t('overview.upload') }}</span>
            <strong>{{ formatRate(smoothRate.txRate) }}</strong>
          </div>
        </div>
        <div class="traffic-line-chart" role="img" :aria-label="t('overview.trafficTitle')">
          <svg viewBox="0 0 760 190" preserveAspectRatio="none" aria-hidden="true">
            <line
              v-for="line in 5"
              :key="line"
              x1="0"
              :y1="14 + ((line - 1) * 162) / 4"
              x2="760"
              :y2="14 + ((line - 1) * 162) / 4"
            />
            <path class="traffic-line traffic-line-download" :d="trafficDownloadPath" />
            <path class="traffic-line traffic-line-upload" :d="trafficUploadPath" />
          </svg>
          <span v-if="!trafficHistory.length" class="traffic-chart-empty">{{ t('common.empty') }}</span>
        </div>
        <div class="traffic-chart-legend">
          <span class="traffic-legend-download">{{ t('overview.download') }}</span>
          <span class="traffic-legend-upload">{{ t('overview.upload') }}</span>
        </div>
      </div>
      <div v-if="trafficRange !== 'day'" class="traffic-history">
        <div class="traffic-history-heading">
          <strong>{{ t(`overview.${trafficRange}Usage`) }}</strong>
          <span
            >{{ activeTrafficRangeData?.start_date || '-' }} <em>/</em>
            {{ activeTrafficRangeData?.end_date || '-' }}</span
          >
        </div>
        <div class="traffic-history-table-wrap">
          <table class="traffic-history-table">
            <thead>
              <tr>
                <th scope="col">{{ t('overview.date') }}</th>
                <th scope="col">{{ t('overview.download') }}</th>
                <th scope="col">{{ t('overview.upload') }}</th>
                <th scope="col">{{ t('overview.total') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in trafficTablePageRows" :key="row.date">
                <th scope="row">{{ row.date }}</th>
                <td>{{ formatBytes(row.rx_bytes) }}</td>
                <td>{{ formatBytes(row.tx_bytes) }}</td>
                <td>{{ formatBytes(row.rx_bytes + row.tx_bytes) }}</td>
              </tr>
              <tr v-if="!trafficTablePageRows.length">
                <td class="traffic-history-empty" colspan="4">{{ t('common.empty') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="trafficTableRows.length > trafficTablePageSize" class="traffic-history-pagination">
          <a-pagination
            v-model:current="trafficTablePage"
            :page-size="trafficTablePageSize"
            :total="trafficTableRows.length"
            :show-size-changer="false"
            size="small"
          />
        </div>
      </div>
    </section>
    <Panel :title="t('overview.availableCapabilities')" :meta="t('overview.serverReported')"
      ><div v-if="Object.keys(deviceCapabilities).length" class="capability-list">
        <a-tag v-for="(_, name) in deviceCapabilities" :key="name" color="blue">{{ name }}</a-tag>
      </div>
      <EmptyState v-else :title="t('overview.capabilityReady')"
    /></Panel>
  </section>
</template>
