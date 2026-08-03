<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { EyeInvisibleOutlined, EyeOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import FieldRow from '../components/FieldRow.vue'
import Panel from '../components/Panel.vue'
import StatusLight from '../components/StatusLight.vue'
import { useViewContext } from './context'

const { t } = useI18n()
const {
  device,
  deviceCapabilities,
  esim,
  loadView,
  networkTraffic,
  overviewNetwork,
  showSensitive,
  stateLabel,
  stateTone,
} = useViewContext()

const radio = computed(() => device.status?.radio)
const networkMode = computed(() => radio.value?.network_mode || t('common.unknown'))
const addresses = computed<string[]>(() => overviewNetwork.value?.addresses || [])
const ipv4 = computed(() => addresses.value.find((address) => !address.includes(':')))
const ipv6 = computed(() => addresses.value.find((address) => address.includes(':')))
const signalLevel = computed(() => {
  const dbm = radio.value?.signal_dbm
  if (typeof dbm !== 'number' || !Number.isFinite(dbm) || dbm === 0 || dbm === -999) return 0
  if (dbm >= -75) return 5
  if (dbm >= -85) return 4
  if (dbm >= -95) return 3
  if (dbm >= -105) return 2
  return 1
})
const signalTone = computed<'success' | 'warning' | 'danger' | 'neutral'>(() => {
  const dbm = radio.value?.signal_dbm
  if (typeof dbm !== 'number' || !Number.isFinite(dbm) || dbm === 0 || dbm === -999) return 'neutral'
  if (dbm >= -85) return 'success'
  if (dbm >= -100) return 'warning'
  return 'danger'
})

function display(value?: unknown) {
  return value === undefined || value === null || value === '' ? t('common.empty') : String(value)
}

function bytes(value: number) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let amount = Math.max(0, value)
  let unit = 0
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024
    unit++
  }
  return `${amount.toFixed(unit === 0 ? 0 : amount >= 100 ? 0 : 1)} ${units[unit]}`
}

function rate(value: number) {
  return `${bytes(value)}/s`
}

function mask(value?: string) {
  if (!value) return t('common.empty')
  if (showSensitive.value) return value
  if (value.length <= 4) return '*'.repeat(value.length)
  return `${'*'.repeat(value.length - 4)}${value.slice(-4)}`
}
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
            :label="radio?.registered ? t('status.registered') : t('status.offline')"
          />
        </div>
        <div class="radio-hero" :class="`radio-${signalTone}`">
          <div class="radio-hero-copy">
            <strong>{{ display(radio?.operator) }}</strong
            ><span>{{ networkMode }}</span>
          </div>
          <div class="signal-bars" :aria-label="`${t('overview.signal')} ${display(radio?.signal_dbm)}`">
            <i
              v-for="bar in 5"
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
            :value="mask(device.status?.identity.imei)"
            monospace
          /><FieldRow
            :label="t('overview.iccid')"
            :value="mask(device.status?.identity.iccid || device.status?.sim.iccid)"
            monospace
          /><FieldRow
            :label="t('overview.imsi')"
            :value="mask(device.status?.identity.imsi || device.status?.sim.imsi)"
            monospace
          /><FieldRow
            :label="t('overview.phoneNumber')"
            :value="mask(device.status?.identity.msisdn)"
            monospace
          /><FieldRow
            :label="t('overview.simState')"
            :value="device.status?.sim.inserted ? t('status.inserted') : t('common.notAvailable')"
          /><FieldRow :label="t('overview.eid')" :value="mask(esim?.eid || device.status?.sim.eid)" monospace /><FieldRow
            :label="t('overview.firmware')"
            :value="device.status?.identity.firmware"
            monospace
          />
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
            :value="bytes(networkTraffic.rxBytes)"
            monospace
          /><FieldRow :label="t('overview.sent')" :value="bytes(networkTraffic.txBytes)" monospace /><FieldRow
            :label="t('overview.downloadRate')"
            :value="rate(networkTraffic.rxRate)"
            monospace
          /><FieldRow :label="t('overview.uploadRate')" :value="rate(networkTraffic.txRate)" monospace />
        </div>
      </article>
    </div>

    <section class="overview-traffic-panel">
      <div class="overview-traffic-heading">
        <div>
          <span class="eyebrow">{{ t('overview.trafficEyebrow') }}</span>
          <h2>{{ t('overview.trafficTitle') }}</h2>
          <p>{{ t('overview.trafficDetail') }}</p>
        </div>
        <div class="overview-traffic-actions" role="group" :aria-label="t('overview.trafficRange')">
          <button type="button" class="active">{{ t('overview.day') }}</button
          ><button type="button" disabled>{{ t('overview.week') }}</button
          ><button type="button" disabled>{{ t('overview.month') }}</button
          ><a-button size="small" @click="loadView('overview')"
            ><ReloadOutlined />{{ t('common.refresh') }}</a-button
          >
        </div>
      </div>
      <div class="traffic-summary-grid">
        <div class="traffic-summary-card traffic-download">
          <span>{{ t('overview.download') }}</span
          ><strong>{{ bytes(networkTraffic.rxBytes) }}</strong
          ><small>{{ rate(networkTraffic.rxRate) }}</small>
          <div class="traffic-meter"><i :style="{ width: networkTraffic.rxRate ? '72%' : '12%' }" /></div>
        </div>
        <div class="traffic-summary-card traffic-upload">
          <span>{{ t('overview.upload') }}</span
          ><strong>{{ bytes(networkTraffic.txBytes) }}</strong
          ><small>{{ rate(networkTraffic.txRate) }}</small>
          <div class="traffic-meter"><i :style="{ width: networkTraffic.txRate ? '42%' : '12%' }" /></div>
        </div>
        <div class="traffic-summary-note">
          <StatusLight
            :tone="radio?.registered ? 'success' : 'neutral'"
            :label="radio?.registered ? t('overview.liveSample') : t('overview.waitingSample')"
          />
          <p>{{ t('overview.trafficCounters') }}</p>
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
