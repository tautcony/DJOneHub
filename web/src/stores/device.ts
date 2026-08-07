import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { APIError, api } from '../services/api'
import { i18n } from '../i18n'
import type { DeviceStatus, Envelope, OperationLog, OperationStatus, Snapshot } from '../types'

// terminalOperationTTL 是终态 operation 在客户端保留的时间: 终态后延迟清除,
// 使长会话中的 operations map 与 operationLogs 保持有界。
const terminalOperationTTL = 5 * 60_000
const RECONNECT_BASE_MS = 1000
const RECONNECT_MAX_MS = 30_000

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
  let reconnectAttempt = 0
  const terminalCleanupTimers = new Map<string, number>()

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
    // 非 ready 状态(初始化/降级)下仍保留已检测到的设备身份,避免界面显示
    // "未检测到兼容的模块";能力集合仅在 ready 时生效,此处照常清空。
    status.value = {
      snapshot: { ...next.snapshot, capabilities: {} },
      identity: next.identity ?? {},
      radio: next.radio ?? { registered: false },
      sim: next.sim ?? { inserted: false },
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
      if (operation?.operation_id) {
        operations.value[operation.operation_id] = operation
        scheduleTerminalCleanup(operation)
      }
      return
    }
    const handler = domainHandlers.get(envelope.type)
    if (handler) {
      handler(envelope.data)
    }
  }

  // scheduleTerminalCleanup 在 operation 到达终态后延迟清除其条目,
  // 使客户端 operations map / operationLogs 在长会话中保持有界。
  function scheduleTerminalCleanup(operation: OperationStatus) {
    if (!['succeeded', 'failed', 'cancelled'].includes(operation.state)) return
    const id = operation.operation_id
    const existing = terminalCleanupTimers.get(id)
    if (existing !== undefined) window.clearTimeout(existing)
    const timer = window.setTimeout(() => {
      terminalCleanupTimers.delete(id)
      delete operations.value[id]
      delete operationLogs.value[id]
    }, terminalOperationTTL)
    terminalCleanupTimers.set(id, timer)
  }

  // reconnectDelay 计算指数退避 + 抖动: 延迟随连续失败增长, 上限
  // RECONNECT_MAX_MS; 连接成功时计数器归零。
  function reconnectDelay() {
    const base = Math.min(RECONNECT_BASE_MS * 2 ** reconnectAttempt, RECONNECT_MAX_MS)
    reconnectAttempt++
    return Math.round(base * (0.5 + Math.random() * 0.5))
  }

  function connect() {
    socket?.close()
    const protocol = location.protocol === 'https:' ? 'wss' : 'ws'
    socket = new WebSocket(`${protocol}://${location.host}${basePath()}/events/ws`)
    socket.onopen = () => {
      connected.value = true
      reconnectAttempt = 0
    }
    socket.onclose = () => {
      connected.value = false
      window.setTimeout(connect, reconnectDelay())
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
  // 领域事件订阅：其它 store（如 eSIM 确认码请求）经 applyEnvelope 分发。
  // 事件到达时以最新注册为准，注册后自动获得后续事件。
  const domainHandlers = new Map<string, (data: unknown) => void>()

  function registerDomainHandler(type: string, handler: (data: unknown) => void) {
    domainHandlers.set(type, handler)
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
    registerDomainHandler,
  }
})
