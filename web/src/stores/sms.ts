import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { useI18n } from 'vue-i18n'
import { api } from '../services/api'
import { useDeviceStore } from './device'
import type { SMSMessage, SMSStorageUsage } from '../types'
import type { SmsThread } from '../views/context'

// SMS 域状态: 消息列表、会话线程、发送草稿与操作追踪。
// 视图经 typed ViewContext 读取本 store 暴露的 refs/actions。
export const useSmsStore = defineStore('sms', () => {
  const device = useDeviceStore()
  const items = ref<SMSMessage[]>([])
  const sentItems = ref<SMSMessage[]>([])
  const query = ref('')
  const simFilter = ref('')
  const selectedPeer = ref('')
  const composeNew = ref(false)
  const to = ref('')
  const body = ref('')
  const operationID = ref('')
  const storageUsage = ref<SMSStorageUsage[]>([])

  const operation = computed(() => (operationID.value ? device.operations[operationID.value] : undefined))

  const { t } = useI18n()

  function peer(item: SMSMessage) {
    return item.sender || item.recipient || t('sms.unknownSender')
  }

  function threadKey(item: SMSMessage) {
    return `${item.iccid || ''}\u0000${item.sender || item.recipient || 'unknown'}`
  }

  // orderingKey resolves the ordering key of a message. The backend sorts by
  // recorded_at (device-local insertion time, one clock for both directions);
  // received_at stays a display attribute because the SMSC clock is not synced
  // with the device clock.
  function orderingKey(item: SMSMessage): number {
    const value = item.recorded_at ?? item.received_at
    const parsed = value ? Date.parse(value) : NaN
    return Number.isFinite(parsed) ? parsed : 0
  }

  const threads = computed(() => {
    const groups = new Map<string, SmsThread>()
    for (const item of [...items.value, ...sentItems.value]) {
      const key = threadKey(item)
      const group = groups.get(key) || { key, peer: peer(item), iccid: item.iccid || '', items: [] }
      group.items.push(item)
      if (!group.latest || orderingKey(item) > orderingKey(group.latest)) group.latest = item
      groups.set(key, group)
    }
    const result = [...groups.values()].sort((a, b) => orderingKey(b.latest!) - orderingKey(a.latest!))
    for (const thread of result) thread.items.sort((a, b) => orderingKey(b) - orderingKey(a))
    return result
  })

  const filteredThreads = computed(() => {
    const term = query.value.trim().toLowerCase()
    return threads.value.filter(
      (thread) =>
        (!simFilter.value || thread.iccid === simFilter.value) &&
        (!term ||
          thread.peer.toLowerCase().includes(term) ||
          thread.items.some((item) => item.body.toLowerCase().includes(term))),
    )
  })

  const selectedThread = computed(() =>
    composeNew.value
      ? undefined
      : filteredThreads.value.find((thread) => thread.key === selectedPeer.value) || filteredThreads.value[0],
  )

  watch(selectedThread, (thread) => {
    if (composeNew.value) return
    selectedPeer.value = thread?.key || ''
    to.value = thread?.peer || ''
  })

  function reconcileSent(itemsNow: SMSMessage[]) {
    sentItems.value = sentItems.value.filter((local) => {
      const localTime = local.received_at ? Date.parse(local.received_at) : NaN
      return !itemsNow.some((remote) => {
        if (!remote.recipient || remote.recipient !== local.recipient || remote.body !== local.body) {
          return false
        }
        if (local.iccid && remote.iccid && local.iccid !== remote.iccid) return false
        const remoteTime = remote.received_at ? Date.parse(remote.received_at) : NaN
        return Number.isFinite(localTime) && Number.isFinite(remoteTime)
          ? Math.abs(remoteTime - localTime) < 60_000
          : true
      })
    })
  }

  function syncSelection() {
    if (composeNew.value) return
    const thread =
      filteredThreads.value.find((item) => item.key === selectedPeer.value) || filteredThreads.value[0]
    if (!thread) {
      selectedPeer.value = ''
      to.value = ''
      return
    }
    selectedPeer.value = thread.key
    to.value = thread.peer
  }

  async function refresh(): Promise<void> {
    const result = await api.smsRefresh()
    const next = Array.isArray(result.items) ? result.items : []
    storageUsage.value = Array.isArray(result.storage) ? result.storage : []
    items.value = next
    reconcileSent(next)
    syncSelection()
  }

  async function clear(): Promise<void> {
    await api.smsClear()
    storageUsage.value = storageUsage.value.map((item) => ({ ...item, used: 0 }))
  }

  function startNew() {
    composeNew.value = true
    selectedPeer.value = ''
    to.value = ''
    body.value = ''
    operationID.value = ''
  }

  function resetOperation() {
    operationID.value = ''
  }

  async function send(): Promise<{ operation_id: string }> {
    const recipient = to.value.trim()
    const message = body.value.trim()
    if (!recipient || !message) throw new Error('sms.requireRecipientAndBody')
    const result = await api.sendSMS(recipient, message)
    operationID.value = result.operation_id
    const now = new Date().toISOString()
    const sent = {
      index: -Date.now(),
      recipient,
      body: message,
      iccid: device.status?.identity.iccid || device.status?.sim.iccid,
      received_at: now,
      recorded_at: now,
    }
    sentItems.value = [...sentItems.value, sent]
    simFilter.value = sent.iccid || ''
    selectedPeer.value = threadKey(sent)
    composeNew.value = false
    body.value = ''
    return result
  }

  return {
    items,
    sentItems,
    query,
    simFilter,
    selectedPeer,
    composeNew,
    to,
    body,
    operationID,
    storageUsage,
    operation,
    threads,
    filteredThreads,
    selectedThread,
    refresh,
    clear,
    startNew,
    resetOperation,
    send,
  }
})

export type SmsStore = ReturnType<typeof useSmsStore>
export type { SmsThread }
