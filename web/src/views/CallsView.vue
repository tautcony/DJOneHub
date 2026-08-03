<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { StopOutlined } from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import LoadingState from '../components/LoadingState.vue'
import Panel from '../components/Panel.vue'
import { useViewContext } from './context'

const { t } = useI18n()
const { calls, device, loadedViews, formatSMSDate, maskSensitive, rejectCall } = useViewContext()
</script>

<template>
  <section class="view-grid calls-view">
    <Panel
      class="call-panel"
      :eyebrow="t('calls.voiceEyebrow')"
      :title="t('calls.title')"
      :meta="calls?.polling ? t('calls.monitoring') : t('status.offline')"
    >
      <LoadingState v-if="!loadedViews.calls" />
      <div v-else-if="calls?.active" class="active-call">
        <div>
          <span>{{ calls.active.state }}</span
          ><strong>{{ maskSensitive(calls.active.number) }}</strong
          ><small>{{ formatSMSDate(calls.active.started_at) }}</small>
        </div>
        <a-button danger :disabled="!device.has('call_monitor')" @click="rejectCall"
          ><StopOutlined />{{ t('calls.reject') }}</a-button
        >
      </div>
      <EmptyState v-else :title="calls?.last_poll_error ? t('calls.monitorOffline') : t('calls.noActive')" />
    </Panel>
    <Panel
      class="call-panel"
      :eyebrow="t('calls.historyEyebrow')"
      :title="t('calls.history')"
      :meta="String(calls?.history?.length || 0)"
    >
      <LoadingState v-if="!loadedViews.calls" />
      <a-list
        v-else-if="calls?.history?.length"
        class="message-list"
        size="small"
        :data-source="calls.history"
        ><template #renderItem="{ item }"
          ><a-list-item class="message-row"
            ><strong>{{ maskSensitive(item.number) }}</strong
            ><span
              ><a-tag :color="item.missed ? 'error' : 'success'">{{
                item.missed ? t('calls.missed') : t('calls.completed')
              }}</a-tag>
              · {{ item.direction }}</span
            ><small>{{ formatSMSDate(item.started_at) }}</small></a-list-item
          ></template
        ></a-list
      >
      <EmptyState v-else :title="t('calls.noHistory')" />
    </Panel>
  </section>
</template>
