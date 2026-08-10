<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useViewContext } from '../context'
import type { NotificationChannelsSettings } from '../../types'

const { t } = useI18n()
const {
  notificationChannels,
  notificationChannelsBusy,
  notificationChannelTesting,
  saveNotificationChannels,
  testNotificationChannel,
  discoverTelegramChatIDs,
} = useViewContext()

// channels 复用 notificationChannels 的响应式对象; v-if 已保证非 null 时才渲染。
const channels = computed<NotificationChannelsSettings>(() => notificationChannels.value!)
type ChannelKey = 'telegram' | 'feishu' | 'webhook' | 'bark' | 'email' | 'pushplus'
const channelTitle = (key: ChannelKey) => t(`settings.channels.${key}.title`)

// Webhook 自定义请求头以 map 存储，这里维护一份可编辑的 key/value 列表，
// 在保存前写回 channels.webhook.headers。watch 在后端回写 channels 时同步。
const webhookHeaderRows = ref<Array<{ key: string; value: string }>>([])
watch(
  notificationChannels,
  (value) => {
    const headers = value?.webhook.headers || {}
    webhookHeaderRows.value = Object.entries(headers).map(([key, value]) => ({ key, value }))
  },
  { immediate: true },
)
function addWebhookHeader() {
  webhookHeaderRows.value.push({ key: '', value: '' })
}
function removeWebhookHeader(index: number) {
  webhookHeaderRows.value.splice(index, 1)
}
function commitWebhookHeaders() {
  const map: Record<string, string> = {}
  for (const row of webhookHeaderRows.value) {
    const key = row.key.trim()
    if (key) map[key] = row.value
  }
  channels.value.webhook.headers = map
}
function saveChannels() {
  commitWebhookHeaders()
  void saveNotificationChannels()
}

// 测试使用当前表单配置（未保存也能验证）。机密字段在表单里是 __unchanged__
// 占位符，后端会用已保存的真实密钥还原，因此无需重新输入即可测试。
function testChannel(key: ChannelKey) {
  commitWebhookHeaders()
  void testNotificationChannel(key, channels.value)
}

const barkLevels = ['active', 'timeSensitive', 'critical', 'passive']
</script>

<template>
  <div v-if="notificationChannels" class="settings-section">
    <div class="settings-section-heading">
      <div>
        <span class="eyebrow">{{ t('settings.channels.eyebrow') }}</span>
        <h3>{{ t('settings.channels.title') }}</h3>
      </div>
    </div>
    <p class="settings-detail">{{ t('settings.channels.detail') }}</p>

    <!-- Telegram -->
    <div class="channel-card">
      <div class="channel-card-head">
        <div>
          <strong>{{ channelTitle('telegram') }}</strong>
          <small>{{ t('settings.channels.telegram.detail') }}</small>
        </div>
        <div class="channel-card-actions">
          <a-switch v-model:checked="channels.telegram.enabled" :disabled="notificationChannelsBusy" />
          <a-button
            :loading="notificationChannelTesting === 'telegram'"
            :disabled="notificationChannelsBusy"
            @click="testChannel('telegram')"
            >{{ t('settings.channels.test') }}</a-button
          >
        </div>
      </div>
      <a-form layout="vertical" class="channel-form">
        <a-form-item :label="t('settings.field.botToken')">
          <a-input-password v-model:value="channels.telegram.bot_token" />
        </a-form-item>
        <a-form-item :label="t('settings.field.chatID')">
          <a-input-number
            v-model:value="channels.telegram.chat_id"
            :min="-1"
            :precision="0"
            style="width: 100%"
          />
          <a-button
            class="discover-chat-id"
            :loading="notificationChannelTesting === 'telegram-chat-id'"
            :disabled="notificationChannelsBusy || notificationChannelTesting !== null"
            @click="discoverTelegramChatIDs()"
            >{{ t('settings.channels.telegram.findChatID') }}</a-button
          >
        </a-form-item>
        <a-form-item :label="t('settings.field.adminID')">
          <a-input-number
            v-model:value="channels.telegram.admin_id"
            :min="-1"
            :precision="0"
            style="width: 100%"
          />
        </a-form-item>
        <a-form-item :label="t('settings.field.baseURL')">
          <a-input v-model:value="channels.telegram.base_url" :placeholder="t('settings.field.baseURL')" />
        </a-form-item>
        <a-form-item :label="t('settings.field.proxy')">
          <a-input v-model:value="channels.telegram.proxy" :placeholder="t('settings.field.proxy')" />
        </a-form-item>
      </a-form>
    </div>

    <!-- Feishu -->
    <div class="channel-card">
      <div class="channel-card-head">
        <div>
          <strong>{{ channelTitle('feishu') }}</strong>
          <small>{{ t('settings.channels.feishu.detail') }}</small>
        </div>
        <div class="channel-card-actions">
          <a-switch v-model:checked="channels.feishu.enabled" :disabled="notificationChannelsBusy" />
          <a-button
            :loading="notificationChannelTesting === 'feishu'"
            :disabled="notificationChannelsBusy"
            @click="testChannel('feishu')"
            >{{ t('settings.channels.test') }}</a-button
          >
        </div>
      </div>
      <a-form layout="vertical" class="channel-form">
        <a-form-item :label="t('settings.field.appID')">
          <a-input v-model:value="channels.feishu.app_id" />
        </a-form-item>
        <a-form-item :label="t('settings.field.appSecret')">
          <a-input-password v-model:value="channels.feishu.app_secret" />
        </a-form-item>
        <a-form-item :label="t('settings.field.chatIDs')">
          <a-select
            v-model:value="channels.feishu.chat_ids"
            mode="tags"
            :placeholder="t('settings.field.chatIDs')"
          />
        </a-form-item>
      </a-form>
    </div>

    <!-- Webhook -->
    <div class="channel-card">
      <div class="channel-card-head">
        <div>
          <strong>{{ channelTitle('webhook') }}</strong>
          <small>{{ t('settings.channels.webhook.detail') }}</small>
        </div>
        <div class="channel-card-actions">
          <a-switch v-model:checked="channels.webhook.enabled" :disabled="notificationChannelsBusy" />
          <a-button
            :loading="notificationChannelTesting === 'webhook'"
            :disabled="notificationChannelsBusy"
            @click="testChannel('webhook')"
            >{{ t('settings.channels.test') }}</a-button
          >
        </div>
      </div>
      <a-form layout="vertical" class="channel-form">
        <a-form-item :label="t('settings.field.urls')">
          <a-select
            v-model:value="channels.webhook.urls"
            mode="tags"
            :placeholder="t('settings.field.urls')"
          />
        </a-form-item>
        <a-form-item :label="t('settings.field.secret')">
          <a-input-password v-model:value="channels.webhook.secret" />
          <small class="field-hint">{{ t('settings.channels.secretHint') }}</small>
        </a-form-item>
        <a-form-item :label="t('settings.field.timeoutMs')">
          <a-input-number
            v-model:value="channels.webhook.timeout_ms"
            :min="1"
            :precision="0"
            style="width: 100%"
          />
        </a-form-item>
        <a-form-item :label="t('settings.field.retryMax')">
          <a-input-number
            v-model:value="channels.webhook.retry_max"
            :min="0"
            :precision="0"
            style="width: 100%"
          />
        </a-form-item>
        <a-form-item :label="t('settings.field.textTemplate')">
          <a-textarea
            v-model:value="channels.webhook.text_template"
            :auto-size="{ minRows: 2, maxRows: 5 }"
          />
        </a-form-item>
        <a-form-item :label="t('settings.field.headers')">
          <div class="header-editor">
            <div v-for="(row, index) in webhookHeaderRows" :key="index" class="header-row">
              <a-input v-model:value="row.key" :placeholder="t('settings.field.headerName')" />
              <a-input v-model:value="row.value" :placeholder="t('settings.field.headerValue')" />
              <a-button type="text" danger @click="removeWebhookHeader(index)">{{
                t('settings.field.removeHeader')
              }}</a-button>
            </div>
            <a-button block @click="addWebhookHeader">{{ t('settings.field.addHeader') }}</a-button>
          </div>
        </a-form-item>
      </a-form>
    </div>

    <!-- Bark -->
    <div class="channel-card">
      <div class="channel-card-head">
        <div>
          <strong>{{ channelTitle('bark') }}</strong>
          <small>{{ t('settings.channels.bark.detail') }}</small>
        </div>
        <div class="channel-card-actions">
          <a-switch v-model:checked="channels.bark.enabled" :disabled="notificationChannelsBusy" />
          <a-button
            :loading="notificationChannelTesting === 'bark'"
            :disabled="notificationChannelsBusy"
            @click="testChannel('bark')"
            >{{ t('settings.channels.test') }}</a-button
          >
        </div>
      </div>
      <a-form layout="vertical" class="channel-form">
        <a-form-item :label="t('settings.field.urls')">
          <a-select v-model:value="channels.bark.urls" mode="tags" :placeholder="t('settings.field.urls')" />
        </a-form-item>
        <a-form-item :label="t('settings.field.group')">
          <a-input v-model:value="channels.bark.group" />
        </a-form-item>
        <a-form-item :label="t('settings.field.icon')">
          <a-input v-model:value="channels.bark.icon" :placeholder="t('settings.field.icon')" />
        </a-form-item>
        <a-form-item :label="t('settings.field.level')">
          <a-select v-model:value="channels.bark.level">
            <a-select-option v-for="level in barkLevels" :key="level" :value="level">{{
              level
            }}</a-select-option>
          </a-select>
        </a-form-item>
      </a-form>
    </div>

    <!-- Email -->
    <div class="channel-card">
      <div class="channel-card-head">
        <div>
          <strong>{{ channelTitle('email') }}</strong>
          <small>{{ t('settings.channels.email.detail') }}</small>
        </div>
        <div class="channel-card-actions">
          <a-switch v-model:checked="channels.email.enabled" :disabled="notificationChannelsBusy" />
          <a-button
            :loading="notificationChannelTesting === 'email'"
            :disabled="notificationChannelsBusy"
            @click="testChannel('email')"
            >{{ t('settings.channels.test') }}</a-button
          >
        </div>
      </div>
      <a-form layout="vertical" class="channel-form">
        <a-form-item :label="t('settings.field.useSSL')">
          <a-switch v-model:checked="channels.email.use_ssl" />
        </a-form-item>
        <a-form-item :label="t('settings.field.smtpHost')">
          <a-input v-model:value="channels.email.smtp_host" />
        </a-form-item>
        <a-form-item :label="t('settings.field.smtpPort')">
          <a-input-number
            v-model:value="channels.email.smtp_port"
            :min="1"
            :max="65535"
            :precision="0"
            style="width: 100%"
          />
        </a-form-item>
        <a-form-item :label="t('settings.field.username')">
          <a-input v-model:value="channels.email.username" />
        </a-form-item>
        <a-form-item :label="t('settings.field.password')">
          <a-input-password v-model:value="channels.email.password" />
          <small class="field-hint">{{ t('settings.channels.secretHint') }}</small>
        </a-form-item>
        <a-form-item :label="t('settings.field.fromAddress')">
          <a-input v-model:value="channels.email.from_address" />
        </a-form-item>
        <a-form-item :label="t('settings.field.toAddresses')">
          <a-select
            v-model:value="channels.email.to_addresses"
            mode="tags"
            :placeholder="t('settings.field.toAddresses')"
          />
        </a-form-item>
      </a-form>
    </div>

    <!-- Pushplus -->
    <div class="channel-card">
      <div class="channel-card-head">
        <div>
          <strong>{{ channelTitle('pushplus') }}</strong>
          <small>{{ t('settings.channels.pushplus.detail') }}</small>
        </div>
        <div class="channel-card-actions">
          <a-switch v-model:checked="channels.pushplus.enabled" :disabled="notificationChannelsBusy" />
          <a-button
            :loading="notificationChannelTesting === 'pushplus'"
            :disabled="notificationChannelsBusy"
            @click="testChannel('pushplus')"
            >{{ t('settings.channels.test') }}</a-button
          >
        </div>
      </div>
      <a-form layout="vertical" class="channel-form">
        <a-form-item :label="t('settings.field.token')">
          <a-input-password v-model:value="channels.pushplus.token" />
          <small class="field-hint">{{ t('settings.channels.secretHint') }}</small>
        </a-form-item>
        <a-form-item :label="t('settings.field.topic')">
          <a-input v-model:value="channels.pushplus.topic" :placeholder="t('settings.field.topic')" />
        </a-form-item>
        <a-form-item :label="t('settings.field.channel')">
          <a-input v-model:value="channels.pushplus.channel" />
        </a-form-item>
      </a-form>
    </div>

    <div class="panel-actions settings-actions">
      <a-button type="primary" :loading="notificationChannelsBusy" @click="saveChannels()">{{
        t('settings.channels.save')
      }}</a-button>
    </div>
  </div>
</template>

<style scoped>
.channel-card {
  margin-top: 16px;
  padding: 16px;
  border: 1px solid var(--ui-border);
  border-radius: 10px;
  background: var(--ui-surface-muted);
}
.channel-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.channel-card-head strong {
  display: block;
  color: var(--ui-text);
  font-size: 14px;
}
.channel-card-head small {
  display: block;
  margin-top: 3px;
  color: var(--ui-text-secondary);
  font-size: 12px;
  line-height: 1.45;
}
.channel-card-actions {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 12px;
}
.channel-form {
  margin-top: 14px;
}
.field-hint {
  display: block;
  margin-top: 4px;
  color: var(--ui-text-tertiary);
  font-size: 12px;
}
.header-editor {
  display: grid;
  gap: 8px;
}
.header-row {
  display: grid;
  grid-template-columns: 1fr 1fr auto;
  gap: 8px;
  align-items: center;
}
</style>
