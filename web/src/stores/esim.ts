import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '../services/api'
import { useDeviceStore } from './device'
import type { EsimNotification, EsimNotificationHistory, EsimOverview } from '../types'
import type { ProfileNote } from '../views/context'

// eSIM 域状态: 总览、profile 标签/备注、下载与设置对话框、操作追踪、通知与确认码交互。
// 视图经 typed ViewContext 读取本 store 暴露的 refs/actions。
export const useEsimStore = defineStore('esim', () => {
  const device = useDeviceStore()
  const overview = ref<EsimOverview | null>(null)
  const downloadOpen = ref(false)
  const settingsOpen = ref(false)
  const settingsICCID = ref('')
  const activationCode = ref('')
  const confirmationCode = ref('')
  const matchingID = ref('')
  const labels = ref<Record<string, string>>({})
  const operationID = ref('')
  const reloadedOperationID = ref('')
  const notes = ref<Record<string, { label: string; phone: string; tags: string }>>({})
  const health = ref<Record<string, unknown> | null>(null)
  const noteICCID = ref('')
  const noteLabel = ref('')
  const notePhone = ref('')
  const noteTags = ref('')
  const notifications = ref<EsimNotification[]>([])
  const notificationHistory = ref<EsimNotificationHistory[]>([])
  const notificationBusy = ref(false)
  const confirmationOpen = ref(false)
  const confirmationOperationID = ref('')
  const confirmationInput = ref('')
  const confirmationBusy = ref(false)

  const operation = computed(() => (operationID.value ? device.operations[operationID.value] : undefined))

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
    const result = await api.esim()
    overview.value = { ...result, profiles: Array.isArray(result.profiles) ? result.profiles : [] }
    const [notesResult, healthResult] = await Promise.all([
      api.esimNotes().catch(() => ({ notes: {} })),
      api.esimHealth().catch(() => null),
    ])
    notes.value = notesResult.notes
    health.value = healthResult
    if (!noteICCID.value) noteICCID.value = overview.value.profiles[0]?.iccid || ''
    syncSelectedNote()
    for (const profile of overview.value.profiles) {
      if (profile.iccid && labels.value[profile.iccid] === undefined) {
        labels.value[profile.iccid] = profile.label || ''
      }
    }
  }

  function openDownload() {
    downloadOpen.value = true
  }

  function closeDownload() {
    downloadOpen.value = false
  }

  function openSettings(iccid?: string) {
    if (!iccid) return
    settingsICCID.value = iccid
    noteICCID.value = iccid
    syncSelectedNote()
    settingsOpen.value = true
  }

  function closeSettings() {
    settingsOpen.value = false
    settingsICCID.value = ''
  }

  async function enable(iccid?: string): Promise<{ operation_id: string }> {
    if (!iccid) throw new Error('esim.missingICCID')
    const result = await api.esimEnable(iccid)
    operationID.value = result.operation_id
    return result
  }

  async function disable(iccid?: string): Promise<{ operation_id: string }> {
    if (!iccid) throw new Error('esim.missingICCID')
    const result = await api.esimDisable(iccid)
    operationID.value = result.operation_id
    return result
  }

  async function loadNotifications(): Promise<void> {
    try {
      const [pendingResult, historyResult] = await Promise.all([
        api.esimNotifications(),
        api.esimNotificationHistory().catch(() => ({ history: [] })),
      ])
      notifications.value = Array.isArray(pendingResult.notifications) ? pendingResult.notifications : []
      notificationHistory.value = Array.isArray(historyResult.history) ? historyResult.history : []
    } catch {
      // 能力不足或读取失败时保持空列表，视图内联展示错误由调用方处理
      notifications.value = []
    }
  }

  async function processNotification(sequence: number): Promise<void> {
    notificationBusy.value = true
    try {
      await api.esimProcessNotification(sequence)
      await loadNotifications()
    } finally {
      notificationBusy.value = false
    }
  }

  async function removeNotification(sequence: number): Promise<void> {
    notificationBusy.value = true
    try {
      await api.esimRemoveNotification(sequence)
      await loadNotifications()
    } finally {
      notificationBusy.value = false
    }
  }

  // 确认码交互：后端下载过程中经 WS 事件请求输入确认码。
  function handleConfirmationRequest(data: unknown) {
    const payload = data as { operation_id?: string; message?: string }
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

  // 注册域事件订阅：确认码请求。设备 store 按事件类型分发到最新注册的处理器。
  device.registerDomainHandler('esim.confirmation_code_request', handleConfirmationRequest)

  async function remove(iccid?: string): Promise<{ operation_id: string }> {
    if (!iccid) throw new Error('esim.missingICCID')
    const result = await api.esimDelete(iccid)
    operationID.value = result.operation_id
    return result
  }

  async function download(): Promise<{ operation_id: string }> {
    const result = await api.esimDownload(activationCode.value, confirmationCode.value, matchingID.value)
    operationID.value = result.operation_id
    return result
  }

  async function saveNote(): Promise<void> {
    if (!noteICCID.value) return
    const profile = overview.value?.profiles.find((item) => item.iccid === noteICCID.value)
    const label = (labels.value[noteICCID.value] || '').trim()
    if (label && label !== profile?.label) await api.esimRename(noteICCID.value, label)
    await api.saveEsimNote(noteICCID.value, {
      label: noteLabel.value,
      phone: notePhone.value,
      tags: noteTags.value,
    })
    notes.value[noteICCID.value] = {
      label: noteLabel.value,
      phone: notePhone.value,
      tags: noteTags.value,
    }
  }

  return {
    overview,
    downloadOpen,
    settingsOpen,
    settingsICCID,
    activationCode,
    confirmationCode,
    matchingID,
    labels,
    operationID,
    reloadedOperationID,
    notes,
    health,
    noteICCID,
    noteLabel,
    notePhone,
    noteTags,
    notifications,
    notificationHistory,
    notificationBusy,
    confirmationOpen,
    confirmationOperationID,
    confirmationInput,
    confirmationBusy,
    operation,
    localProfileNote,
    noteSummary,
    load,
    openDownload,
    closeDownload,
    openSettings,
    closeSettings,
    enable,
    disable,
    remove,
    download,
    saveNote,
    loadNotifications,
    processNotification,
    removeNotification,
    submitConfirmationCode,
    declineConfirmationCode,
  }
})

export type EsimStore = ReturnType<typeof useEsimStore>
