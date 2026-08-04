<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { SendOutlined } from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import LoadingState from '../components/LoadingState.vue'
import StatusLight from '../components/StatusLight.vue'
import { formatDateTime } from '../utils/date'
import { useViewContext } from './context'

const { t } = useI18n()
const {
  device,
  filteredSmsThreads,
  loadedViews,
  resetSMSOperation,
  selectedSmsPeer,
  selectedSmsThread,
  smsBody,
  smsComposeNew,
  smsOperation,
  smsQuery,
  smsTo,
  sendSMS,
} = useViewContext()

const deviceReady = computed(() => device.snapshot?.state === 'ready' && !device.error)
const selectedPeer = computed(() => selectedSmsThread.value?.peer || '')
const chronologicalMessages = computed(() =>
  selectedSmsThread.value ? [...selectedSmsThread.value.items].reverse() : [],
)
const canSend = computed(() => device.has('sms_send') && !!smsTo.value.trim() && !!smsBody.value.trim())
const operationLabel = computed(() => {
  if (!smsOperation.value) return ''
  if (smsOperation.value.state === 'succeeded') return t('sms.sent')
  if (smsOperation.value.state === 'failed' || smsOperation.value.state === 'cancelled') {
    return t('sms.sendFailed')
  }
  return t('sms.sending')
})
const operationTone = computed(() => {
  if (!smsOperation.value) return 'neutral'
  if (smsOperation.value.state === 'succeeded') return 'success'
  if (smsOperation.value.state === 'failed' || smsOperation.value.state === 'cancelled') {
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
        <div>
          <h2>{{ t('sms.conversationList') }}</h2>
          <span>{{ filteredSmsThreads.length }} {{ t('sms.conversations') }}</span>
        </div>
      </div>

      <a-input-search
        v-model:value="smsQuery"
        class="sms-search"
        allow-clear
        :placeholder="t('sms.search')"
      />

      <LoadingState v-if="!loadedViews.sms" />
      <div v-else class="sms-thread-list">
        <button
          v-for="thread in filteredSmsThreads"
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
          </span>
        </button>

        <EmptyState
          v-if="!filteredSmsThreads.length"
          :title="smsQuery ? t('sms.noSearchResults') : t('sms.noMessages')"
          :detail="t('sms.emptyDetail')"
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
            <p>{{ selectedSmsThread.items.length }} {{ t('sms.messagesCount') }}</p>
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
              v-if="smsOperation"
              :tone="operationTone"
              :pulse="smsOperation.state === 'running' || smsOperation.state === 'pending'"
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
