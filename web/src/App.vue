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
import { useSimProfilesStore } from './stores/simprofiles'
import { useVowifiStore } from './stores/vowifi'
import type {
  CallStatus,
  FirmwareStatus,
  NotificationChannelsSettings,
  NotificationDebugEvent,
  NotificationDebugInfo,
  NotificationDebugRequest,
  NotificationPermissionStatus,
  NotificationPreferences,
  OperationStatus,
  StartupStatus,
  SimProfile,
} from './types'
import AppShell, { type ShellNavGroup } from './components/AppShell.vue'
import PageHeader from './components/PageHeader.vue'
import { viewContextKey } from './views/context'
import { viewFromRoute, viewPaths, type ViewID } from './router'

type NavGroupID = 'main' | 'voice' | 'tools'

const VIEW_REFRESH_MIN_INTERVAL_MS = 500
const OVERVIEW_REFRESH_MIN_INTERVAL_MS = 2000
const SETTINGS_REFRESH_MIN_INTERVAL_MS = 5000
// 拨号后等待轮询器确认通话建立的窗口: 覆盖一次前端 15s 视图轮询周期。
const DIAL_WAIT_TIMEOUT_MS = 20000
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
const callsDialOpen = ref(false)
const dialNumber = ref('')
const dialCallBusy = ref(false)
// dialWaiting 表示 ATD 已被模块接受但轮询器尚未确认通话建立; 确认后由
// watch(calls) 关闭弹窗, 超时则给出提示, 避免「点了拨号没反应」的观感。
const dialWaiting = ref(false)
let dialWaitTimer: number | undefined
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
  simFilter: smsSimFilter,
  selectedPeer: selectedSmsPeer,
  composeNew: smsComposeNew,
  to: smsTo,
  body: smsBody,
  operation: smsOperation,
  threads: smsThreads,
  filteredThreads: filteredSmsThreads,
  selectedThread: selectedSmsThread,
} = storeToRefs(sms)

const esimStore = useEsimStore()
const {
  overview: esim,
  overviewError: esimOverviewError,
  metadataError: esimMetadataError,
  healthError: esimHealthError,
  notificationsError: esimNotificationsError,
  notificationsLoading: esimNotificationsLoading,
  activeWorkspace: esimWorkspace,
  notificationMode: esimNotificationMode,
  profileQuery: esimProfileQuery,
  profileStateFilter: esimProfileStateFilter,
  notificationQuery: esimNotificationQuery,
  notificationEventFilter: esimNotificationEventFilter,
  notificationProfileFilter: esimNotificationProfileFilter,
  notificationStateFilter: esimNotificationStateFilter,
  focusedICCID: esimFocusedICCID,
  filteredProfiles: esimFilteredProfiles,
  filteredNotifications: esimFilteredNotifications,
  filteredNotificationHistory: esimFilteredNotificationHistory,
  notificationEvents: esimNotificationEvents,
  operationActive: esimOperationActive,
  downloadOpen: esimDownloadOpen,
  downloadPhase: esimDownloadPhase,
  settingsOpen: esimSettingsOpen,
  settingsICCID: esimSettingsICCID,
  activationCode: esimActivationCode,
  confirmationCode: esimConfirmationCode,
  matchingID: esimMatchingID,
  labels: esimLabels,
  operation: esimOperation,
  reloadedOperationID: esimReloadedOperationID,
  notifications: esimNotifications,
  notificationHistory: esimNotificationHistory,
  notificationBusy: esimNotificationBusy,
  notificationActionState: esimNotificationActionState,
  health: esimHealth,
  confirmationOpen: esimConfirmationOpen,
  confirmationOperationID: esimConfirmationOperationID,
  confirmationInput: esimConfirmationInput,
  confirmationBusy: esimConfirmationBusy,
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

const simProfilesStore = useSimProfilesStore()
const { profiles: simProfiles, busy: simProfilesBusy } = storeToRefs(simProfilesStore)

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
const notificationChannels = ref<NotificationChannelsSettings | null>(null)
const notificationChannelsBusy = ref(false)
const notificationChannelTesting = ref<string | null>(null)
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
      { id: 'sim-profiles', label: t('nav.simProfiles') },
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
    // 后端返回的具体错误信息（如渠道配置无效、投递失败原因）优先展示，
    // 仅当没有 message 才退回到通用文案或按错误码本地化。
    if (cause.message) return cause.message
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
    void esimStore.loadNotifications()
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'esim.unableLoad')
  } finally {
    markViewLoaded('esim')
  }
}

async function loadSimProfiles() {
  try {
    await simProfilesStore.load()
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'simProfiles.unableLoad')
  } finally {
    markViewLoaded('sim-profiles')
  }
}

async function createSimProfile(input: Parameters<typeof simProfilesStore.create>[0]) {
  try {
    await simProfilesStore.create(input)
    notifySuccess(t('simProfiles.saved'))
  } catch (error) {
    notifyError('view', errorText(error, 'simProfiles.unableSave'))
  }
}

async function updateSimProfile(iccid: string, input: Parameters<typeof simProfilesStore.update>[1]) {
  try {
    await simProfilesStore.update(iccid, input)
    notifySuccess(t('simProfiles.saved'))
  } catch (error) {
    notifyError('view', errorText(error, 'simProfiles.unableSave'))
  }
}

async function deleteSimProfile(iccid: string) {
  try {
    await simProfilesStore.remove(iccid)
    notifySuccess(t('simProfiles.deleted'))
  } catch (error) {
    notifyError('view', errorText(error, 'simProfiles.unableDelete'))
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
function openCallsDial() {
  dialNumber.value = ''
  callsDialOpen.value = true
}
function clearDialWait() {
  dialWaiting.value = false
  if (dialWaitTimer !== undefined) {
    window.clearTimeout(dialWaitTimer)
    dialWaitTimer = undefined
  }
}
function closeCallsDial() {
  clearDialWait()
  callsDialOpen.value = false
}
async function dialCall() {
  const number = dialNumber.value.trim()
  if (!number) {
    viewError.value = t('calls.dialEmpty')
    return
  }
  if (dialCallBusy.value || dialWaiting.value) return
  dialCallBusy.value = true
  try {
    await api.dialCall(number)
    // ATD 返回 OK 只代表指令被接受; 通话真正出现要等轮询器在 AT+CLCC
    // 里发现, 弹窗保持打开并在确认后由 watch(calls) 关闭。
    dialWaiting.value = true
    dialWaitTimer = window.setTimeout(() => {
      clearDialWait()
      closeCallsDial()
      viewError.value = t('calls.dialTimeout')
    }, DIAL_WAIT_TIMEOUT_MS)
  } catch (error) {
    viewError.value = errorText(error, 'calls.unableDial')
  } finally {
    dialCallBusy.value = false
  }
}
// 轮询器确认通话建立 (calls.active 出现) 后关闭拨号弹窗, 让位给活动通话面板。
watch(calls, (status) => {
  if (dialWaiting.value && status?.active) {
    clearDialWait()
    closeCallsDial()
  }
})
function localSimProfile(iccid?: string) {
  return simProfilesStore.find(iccid)
}
function simProfileSummary(profile?: SimProfile) {
  return profile ? [profile.name, profile.local_phone, profile.tags].filter(Boolean).join(' · ') : ''
}
function openEsimSettings(iccid?: string) {
  esimStore.openSettings(iccid)
}
function closeEsimSettings() {
  esimStore.closeSettings()
}
async function saveEsimProfileName() {
  try {
    await esimStore.saveProfileName()
    notifySuccess(t('esim.profileSaved'))
    closeEsimSettings()
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableRename'))
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
  window.setTimeout(() => void esimStore.refreshAfterOperation(), delay)
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

async function disableEsim(iccid?: string) {
  if (!iccid) return
  try {
    const result = await esimStore.disable(iccid)
    notifySuccess(t('esim.operationAccepted', { id: result.operation_id }))
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableDisable'))
  }
}

async function loadNotifications() {
  try {
    await esimStore.loadNotifications()
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableNotifications'))
  }
}

async function processNotification(sequence: number) {
  try {
    await esimStore.processNotification(sequence)
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableProcessNotification'))
  }
}

async function removeNotification(sequence: number) {
  try {
    await esimStore.removeNotification(sequence)
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableRemoveNotification'))
  }
}

async function submitConfirmationCode() {
  try {
    await esimStore.submitConfirmationCode()
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableConfirmationCode'))
  }
}

async function declineConfirmationCode() {
  try {
    await esimStore.declineConfirmationCode()
  } catch (error) {
    notifyError('view', errorText(error, 'esim.unableConfirmationCode'))
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

async function backupFirmware(
  outputPath: string,
  loaderPath: string,
  edlPath: string,
  edlRunner: 'python' | 'uv',
) {
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
    const [permissions, preferences, startup, channels] = await Promise.all([
      api.notificationPermissions(),
      api.notificationPreferences(),
      api.startupSettings(),
      api.notificationChannels(),
    ])
    notificationPermissions.value = permissions
    notificationPreferences.value = preferences.preferences
    startupSettings.value = startup
    notificationChannels.value = channels.channels
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

async function loadNotificationChannels() {
  try {
    const result = await api.notificationChannels()
    notificationChannels.value = result.channels
    viewError.value = ''
  } catch (error) {
    viewError.value = errorText(error, 'settings.channels.loadFailed')
  } finally {
    markViewLoaded('settings')
  }
}

async function saveNotificationChannels() {
  if (!notificationChannels.value) return
  notificationChannelsBusy.value = true
  try {
    const result = await api.updateNotificationChannels({ ...notificationChannels.value })
    notificationChannels.value = result.channels
    notifySuccess(t('settings.channels.saved'))
  } catch (error) {
    notifyError('view', errorText(error, 'settings.channels.saveFailed'))
    if (isActiveView('settings')) await loadNotificationChannels()
  } finally {
    notificationChannelsBusy.value = false
  }
}

async function testNotificationChannel(channel: string, probe: NotificationChannelsSettings) {
  if (!channel) return
  notificationChannelTesting.value = channel
  try {
    await api.testNotificationChannel(channel, probe)
    notifySuccess(t('settings.channels.testSent', { channel }))
  } catch (error) {
    notifyError('view', errorText(error, 'settings.channels.testFailed'))
  } finally {
    notificationChannelTesting.value = null
  }
}

async function discoverTelegramChatIDs() {
  if (!notificationChannels.value) return
  notificationChannelTesting.value = 'telegram-chat-id'
  try {
    const result = await api.discoverTelegramChatIDs(notificationChannels.value.telegram)
    if (result.chat_ids.length === 0) {
      notifyError('view', t('settings.channels.telegram.noChatID'))
    } else {
      notificationChannels.value.telegram.chat_id = result.chat_ids[0]
      notifySuccess(t('settings.channels.telegram.chatIDFound', { id: result.chat_ids[0] }))
    }
  } catch (error) {
    notifyError('view', errorText(error, 'settings.channels.telegram.chatIDFailed'))
  } finally {
    notificationChannelTesting.value = null
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
  calls: async () => Promise.all([loadCalls(), simProfilesStore.load()]).then(() => undefined),
  sms: async () => Promise.all([loadSMS(), simProfilesStore.load()]).then(() => undefined),
  esim: loadEsim,
  'sim-profiles': loadSimProfiles,
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
  const minInterval =
    view === 'overview'
      ? OVERVIEW_REFRESH_MIN_INTERVAL_MS
      : view === 'settings'
        ? SETTINGS_REFRESH_MIN_INTERVAL_MS
        : VIEW_REFRESH_MIN_INTERVAL_MS
  if (Date.now() - (lastViewRefreshAt.get(view) || 0) < minInterval) return
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
  callsDialOpen,
  dialNumber,
  dialCall,
  dialCallBusy,
  dialWaiting,
  openCallsDial,
  closeCallsDial,
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
  smsSimFilter,
  smsThreads,
  smsTo,
  startNewSMS,
  closeEsimDownload,
  closeEsimSettings,
  deleteEsim,
  disableEsim,
  downloadEsim,
  enableEsim,
  esim,
  esimOverviewError,
  esimMetadataError,
  esimHealthError,
  esimNotificationsError,
  esimNotificationsLoading,
  esimWorkspace,
  esimNotificationMode,
  esimProfileQuery,
  esimProfileStateFilter,
  esimNotificationQuery,
  esimNotificationEventFilter,
  esimNotificationProfileFilter,
  esimNotificationStateFilter,
  esimFocusedICCID,
  esimFilteredProfiles,
  esimFilteredNotifications,
  esimFilteredNotificationHistory,
  esimNotificationEvents,
  esimOperationActive,
  esimActivationCode,
  esimConfirmationCode,
  esimDownloadOpen,
  esimDownloadPhase,
  esimLabels,
  esimMatchingID,
  esimOperation,
  esimSettingsOpen,
  esimSettingsICCID,
  loadNotifications,
  processNotification,
  removeNotification,
  submitConfirmationCode,
  declineConfirmationCode,
  esimNotifications,
  esimNotificationHistory,
  esimNotificationBusy,
  esimNotificationActionState,
  esimHealth,
  simProfiles,
  simProfilesBusy,
  createSimProfile,
  updateSimProfile,
  deleteSimProfile,
  esimConfirmationOpen,
  esimConfirmationOperationID,
  esimConfirmationInput,
  esimConfirmationBusy,
  localSimProfile,
  simProfileSummary,
  openEsimDownload,
  resetEsimDownloadForRetry: esimStore.resetDownloadForRetry,
  openEsimSettings,
  showEsimWorkspace: esimStore.showWorkspace,
  showEsimProfileNotifications: esimStore.showProfileNotifications,
  showEsimNotificationProfile: esimStore.showNotificationProfile,
  clearEsimNotificationProfileFilter: esimStore.clearNotificationProfileFilter,
  refreshEsimSnapshots: esimStore.refreshSnapshots,
  refreshEsimAfterOperation: esimStore.refreshAfterOperation,
  saveEsimProfileName,
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
  notificationChannels,
  notificationChannelsBusy,
  notificationChannelTesting,
  loadNotificationChannels,
  saveNotificationChannels,
  testNotificationChannel,
  discoverTelegramChatIDs,
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
