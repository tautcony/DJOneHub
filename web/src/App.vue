<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { notification } from 'ant-design-vue'
import { EditOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { api } from './services/api'
import { APIError } from './services/api'
import { persistLocale } from './i18n'
import { AT_PRESETS, parseATResponse } from './services/at'
import { useDeviceStore } from './stores/device'
import type {
  CallStatus,
  EsimOverview,
  NotificationDebugEvent,
  NotificationDebugInfo,
  NotificationDebugRequest,
  NotificationPermissionStatus,
  NotificationPreferences,
  OperationStatus,
  NetworkStatus,
  NetworkTrafficRange,
  NetworkTrafficUpdate,
  SMSMessage,
  VowifiStatus,
} from './types'
import AppShell, { type ShellNavGroup } from './components/AppShell.vue'
import PageHeader from './components/PageHeader.vue'
import { viewContextKey } from './views/context'
import { viewFromRoute, viewPaths, type ViewID } from './router'

type NavGroupID = 'main' | 'voice' | 'tools'

const VIEW_REFRESH_MIN_INTERVAL_MS = 500
const OVERVIEW_REFRESH_MIN_INTERVAL_MS = 2000
const ACTIVE_VIEW_FALLBACK_INTERVALS: Partial<Record<ViewID, number>> = {
  calls: 15000,
  sms: 30000,
  esim: 60000,
  network: 15000,
  vowifi: 30000,
}

const device = useDeviceStore()
const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const active = computed<ViewID>(() => viewFromRoute(route.path))
const viewError = ref('')
const smsTo = ref('')
const smsBody = ref('')
const smsOperationID = ref('')
const smsOperation = computed(() =>
  smsOperationID.value ? device.operations[smsOperationID.value] : undefined,
)
const smsItems = ref<SMSMessage[]>([])
const smsSentItems = ref<SMSMessage[]>([])
const loadedViews = ref<Partial<Record<ViewID, boolean>>>({})
const smsQuery = ref('')
const selectedSmsPeer = ref('')
const smsComposeNew = ref(false)
const esim = ref<EsimOverview | null>(null)
const esimDownloadOpen = ref(false)
const esimSettingsOpen = ref(false)
const esimSettingsICCID = ref('')
const esimActivationCode = ref('')
const esimConfirmationCode = ref('')
const esimMatchingID = ref('')
const esimLabels = ref<Record<string, string>>({})
const esimOperationID = ref('')
const esimOperation = computed(() =>
  esimOperationID.value ? device.operations[esimOperationID.value] : undefined,
)
const esimReloadedOperationID = ref('')
const calls = ref<CallStatus | null>(null)
const esimNotes = ref<Record<string, { label: string; phone: string; tags: string }>>({})
const esimHealth = ref<Record<string, unknown> | null>(null)
const noteICCID = ref('')
const noteLabel = ref('')
const notePhone = ref('')
const noteTags = ref('')
const pageVisible = ref(document.visibilityState === 'visible')
const pendingViewRefreshes = new Map<ViewID, { timer: number; dueAt: number }>()
const viewLoadInFlight = new Map<ViewID, Promise<void>>()
const queuedViewRefreshes = new Set<ViewID>()
const lastViewRefreshAt = new Map<ViewID, number>()
let activeFallbackTimer: number | undefined
const network = ref<NetworkStatus | null>(null)
const overviewNetwork = ref<NetworkStatus | null>(null)
const networkMode = ref('')
const networkTraffic = ref({
  rxRate: 0,
  txRate: 0,
  rxBytes: 0,
  txBytes: 0,
  dailyAvailable: false,
  sampledAt: '',
  date: '',
})
type TrafficRange = 'day' | 'week' | 'month'
const trafficHistory = ref<Array<{ at: number; rxRate: number; txRate: number }>>([])
const trafficRangeData = ref<NetworkTrafficRange | null>(null)
let trafficRangeRequest = 0
let previousTrafficSample: { rx: number; tx: number; at: number } | undefined
const vowifi = ref<VowifiStatus | null>(null)
const vowifiOperationID = ref('')
const vowifiOperation = computed(() =>
  vowifiOperationID.value ? device.operations[vowifiOperationID.value] : undefined,
)
const rawATCommand = ref('')
const rawATExecutedCommand = ref('')
const rawATResponse = ref('')
const rawATPreset = ref('')
const parsedATResponse = computed(() =>
  rawATResponse.value
    ? parseATResponse(rawATExecutedCommand.value || rawATCommand.value, rawATResponse.value)
    : null,
)
const notifierInfo = ref<NotificationDebugInfo | null>(null)
const notifierEvents = ref<NotificationDebugEvent[]>([])
const notifierCallID = ref(`debug-call-${Date.now()}`)
const notifierNumber = ref('13800138000')
const notifierSender = ref('10086')
const notifierRecipient = ref('')
const notifierBody = ref('DJOneHubNotifier debug message')
const notificationPermissions = ref<NotificationPermissionStatus | null>(null)
const notificationPermissionBusy = ref(false)
const notificationPreferences = ref<NotificationPreferences | null>(null)
const notificationPreferencesBusy = ref(false)
const sensitiveStorageKey = 'djonehub.show-sensitive'
const showSensitive = ref(localStorage.getItem(sensitiveStorageKey) === '1')
const mobileNavOpen = ref(false)
const mobileNavExpanded = ref<Record<NavGroupID, boolean>>({ main: true, voice: true, tools: true })
const showNotificationDebug = computed(() => notificationPreferences.value?.show_debug !== false)

const navGroups = computed<ShellNavGroup[]>(() => [
  {
    id: 'main',
    label: t('nav.groups.main'),
    items: [
      { id: 'overview', label: t('nav.overview') },
      { id: 'sms', label: t('nav.sms'), capability: 'sms_read' },
      { id: 'esim', label: t('nav.esim'), capability: 'esim' },
      { id: 'network', label: t('nav.network'), capability: 'network_status' },
    ],
  },
  {
    id: 'voice',
    label: t('nav.groups.voice'),
    items: [
      { id: 'calls', label: t('nav.calls'), capability: 'call_monitor' },
      { id: 'vowifi', label: t('nav.vowifi'), capability: 'vowifi_inspect' },
    ],
  },
  {
    id: 'tools',
    label: t('nav.groups.tools'),
    items: [
      { id: 'raw-at', label: t('nav.rawAt'), capability: 'raw_at' },
      ...(showNotificationDebug.value ? [{ id: 'notifications', label: t('nav.notifications') }] : []),
      { id: 'settings', label: t('nav.settings') },
    ],
  },
])
const nav = computed(() => navGroups.value.flatMap((group) => group.items))
const stateValue = computed(() => device.snapshot?.state || 'offline')
const effectiveStateValue = computed(() => (device.error ? 'offline' : stateValue.value))
const stateLabel = computed(() =>
  te(`states.${effectiveStateValue.value}`) ? t(`states.${effectiveStateValue.value}`) : t('status.offline'),
)
const sidebarDeviceLabel = computed(() =>
  te(`status.deviceStates.${effectiveStateValue.value}`)
    ? t(`status.deviceStates.${effectiveStateValue.value}`)
    : t('status.deviceStates.offline'),
)
const activeLabel = computed(
  () => nav.value.find((item) => item.id === active.value)?.label || t('nav.overview'),
)
const activeDescription = computed(() => t(`header.descriptions.${active.value}`))
const deviceCapabilities = computed(() => (effectiveStateValue.value === 'ready' ? device.capabilities : {}))
const stateTone = computed<'success' | 'warning' | 'danger' | 'info' | 'neutral'>(() => {
  if (effectiveStateValue.value === 'ready') return 'success'
  if (effectiveStateValue.value === 'degraded') return 'warning'
  if (
    effectiveStateValue.value === 'connecting' ||
    effectiveStateValue.value === 'initializing' ||
    effectiveStateValue.value === 'discovered'
  )
    return 'info'
  return 'neutral'
})
const usbNetworkModeOptions = computed(() => [
  { value: '0', label: t('network.modes.rmnet') },
  { value: '1', label: t('network.modes.ecm') },
  { value: '2', label: t('network.modes.mbim') },
  { value: '3', label: t('network.modes.rndis') },
])
watch(locale, (value) => {
  persistLocale(value)
  notifySuccess(t('settings.saved'))
})
watch(showSensitive, (value) => {
  localStorage.setItem(sensitiveStorageKey, value ? '1' : '0')
  notifySuccess(t('settings.saved'))
})

function maskSensitive(value?: string) {
  if (!value) return t('common.empty')
  if (showSensitive.value) return value
  if (value.length <= 4) return '*'.repeat(value.length)
  return `${'*'.repeat(value.length - 4)}${value.slice(-4)}`
}

function usbNetworkModeLabel(mode?: string) {
  const option = usbNetworkModeOptions.value.find((item) => item.value === mode)
  return option ? `${option.label} (${option.value})` : mode || t('common.empty')
}

function errorText(cause: unknown, fallback: string) {
  if (cause instanceof APIError) {
    const key = `errors.${cause.code}`
    return te(key) ? t(key) : t('errors.generic')
  }
  return cause instanceof Error ? cause.message : t(fallback)
}

let lastErrorNotification: { message: string; at: number } | undefined

function notifyError(scope: 'device' | 'view', message: string) {
  if (!message) return
  const now = Date.now()
  if (lastErrorNotification?.message === message && now - lastErrorNotification.at < 10000) return
  lastErrorNotification = { message, at: now }
  const key = 'djonehub-error'
  const retryButton =
    scope === 'device'
      ? h(
          'button',
          {
            type: 'button',
            class: 'ant-btn ant-btn-link ant-btn-sm',
            onClick: () => {
              notification.close(key)
              void device.refresh()
            },
          },
          t('common.retry'),
        )
      : undefined
  notification.error({
    key,
    message: t('common.error'),
    description: message,
    btn: retryButton,
    placement: 'topRight',
    duration: 3.5,
  })
}

function notifySuccess(message: string) {
  if (!message) return
  notification.success({
    message,
    placement: 'topRight',
    duration: 3.5,
  })
}

function notifyInfo(message: string) {
  if (!message) return
  notification.info({
    message,
    placement: 'topRight',
    duration: 3.5,
  })
}

watch(
  () => device.error,
  (message) => {
    if (message) notifyError('device', message)
  },
)
watch(viewError, (message) => {
  if (message) notifyError('view', message)
})

function markViewLoaded(view: ViewID) {
  loadedViews.value[view] = true
}

function toggleMobileGroup(group: string) {
  if (!(group in mobileNavExpanded.value)) return
  const key = group as NavGroupID
  mobileNavExpanded.value[key] = !mobileNavExpanded.value[key]
}

function applyATPreset() {
  const preset = AT_PRESETS.find((item) => item.id === rawATPreset.value)
  if (preset) rawATCommand.value = preset.command
}

async function updateSMS() {
  const result = await api.smsRefresh()
  const items = Array.isArray(result.items) ? result.items : []
  smsItems.value = items
  reconcileSentSMS(items)
  syncSmsSelection()
}

async function loadSMS() {
  try {
    await updateSMS()
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'sms.unableLoad')
  } finally {
    markViewLoaded('sms')
  }
}

async function refreshSMS() {
  try {
    await updateSMS()
    notifySuccess(t('sms.refreshed'))
    viewError.value = ''
  } catch (error) {
    notifyError('view', errorText(error, 'sms.unableRefresh'))
    markViewLoaded('sms')
  }
}

async function clearModuleSMS() {
  try {
    await api.smsClear()
    notifySuccess(t('sms.cleared'))
  } catch (error) {
    notifyError('view', errorText(error, 'sms.unableClear'))
  }
}

function smsPeer(item: SMSMessage) {
  return item.sender || item.recipient || t('sms.unknownSender')
}

function smsThreadKey(item: SMSMessage) {
  return item.sender || item.recipient || 'unknown'
}

function reconcileSentSMS(items: SMSMessage[]) {
  smsSentItems.value = smsSentItems.value.filter((local) => {
    const localTime = local.received_at ? Date.parse(local.received_at) : NaN
    return !items.some((remote) => {
      if (!remote.recipient || remote.recipient !== local.recipient || remote.body !== local.body) {
        return false
      }
      const remoteTime = remote.received_at ? Date.parse(remote.received_at) : NaN
      return Number.isFinite(localTime) && Number.isFinite(remoteTime)
        ? Math.abs(remoteTime - localTime) < 60_000
        : true
    })
  })
}

// smsOrderingKey resolves the ordering key of a message. The backend sorts by
// recorded_at (device-local insertion time, one clock for both directions);
// received_at stays a display attribute because the SMSC clock is not synced
// with the device clock.
function smsOrderingKey(item: SMSMessage): number {
  const value = item.recorded_at ?? item.received_at
  const parsed = value ? Date.parse(value) : NaN
  return Number.isFinite(parsed) ? parsed : 0
}

const smsThreads = computed(() => {
  const groups = new Map<string, { key: string; peer: string; items: SMSMessage[]; latest?: SMSMessage }>()
  for (const item of [...smsItems.value, ...smsSentItems.value]) {
    const key = smsThreadKey(item)
    const group = groups.get(key) || { key, peer: smsPeer(item), items: [] }
    group.items.push(item)
    if (!group.latest || smsOrderingKey(item) > smsOrderingKey(group.latest)) group.latest = item
    groups.set(key, group)
  }
  const threads = [...groups.values()].sort((a, b) => smsOrderingKey(b.latest!) - smsOrderingKey(a.latest!))
  for (const thread of threads) thread.items.sort((a, b) => smsOrderingKey(b) - smsOrderingKey(a))
  return threads
})

const filteredSmsThreads = computed(() => {
  const query = smsQuery.value.trim().toLowerCase()
  if (!query) return smsThreads.value
  return smsThreads.value.filter(
    (thread) =>
      thread.peer.toLowerCase().includes(query) ||
      thread.items.some((item) => item.body.toLowerCase().includes(query)),
  )
})

const selectedSmsThread = computed(() =>
  smsComposeNew.value
    ? undefined
    : smsThreads.value.find((thread) => thread.key === selectedSmsPeer.value) || smsThreads.value[0],
)

function syncSmsSelection() {
  if (smsComposeNew.value) return
  const thread = smsThreads.value.find((item) => item.key === selectedSmsPeer.value) || smsThreads.value[0]
  if (!thread) {
    selectedSmsPeer.value = ''
    smsTo.value = ''
    return
  }
  selectedSmsPeer.value = thread.key
  smsTo.value = thread.peer
}

function startNewSMS() {
  smsComposeNew.value = true
  selectedSmsPeer.value = ''
  smsTo.value = ''
  smsBody.value = ''
  smsOperationID.value = ''
}

function resetSMSOperation() {
  smsOperationID.value = ''
}

async function loadEsim() {
  try {
    const result = await api.esim()
    esim.value = { ...result, profiles: Array.isArray(result.profiles) ? result.profiles : [] }
    const [notes, health] = await Promise.all([
      api.esimNotes().catch(() => ({ notes: {} })),
      api.esimHealth().catch(() => null),
    ])
    esimNotes.value = notes.notes
    esimHealth.value = health
    if (!noteICCID.value) noteICCID.value = esim.value.profiles[0]?.iccid || ''
    syncSelectedNote()
    for (const profile of esim.value.profiles) {
      if (profile.iccid && esimLabels.value[profile.iccid] === undefined) {
        esimLabels.value[profile.iccid] = profile.label || ''
      }
    }
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'esim.unableLoad')
  } finally {
    markViewLoaded('esim')
  }
}

async function loadCalls() {
  try {
    const result = await api.calls()
    calls.value = { ...result, history: Array.isArray(result.history) ? result.history : [] }
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'calls.unableLoad')
  } finally {
    markViewLoaded('calls')
  }
}
async function rejectCall() {
  try {
    await api.rejectCall()
    await loadView('calls')
  } catch (error) {
    viewError.value = errorText(error, 'calls.unableReject')
  }
}
function localProfileNote(iccid?: string) {
  return iccid ? esimNotes.value[iccid] : undefined
}
function noteSummary(note?: { label?: string; phone?: string; tags?: string; profile_class?: string }) {
  return note ? [note.label, note.phone, note.tags || note.profile_class].filter(Boolean).join(' · ') : ''
}
function syncSelectedNote() {
  const note = localProfileNote(noteICCID.value)
  noteLabel.value = note?.label || ''
  notePhone.value = note?.phone || ''
  noteTags.value = note?.tags || ''
}
function openEsimSettings(iccid?: string) {
  if (!iccid) return
  esimSettingsICCID.value = iccid
  noteICCID.value = iccid
  syncSelectedNote()
  esimSettingsOpen.value = true
}
function closeEsimSettings() {
  esimSettingsOpen.value = false
  esimSettingsICCID.value = ''
}
async function saveProfileNote() {
  if (!noteICCID.value) return
  try {
    const profile = esim.value?.profiles.find((item) => item.iccid === noteICCID.value)
    const label = (esimLabels.value[noteICCID.value] || '').trim()
    if (label && label !== profile?.label) await api.esimRename(noteICCID.value, label)
    await api.saveEsimNote(noteICCID.value, {
      label: noteLabel.value,
      phone: notePhone.value,
      tags: noteTags.value,
    })
    esimNotes.value[noteICCID.value] = {
      label: noteLabel.value,
      phone: notePhone.value,
      tags: noteTags.value,
    }
    notifySuccess(t('esim.noteSaved'))
    closeEsimSettings()
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableNote'))
  }
}
function isActiveView(view: ViewID) {
  return pageVisible.value && active.value === view
}

function stopActiveFallbackPolling() {
  if (activeFallbackTimer !== undefined) window.clearInterval(activeFallbackTimer)
  activeFallbackTimer = undefined
}

function startActiveFallbackPolling() {
  stopActiveFallbackPolling()
  const view = active.value
  const interval = ACTIVE_VIEW_FALLBACK_INTERVALS[view]
  if (pageVisible.value && !device.connected && interval) {
    activeFallbackTimer = window.setInterval(() => {
      if (!isActiveView(view) || device.connected) {
        syncActiveRefreshers()
        return
      }
      void loadView(view)
    }, interval)
  }
}

function clearPendingViewRefreshes() {
  for (const pending of pendingViewRefreshes.values()) window.clearTimeout(pending.timer)
  pendingViewRefreshes.clear()
  queuedViewRefreshes.clear()
}

function syncActiveRefreshers() {
  stopActiveFallbackPolling()
  if (!pageVisible.value) return
  if (!device.connected) startActiveFallbackPolling()
}

function scheduleViewRefresh(view: ViewID, delay = 0) {
  if (!isActiveView(view)) return
  const now = Date.now()
  const minInterval = view === 'overview' ? OVERVIEW_REFRESH_MIN_INTERVAL_MS : VIEW_REFRESH_MIN_INTERVAL_MS
  const earliestAt = (lastViewRefreshAt.get(view) || 0) + minInterval
  const dueAt = Math.max(now + delay, earliestAt)
  const pending = pendingViewRefreshes.get(view)
  if (pending && pending.dueAt <= dueAt) return
  if (pending) window.clearTimeout(pending.timer)
  const timer = window.setTimeout(
    () => {
      pendingViewRefreshes.delete(view)
      if (isActiveView(view)) void loadView(view)
    },
    Math.max(0, dueAt - now),
  )
  pendingViewRefreshes.set(view, { timer, dueAt })
}

function operationView(operation: OperationStatus): ViewID | undefined {
  if (operation.type.startsWith('sms.')) return 'sms'
  if (operation.type.startsWith('esim.')) return 'esim'
  if (operation.type.startsWith('network.')) return 'network'
  if (operation.type.startsWith('vowifi.')) return 'vowifi'
  return undefined
}

function eventViews(eventType: string): ViewID[] {
  if (eventType.startsWith('call.')) return ['calls']
  switch (eventType) {
    case 'sms.received':
    case 'sms.updated':
      return ['sms']
    case 'esim.updated':
      return ['esim']
    case 'network.updated':
      return ['network', 'overview']
    case 'vowifi.updated':
      return ['vowifi']
    case 'sim.updated':
    case 'device.offline':
      return ['overview']
    default:
      return []
  }
}

watch(esimOperation, (operation) => {
  if (!operation || esimReloadedOperationID.value === operation.operation_id) return
  if (operation.state !== 'succeeded' && operation.state !== 'failed' && operation.state !== 'cancelled')
    return

  esimReloadedOperationID.value = operation.operation_id
  const delay = operation.state === 'succeeded' ? 1200 : 0
  scheduleViewRefresh('esim', delay)
})

watch(
  () => device.eventRevision,
  () => {
    const eventType = device.lastEventType
    if (eventType === 'snapshot' || eventType === 'resync.required') {
      if (active.value !== 'overview') scheduleViewRefresh(active.value)
      return
    }
    if (eventType === 'device.status.changed') {
      scheduleViewRefresh('overview')
      return
    }
    if (eventType === 'network.traffic.updated') {
      applyNetworkTraffic(device.lastEventData)
      return
    }
    if (eventType.startsWith('backend.')) {
      scheduleViewRefresh('overview', 250)
      return
    }
    const views = eventViews(eventType)
    if (views.length) {
      for (const view of views) scheduleViewRefresh(view, view === 'esim' ? 1200 : 0)
      return
    }
    if (eventType !== 'operation.completed') return
    const operation = device.lastEventData as OperationStatus
    if (!operation || typeof operation.type !== 'string') return
    const view = operationView(operation)
    if (!view) return
    scheduleViewRefresh(view, view === 'esim' && operation.state === 'succeeded' ? 1200 : 0)
  },
)

async function enableEsim(iccid?: string) {
  if (!iccid) return
  try {
    const result = await api.esimEnable(iccid)
    esimOperationID.value = result.operation_id
    notifySuccess(t('esim.operationAccepted', { id: result.operation_id }))
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableEnable'))
  }
}

async function deleteEsim(iccid?: string) {
  if (!iccid) return
  try {
    const result = await api.esimDelete(iccid)
    esimOperationID.value = result.operation_id
    notifySuccess(t('esim.operationAccepted', { id: result.operation_id }))
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableDelete'))
  }
}

function openEsimDownload() {
  esimDownloadOpen.value = true
}

function closeEsimDownload() {
  esimDownloadOpen.value = false
}

async function downloadEsim() {
  try {
    const result = await api.esimDownload(
      esimActivationCode.value,
      esimConfirmationCode.value,
      esimMatchingID.value,
    )
    esimOperationID.value = result.operation_id
    notifySuccess(t('esim.operationAccepted', { id: result.operation_id }))
    closeEsimDownload()
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableDownload'))
  }
}

async function loadNetwork() {
  try {
    network.value = await api.network()
    networkMode.value = network.value.mode || ''
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'network.unableLoad')
  } finally {
    markViewLoaded('network')
  }
}

async function loadOverviewNetwork() {
  try {
    overviewNetwork.value = await api.network()
  } catch {
    overviewNetwork.value = null
  }
}

async function loadOverviewTraffic() {
  try {
    const result = await api.networkTrafficDaily()
    networkTraffic.value = {
      ...networkTraffic.value,
      rxBytes: result.rx_bytes,
      txBytes: result.tx_bytes,
      dailyAvailable: result.available,
      sampledAt: result.sampled_at || '',
      date: result.date,
    }
  } catch {
    networkTraffic.value = { ...networkTraffic.value, dailyAvailable: false }
  }
}

async function loadTrafficRange(period: TrafficRange) {
  const requestID = ++trafficRangeRequest
  try {
    const result = await api.networkTrafficRange(period)
    if (requestID === trafficRangeRequest) trafficRangeData.value = result
  } catch {
    if (requestID === trafficRangeRequest) trafficRangeData.value = null
  }
}

function applyNetworkTraffic(data: unknown) {
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
  networkTraffic.value = {
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

async function loadVowifi() {
  try {
    vowifi.value = await api.vowifi()
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'vowifi.unableLoad')
  } finally {
    markViewLoaded('vowifi')
  }
}

function newNotifierCall() {
  notifierCallID.value = `debug-call-${Date.now()}`
}

function debugEventData(event: NotificationDebugEvent) {
  return JSON.stringify(event.data, null, 2) || '{}'
}

async function loadNotifierDebug() {
  try {
    const result = await api.notificationDebugInfo()
    notifierInfo.value = { ...result, actions: Array.isArray(result.actions) ? result.actions : [] }
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'notifications.unableLoad')
  } finally {
    markViewLoaded('notifications')
  }
}

async function loadNotificationPermissions() {
  try {
    notificationPermissions.value = await api.notificationPermissions()
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'settings.unableLoadNotifications')
  } finally {
    markViewLoaded('settings')
  }
}

async function loadSettings() {
  try {
    const [permissions, preferences] = await Promise.all([
      api.notificationPermissions(),
      api.notificationPreferences(),
    ])
    notificationPermissions.value = permissions
    notificationPreferences.value = preferences.preferences
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'settings.unableLoadNotifications')
  } finally {
    markViewLoaded('settings')
  }
}

function notificationPermissionLabel(state?: NotificationPermissionStatus['state']) {
  return state && te(`settings.notificationStates.${state}`)
    ? t(`settings.notificationStates.${state}`)
    : t('common.unknown')
}

async function requestNotificationPermission() {
  notificationPermissionBusy.value = true
  try {
    const result = await api.requestNotificationPermission()
    notificationPermissions.value = result
    if (result.accepted) {
      notifyInfo(t('settings.notificationPrompt'))
      window.setTimeout(() => {
        if (isActiveView('settings')) void loadNotificationPermissions()
      }, 1000)
    } else {
      notifyError('view', t('settings.notificationsUnavailable'))
    }
  } catch (error) {
    notifyError('view', errorText(error, 'settings.unableUpdateNotifications'))
  } finally {
    notificationPermissionBusy.value = false
  }
}

async function openNotificationSettings() {
  notificationPermissionBusy.value = true
  try {
    const result = await api.openNotificationSettings()
    notificationPermissions.value = result
    if (result.accepted) {
      notifyInfo(t('settings.notificationSettingsOpened'))
    } else {
      notifyError('view', t('settings.notificationsUnavailable'))
    }
  } catch (error) {
    notifyError('view', errorText(error, 'settings.unableUpdateNotifications'))
  } finally {
    notificationPermissionBusy.value = false
  }
}

async function saveNotificationPreferences() {
  if (!notificationPreferences.value) return
  notificationPreferencesBusy.value = true
  try {
    const result = await api.updateNotificationPreferences({ ...notificationPreferences.value })
    notificationPreferences.value = result.preferences
    notifySuccess(t('settings.presentationSaved'))
  } catch (error) {
    notifyError('view', errorText(error, 'settings.unableUpdateNotifications'))
    if (isActiveView('settings')) await loadSettings()
  } finally {
    notificationPreferencesBusy.value = false
  }
}

async function triggerNotifierDebug(action: string) {
  const payload: NotificationDebugRequest = { action }
  if (action.startsWith('call_')) {
    payload.call_id = notifierCallID.value.trim()
    payload.number = notifierNumber.value.trim()
  }
  if (action === 'sms_received') {
    payload.sender = notifierSender.value.trim()
    payload.recipient = notifierRecipient.value.trim()
    payload.body = notifierBody.value
  }
  try {
    const result = await api.notificationDebug(payload)
    notifierInfo.value = notifierInfo.value
      ? { ...notifierInfo.value, native_ui: result.native_ui }
      : notifierInfo.value
    notifierEvents.value = [...result.events.slice().reverse(), ...notifierEvents.value].slice(0, 40)
    notifySuccess(t('notifications.published', { count: result.events.length }))
    const firstData = result.events[0]?.data
    if (action === 'call_incoming' && firstData && typeof firstData === 'object' && 'id' in firstData) {
      notifierCallID.value = String((firstData as { id: unknown }).id)
    }
  } catch (error) {
    notifyError('view', errorText(error, 'notifications.unablePublish'))
  }
}

const viewLoaders: Partial<Record<ViewID, () => Promise<void>>> = {
  overview: async () => {
    await Promise.all([device.refresh(), loadOverviewNetwork(), loadOverviewTraffic(), loadEsim()])
  },
  calls: loadCalls,
  sms: loadSMS,
  esim: loadEsim,
  network: loadNetwork,
  vowifi: loadVowifi,
  notifications: loadNotifierDebug,
  settings: loadSettings,
}

function handleViewLoadError(view: ViewID, error: unknown) {
  if (isActiveView(view)) viewError.value = errorText(error, 'errors.generic')
}

async function loadView(view: ViewID) {
  if (!isActiveView(view)) return
  const existing = viewLoadInFlight.get(view)
  if (existing) {
    queuedViewRefreshes.add(view)
    try {
      await existing
    } catch {
      // The first caller reports the error; this waiter must still resolve.
    }
    return
  }
  const loader = viewLoaders[view]
  if (!loader) return
  lastViewRefreshAt.set(view, Date.now())
  let request: Promise<void>
  try {
    request = loader()
  } catch (error) {
    handleViewLoadError(view, error)
    return
  }
  viewLoadInFlight.set(view, request)
  try {
    await request
  } catch (error) {
    handleViewLoadError(view, error)
  } finally {
    if (viewLoadInFlight.get(view) === request) viewLoadInFlight.delete(view)
    if (queuedViewRefreshes.delete(view) && isActiveView(view)) scheduleViewRefresh(view)
  }
}

function selectView(view: string) {
  if (!nav.value.some((item) => item.id === view)) return
  const group = navGroups.value.find((item) => item.items.some((item) => item.id === view))
  if (group) mobileNavExpanded.value[group.id as NavGroupID] = true
  mobileNavOpen.value = false
  const nextView = view as ViewID
  if (active.value === nextView) {
    void loadView(nextView)
    return
  }
  void router.push(viewPaths[nextView])
  viewError.value = ''
}

watch(active, (view) => {
  syncActiveRefreshers()
  void loadView(view)
})

watch(
  () => device.connected,
  () => {
    syncActiveRefreshers()
  },
)

function handleVisibilityChange() {
  pageVisible.value = document.visibilityState === 'visible'
  if (!pageVisible.value) clearPendingViewRefreshes()
  syncActiveRefreshers()
  if (pageVisible.value) void loadView(active.value)
}

async function sendSMS() {
  const recipient = smsTo.value.trim()
  const body = smsBody.value.trim()
  if (!recipient || !body) return
  try {
    const result = await api.sendSMS(recipient, body)
    smsOperationID.value = result.operation_id
    smsSentItems.value = [
      ...smsSentItems.value,
      {
        index: -Date.now(),
        recipient,
        body,
        received_at: new Date().toISOString(),
        recorded_at: new Date().toISOString(),
      },
    ]
    selectedSmsPeer.value = recipient
    smsComposeNew.value = false
    notifySuccess(t('sms.accepted', { id: result.operation_id }))
    smsBody.value = ''
  } catch (error) {
    notifyError('view', errorText(error, 'sms.unableSend'))
  }
}

async function runVowifi(action: 'enable' | 'disable' | 'reconnect') {
  try {
    const result =
      action === 'enable'
        ? await api.vowifiEnable()
        : action === 'disable'
          ? await api.vowifiDisable()
          : await api.vowifiReconnect()
    vowifiOperationID.value = result.operation_id
    notifySuccess(t('esim.operationAccepted', { id: result.operation_id }))
  } catch (error) {
    notifyError('view', errorText(error, 'vowifi.unableUpdate'))
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
  notifyInfo(t('network.rebooting'))
  try {
    await api.reboot()
    notifySuccess(t('network.rebootAccepted'))
    window.setTimeout(() => void device.refresh(), 8000)
    scheduleViewRefresh('network', 12000)
  } catch (error) {
    notifyError('view', errorText(error, 'network.unableReboot'))
  }
}

async function checkNetwork() {
  try {
    const result = await api.networkCheck()
    const message = result.detail ? `${result.summary}: ${result.detail}` : result.summary
    if (result.ok) notifySuccess(message)
    else notifyError('view', message)
  } catch (error) {
    notifyError('view', errorText(error, 'network.unableCheck'))
  }
}

async function setNetworkMode() {
  try {
    const result = await api.networkMode(networkMode.value)
    notifySuccess(t('network.accepted', { id: result.operation_id }))
  } catch (error) {
    notifyError('view', errorText(error, 'network.unableChange'))
  }
}

async function executeRawAT() {
  rawATResponse.value = ''
  rawATExecutedCommand.value = ''
  if (!device.has('raw_at')) {
    notifyError('view', t('rawAt.unavailableDetail'))
    return
  }
  try {
    const command = rawATCommand.value.trim()
    rawATResponse.value = (await api.rawAT(command)).response
    rawATExecutedCommand.value = command
  } catch (error) {
    notifyError('view', errorText(error, 'rawAt.unableExecute'))
  }
}

provide(viewContextKey, {
  device,
  deviceCapabilities,
  loadView,
  loadedViews,
  networkTraffic,
  trafficHistory,
  trafficRangeData,
  loadTrafficRange,
  overviewNetwork,
  showSensitive,
  stateLabel,
  stateTone,
  calls,
  rejectCall,
  maskSensitive,
  clearModuleSMS,
  filteredSmsThreads,
  refreshSMS,
  selectedSmsPeer,
  selectedSmsThread,
  resetSMSOperation,
  smsComposeNew,
  sendSMS,
  smsBody,
  smsOperation,
  smsQuery,
  smsTo,
  startNewSMS,
  closeEsimDownload,
  closeEsimSettings,
  deleteEsim,
  downloadEsim,
  enableEsim,
  esim,
  esimActivationCode,
  esimConfirmationCode,
  esimDownloadOpen,
  esimLabels,
  esimMatchingID,
  esimOperation,
  esimSettingsOpen,
  esimSettingsICCID,
  localProfileNote,
  noteLabel,
  notePhone,
  noteTags,
  noteSummary,
  openEsimDownload,
  openEsimSettings,
  saveProfileNote,
  checkNetwork,
  loadNetwork,
  network,
  networkMode,
  rebootModule,
  setNetworkMode,
  usbNetworkModeLabel,
  usbNetworkModeOptions,
  AT_PRESETS,
  applyATPreset,
  executeRawAT,
  parsedATResponse,
  rawATCommand,
  rawATPreset,
  rawATResponse,
  loadVowifi,
  runVowifi,
  vowifi,
  vowifiOperation,
  debugEventData,
  loadNotificationPermissions,
  newNotifierCall,
  notifierBody,
  notifierCallID,
  notifierEvents,
  notifierInfo,
  notifierNumber,
  notifierRecipient,
  notifierSender,
  triggerNotifierDebug,
  locale,
  notificationPermissionBusy,
  notificationPermissionLabel,
  notificationPermissions,
  notificationPreferences,
  notificationPreferencesBusy,
  openNotificationSettings,
  requestNotificationPermission,
  saveNotificationPreferences,
})

onMounted(async () => {
  document.addEventListener('visibilitychange', handleVisibilityChange)
  await device.refresh()
  device.connect()
  syncActiveRefreshers()
})
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', handleVisibilityChange)
  clearPendingViewRefreshes()
  stopActiveFallbackPolling()
})
</script>

<template>
  <AppShell
    :nav-groups="navGroups"
    :active="active"
    :connected="device.connected"
    :connection-label="device.connected ? t('status.linkReady') : t('status.linkReconnecting')"
    :device-ready="effectiveStateValue === 'ready'"
    :device-label="sidebarDeviceLabel"
    :rescan-label="t('common.rescan')"
    :primary-label="t('nav.primary')"
    :menu-label="t('nav.menu')"
    :close-label="t('nav.close')"
    :mobile-open="mobileNavOpen"
    :mobile-expanded="mobileNavExpanded"
    @select-view="selectView"
    @toggle-mobile="mobileNavOpen = !mobileNavOpen"
    @toggle-group="toggleMobileGroup"
    @rescan="rescan"
  >
    <PageHeader :eyebrow="t('header.controlPlane')" :title="activeLabel" :subtitle="activeDescription">
      <template v-if="active === 'sms'" #actions>
        <a-button @click="refreshSMS"><ReloadOutlined />{{ t('common.refresh') }}</a-button>
        <a-button type="primary" :disabled="!device.has('sms_send')" @click="startNewSMS">
          <EditOutlined />{{ t('sms.newMessage') }}
        </a-button>
      </template>
    </PageHeader>
    <RouterView />
  </AppShell>
</template>
