<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { CopyOutlined, SendOutlined } from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import LoadingState from '../components/LoadingState.vue'
import OperationStatusView from '../components/OperationStatus.vue'
import StatusLight from '../components/StatusLight.vue'
import { useViewContext } from './context'

const { t } = useI18n()
const {
  copySMSCode,
  device,
  filteredSmsThreads,
  formatSMSDate,
  loadedViews,
  maskSensitive,
  smsComposeNew,
  selectedSmsPeer,
  selectedSmsThread,
  smsBody,
  smsOperation,
  smsQuery,
  smsTo,
  sendSMS,
} = useViewContext()

const deviceReady = computed(() => device.snapshot?.state === 'ready' && !device.error)
const selectedPeer = computed(() => selectedSmsThread.value?.peer || '')

function selectThread(thread: { key: string; peer: string }) {
  smsComposeNew.value = false
  selectedSmsPeer.value = thread.key
  smsTo.value = thread.peer
  smsBody.value = ''
}

function initials(value: string) {
  return value.replace(/\D/g, '').slice(-2) || value.slice(0, 1).toUpperCase()
}

function threadDate(value?: string) {
  return value ? formatSMSDate(value) : ''
}
</script>

<template>
  <section class="sms-workbench">
    <section class="sms-thread-pane">
      <a-input-search
        v-model:value="smsQuery"
        class="sms-search"
        allow-clear
        :placeholder="t('sms.search')"
      />
      <div class="sms-thread-toolbar">
        <span>{{ filteredSmsThreads.length }} {{ t('sms.conversations') }}</span>
        <span>{{ t('sms.latestFirst') }}</span>
      </div>
      <LoadingState v-if="!loadedViews.sms" />
      <div v-else class="sms-thread-list">
        <button
          v-for="thread in filteredSmsThreads"
          :key="thread.key"
          type="button"
          :class="[
            'sms-thread-item',
            { active: !smsComposeNew && selectedSmsThread?.key === thread.key },
          ]"
          @click="selectThread(thread)"
        >
          <span class="sms-avatar">{{ initials(thread.peer) }}</span>
          <span class="sms-thread-copy"
            ><strong>{{ maskSensitive(thread.peer) }}</strong
            ><small>{{ thread.latest?.body || t('sms.backendContent') }}</small></span
          >
          <time>{{ threadDate(thread.latest?.received_at) }}</time>
        </button>
        <EmptyState
          v-if="!filteredSmsThreads.length"
          :title="smsQuery ? t('sms.noSearchResults') : t('sms.noMessages')"
          :detail="t('sms.emptyDetail')"
        />
      </div>
    </section>

    <section class="sms-chat-pane">
      <header class="sms-chat-heading">
        <div v-if="smsComposeNew" class="sms-chat-identity">
          <span class="sms-avatar sms-avatar-large">+</span>
          <div>
            <h2>{{ t('sms.newMessage') }}</h2>
            <p>{{ t('sms.newMessageDetail') }}</p>
          </div>
        </div>
        <div v-else-if="selectedSmsThread" class="sms-chat-identity">
          <span class="sms-avatar sms-avatar-large">{{ initials(selectedPeer) }}</span>
          <div>
            <h2>{{ maskSensitive(selectedPeer) }}</h2>
            <p>
              {{ selectedSmsThread.items.length }} {{ t('sms.messagesCount') }} ·
              {{ t('sms.conversationEyebrow') }}
            </p>
          </div>
        </div>
        <div v-else class="sms-chat-identity">
          <span class="sms-avatar sms-avatar-large">?</span>
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

      <a-form
        v-if="smsComposeNew"
        class="sms-new-message"
        layout="vertical"
        @submit.prevent="sendSMS"
      >
        <a-form-item :label="t('sms.recipient')">
          <a-input v-model:value="smsTo" autofocus :placeholder="t('sms.recipientPlaceholder')" />
        </a-form-item>
        <a-form-item :label="t('sms.message')">
          <a-textarea
            v-model:value="smsBody"
            :placeholder="t('sms.messagePlaceholder')"
            :rows="7"
          />
        </a-form-item>
        <div class="sms-new-message-footer">
          <span>{{ smsBody.length }} / 160 {{ t('sms.characters') }}</span>
          <a-button
            type="primary"
            html-type="submit"
            :disabled="!device.has('sms_send') || !smsTo.trim() || !smsBody.trim()"
          >
            <SendOutlined />{{ t('common.send') }}
          </a-button>
        </div>
        <OperationStatusView :operation="smsOperation" :label="t('sms.sendStatus')" />
      </a-form>

      <div v-else-if="selectedSmsThread" class="sms-message-stream">
        <div class="sms-date-divider">
          <span>{{ t('sms.conversationHistory') }}</span>
        </div>
        <div
          v-for="item in [...selectedSmsThread.items].reverse()"
          :key="item.sender + ':' + item.received_at + ':' + item.index"
          :class="['sms-message-row', { outgoing: !item.sender && !!item.recipient }]"
        >
          <div class="sms-message-meta">
            <strong>{{ item.sender ? maskSensitive(item.sender) : t('sms.you') }}</strong
            ><time>{{ threadDate(item.received_at) }}</time>
          </div>
          <div class="sms-bubble">
            <p>{{ item.body || t('sms.backendContent') }}</p>
            <a-button
              v-if="item.code"
              type="link"
              size="small"
              class="link-button sms-code"
              @click="copySMSCode(item.code)"
              ><CopyOutlined />{{ t('sms.code', { code: item.code }) }}</a-button
            >
          </div>
        </div>
      </div>
      <EmptyState
        v-else-if="!smsComposeNew"
        class="sms-chat-empty"
        :title="t('sms.selectConversation')"
        :detail="t('sms.chatEmptyDetail')"
      />

      <a-form
        v-if="selectedSmsThread && !smsComposeNew"
        class="sms-composer"
        @submit.prevent="sendSMS"
      >
        <div class="sms-composer-footer">
          <span class="sms-composer-count">{{ smsBody.length }} / 160 {{ t('sms.characters') }}</span>
          <div class="sms-reply-row">
            <a-input
              v-model:value="smsBody"
              :placeholder="t('sms.replyPlaceholder')"
            />
            <a-button
            type="primary"
            html-type="submit"
            :disabled="!device.has('sms_send') || !smsTo.trim() || !smsBody.trim()"
            ><SendOutlined />{{ t('common.send') }}</a-button>
          </div>
        </div>
        <OperationStatusView :operation="smsOperation" :label="t('sms.sendStatus')" />
      </a-form>
    </section>
  </section>
</template>
