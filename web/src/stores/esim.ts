import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { api } from '../services/api'
import { useDeviceStore } from './device'
import type {
  EsimHealth,
  EsimNotification,
  EsimNotificationHistory,
  EsimOverview,
  OperationStatus,
  SimProfile,
} from '../types'
import { useSimProfilesStore } from './simprofiles'

export type EsimWorkspace = 'profiles' | 'notifications'
export type NotificationMode = 'pending' | 'history'
export type ProfileStateFilter = 'all' | 'enabled' | 'disabled'

export interface NotificationActionState {
  action: 'process' | 'remove'
  busy: boolean
  error: string
}

export type EsimDownloadPhase = 'input' | 'progress' | 'terminal'

const snapshotCacheTTL = 60_000

// eSIM 域状态同时持有服务快照与工作台组织状态。筛选只作用于当前快照，
// 不会把用户输入转换成设备请求。
export const useEsimStore = defineStore('esim', () => {
  const device = useDeviceStore()
  const simProfiles = useSimProfilesStore()
  const overview = ref<EsimOverview | null>(null)
  const overviewError = ref('')
  const metadataError = ref('')
  const healthError = ref('')
  const notificationsError = ref('')
  const notificationsLoading = ref(false)
  const activeWorkspace = ref<EsimWorkspace>('profiles')
  const notificationMode = ref<NotificationMode>('pending')
  const profileQuery = ref('')
  const profileStateFilter = ref<ProfileStateFilter>('all')
  const notificationQuery = ref('')
  const notificationEventFilter = ref('')
  const notificationProfileFilter = ref('')
  const notificationStateFilter = ref('')
  const focusedICCID = ref('')

  const downloadOpen = ref(false)
  const downloadPhase = ref<EsimDownloadPhase>('input')
  const settingsOpen = ref(false)
  const settingsICCID = ref('')
  const activationCode = ref('')
  const confirmationCode = ref('')
  const matchingID = ref('')
  const labels = ref<Record<string, string>>({})
  const operationID = ref('')
  const operationType = ref('')
  const reloadedOperationID = ref('')
  const health = ref<EsimHealth | null>(null)
  const notifications = ref<EsimNotification[]>([])
  const notificationHistory = ref<EsimNotificationHistory[]>([])
  const notificationActionState = ref<Record<number, NotificationActionState>>({})
  const confirmationOpen = ref(false)
  const confirmationOperationID = ref('')
  const confirmationInput = ref('')
  const confirmationBusy = ref(false)
  let overviewLoadedAt = 0
  let notificationsLoadedAt = 0

  const operation = computed<OperationStatus | undefined>(() => {
    if (!operationID.value) return undefined
    return (
      device.operations[operationID.value] || {
        operation_id: operationID.value,
        type: operationType.value || 'esim.operation',
        state: 'pending',
        progress: 0,
        message: '',
      }
    )
  })
  const operationActive = computed(
    () => !!operation.value && ['pending', 'running'].includes(operation.value.state),
  )
  const notificationBusy = computed(() =>
    Object.values(notificationActionState.value).some((state) => state.busy),
  )

  const filteredProfiles = computed(() => {
    const query = profileQuery.value.trim().toLocaleLowerCase()
    return (overview.value?.profiles || []).filter((profile) => {
      const state = profile.state === 'enabled' || profile.state === 'disabled' ? profile.state : 'unknown'
      if (profileStateFilter.value !== 'all' && state !== profileStateFilter.value) return false
      if (!query) return true
      const localProfile = simProfiles.find(profile.iccid)
      return [
        profile.label,
        profile.service_provider_name,
        profile.iccid,
        localProfile?.name,
        localProfile?.local_phone,
        localProfile?.tags,
        localProfile?.notes,
      ]
        .filter(Boolean)
        .some((value) => String(value).toLocaleLowerCase().includes(query))
    })
  })

  const notificationEvents = computed(() => {
    const values = new Set<string>()
    for (const item of [...notifications.value, ...notificationHistory.value]) {
      if (item.event) values.add(item.event)
    }
    return [...values].sort()
  })

  const profileNotificationFilter = computed(() => notificationProfileFilter.value)

  function matchesNotification(item: EsimNotification | EsimNotificationHistory, history: boolean) {
    const query = notificationQuery.value.trim().toLocaleLowerCase()
    if (notificationEventFilter.value && item.event !== notificationEventFilter.value) return false
    if (notificationProfileFilter.value && item.iccid !== notificationProfileFilter.value) return false
    if (
      history &&
      notificationStateFilter.value &&
      (item as EsimNotificationHistory).state !== notificationStateFilter.value
    )
      return false
    if (!query) return true
    const historyFields = history
      ? [(item as EsimNotificationHistory).aid, (item as EsimNotificationHistory).state]
      : []
    return [item.event, item.iccid, item.address, ...historyFields]
      .filter(Boolean)
      .some((value) => String(value).toLocaleLowerCase().includes(query))
  }

  const filteredNotifications = computed(() =>
    notifications.value.filter((item) => matchesNotification(item, false)),
  )
  const filteredNotificationHistory = computed(() =>
    notificationHistory.value.filter((item) => matchesNotification(item, true)),
  )

  function localSimProfile(iccid?: string): SimProfile | undefined {
    return simProfiles.find(iccid)
  }

  function simProfileSummary(profile?: SimProfile) {
    return profile ? [profile.name, profile.local_phone, profile.tags].filter(Boolean).join(' · ') : ''
  }

  async function load(force = false): Promise<void> {
    if (!force && overview.value && Date.now() - overviewLoadedAt < snapshotCacheTTL) return
    overviewError.value = ''
    const result = await api.esim()
    overview.value = { ...result, profiles: Array.isArray(result.profiles) ? result.profiles : [] }
    overviewLoadedAt = Date.now()
    if (!focusedICCID.value) focusedICCID.value = overview.value.profiles[0]?.iccid || ''
    for (const profile of overview.value.profiles) {
      if (profile.iccid && labels.value[profile.iccid] === undefined)
        labels.value[profile.iccid] = profile.label || ''
    }

    metadataError.value = ''
    void simProfiles.load().catch(() => {
      metadataError.value = 'esim.unableMetadata'
    })
    healthError.value = ''
    void api
      .esimHealth()
      .then((result) => {
        health.value = result
      })
      .catch(() => {
        healthError.value = 'esim.healthUnavailable'
        health.value = null
      })
  }

  async function refreshSnapshots(): Promise<void> {
    await Promise.allSettled([load(true), loadNotifications(true)])
  }

  function openDownload() {
    if (!operationActive.value || operation.value?.type !== 'esim.download') downloadPhase.value = 'input'
    downloadOpen.value = true
  }

  function closeDownload() {
    downloadOpen.value = false
  }

  function resetDownloadForRetry() {
    if (operationActive.value) return
    operationID.value = ''
    operationType.value = ''
    downloadPhase.value = 'input'
    downloadOpen.value = true
  }

  function openSettings(iccid?: string) {
    if (!iccid) return
    settingsICCID.value = iccid
    focusedICCID.value = iccid
    settingsOpen.value = true
  }

  function closeSettings() {
    settingsOpen.value = false
    settingsICCID.value = ''
  }

  function showWorkspace(workspace: EsimWorkspace) {
    activeWorkspace.value = workspace
  }

  function showProfileNotifications(iccid?: string) {
    if (!iccid) return
    notificationProfileFilter.value = iccid
    activeWorkspace.value = 'notifications'
  }

  function showNotificationProfile(iccid?: string) {
    if (!iccid) return
    focusedICCID.value = iccid
    profileQuery.value = ''
    activeWorkspace.value = 'profiles'
  }

  function clearNotificationProfileFilter() {
    notificationProfileFilter.value = ''
  }

  async function enable(iccid?: string): Promise<{ operation_id: string }> {
    if (!iccid) throw new Error('esim.missingICCID')
    const result = await api.esimEnable(iccid)
    operationID.value = result.operation_id
    operationType.value = 'esim.enable'
    return result
  }

  async function disable(iccid?: string): Promise<{ operation_id: string }> {
    if (!iccid) throw new Error('esim.missingICCID')
    const result = await api.esimDisable(iccid)
    operationID.value = result.operation_id
    operationType.value = 'esim.disable'
    return result
  }

  async function loadNotifications(force = false): Promise<void> {
    if (!force && notificationsLoadedAt > 0 && Date.now() - notificationsLoadedAt < snapshotCacheTTL) return
    notificationsLoading.value = true
    notificationsError.value = ''
    try {
      const [pendingResult, historyResult] = await Promise.allSettled([
        api.esimNotifications(),
        api.esimNotificationHistory(),
      ])
      const errors: string[] = []
      if (pendingResult.status === 'fulfilled') {
        notifications.value = Array.isArray(pendingResult.value.notifications)
          ? pendingResult.value.notifications
          : []
      } else {
        errors.push('pending')
      }
      if (historyResult.status === 'fulfilled') {
        notificationHistory.value = Array.isArray(historyResult.value.history)
          ? historyResult.value.history
          : []
      } else {
        errors.push('history')
      }
      if (errors.length) notificationsError.value = 'esim.notificationsPartialError'
      notificationsLoadedAt = Date.now()
    } catch {
      notificationsError.value = 'esim.unableNotifications'
    } finally {
      notificationsLoading.value = false
    }
  }

  function setNotificationAction(sequence: number, state: NotificationActionState) {
    notificationActionState.value = { ...notificationActionState.value, [sequence]: state }
  }

  async function processNotification(sequence: number): Promise<void> {
    setNotificationAction(sequence, { action: 'process', busy: true, error: '' })
    try {
      await api.esimProcessNotification(sequence)
      await loadNotifications()
      setNotificationAction(sequence, { action: 'process', busy: false, error: '' })
    } catch (error) {
      setNotificationAction(sequence, {
        action: 'process',
        busy: false,
        error: error instanceof Error ? error.message : 'esim.unableProcessNotification',
      })
      throw error
    }
  }

  async function removeNotification(sequence: number): Promise<void> {
    setNotificationAction(sequence, { action: 'remove', busy: true, error: '' })
    try {
      await api.esimRemoveNotification(sequence)
      await loadNotifications()
      setNotificationAction(sequence, { action: 'remove', busy: false, error: '' })
    } catch (error) {
      setNotificationAction(sequence, {
        action: 'remove',
        busy: false,
        error: error instanceof Error ? error.message : 'esim.unableRemoveNotification',
      })
      throw error
    }
  }

  function handleConfirmationRequest(data: unknown) {
    const payload = data as { operation_id?: string }
    if (!payload?.operation_id) return
    confirmationOperationID.value = payload.operation_id
    confirmationInput.value = ''
    confirmationOpen.value = true
  }

  async function submitConfirmationCode(): Promise<void> {
    if (!confirmationOperationID.value || confirmationBusy.value) return
    confirmationBusy.value = true
    try {
      await api.esimConfirmationCode(confirmationOperationID.value, confirmationInput.value, false)
      confirmationOpen.value = false
    } finally {
      confirmationBusy.value = false
    }
  }

  async function declineConfirmationCode(): Promise<void> {
    if (!confirmationOperationID.value || confirmationBusy.value) return
    confirmationBusy.value = true
    try {
      await api.esimConfirmationCode(confirmationOperationID.value, '', true)
      confirmationOpen.value = false
    } finally {
      confirmationBusy.value = false
    }
  }

  device.registerDomainHandler('esim.confirmation_code_request', handleConfirmationRequest)

  async function remove(iccid?: string): Promise<{ operation_id: string }> {
    if (!iccid) throw new Error('esim.missingICCID')
    const result = await api.esimDelete(iccid)
    operationID.value = result.operation_id
    operationType.value = 'esim.delete'
    return result
  }

  async function download(): Promise<{ operation_id: string }> {
    const result = await api.esimDownload(activationCode.value, confirmationCode.value, matchingID.value)
    operationID.value = result.operation_id
    operationType.value = 'esim.download'
    downloadPhase.value = 'progress'
    return result
  }

  async function saveProfileName(): Promise<void> {
    if (!settingsICCID.value) return
    const profile = overview.value?.profiles.find((item) => item.iccid === settingsICCID.value)
    const label = (labels.value[settingsICCID.value] || '').trim()
    if (!label || label === profile?.label) return
    await api.esimRename(settingsICCID.value, label)
    if (profile) profile.label = label
  }

  async function refreshAfterOperation(): Promise<void> {
    await refreshSnapshots()
  }

  watch(operation, (next) => {
    if (!next || !['succeeded', 'failed', 'cancelled'].includes(next.state)) return
    downloadPhase.value = next.type === 'esim.download' ? 'terminal' : downloadPhase.value
  })

  return {
    overview,
    overviewError,
    metadataError,
    healthError,
    notificationsError,
    notificationsLoading,
    activeWorkspace,
    notificationMode,
    profileQuery,
    profileStateFilter,
    notificationQuery,
    notificationEventFilter,
    notificationProfileFilter,
    notificationStateFilter,
    focusedICCID,
    downloadOpen,
    downloadPhase,
    settingsOpen,
    settingsICCID,
    activationCode,
    confirmationCode,
    matchingID,
    labels,
    operationID,
    operationType,
    reloadedOperationID,
    health,
    notifications,
    notificationHistory,
    notificationActionState,
    confirmationOpen,
    confirmationOperationID,
    confirmationInput,
    confirmationBusy,
    operation,
    operationActive,
    notificationBusy,
    filteredProfiles,
    filteredNotifications,
    filteredNotificationHistory,
    notificationEvents,
    profileNotificationFilter,
    localSimProfile,
    simProfileSummary,
    load,
    refreshSnapshots,
    refreshAfterOperation,
    openDownload,
    closeDownload,
    resetDownloadForRetry,
    openSettings,
    closeSettings,
    showWorkspace,
    showProfileNotifications,
    showNotificationProfile,
    clearNotificationProfileFilter,
    enable,
    disable,
    remove,
    download,
    saveProfileName,
    loadNotifications,
    processNotification,
    removeNotification,
    submitConfirmationCode,
    declineConfirmationCode,
  }
})

export type EsimStore = ReturnType<typeof useEsimStore>
