<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { notification } from 'ant-design-vue'
import { EditOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { api } from './services/api'
import { APIError } from './services/api'
import { persistLocale } from './i18n'
import { AT_PRESETS, parseATResponse } from './services/at'
import { useDeviceStore } from './stores/device'
import { useEsimStore } from './stores/esim'
import { useNetworkStore } from './stores/network'
import { useSmsStore } from './stores/sms'
import { useVowifiStore } from './stores/vowifi'
import type {
  CallStatus,
  FirmwareStatus,
  NotificationDebugEvent,
  NotificationDebugInfo,
  NotificationDebugRequest,
  NotificationPermissionStatus,
  NotificationPreferences,
  OperationStatus,
  StartupStatus,
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
const loadedViews = ref<Partial<Record<ViewID, boolean>>>({})
const calls = ref<CallStatus | null>(null)
const pageVisible = ref(document.visibilityState === 'visible')
const pendingViewRefreshes = new Map<ViewID, { timer: number; dueAt: number }>()
const viewLoadInFlight = new Map<ViewID, Promise<void>>()
const queuedViewRefreshes = new Set<ViewID>()
const lastViewRefreshAt = new Map<ViewID, number>()
let activeFallbackTimer: number | undefined

// 域状态按 domain 拆分到 Pinia stores (SMS/eSIM/network/VoWiFi); 此处仅
// 保留 shell 编排 (导航、连接、能力门控、刷新调度、通知) 与未拆分域。
const sms = useSmsStore()
const {
  query: smsQuery,
  selectedPeer: selectedSmsPeer,
  composeNew: smsComposeNew,
  to: smsTo,
  body: smsBody,
  operation: smsOperation,
  filteredThreads: filteredSmsThreads,
  selectedThread: selectedSmsThread,
} = storeToRefs(sms)

const esimStore = useEsimStore()
const {
  overview: esim,
  downloadOpen: esimDownloadOpen,
  settingsOpen: esimSettingsOpen,
  settingsICCID: esimSettingsICCID,
  activationCode: esimActivationCode,
  confirmationCode: esimConfirmationCode,
  matchingID: esimMatchingID,
  labels: esimLabels,
  operation: esimOperation,
  reloadedOperationID: esimReloadedOperationID,
  noteICCID,
  noteLabel,
  notePhone,
  noteTags,
} = storeToRefs(esimStore)

const networkStore = useNetworkStore()
const {
  status: network,
  overviewStatus: overviewNetwork,
  mode: networkMode,
  traffic: networkTraffic,
  trafficHistory,
  trafficRangeData,
} = storeToRefs(networkStore)
type TrafficRange = 'day' | 'week' | 'month'

const vowifiStore = useVowifiStore()
const { status: vowifi, operation: vowifiOperation } = storeToRefs(vowifiStore)
const firmware = ref<FirmwareStatus | null>(null)
const firmwareOperationID = ref('')
const firmwareOperationModalOpen = ref(false)
const firmwareOperation = computed(() =>
  firmwareOperationID.value ? device.operations[firmwareOperationID.value] : undefined,
)
const firmwareOperationLogs = computed(() =>
  firmwareOperationID.value ? device.operationLogs[firmwareOperationID.value] || [] : [],
)
const notifiedFirmwareOperations = new Set<string>()
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
const startupSettings = ref<StartupStatus | null>(null)
const startupBusy = ref(false)
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
      { id: 'firmware', label: t('nav.firmware'), capability: 'raw_at' },
      ...(showNotificationDebug.value ? [{ id: 'notifications', label: t('nav.notifications') }] : []),
      { id: 'settings', label: t('nav.settings') },
    ],
  },
])
const nav = computed(() => navGroups.value.flatMap((group) => group.items))
// 导航项常驻显示: 能力缺失交由各视图内部处理 (device.has 门控、RawAt 的
// "不可用" 提示等), 不在导航层按能力快照隐藏, 避免设备未就绪时菜单塌缩。
const stateValue = computed(() => device.snapshot?.state || 'offline')
const effectiveStateValue = computed(() => (device.error ? 'offline' : stateValue.value))
watch(effectiveStateValue, (state) => {
  if (state === 'ready') return
  firmware.value = null
  const operation = firmwareOperation.value
  if (!operation || ['succeeded', 'failed', 'cancelled'].includes(operation.state)) {
    firmwareOperationID.value = ''
    firmwareOperationModalOpen.value = false
  }
})
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
// 文档语言属性与激活 locale 保持同步 (BCP 47 标签)。
function syncDocumentLang(value: string) {
  document.documentElement.lang = value
}
watch(locale, (value) => {
  persistLocale(value)
  syncDocumentLang(value)
  notifySuccess(t('settings.languageSwitched'))
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

async function loadSMS() {
  try {
    await sms.refresh()
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'sms.unableLoad')
  } finally {
    markViewLoaded('sms')
  }
}

async function refreshSMS() {
  try {
    await sms.refresh()
    notifySuccess(t('sms.refreshed'))
    viewError.value = ''
  } catch (error) {
    notifyError('view', errorText(error, 'sms.unableRefresh'))
    markViewLoaded('sms')
  }
}

async function clearModuleSMS() {
  try {
    await sms.clear()
    notifySuccess(t('sms.cleared'))
  } catch (error) {
    notifyError('view', errorText(error, 'sms.unableClear'))
  }
}

function startNewSMS() {
  sms.startNew()
}

function resetSMSOperation() {
  sms.resetOperation()
}

async function loadEsim() {
  try {
    await esimStore.load()
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
  return esimStore.localProfileNote(iccid)
}
function noteSummary(note?: { label?: string; phone?: string; tags?: string; profile_class?: string }) {
  return esimStore.noteSummary(note)
}
function openEsimSettings(iccid?: string) {
  esimStore.openSettings(iccid)
}
function closeEsimSettings() {
  esimStore.closeSettings()
}
async function saveProfileNote() {
  if (!noteICCID.value) return
  try {
    await esimStore.saveNote()
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
  if (operation.type.startsWith('firmware.')) return 'firmware'
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

watch(firmwareOperation, (operation) => {
  if (!operation || !['succeeded', 'failed', 'cancelled'].includes(operation.state)) return
  if (notifiedFirmwareOperations.has(operation.operation_id)) return
  notifiedFirmwareOperations.add(operation.operation_id)
  if (operation.state === 'succeeded') notifySuccess(t('firmware.operationSucceeded'))
  else notifyError('view', operation.error?.message || t('firmware.operationFailed'))
})

watch(firmwareOperationID, (operationID) => {
  if (operationID) firmwareOperationModalOpen.value = true
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
      if (active.value === 'firmware') scheduleViewRefresh('firmware', 700)
      // 状态变化也应刷新当前激活视图, 避免非 overview/firmware 视图在设备
      // 就绪后仍停留在过期/加载态。
      else if (active.value !== 'overview') scheduleViewRefresh(active.value)
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
    const delay =
      operation.type === 'firmware.enter_edl' && operation.state === 'succeeded'
        ? 1200
        : view === 'esim' && operation.state === 'succeeded'
          ? 1200
          : 0
    scheduleViewRefresh(view, delay)
  },
)

async function enableEsim(iccid?: string) {
  if (!iccid) return
  try {
    const result = await esimStore.enable(iccid)
    notifySuccess(t('esim.operationAccepted', { id: result.operation_id }))
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableEnable'))
  }
}

async function deleteEsim(iccid?: string) {
  if (!iccid) return
  try {
    const result = await esimStore.remove(iccid)
    notifySuccess(t('esim.operationAccepted', { id: result.operation_id }))
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableDelete'))
  }
}

function openEsimDownload() {
  esimStore.openDownload()
}

function closeEsimDownload() {
  esimStore.closeDownload()
}

async function downloadEsim() {
  try {
    const result = await esimStore.download()
    notifySuccess(t('esim.operationAccepted', { id: result.operation_id }))
    closeEsimDownload()
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableDownload'))
  }
}

async function loadNetwork() {
  try {
    await networkStore.load()
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'network.unableLoad')
  } finally {
    markViewLoaded('network')
  }
}

async function loadOverviewNetwork() {
  await networkStore.loadOverview()
}

async function loadOverviewTraffic() {
  await networkStore.loadTrafficDaily()
}

async function loadTrafficRange(period: TrafficRange) {
  await networkStore.loadTrafficRange(period)
}

function applyNetworkTraffic(data: unknown) {
  networkStore.applyTraffic(data)
}

async function loadVowifi() {
  try {
    await vowifiStore.load()
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'vowifi.unableLoad')
  } finally {
    markViewLoaded('vowifi')
  }
}

async function loadFirmware() {
  try {
    firmware.value = await api.firmware()
    viewError.value = ''
  } catch (error) {
    firmware.value = null
    viewError.value = errorText(error, 'firmware.unableLoad')
  } finally {
    markViewLoaded('firmware')
  }
}

async function refreshFirmware() {
  await loadFirmware()
}

async function runFirmwareAction(action: 'unlock' | 'enable' | 'disable' | 'edl', serial = '') {
  try {
    const result =
      action === 'unlock'
        ? await api.firmwareADBUnlock()
        : action === 'enable'
          ? await api.firmwareADBMode(true)
          : action === 'disable'
            ? await api.firmwareADBMode(false)
            : await api.firmwareMode('edl', serial)
    firmwareOperationID.value = result.operation_id
    notifySuccess(t('firmware.operationAccepted', { id: result.operation_id }))
  } catch (error) {
    notifyError('view', errorText(error, 'firmware.unableAction'))
  }
}

async function updateFirmwareUSBID(vid: string, pid: string) {
  try {
    const result = await api.firmwareUSBID(vid, pid)
    firmwareOperationID.value = result.operation_id
    notifySuccess(t('firmware.operationAccepted', { id: result.operation_id }))
  } catch (error) {
    notifyError('view', errorText(error, 'firmware.unableUSBID'))
  }
}

async function backupFirmware(outputPath: string, loaderPath: string, edlPath: string, edlRunner: 'python' | 'uv') {
  try {
    const result = await api.firmwareBackup(outputPath, loaderPath, edlPath, edlRunner)
    firmwareOperationID.value = result.operation_id
    notifySuccess(t('firmware.operationAccepted', { id: result.operation_id }))
  } catch (error) {
    notifyError('view', errorText(error, 'firmware.unableBackup'))
  }
}

async function selectFirmwareEDLDirectory() {
  try {
    const result = await api.selectFirmwareEDLDirectory()
    return result.directory
  } catch (error) {
    if (error instanceof APIError && error.message.includes('cancelled')) return ''
    notifyError('view', errorText(error, 'firmware.unableSelectEDLDirectory'))
    return ''
  }
}

async function selectFirmwareBackupDirectory() {
  try {
    const result = await api.selectFirmwareBackupDirectory()
    return result.directory
  } catch (error) {
    if (error instanceof APIError && error.message.includes('cancelled')) return ''
    notifyError('view', errorText(error, 'firmware.unableSelectDirectory'))
    return ''
  }
}

async function selectFirmwareADBFile() {
  try {
    const result = await api.selectFirmwareADBFile()
    return result.path
  } catch (error) {
    if (error instanceof APIError && error.message.includes('cancelled')) return ''
    notifyError('view', errorText(error, 'firmware.adb.unableSelectADBFile'))
    return ''
  }
}

async function saveFirmwareADBCommand(command: string) {
  try {
    const result = await api.firmwareSetADBCommand(command)
    notifySuccess(t('firmware.adb.commandSaved'))
    return result.command
  } catch (error) {
    notifyError('view', errorText(error, 'firmware.adb.unableSaveCommand'))
    return ''
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
    const [permissions, preferences, startup] = await Promise.all([
      api.notificationPermissions(),
      api.notificationPreferences(),
      api.startupSettings(),
    ])
    notificationPermissions.value = permissions
    notificationPreferences.value = preferences.preferences
    startupSettings.value = startup
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'settings.unableLoadNotifications')
  } finally {
    markViewLoaded('settings')
  }
}

async function toggleStartup(enabled: boolean) {
  startupBusy.value = true
  try {
    startupSettings.value = await api.updateStartupSettings(enabled)
    notifySuccess(t('settings.startupSaved'))
  } catch (error) {
    notifyError('view', errorText(error, 'settings.unableUpdateStartup'))
    if (isActiveView('settings')) await loadSettings()
  } finally {
    startupBusy.value = false
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
  firmware: loadFirmware,
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
  if (!smsTo.value.trim() || !smsBody.value.trim()) return
  try {
    const result = await sms.send()
    notifySuccess(t('sms.accepted', { id: result.operation_id }))
  } catch (error) {
    notifyError('view', errorText(error, 'sms.unableSend'))
  }
}

async function runVowifi(action: 'enable' | 'disable' | 'reconnect') {
  try {
    const result = await vowifiStore.run(action)
    // VoWiFi 操作文案使用专用 vowifi 命名空间, 不复用 eSIM 的键。
    notifySuccess(t('vowifi.operationAccepted', { id: result.operation_id }))
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
    await networkStore.reboot()
    notifySuccess(t('network.rebootAccepted'))
    window.setTimeout(() => void device.refresh(), 8000)
    scheduleViewRefresh('network', 12000)
  } catch (error) {
    notifyError('view', errorText(error, 'network.unableReboot'))
  }
}

async function checkNetwork() {
  try {
    const result = await networkStore.check()
    const message = result.detail ? `${result.summary}: ${result.detail}` : result.summary
    if (result.ok) notifySuccess(message)
    else notifyError('view', message)
  } catch (error) {
    notifyError('view', errorText(error, 'network.unableCheck'))
  }
}

async function setNetworkMode() {
  try {
    const result = await networkStore.setMode()
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
  startupBusy,
  startupSettings,
  firmware,
  firmwareOperation,
  firmwareOperationLogs,
  firmwareOperationModalOpen,
  refreshFirmware,
  runFirmwareAction,
  updateFirmwareUSBID,
  backupFirmware,
  selectFirmwareBackupDirectory,
  selectFirmwareEDLDirectory,
  selectFirmwareADBFile,
  saveFirmwareADBCommand,
  toggleStartup,
  openNotificationSettings,
  requestNotificationPermission,
  saveNotificationPreferences,
})

onMounted(async () => {
  syncDocumentLang(locale.value)
  document.addEventListener('visibilitychange', handleVisibilityChange)
  await device.refresh()
  device.connect()
  syncActiveRefreshers()
  // 首屏：主动加载当前激活视图的数据。active 的 watch 未设 immediate，
  // 且 device.status.changed 仅调度 overview/firmware, 若不在其中且后端
  // 未推送对应领域事件, 视图会永久停留在加载态。
  void loadView(active.value)
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
