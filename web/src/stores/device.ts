import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { APIError, api } from '../services/api'
import { i18n } from '../i18n'
import type { DeviceStatus, Envelope, OperationLog, OperationStatus, Snapshot } from '../types'

export const useDeviceStore = defineStore('device', () => {
  const status = ref<DeviceStatus | null>(null)
  const error = ref('')
  const connected = ref(false)
  const operations = ref<Record<string, OperationStatus>>({})
  const operationLogs = ref<Record<string, string[]>>({})
  const eventRevision = ref(0)
  const lastEventType = ref('')
  const lastEventData = ref<unknown>(undefined)
  let lastEventID = 0
  let socket: WebSocket | undefined

  const snapshot = computed<Snapshot | null>(() => status.value?.snapshot || null)
  const capabilities = computed(() => snapshot.value?.capabilities || {})
  const has = (name: string) =>
    !error.value &&
    snapshot.value?.state === 'ready' &&
    Object.prototype.hasOwnProperty.call(capabilities.value, name)

  function applyStatus(next: DeviceStatus) {
    if (next.snapshot.state === 'ready') {
      status.value = next
      return
    }
    status.value = {
      snapshot: { ...next.snapshot, identity: { stable_id: '' }, capabilities: {} },
      identity: {},
      radio: { registered: false },
      sim: { inserted: false },
    }
  }

  async function refresh() {
    try {
      applyStatus(await api.status())
      error.value = ''
    } catch (cause) {
      if (cause instanceof APIError) {
        const key = `errors.${cause.code}`
        error.value = i18n.global.te(key)
          ? String(i18n.global.t(key))
          : String(i18n.global.t('errors.generic'))
      } else {
        error.value = cause instanceof Error ? cause.message : String(i18n.global.t('errors.apiUnavailable'))
      }
      if (status.value) {
        applyStatus({
          ...status.value,
          snapshot: { ...status.value.snapshot, state: 'disconnected' },
        })
      }
    }
  }

  function applyEnvelope(envelope: Envelope) {
    if (envelope.type === 'snapshot') {
      const next = envelope.data as DeviceStatus
      if ((envelope.data as { snapshot?: Snapshot }).snapshot) applyStatus(next)
      lastEventID = envelope.id
      lastEventType.value = envelope.type
      lastEventData.value = envelope.data
      eventRevision.value++
      return
    }
    if (lastEventID > 0 && envelope.id > lastEventID + 1) {
      lastEventID = envelope.id
      lastEventType.value = 'resync.required'
      lastEventData.value = undefined
      eventRevision.value++
      void refresh()
      return
    }
    if (envelope.id <= lastEventID) return
    lastEventID = envelope.id
    lastEventType.value = envelope.type
    lastEventData.value = envelope.data
    eventRevision.value++
    if (envelope.type === 'device.status.changed') {
      const snapshotData = envelope.data as Snapshot
      if (status.value) applyStatus({ ...status.value, snapshot: snapshotData })
      return
    }
    if (envelope.type === 'operation.log') {
      const log = envelope.data as OperationLog
      if (log?.operation_id && log.message) {
        const current = operationLogs.value[log.operation_id] || []
        operationLogs.value[log.operation_id] = [...current, log.message].slice(-10_000)
      }
      return
    }
    if (
      envelope.type === 'operation.progress' ||
      envelope.type === 'operation.completed' ||
      envelope.type === 'operation.changed'
    ) {
      const operation = envelope.data as OperationStatus
      if (operation?.operation_id) operations.value[operation.operation_id] = operation
    }
  }

  function connect() {
    socket?.close()
    const protocol = location.protocol === 'https:' ? 'wss' : 'ws'
    socket = new WebSocket(`${protocol}://${location.host}${basePath()}/events/ws`)
    socket.onopen = () => {
      connected.value = true
    }
    socket.onclose = () => {
      connected.value = false
      window.setTimeout(connect, 2500)
    }
    socket.onerror = () => {
      connected.value = false
    }
    socket.onmessage = (event) => {
      try {
        applyEnvelope(JSON.parse(event.data) as Envelope)
      } catch {
        /* ignore malformed event */
      }
    }
  }

  function basePath() {
    return '/api/v1'
  }
  return {
    status,
    snapshot,
    capabilities,
    error,
    connected,
    operations,
    operationLogs,
    eventRevision,
    lastEventType,
    lastEventData,
    has,
    refresh,
    connect,
  }
})
