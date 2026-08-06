<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { StopOutlined } from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import LoadingState from '../components/LoadingState.vue'
import Panel from '../components/Panel.vue'
import { formatDateTime } from '../utils/date'
import { useViewContext } from './context'

const { t, te } = useI18n()
const { calls, device, loadedViews, rejectCall } = useViewContext()

function displayNumber(number?: string) {
  return number?.trim() || t('calls.unknownNumber')
}

// 状态/方向经 i18n 目录解析, 缺失时渲染回退文案而非原始 key。
function callState(state: string) {
  const key = `calls.state.${state}`
  return te(key) ? t(key) : t('calls.state.unknown')
}

function callDirection(direction: string) {
  const key = `calls.direction.${direction}`
  return te(key) ? t(key) : t('common.unknown')
}
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
        <div class="active-call-content">
          <span class="active-call-state">{{ callState(calls.active.state) }}</span
          ><strong class="active-call-number">{{ displayNumber(calls.active.number) }}</strong
          ><time>{{ formatDateTime(calls.active.started_at) }}</time>
        </div>
        <a-button
          class="active-call-action"
          danger
          :disabled="!device.has('call_monitor')"
          @click="rejectCall"
          ><StopOutlined />{{ t('calls.reject') }}</a-button
        >
      </div>
      <EmptyState
        v-else
        class="call-empty-state"
        :title="calls?.last_poll_error ? t('calls.monitorOffline') : t('calls.noActive')"
      />
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
        class="call-history-list"
        size="small"
        :data-source="calls.history"
      >
        <template #renderItem="{ item }">
          <a-list-item class="call-history-row">
            <div class="call-history-main">
              <strong class="call-history-number">{{ displayNumber(item.number) }}</strong>
              <div class="call-history-meta">
                <a-tag :color="item.missed ? 'error' : 'success'">{{
                  item.missed ? t('calls.missed') : t('calls.completed')
                }}</a-tag>
                <span>{{ callDirection(item.direction) }}</span>
              </div>
            </div>
            <time class="call-history-time">{{ formatDateTime(item.started_at) }}</time>
          </a-list-item>
        </template>
      </a-list>
      <EmptyState v-else class="call-empty-state" :title="t('calls.noHistory')" />
    </Panel>
  </section>
</template>
