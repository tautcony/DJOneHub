<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from './services/api'
import { APIError } from './services/api'
import { persistLocale } from './i18n'
import { AT_PRESETS, parseATResponse } from './services/at'
import { useDeviceStore } from './stores/device'
import type { EsimOverview, NetworkStatus, OperationStatus, SMSMessage, VowifiStatus } from './types'

type ViewID = 'overview' | 'sms' | 'esim' | 'network' | 'raw-at' | 'vowifi' | 'settings'

const device = useDeviceStore()
const { t, te, locale } = useI18n()
const active = ref<ViewID>('overview')
const viewError = ref('')
const smsTo = ref('')
const smsBody = ref('')
const smsMessage = ref('')
const smsOperationID = ref('')
const smsOperation = computed(() => smsOperationID.value ? device.operations[smsOperationID.value] : undefined)
const smsItems = ref<SMSMessage[]>([])
const esim = ref<EsimOverview | null>(null)
const esimMessage = ref('')
const esimDownloadOpen = ref(false)
const esimRenameOpen = ref(false)
const esimRenameICCID = ref('')
const esimActivationCode = ref('')
const esimConfirmationCode = ref('')
const esimMatchingID = ref('')
const esimLabels = ref<Record<string, string>>({})
const esimOperationID = ref('')
const esimOperation = computed(() => esimOperationID.value ? device.operations[esimOperationID.value] : undefined)
const esimReloadedOperationID = ref('')
const pendingViewRefreshes = new Map<ViewID, { timer: number; dueAt: number }>()
const network = ref<NetworkStatus | null>(null)
const networkMessage = ref('')
const networkMode = ref('')
const networkRebootMessage = ref('')
const networkTraffic = ref({ rxRate: 0, txRate: 0, rxBytes: 0, txBytes: 0 })
let networkTrafficTimer: number | undefined
let previousTrafficSample: { rx: number; tx: number; at: number } | undefined
const vowifi = ref<VowifiStatus | null>(null)
const vowifiMessage = ref('')
const vowifiOperationID = ref('')
const vowifiOperation = computed(() => vowifiOperationID.value ? device.operations[vowifiOperationID.value] : undefined)
const rawATCommand = ref('')
const rawATExecutedCommand = ref('')
const rawATResponse = ref('')
const rawATMessage = ref('')
const rawATPreset = ref('')
const parsedATResponse = computed(() => rawATResponse.value ? parseATResponse(rawATExecutedCommand.value || rawATCommand.value, rawATResponse.value) : null)

const nav = computed<Array<{ id: ViewID; label: string; capability?: string }>>(() => [
  { id: 'overview', label: t('nav.overview') },
  { id: 'sms', label: t('nav.sms'), capability: 'sms_read' },
  { id: 'esim', label: t('nav.esim'), capability: 'esim' },
  { id: 'network', label: t('nav.network'), capability: 'network_status' },
  { id: 'raw-at', label: t('nav.rawAt'), capability: 'raw_at' },
  { id: 'vowifi', label: t('nav.vowifi'), capability: 'vowifi_inspect' },
  { id: 'settings', label: t('nav.settings') },
])
const stateValue = computed(() => device.snapshot?.state || 'offline')
const stateLabel = computed(() => te(`states.${stateValue.value}`) ? t(`states.${stateValue.value}`) : t('status.offline'))
const activeLabel = computed(() => nav.value.find(item => item.id === active.value)?.label || t('nav.overview'))
const usbNetworkModeOptions = computed(() => [
  { value: '0', label: t('network.modes.rmnet') },
  { value: '1', label: t('network.modes.ecm') },
  { value: '2', label: t('network.modes.mbim') },
  { value: '3', label: t('network.modes.rndis') },
])

watch(locale, value => persistLocale(value))

function usbNetworkModeLabel(mode?: string) {
  const option = usbNetworkModeOptions.value.find(item => item.value === mode)
  return option ? `${option.label} (${option.value})` : mode || t('common.empty')
}

function errorText(cause: unknown, fallback: string) {
  if (cause instanceof APIError) {
    const key = `errors.${cause.code}`
    return te(key) ? t(key) : t('errors.generic')
  }
  return cause instanceof Error ? cause.message : t(fallback)
}

function applyATPreset() {
  const preset = AT_PRESETS.find(item => item.id === rawATPreset.value)
  if (preset) rawATCommand.value = preset.command
}

async function loadSMS() {
  try {
    smsItems.value = (await api.sms()).items
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'sms.unableLoad')
  }
}

async function refreshSMS() {
  try {
    smsItems.value = (await api.smsRefresh()).items
    smsMessage.value = t('sms.refreshed')
    viewError.value = ''
  } catch (error) {
    smsMessage.value = errorText(error, 'sms.unableRefresh')
  }
}

async function clearModuleSMS() {
  try {
    await api.smsClear()
    smsMessage.value = t('sms.cleared')
  } catch (error) {
    smsMessage.value = errorText(error, 'sms.unableClear')
  }
}

function formatSMSDate(value?: string) {
  return value ? new Date(value).toLocaleString() : ''
}

function formatBytes(value: number) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let amount = Math.max(0, value)
  let unit = 0
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024
    unit++
  }
  return `${amount.toFixed(unit === 0 ? 0 : amount >= 100 ? 0 : 1)} ${units[unit]}`
}

async function copySMSCode(code?: string) {
  if (!code) return
  try {
    await navigator.clipboard.writeText(code)
    smsMessage.value = t('sms.codeCopied')
  } catch {
    smsMessage.value = t('sms.codeCopyFailed')
  }
}

async function loadEsim() {
  try {
    esim.value = await api.esim()
    for (const profile of esim.value.profiles) {
      if (profile.iccid && esimLabels.value[profile.iccid] === undefined) {
        esimLabels.value[profile.iccid] = profile.label || ''
      }
    }
    esimMessage.value = ''
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'esim.unableLoad')
  }
}

function refreshView(view: ViewID) {
  if (view === 'sms') return loadSMS()
  if (view === 'esim') return loadEsim()
  if (view === 'network') return loadNetwork()
  if (view === 'vowifi') return loadVowifi()
  return Promise.resolve()
}

function scheduleViewRefresh(view: ViewID, delay = 0) {
  const dueAt = Date.now() + delay
  const pending = pendingViewRefreshes.get(view)
  if (pending && pending.dueAt <= dueAt) return
  if (pending) window.clearTimeout(pending.timer)
  const timer = window.setTimeout(() => {
    pendingViewRefreshes.delete(view)
    if (active.value === view) void refreshView(view)
  }, delay)
  pendingViewRefreshes.set(view, { timer, dueAt })
}

function operationView(operation: OperationStatus): ViewID | undefined {
  if (operation.type.startsWith('sms.')) return 'sms'
  if (operation.type.startsWith('esim.')) return 'esim'
  if (operation.type.startsWith('network.')) return 'network'
  if (operation.type.startsWith('vowifi.')) return 'vowifi'
  return undefined
}

watch(esimOperation, (operation) => {
  if (!operation || esimReloadedOperationID.value === operation.operation_id) return
  if (operation.state !== 'succeeded' && operation.state !== 'failed' && operation.state !== 'cancelled') return

  esimReloadedOperationID.value = operation.operation_id
  const delay = operation.state === 'succeeded' ? 1200 : 0
  scheduleViewRefresh('esim', delay)
})

watch(() => device.eventRevision, () => {
  const eventType = device.lastEventType
  if (eventType === 'snapshot' || eventType === 'resync.required') {
    if (active.value !== 'overview') scheduleViewRefresh(active.value)
    return
  }
  if (eventType === 'device.status.changed' || eventType.startsWith('backend.')) {
    void device.refresh()
    return
  }
  if (eventType === 'sms.updated') { scheduleViewRefresh('sms'); return }
  if (eventType === 'esim.updated') { scheduleViewRefresh('esim', 1200); return }
  if (eventType === 'network.updated') { scheduleViewRefresh('network'); return }
  if (eventType === 'vowifi.updated') { scheduleViewRefresh('vowifi'); return }
  if (eventType !== 'operation.completed') return
  const operation = device.lastEventData as OperationStatus
  if (!operation || typeof operation.type !== 'string') return
  const view = operationView(operation)
  if (!view) return
  scheduleViewRefresh(view, view === 'esim' && operation.state === 'succeeded' ? 1200 : 0)
})

async function enableEsim(iccid?: string) {
  if (!iccid) return
  try {
    const result = await api.esimEnable(iccid)
    esimOperationID.value = result.operation_id
    esimMessage.value = t('esim.operationAccepted', { id: result.operation_id })
  } catch (error) {
    esimMessage.value = errorText(error, 'esim.unableEnable')
  }
}

async function renameEsim(iccid?: string) {
  if (!iccid) return
  const label = (esimLabels.value[iccid] || '').trim()
  if (!label) return
  try {
    await api.esimRename(iccid, label)
    esimMessage.value = t('esim.nameUpdated')
    await loadEsim()
  } catch (error) {
    esimMessage.value = errorText(error, 'esim.unableRename')
  }
}

async function deleteEsim(iccid?: string) {
  if (!iccid) return
  try {
    const result = await api.esimDelete(iccid)
    esimOperationID.value = result.operation_id
    esimMessage.value = t('esim.operationAccepted', { id: result.operation_id })
  } catch (error) {
    esimMessage.value = errorText(error, 'esim.unableDelete')
  }
}

function openEsimDownload() {
  esimDownloadOpen.value = true
}

function closeEsimDownload() {
  esimDownloadOpen.value = false
}

function openEsimRename(iccid?: string) {
  if (!iccid) return
  esimRenameICCID.value = iccid
  esimRenameOpen.value = true
}

function closeEsimRename() {
  esimRenameOpen.value = false
  esimRenameICCID.value = ''
}

async function submitEsimRename() {
  await renameEsim(esimRenameICCID.value)
  if (esimMessage.value === t('esim.nameUpdated')) closeEsimRename()
}

async function downloadEsim() {
  try {
    const result = await api.esimDownload(esimActivationCode.value, esimConfirmationCode.value, esimMatchingID.value)
    esimOperationID.value = result.operation_id
    esimMessage.value = t('esim.operationAccepted', { id: result.operation_id })
    closeEsimDownload()
  } catch (error) {
    esimMessage.value = errorText(error, 'esim.unableDownload')
  }
}

async function loadNetwork() {
  try {
    network.value = await api.network()
    networkMode.value = network.value.mode || ''
    await loadNetworkTraffic()
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'network.unableLoad')
  }
}

async function loadNetworkTraffic() {
  try {
    const sample = await api.networkTraffic()
    const at = Date.now()
    const previous = previousTrafficSample
    const seconds = previous ? Math.max((at - previous.at) / 1000, 0.1) : 0
    networkTraffic.value = {
      rxBytes: sample.rx_bytes,
      txBytes: sample.tx_bytes,
      rxRate: previous ? Math.max(0, sample.rx_bytes - previous.rx) / seconds : 0,
      txRate: previous ? Math.max(0, sample.tx_bytes - previous.tx) / seconds : 0,
    }
    previousTrafficSample = { rx: sample.rx_bytes, tx: sample.tx_bytes, at }
  } catch (error) {
    networkMessage.value = errorText(error, 'network.unableLoad')
  }
}

function stopNetworkTrafficPolling() {
  if (networkTrafficTimer !== undefined) window.clearInterval(networkTrafficTimer)
  networkTrafficTimer = undefined
  previousTrafficSample = undefined
}

function startNetworkTrafficPolling() {
  stopNetworkTrafficPolling()
  void loadNetworkTraffic()
  networkTrafficTimer = window.setInterval(() => void loadNetworkTraffic(), 1000)
}

async function loadVowifi() {
  try {
    vowifi.value = await api.vowifi()
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'vowifi.unableLoad')
  }
}

async function selectView(view: ViewID) {
  if (active.value === 'network' && view !== 'network') stopNetworkTrafficPolling()
  active.value = view
  viewError.value = ''
  if (view === 'sms') await loadSMS()
  if (view === 'esim') await loadEsim()
  if (view === 'network') { await loadNetwork(); startNetworkTrafficPolling() }
  if (view === 'vowifi') await loadVowifi()
}

async function sendSMS() {
  try {
    const result = await api.sendSMS(smsTo.value, smsBody.value)
    smsOperationID.value = result.operation_id
    smsMessage.value = t('sms.accepted', { id: result.operation_id })
    smsTo.value = ''
    smsBody.value = ''
  } catch (error) {
    smsMessage.value = errorText(error, 'sms.unableSend')
  }
}

async function runVowifi(action: 'enable' | 'disable' | 'reconnect') {
  vowifiMessage.value = ''
  try {
    const result = action === 'enable' ? await api.vowifiEnable() : action === 'disable' ? await api.vowifiDisable() : await api.vowifiReconnect()
    vowifiOperationID.value = result.operation_id
    vowifiMessage.value = t('esim.operationAccepted', { id: result.operation_id })
  } catch (error) {
    vowifiMessage.value = errorText(error, 'vowifi.unableUpdate')
  }
}

async function rescan() {
  try {
    await api.rescan()
    await device.refresh()
  } catch (error) {
    device.error = errorText(error, 'errors.generic')
  }
}

async function rebootModule() {
  if (!device.has('device_control')) return
  if (!window.confirm(t('network.rebootConfirm'))) return
  networkRebootMessage.value = t('network.rebooting')
  try {
    await api.reboot()
    networkRebootMessage.value = t('network.rebootAccepted')
    window.setTimeout(() => void device.refresh(), 8000)
    window.setTimeout(() => void loadNetwork(), 12000)
  } catch (error) {
    networkRebootMessage.value = errorText(error, 'network.unableReboot')
  }
}

async function checkNetwork() {
  try {
    const result = await api.networkCheck()
    networkMessage.value = result.detail ? `${result.summary}: ${result.detail}` : result.summary
  } catch (error) {
    networkMessage.value = errorText(error, 'network.unableCheck')
  }
}

async function setNetworkMode() {
  networkMessage.value = ''
  try {
    const result = await api.networkMode(networkMode.value)
    networkMessage.value = t('network.accepted', { id: result.operation_id })
  } catch (error) {
    networkMessage.value = errorText(error, 'network.unableChange')
  }
}

async function executeRawAT() {
  rawATMessage.value = ''
  rawATResponse.value = ''
  rawATExecutedCommand.value = ''
  if (!device.has('raw_at')) {
    rawATMessage.value = t('rawAt.unavailableDetail')
    return
  }
  try {
    const command = rawATCommand.value.trim()
    rawATResponse.value = (await api.rawAT(command)).response
    rawATExecutedCommand.value = command
  } catch (error) {
    rawATMessage.value = errorText(error, 'rawAt.unableExecute')
  }
}

onMounted(async () => { await device.refresh(); device.connect() })
onBeforeUnmount(stopNetworkTrafficPolling)
</script>

<template>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="brand"><span class="brand-mark">D</span><div><strong>DJOneHub</strong><small>{{ t('brand.subtitle') }}</small></div></div>
      <nav :aria-label="t('nav.primary')">
        <button
          v-for="item in nav"
          :key="item.id"
          :class="['nav-item', { active: active === item.id, muted: item.capability && !device.has(item.capability) }]"
          :aria-current="active === item.id ? 'page' : undefined"
          @click="selectView(item.id)"
        >{{ item.label }}</button>
      </nav>
      <div class="sidebar-footer"><span :class="['dot', { live: device.connected }]" />{{ device.connected ? t('status.live') : t('status.reconnecting') }}</div>
    </aside>
    <main class="main-content">
      <header class="topbar"><div><p class="eyebrow">{{ t('header.controlPlane') }}</p><h1>{{ activeLabel }}</h1></div><button class="button secondary" @click="rescan">{{ t('common.rescan') }}</button></header>
      <div v-if="device.error" class="alert">{{ device.error }} <button class="link-button" @click="device.refresh">{{ t('common.retry') }}</button></div>
      <div v-if="viewError" class="alert">{{ viewError }}</div>

      <section v-if="active === 'overview'" class="view-grid">
        <div class="status-banner"><div><span class="eyebrow">{{ t('overview.deviceStatus') }}</span><h2>{{ stateLabel }}</h2><p>{{ device.snapshot?.identity.product || t('overview.noModem') }}</p></div><span :class="['status-pill', stateValue]">{{ stateLabel }}</span></div>
        <div class="metric-grid"><article><span>{{ t('overview.imei') }}</span><strong>{{ device.status?.identity.imei || t('common.empty') }}</strong></article><article><span>{{ t('overview.sim') }}</span><strong>{{ device.status?.sim.inserted ? t('status.inserted') : t('common.notAvailable') }}</strong></article><article><span>{{ t('overview.registration') }}</span><strong>{{ device.status?.radio.registered ? t('status.registered') : t('status.offline') }}</strong></article><article><span>{{ t('overview.backend') }}</span><strong>{{ device.snapshot?.backend || t('common.empty') }}</strong></article></div>
        <div class="panel"><div class="panel-heading"><h2>{{ t('overview.radioNetwork') }}</h2><span>{{ device.status?.radio.network_mode || t('common.unknown') }}</span></div><div class="detail-list"><div><span>{{ t('overview.operator') }}</span><strong>{{ device.status?.radio.operator || t('common.empty') }}</strong></div><div><span>{{ t('overview.signal') }}</span><strong>{{ device.status?.radio.signal_dbm ? `${device.status.radio.signal_dbm} dBm` : t('common.empty') }}</strong></div><div><span>{{ t('overview.iccid') }}</span><strong>{{ device.status?.sim.iccid || t('common.empty') }}</strong></div><div><span>{{ t('overview.capabilities') }}</span><strong>{{ Object.keys(device.capabilities).length }}</strong></div></div></div>
        <div class="panel"><div class="panel-heading"><h2>{{ t('overview.availableCapabilities') }}</h2><span>{{ t('overview.serverReported') }}</span></div><div class="capability-list"><span v-for="(_, name) in device.capabilities" :key="name" class="capability">{{ name }}</span><span v-if="!Object.keys(device.capabilities).length" class="empty">{{ t('overview.capabilityReady') }}</span></div></div>
      </section>

      <section v-else-if="active === 'sms'" class="view-grid"><div class="panel"><div class="panel-heading"><h2>{{ t('sms.title') }}</h2><div class="panel-actions"><button class="button secondary" @click="refreshSMS">{{ t('common.refresh') }}</button><button class="button secondary" :disabled="!device.has('sms_read')" @click="clearModuleSMS">{{ t('common.clear') }}</button></div></div><div class="message-list"><div v-for="item in smsItems" :key="item.sender + ':' + item.received_at + ':' + item.index" class="message-row"><strong>{{ item.sender || t('sms.unknownSender') }}</strong><span>{{ item.body || t('sms.backendContent') }}</span><small v-if="item.received_at">{{ formatSMSDate(item.received_at) }}<span v-if="item.code"> · {{ t('sms.code', { code: item.code }) }} <button class="link-button" type="button" @click="copySMSCode(item.code)">{{ t('sms.copyCode') }}</button></span><span v-if="item.total_parts && item.total_parts > 1"> · {{ t('sms.part', { current: item.part_number, total: item.total_parts }) }}</span></small></div><p v-if="!smsItems.length" class="empty">{{ t('sms.noMessages') }}</p></div><p v-if="smsMessage" class="form-message">{{ smsMessage }}</p></div><div class="panel"><div class="panel-heading"><h2>{{ t('sms.sendTitle') }}</h2></div><form class="form" @submit.prevent="sendSMS"><label>{{ t('sms.recipient') }}<input v-model="smsTo" name="recipient" placeholder="+1 555 0100" required /></label><label>{{ t('sms.message') }}<textarea v-model="smsBody" name="message" rows="5" required /></label><button class="button primary" :disabled="!device.has('sms_send')">{{ t('common.send') }}</button><p v-if="smsMessage" class="form-message">{{ smsMessage }}</p><div v-if="smsOperation" class="operation-status"><span>{{ t('sms.sendStatus') }}</span><strong>{{ smsOperation.state }} · {{ smsOperation.progress }}%</strong><small>{{ smsOperation.message || smsOperation.operation_id }}</small></div></form></div></section>

      <section v-else-if="active === 'esim'" class="view-grid">
        <div class="panel esim-panel">
          <div class="panel-heading"><div><span class="eyebrow">{{ t('esim.eyebrow') }}</span><div class="section-title"><h2>{{ t('esim.profiles') }}</h2><button class="icon-button" type="button" :title="t('common.download')" :aria-label="t('common.download')" :disabled="!device.has('esim')" @click="openEsimDownload">+</button></div></div><button class="button secondary" @click="loadEsim">{{ t('common.refresh') }}</button></div>
          <div class="detail-list"><div><span>{{ t('esim.eid') }}</span><strong>{{ esim?.eid || t('common.empty') }}</strong></div><div><span>{{ t('esim.profiles') }}</span><strong>{{ esim?.profiles.length || 0 }}</strong></div></div>
          <div v-if="esim?.card_type === 'physical_sim'" class="unavailable"><h2>{{ t('esim.physical') }}</h2><p>{{ t('esim.physicalDetail') }}</p></div><div v-else-if="esim?.card_type === 'unknown'" class="unavailable"><h2>{{ t('esim.unavailable') }}</h2><p>{{ esim?.message || t('esim.unavailableDetail') }}</p></div>
          <div v-else class="message-list">
            <div v-for="profile in esim?.profiles" :key="profile.aid + ':' + profile.iccid" class="message-row profile-row">
              <div class="profile-summary"><strong>{{ profile.label || profile.iccid || t('esim.unnamed') }}</strong><span>{{ profile.state === 'enabled' ? t('esim.enabled') : profile.state === 'disabled' ? t('esim.disabled') : t('esim.stateUnavailable') }}</span><small>{{ profile.service_provider_name || t('esim.unknownProvider') }} · {{ profile.profile_class || t('esim.unknownClass') }}</small></div>
              <div class="profile-actions"><button class="button secondary" type="button" @click="openEsimRename(profile.iccid)">{{ t('common.rename') }}</button><button v-if="profile.state === 'disabled'" class="button primary" type="button" @click="enableEsim(profile.iccid)">{{ t('common.enable') }}</button><button v-if="profile.state !== 'enabled'" class="button secondary" type="button" @click="deleteEsim(profile.iccid)">{{ t('common.delete') }}</button></div>
            </div>
            <p v-if="!esim?.profiles.length" class="empty">{{ t('esim.noProfiles') }}</p>
          </div>
          <p v-if="esimMessage" class="form-message">{{ esimMessage }}</p>
          <div v-if="esimOperation" class="operation-status"><span>{{ t('common.operation') }}</span><strong>{{ esimOperation.state }} · {{ esimOperation.progress }}%</strong><small>{{ esimOperation.message || esimOperation.operation_id }}</small></div>
        </div>
        <div v-if="esimDownloadOpen" class="modal-backdrop" role="presentation" @click.self="closeEsimDownload">
          <section class="modal" role="dialog" aria-modal="true" aria-labelledby="download-profile-title">
            <div class="modal-heading"><div><span class="eyebrow">{{ t('esim.eyebrow') }}</span><h2 id="download-profile-title">{{ t('esim.downloadTitle') }}</h2></div><button class="icon-button modal-close" type="button" :title="t('common.close')" :aria-label="t('common.close')" @click="closeEsimDownload">x</button></div>
            <form class="form" @submit.prevent="downloadEsim"><label>{{ t('esim.activationCode') }}<input v-model="esimActivationCode" required placeholder="LPA:1$..." /></label><label>{{ t('esim.confirmationCode') }}<input v-model="esimConfirmationCode" /></label><label>{{ t('esim.matchingId') }}<input v-model="esimMatchingID" /></label><div class="modal-actions"><button class="button secondary" type="button" @click="closeEsimDownload">{{ t('common.cancel') }}</button><button class="button primary" type="submit" :disabled="!device.has('esim') || !esimActivationCode.trim()">{{ t('common.download') }}</button></div></form>
          </section>
        </div>
        <div v-if="esimRenameOpen" class="modal-backdrop" role="presentation" @click.self="closeEsimRename">
          <section class="modal" role="dialog" aria-modal="true" aria-labelledby="rename-profile-title">
            <div class="modal-heading"><div><span class="eyebrow">PROFILE</span><h2 id="rename-profile-title">{{ t('esim.renameTitle') }}</h2></div><button class="icon-button modal-close" type="button" :title="t('common.close')" :aria-label="t('common.close')" @click="closeEsimRename">x</button></div>
            <form class="form" @submit.prevent="submitEsimRename"><label>{{ t('esim.profileName') }}<input v-model="esimLabels[esimRenameICCID]" required maxlength="64" /></label><div class="modal-actions"><button class="button secondary" type="button" @click="closeEsimRename">{{ t('common.cancel') }}</button><button class="button primary" type="submit">{{ t('common.save') }}</button></div></form>
          </section>
        </div>
      </section>

      <section v-else-if="active === 'network'" class="view-grid"><div class="panel"><div class="panel-heading"><h2>{{ t('network.title') }}</h2><button class="button secondary" @click="loadNetwork">{{ t('common.refresh') }}</button></div><div class="detail-list"><div><span>{{ t('network.usbMode') }}</span><strong>{{ usbNetworkModeLabel(network?.mode) }}</strong></div><div><span>{{ t('network.radioMode') }}</span><strong>{{ network?.network_mode || t('common.empty') }}</strong></div><div><span>{{ t('network.interface') }}</span><strong>{{ network?.interface || t('common.empty') }}</strong></div><div><span>{{ t('network.defaultRoute') }}</span><strong>{{ network?.default_route || t('common.empty') }}</strong></div><div><span>{{ t('network.addresses') }}</span><strong>{{ network?.addresses?.join(', ') || t('common.empty') }}</strong></div><div><span>{{ t('network.traffic') }}</span><strong>{{ network ? `${formatBytes(networkTraffic.rxBytes)} ${t('network.received')} · ${formatBytes(networkTraffic.txBytes)} ${t('network.sent')}` : t('common.empty') }}</strong></div><div><span>{{ t('network.currentDownload') }}</span><strong>{{ formatBytes(networkTraffic.rxRate) }}/s</strong></div><div><span>{{ t('network.currentUpload') }}</span><strong>{{ formatBytes(networkTraffic.txRate) }}/s</strong></div></div><div class="panel-actions network-actions"><button class="button secondary" :disabled="!device.has('network_status')" @click="checkNetwork">{{ t('common.check') }}</button><button class="button secondary danger-button" :disabled="!device.has('device_control')" @click="rebootModule">{{ t('network.reboot') }}</button></div><p v-if="networkMessage" class="form-message">{{ networkMessage }}</p><p v-if="networkRebootMessage" class="form-message">{{ networkRebootMessage }}</p></div><div class="panel"><div class="panel-heading"><h2>{{ t('network.usbNetworkMode') }}</h2><span>{{ usbNetworkModeLabel(networkMode) }}</span></div><form class="form" @submit.prevent="setNetworkMode"><label>{{ t('network.mode') }}<select v-model="networkMode" required><option v-for="option in usbNetworkModeOptions" :key="option.value" :value="option.value">{{ option.label }} ({{ option.value }})</option></select></label><button class="button primary" type="submit" :disabled="!device.has('network_control')">{{ t('common.apply') }}</button><p v-if="!device.has('network_control')" class="form-message">{{ t('network.unavailableControl') }}</p></form></div></section>

      <section v-else-if="active === 'raw-at'" class="panel">
        <div v-if="device.has('raw_at')">
          <div class="panel-heading"><div><span class="eyebrow">{{ t('rawAt.ready') }}</span><h2>{{ t('rawAt.title') }}</h2></div></div>
          <form class="form" @submit.prevent="executeRawAT">
            <label>{{ t('rawAt.preset') }}<select v-model="rawATPreset" @change="applyATPreset"><option value="">{{ t('rawAt.selectPreset') }}</option><option v-for="preset in AT_PRESETS" :key="preset.id" :value="preset.id">{{ t(preset.labelKey) }} · {{ preset.command }}</option></select></label>
            <label>{{ t('rawAt.command') }}<input v-model="rawATCommand" name="at-command" placeholder="AT+CSQ" required @input="rawATPreset = ''" /></label>
            <button class="button primary" type="submit" :disabled="!rawATCommand.trim()">{{ t('common.execute') }}</button>
          </form>
          <p v-if="rawATMessage" class="form-message">{{ rawATMessage }}</p>
          <div v-if="parsedATResponse" class="at-result">
            <div class="at-result-heading"><h3>{{ t('rawAt.parsedTitle') }}</h3><span :class="['at-status', parsedATResponse.statusKey.endsWith('error') ? 'error' : parsedATResponse.statusKey.endsWith('ok') ? 'ok' : 'unknown']">{{ t(parsedATResponse.statusKey) }}</span></div>
            <dl v-if="parsedATResponse.fields.length" class="at-fields"><template v-for="(field, index) in parsedATResponse.fields" :key="`${field.labelKey}-${index}`"><dt>{{ t(field.labelKey) }}</dt><dd>{{ field.valueKey ? t(field.valueKey) : field.value }}</dd></template></dl>
            <p v-else class="empty">{{ t('rawAt.noParsedFields') }}</p>
          </div>
          <div v-if="rawATResponse" class="at-raw"><h3>{{ t('rawAt.rawTitle') }}</h3><pre class="at-response">{{ rawATResponse }}</pre></div>
        </div>
        <div v-else class="unavailable"><span class="eyebrow">{{ t('rawAt.unavailable') }}</span><h2>{{ t('rawAt.title') }}</h2><p>{{ t('rawAt.unavailableDetail') }}</p></div>
      </section>

      <section v-else-if="active === 'vowifi'" class="panel"><div class="panel-heading"><h2>{{ t('vowifi.title') }}</h2><button class="button secondary" @click="loadVowifi">{{ t('common.refresh') }}</button></div><div class="detail-list"><div><span>{{ t('vowifi.availability') }}</span><strong>{{ vowifi?.available === false ? t('vowifi.unavailable') : vowifi?.available ? t('vowifi.available') : t('common.empty') }}</strong></div><div><span>{{ t('vowifi.state') }}</span><strong>{{ vowifi?.state || t('common.empty') }}</strong></div><div><span>{{ t('vowifi.reason') }}</span><strong>{{ vowifi?.reason || t('common.empty') }}</strong></div></div><div class="panel-actions vowifi-actions"><button class="button primary" :disabled="!device.has('vowifi_control')" @click="runVowifi('enable')">{{ t('common.enable') }}</button><button class="button secondary" :disabled="!device.has('vowifi_control')" @click="runVowifi('disable')">{{ t('common.disable') }}</button><button class="button secondary" :disabled="!device.has('vowifi_control')" @click="runVowifi('reconnect')">{{ t('common.reconnect') }}</button></div><p v-if="!device.has('vowifi_control')" class="form-message">{{ t('vowifi.unavailableControl') }}</p><p v-if="vowifiMessage" class="form-message">{{ vowifiMessage }}</p><div v-if="vowifiOperation" class="operation-status"><span>{{ t('vowifi.operationStatus') }}</span><strong>{{ vowifiOperation.state }} · {{ vowifiOperation.progress }}%</strong><small>{{ vowifiOperation.message || vowifiOperation.operation_id }}</small></div></section>

      <section v-else-if="active === 'settings'" class="panel settings-panel">
        <div class="panel-heading"><div><span class="eyebrow">{{ t('settings.appearance') }}</span><h2>{{ t('settings.title') }}</h2></div></div>
        <form class="form settings-form"><label for="language-select">{{ t('language.title') }}<select id="language-select" v-model="locale"><option value="en-US">{{ t('language.english') }}</option><option value="zh-CN">{{ t('language.chinese') }}</option></select></label><p class="settings-detail">{{ t('settings.languageDetail') }}</p><p class="form-message">{{ t('settings.saved') }}</p></form>
      </section>
    </main>
  </div>
</template>
