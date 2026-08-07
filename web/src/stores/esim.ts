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
} from '../types'
import type { ProfileNote } from '../views/context'

export type EsimWorkspace = 'profiles' | 'notifications'
export type NotificationMode = 'pending' | 'history'
export type ProfileStateFilter = 'all' | 'enabled' | 'disabled'

export interface NotificationActionState {
  action: 'process' | 'remove'
  busy: boolean
  error: string
}

export type EsimDownloadPhase = 'input' | 'progress' | 'terminal'

// eSIM 域状态同时持有服务快照与工作台组织状态。筛选只作用于当前快照，
// 不会把用户输入转换成设备请求。
export const useEsimStore = defineStore('esim', () => {
  const device = useDeviceStore()
  const overview = ref<EsimOverview | null>(null)
  const overviewError = ref('')
  const notesError = ref('')
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
  const notes = ref<Record<string, { label: string; phone: string; tags: string }>>({})
  const health = ref<EsimHealth | null>(null)
  const noteICCID = ref('')
  const noteLabel = ref('')
  const notePhone = ref('')
  const noteTags = ref('')
  const notifications = ref<EsimNotification[]>([])
  const notificationHistory = ref<EsimNotificationHistory[]>([])
  const notificationActionState = ref<Record<number, NotificationActionState>>({})
  const confirmationOpen = ref(false)
  const confirmationOperationID = ref('')
  const confirmationInput = ref('')
  const confirmationBusy = ref(false)

  const operation = computed<OperationStatus | undefined>(() => {
    if (!operationID.value) return undefined
    return device.operations[operationID.value] || {
      operation_id: operationID.value,
      type: operationType.value || 'esim.operation',
      state: 'pending',
      progress: 0,
      message: '',
    }
  })
  const operationActive = computed(() =>
    !!operation.value && ['pending', 'running'].includes(operation.value.state),
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
      const note = profile.iccid ? notes.value[profile.iccid] : undefined
      return [
        profile.label,
        profile.service_provider_name,
        profile.iccid,
        note?.label,
        note?.phone,
        note?.tags,
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
    ) return false
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

  function localProfileNote(iccid?: string): ProfileNote | undefined {
    return iccid ? notes.value[iccid] : undefined
  }

  function noteSummary(note?: ProfileNote) {
    return note ? [note.label, note.phone, note.tags || note.profile_class].filter(Boolean).join(' · ') : ''
  }

  function syncSelectedNote() {
    const note = localProfileNote(noteICCID.value)
    noteLabel.value = note?.label || ''
    notePhone.value = note?.phone || ''
    noteTags.value = note?.tags || ''
  }

  async function load(): Promise<void> {
    overviewError.value = ''
    const result = await api.esim()
    overview.value = { ...result, profiles: Array.isArray(result.profiles) ? result.profiles : [] }
    if (!noteICCID.value) noteICCID.value = overview.value.profiles[0]?.iccid || ''
    if (!focusedICCID.value) focusedICCID.value = overview.value.profiles[0]?.iccid || ''
    syncSelectedNote()
    for (const profile of overview.value.profiles) {
      if (profile.iccid && labels.value[profile.iccid] === undefined) labels.value[profile.iccid] = profile.label || ''
    }

    notesError.value = ''
    void api.esimNotes()
      .then((result) => {
        notes.value = result.notes || {}
        syncSelectedNote()
      })
      .catch(() => {
        notesError.value = 'esim.unableNote'
      })
    healthError.value = ''
    void api.esimHealth()
      .then((result) => {
        health.value = result
      })
      .catch(() => {
        healthError.value = 'esim.healthUnavailable'
        health.value = null
      })
  }

  async function refreshSnapshots(): Promise<void> {
    await Promise.allSettled([load(), loadNotifications()])
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
    noteICCID.value = iccid
    focusedICCID.value = iccid
    syncSelectedNote()
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

  async function loadNotifications(): Promise<void> {
    notificationsLoading.value = true
    notificationsError.value = ''
    try {
      const [pendingResult, historyResult] = await Promise.allSettled([
        api.esimNotifications(),
        api.esimNotificationHistory(),
      ])
      const errors: string[] = []
      if (pendingResult.status === 'fulfilled') {
        notifications.value = Array.isArray(pendingResult.value.notifications) ? pendingResult.value.notifications : []
      } else {
        errors.push('pending')
      }
      if (historyResult.status === 'fulfilled') {
        notificationHistory.value = Array.isArray(historyResult.value.history) ? historyResult.value.history : []
      } else {
        errors.push('history')
      }
      if (errors.length) notificationsError.value = 'esim.notificationsPartialError'
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

  async function saveNote(): Promise<void> {
    if (!noteICCID.value) return
    const profile = overview.value?.profiles.find((item) => item.iccid === noteICCID.value)
    const label = (labels.value[noteICCID.value] || '').trim()
    if (label && label !== profile?.label) await api.esimRename(noteICCID.value, label)
    await api.saveEsimNote(noteICCID.value, {
      label: noteLabel.value.trim(),
      phone: notePhone.value.trim(),
      tags: noteTags.value.trim(),
    })
    notes.value[noteICCID.value] = {
      label: noteLabel.value.trim(),
      phone: notePhone.value.trim(),
      tags: noteTags.value.trim(),
    }
  }

  async function refreshAfterOperation(): Promise<void> {
    await refreshSnapshots()
  }

  watch(operation, (next) => {
    if (!next || !['succeeded', 'failed', 'cancelled'].includes(next.state)) return
    downloadPhase.value = next.type === 'esim.download' ? 'terminal' : downloadPhase.value
  })

  return {
    overview, overviewError, notesError, healthError, notificationsError, notificationsLoading,
    activeWorkspace, notificationMode, profileQuery, profileStateFilter, notificationQuery,
    notificationEventFilter, notificationProfileFilter, notificationStateFilter, focusedICCID,
    downloadOpen, downloadPhase, settingsOpen, settingsICCID, activationCode, confirmationCode,
    matchingID, labels, operationID, operationType, reloadedOperationID, notes, health, noteICCID, noteLabel,
    notePhone, noteTags, notifications, notificationHistory, notificationActionState,
    confirmationOpen, confirmationOperationID, confirmationInput, confirmationBusy, operation,
    operationActive, notificationBusy, filteredProfiles, filteredNotifications,
    filteredNotificationHistory, notificationEvents, profileNotificationFilter, localProfileNote,
    noteSummary, load, refreshSnapshots, refreshAfterOperation, openDownload, closeDownload,
    resetDownloadForRetry, openSettings, closeSettings, showWorkspace, showProfileNotifications,
    showNotificationProfile, clearNotificationProfileFilter, enable, disable, remove, download,
    saveNote, loadNotifications, processNotification, removeNotification, submitConfirmationCode,
    declineConfirmationCode,
  }
})

export type EsimStore = ReturnType<typeof useEsimStore>
