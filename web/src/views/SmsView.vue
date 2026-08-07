<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { SendOutlined, DeleteOutlined } from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import LoadingState from '../components/LoadingState.vue'
import StatusLight from '../components/StatusLight.vue'
import { formatDateTime } from '../utils/date'
import { simProfileICCIDs, simProfileLabel } from '../utils/simprofiles'
import type { OperationStatus } from '../types'
import { useViewContext } from './context'

// 会话列表惰性渲染: 超过阈值时不挂载全部行, 分批展开。
const SMS_THREAD_LAZY_THRESHOLD = 100
const SMS_THREAD_LAZY_BATCH = 50

const { t } = useI18n()
const {
  clearModuleSMS,
  device,
  filteredSmsThreads,
  loadedViews,
  maskSensitive,
  resetSMSOperation,
  simProfiles,
  selectedSmsPeer,
  selectedSmsThread,
  smsBody,
  smsComposeNew,
  smsOperation,
  smsQuery,
  smsSimFilter,
  smsThreads,
  smsTo,
  sendSMS,
} = useViewContext()

const deviceReady = computed(() => device.snapshot?.state === 'ready' && !device.error)
// 未插卡时模组短信存储不可用：明确提示而非把"无卡"当成加载失败。
const noSim = computed(() => device.status != null && device.status.sim?.inserted !== true)
// 设备离线/未就绪时仍展示已持久化的本地缓存短信, 并给出明确提示。
const offline = computed(() => !deviceReady.value)
const selectedPeer = computed(() => selectedSmsThread.value?.peer || '')
const chronologicalMessages = computed(() =>
  selectedSmsThread.value ? [...selectedSmsThread.value.items].reverse() : [],
)
const canSend = computed(() => device.has('sms_send') && !!smsTo.value.trim() && !!smsBody.value.trim())
const simOptions = computed(() =>
  simProfileICCIDs(
    simProfiles.value,
    smsThreads.value.map((thread) => thread.iccid),
  ).map((iccid) => ({ value: iccid, label: simProfileLabel(iccid, simProfiles.value, maskSensitive) })),
)
const selectedThreadSim = computed(() =>
  selectedSmsThread.value?.iccid
    ? simProfileLabel(selectedSmsThread.value.iccid, simProfiles.value, maskSensitive)
    : '',
)

// threadListLazy 报告会话数是否超过惰性阈值; visibleThreads 只挂载前 N 行。
const threadListLazy = computed(() => filteredSmsThreads.value.length > SMS_THREAD_LAZY_THRESHOLD)
const visibleThreadCount = ref(SMS_THREAD_LAZY_BATCH)
const visibleThreads = computed(() => filteredSmsThreads.value.slice(0, visibleThreadCount.value))
watch(
  filteredSmsThreads,
  () => {
    if (!threadListLazy.value) visibleThreadCount.value = SMS_THREAD_LAZY_BATCH
  },
  { immediate: true },
)
function showMoreThreads() {
  visibleThreadCount.value += SMS_THREAD_LAZY_BATCH
}
// 发送状态指示在会话切换前保留终态快照, 避免 operation 被 5 分钟 TTL 清理后
// 指示器突然消失 (仍由 selectThread 复位)。
const smsOperationSnapshot = ref<OperationStatus | undefined>(undefined)
watch(
  smsOperation,
  (operation) => {
    if (operation) smsOperationSnapshot.value = operation
  },
  { immediate: true },
)

const operationLabel = computed(() => {
  if (!smsOperationSnapshot.value) return ''
  if (smsOperationSnapshot.value.state === 'succeeded') return t('sms.sent')
  if (smsOperationSnapshot.value.state === 'failed' || smsOperationSnapshot.value.state === 'cancelled') {
    return t('sms.sendFailed')
  }
  return t('sms.sending')
})
const operationTone = computed(() => {
  if (!smsOperationSnapshot.value) return 'neutral'
  if (smsOperationSnapshot.value.state === 'succeeded') return 'success'
  if (smsOperationSnapshot.value.state === 'failed' || smsOperationSnapshot.value.state === 'cancelled') {
    return 'danger'
  }
  return 'info'
})

function selectThread(thread: { key: string; peer: string }) {
  smsComposeNew.value = false
  selectedSmsPeer.value = thread.key
  smsTo.value = thread.peer
  smsBody.value = ''
  resetSMSOperation()
  smsOperationSnapshot.value = undefined
}

function initials(value: string) {
  return value.replace(/\D/g, '').slice(-2) || value.slice(0, 1).toUpperCase()
}

function threadDate(value?: string) {
  return formatDateTime(value)
}
</script>

<template>
  <section class="sms-workbench">
    <aside class="sms-thread-pane">
      <div class="sms-list-heading">
        <div class="sms-title">
          <h2>{{ t('sms.conversationList') }}</h2>
          <span>{{ filteredSmsThreads.length }} {{ t('sms.conversations') }}</span>
        </div>
        <a-button
          class="sms-clear-btn"
          type="text"
          size="small"
          :disabled="!device.has('sms_read')"
          :title="t('common.clear')"
          @click="clearModuleSMS"
        >
          <DeleteOutlined />{{ t('common.clear') }}
        </a-button>
      </div>

      <a-input-search
        v-model:value="smsQuery"
        class="sms-search"
        allow-clear
        :placeholder="t('sms.search')"
      />

      <a-select
        v-model:value="smsSimFilter"
        class="sms-sim-filter"
        :aria-label="t('sms.simFilter')"
        :options="[{ value: '', label: t('sms.allSimCards') }, ...simOptions]"
      />

      <LoadingState v-if="!loadedViews.sms" />
      <div v-else class="sms-thread-list">
        <div v-if="offline" class="sms-offline-banner">{{ t('sms.offlineHint') }}</div>

        <button
          v-for="thread in visibleThreads"
          :key="thread.key"
          type="button"
          :class="['sms-thread-item', { active: !smsComposeNew && selectedSmsThread?.key === thread.key }]"
          @click="selectThread(thread)"
        >
          <span class="sms-avatar">{{ initials(thread.peer) }}</span>
          <span class="sms-thread-copy">
            <span class="sms-thread-line">
              <strong>{{ thread.peer }}</strong>
              <time>{{ threadDate(thread.latest?.received_at) }}</time>
            </span>
            <small>{{ thread.latest?.body || t('sms.backendContent') }}</small>
            <span v-if="thread.iccid" class="sms-thread-sim">{{
              simProfileLabel(thread.iccid, simProfiles, maskSensitive)
            }}</span>
          </span>
        </button>

        <button
          v-if="threadListLazy && visibleThreads.length < filteredSmsThreads.length"
          type="button"
          class="sms-thread-more"
          @click="showMoreThreads"
        >
          {{ t('sms.showMoreThreads', { remaining: filteredSmsThreads.length - visibleThreads.length }) }}
        </button>

        <EmptyState
          v-if="!filteredSmsThreads.length"
          :title="noSim ? t('sms.noSim') : (smsQuery ? t('sms.noSearchResults') : t('sms.noMessages'))"
          :detail="noSim ? t('sms.noSimDetail') : t('sms.emptyDetail')"
        />
      </div>
    </aside>

    <section class="sms-chat-pane">
      <header class="sms-chat-heading">
        <div v-if="smsComposeNew" class="sms-chat-identity">
          <div>
            <h2>{{ t('sms.newMessage') }}</h2>
            <p>{{ t('sms.newMessageDetail') }}</p>
          </div>
        </div>
        <div v-else-if="selectedSmsThread" class="sms-chat-identity">
          <span class="sms-avatar sms-avatar-large">{{ initials(selectedPeer) }}</span>
          <div>
            <h2>{{ selectedPeer }}</h2>
            <p>
              {{ selectedSmsThread.items.length }} {{ t('sms.messagesCount') }}
              <template v-if="selectedThreadSim"> · {{ selectedThreadSim }}</template>
            </p>
          </div>
        </div>
        <div v-else class="sms-chat-identity">
          <div>
            <h2>{{ t('sms.selectConversation') }}</h2>
            <p>{{ t('sms.chatEmptyDetail') }}</p>
          </div>
        </div>
        <StatusLight
          v-if="selectedSmsThread || smsComposeNew"
          :tone="deviceReady ? 'success' : 'neutral'"
          :label="deviceReady ? t('sms.readyToSend') : t('status.offline')"
        />
      </header>

      <div v-if="smsComposeNew" class="sms-recipient-bar">
        <label for="sms-recipient">{{ t('sms.recipient') }}</label>
        <a-input
          id="sms-recipient"
          v-model:value="smsTo"
          autofocus
          :placeholder="t('sms.recipientPlaceholder')"
        />
      </div>

      <div v-if="selectedSmsThread" class="sms-message-stream">
        <div
          v-for="item in chronologicalMessages"
          :key="item.sender + ':' + item.received_at + ':' + item.index"
          :class="['sms-message-row', { outgoing: !item.sender && !!item.recipient }]"
        >
          <div class="sms-bubble">
            <p>{{ item.body || t('sms.backendContent') }}</p>
          </div>
          <time>{{ threadDate(item.received_at) }}</time>
        </div>
      </div>

      <div v-else-if="smsComposeNew" class="sms-compose-empty">
        <p>{{ t('sms.composePrompt') }}</p>
      </div>

      <EmptyState
        v-else
        class="sms-chat-empty"
        :title="t('sms.selectConversation')"
        :detail="t('sms.chatEmptyDetail')"
      />

      <form v-if="selectedSmsThread || smsComposeNew" class="sms-composer" @submit.prevent="sendSMS">
        <a-textarea
          v-model:value="smsBody"
          :auto-size="{ minRows: 2, maxRows: 5 }"
          :placeholder="smsComposeNew ? t('sms.messagePlaceholder') : t('sms.replyPlaceholder')"
        />
        <div class="sms-composer-actions">
          <div class="sms-composer-meta">
            <span>{{ smsBody.length }} {{ t('sms.characters') }}</span>
            <StatusLight
              v-if="smsOperationSnapshot"
              :tone="operationTone"
              :pulse="smsOperationSnapshot.state === 'running' || smsOperationSnapshot.state === 'pending'"
              :label="operationLabel"
            />
          </div>
          <a-button type="primary" html-type="submit" :disabled="!canSend">
            <SendOutlined />{{ t('common.send') }}
          </a-button>
        </div>
      </form>
    </section>
  </section>
</template>
