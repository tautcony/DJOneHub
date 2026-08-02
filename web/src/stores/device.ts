import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { APIError, api } from '../services/api'
import { i18n } from '../i18n'
import type { DeviceStatus, Envelope, OperationStatus, Snapshot } from '../types'

export const useDeviceStore = defineStore('device', () => {
  const status = ref<DeviceStatus | null>(null)
  const error = ref('')
  const connected = ref(false)
  const operations = ref<Record<string, OperationStatus>>({})
  const eventRevision = ref(0)
  const lastEventType = ref('')
  const lastEventData = ref<unknown>(undefined)
  let lastEventID = 0
  let socket: WebSocket | undefined

  const snapshot = computed<Snapshot | null>(() => status.value?.snapshot || null)
  const capabilities = computed(() => snapshot.value?.capabilities || {})
  const has = (name: string) => Object.prototype.hasOwnProperty.call(capabilities.value, name)

  async function refresh() {
    try {
      status.value = await api.status()
      error.value = ''
    } catch (cause) {
      if (cause instanceof APIError) {
        const key = `errors.${cause.code}`
        error.value = i18n.global.te(key) ? String(i18n.global.t(key)) : String(i18n.global.t('errors.generic'))
      } else {
        error.value = cause instanceof Error ? cause.message : String(i18n.global.t('errors.apiUnavailable'))
      }
    }
  }

  function applyEnvelope(envelope: Envelope) {
    if (envelope.type === 'snapshot') {
      status.value = (envelope.data as { snapshot?: DeviceStatus }).snapshot ? (envelope.data as DeviceStatus) : status.value
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
    if (envelope.type === 'device.status.changed') { const snapshotData = envelope.data as Snapshot; if (status.value) status.value.snapshot = snapshotData; return }
    if (envelope.type === 'operation.progress' || envelope.type === 'operation.completed' || envelope.type === 'operation.changed') {
      const operation = envelope.data as OperationStatus; if (operation?.operation_id) operations.value[operation.operation_id] = operation
    }
  }

  function connect() {
    socket?.close()
    const protocol = location.protocol === 'https:' ? 'wss' : 'ws'
    socket = new WebSocket(`${protocol}://${location.host}${basePath()}/events/ws`)
    socket.onopen = () => { connected.value = true }
    socket.onclose = () => { connected.value = false; window.setTimeout(connect, 2500) }
    socket.onerror = () => { connected.value = false }
    socket.onmessage = (event) => { try { applyEnvelope(JSON.parse(event.data) as Envelope) } catch { /* ignore malformed event */ } }
  }

  function basePath() { return '/api/v1' }
  return { status, snapshot, capabilities, error, connected, operations, eventRevision, lastEventType, lastEventData, has, refresh, connect }
})
