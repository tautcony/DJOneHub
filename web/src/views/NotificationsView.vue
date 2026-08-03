<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import {
  BellOutlined,
  CheckOutlined,
  EnvironmentOutlined,
  MessageOutlined,
  PhoneOutlined,
  ReloadOutlined,
  StopOutlined,
  WifiOutlined,
} from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import LoadingState from '../components/LoadingState.vue'
import Panel from '../components/Panel.vue'
import { useViewContext } from './context'

const { t } = useI18n()
const {
  debugEventData,
  loadedViews,
  newNotifierCall,
  notifierBody,
  notifierCallID,
  notifierCode,
  notifierEvents,
  notifierInfo,
  notifierNumber,
  notifierRecipient,
  notifierSender,
  triggerNotifierDebug,
} = useViewContext()
</script>

<template>
  <section class="view-grid notifier-view">
    <LoadingState v-if="!loadedViews.notifications" /><template v-else
      ><div class="status-banner notifier-status">
        <div>
          <span class="eyebrow">{{ t('notifications.eyebrow') }}</span>
          <h2>{{ t('notifications.title') }}</h2>
          <p>
            {{ notifierInfo?.native_ui ? t('notifications.nativeUIHint') : t('notifications.headlessHint') }}
          </p>
        </div>
        <a-tag :color="notifierInfo?.native_ui ? 'green' : 'default'"
          ><BellOutlined />
          {{ notifierInfo?.native_ui ? t('notifications.nativeUI') : t('notifications.headless') }}</a-tag
        >
      </div>
      <Panel :eyebrow="t('notifications.call')" :title="t('notifications.call')" :meta="notifierCallID"
        ><div class="notifier-fields">
          <a-form-item :label="t('notifications.callID')"
            ><a-input v-model:value="notifierCallID" /></a-form-item
          ><a-form-item :label="t('notifications.number')"
            ><a-input v-model:value="notifierNumber"
          /></a-form-item>
        </div>
        <div class="panel-actions notifier-actions">
          <a-button type="primary" @click="triggerNotifierDebug('call_incoming')"
            ><PhoneOutlined />{{ t('notifications.incoming') }}</a-button
          ><a-button @click="triggerNotifierDebug('call_updated')"
            ><PhoneOutlined />{{ t('notifications.updated') }}</a-button
          ><a-button @click="triggerNotifierDebug('call_ended')"
            ><StopOutlined />{{ t('notifications.ended') }}</a-button
          ><a-button danger @click="triggerNotifierDebug('call_missed')"
            ><PhoneOutlined />{{ t('notifications.missed') }}</a-button
          ><a-button @click="newNotifierCall"><ReloadOutlined />{{ t('notifications.newCall') }}</a-button>
        </div></Panel
      ><Panel :eyebrow="t('notifications.sms')" :title="t('notifications.sms')"
        ><div class="notifier-fields notifier-sms-fields">
          <a-form-item :label="t('notifications.sender')"
            ><a-input v-model:value="notifierSender" /></a-form-item
          ><a-form-item :label="t('notifications.recipient')"
            ><a-input v-model:value="notifierRecipient" /></a-form-item
          ><a-form-item :label="t('notifications.message')"
            ><a-textarea v-model:value="notifierBody" :rows="3" /></a-form-item
          ><a-form-item :label="t('notifications.code')"
            ><a-input v-model:value="notifierCode"
          /></a-form-item>
        </div>
        <div class="panel-actions notifier-actions">
          <a-button type="primary" @click="triggerNotifierDebug('sms_received')"
            ><MessageOutlined />{{ t('notifications.sendSMS') }}</a-button
          >
        </div></Panel
      ><Panel :eyebrow="t('notifications.device')" :title="t('notifications.device')"
        ><div class="panel-actions notifier-actions notifier-state-actions">
          <a-button danger @click="triggerNotifierDebug('device_offline')"
            ><StopOutlined />{{ t('notifications.offline') }}</a-button
          ><a-button @click="triggerNotifierDebug('device_ready')"
            ><CheckOutlined />{{ t('notifications.ready') }}</a-button
          >
        </div></Panel
      ><Panel :eyebrow="t('notifications.gps')" :title="t('notifications.gps')"
        ><div class="panel-actions notifier-actions notifier-state-actions">
          <a-button @click="triggerNotifierDebug('gps_searching')"
            ><EnvironmentOutlined />{{ t('notifications.searching') }}</a-button
          ><a-button type="primary" @click="triggerNotifierDebug('gps_fix')"
            ><EnvironmentOutlined />{{ t('notifications.fix') }}</a-button
          ><a-button @click="triggerNotifierDebug('gps_disabled')"
            ><StopOutlined />{{ t('notifications.disabled') }}</a-button
          >
        </div></Panel
      ><Panel :eyebrow="t('notifications.network')" :title="t('notifications.network')"
        ><div class="panel-actions notifier-actions notifier-state-actions">
          <a-button type="primary" @click="triggerNotifierDebug('network_connected')"
            ><WifiOutlined />{{ t('notifications.connected') }}</a-button
          ><a-button @click="triggerNotifierDebug('network_weak')"
            ><WifiOutlined />{{ t('notifications.weak') }}</a-button
          ><a-button @click="triggerNotifierDebug('network_offline')"
            ><StopOutlined />{{ t('notifications.networkOffline') }}</a-button
          >
        </div></Panel
      ><Panel
        class="notifier-log-panel"
        :eyebrow="t('notifications.eventLog')"
        :title="t('notifications.eventLog')"
        :meta="t('notifications.eventCount', { count: notifierEvents.length })"
        ><div v-if="notifierEvents.length" class="notifier-event-list">
          <article v-for="event in notifierEvents" :key="event.id" class="notifier-event">
            <div class="notifier-event-heading">
              <strong>{{ event.type }}</strong
              ><time>{{ new Date(event.occurred_at).toLocaleString() }}</time>
            </div>
            <pre>{{ debugEventData(event) }}</pre>
          </article>
        </div>
        <EmptyState v-else :title="t('notifications.noEvents')" /></Panel
    ></template>
  </section>
</template>
