<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ClockCircleOutlined, DeleteOutlined, PhoneOutlined, StopOutlined } from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import LoadingState from '../components/LoadingState.vue'
import Panel from '../components/Panel.vue'
import { formatDateTime, formatDuration } from '../utils/date'
import { simProfileICCIDs, simProfileLabel } from '../utils/simprofiles'
import { useViewContext } from './context'

const { t, te } = useI18n()
const {
  calls,
  callsDialOpen,
  closeCallsDial,
  device,
  dialCall,
  dialCallBusy,
  dialNumber,
  dialWaiting,
  loadedViews,
  maskSensitive,
  openCallsDial,
  rejectCall,
  simProfiles,
} = useViewContext()
const simFilter = ref('')
const now = ref(Date.now())
let durationTimer: number | undefined
const simOptions = computed(() =>
  simProfileICCIDs(simProfiles.value, [
    calls.value?.active?.iccid,
    ...(calls.value?.history || []).map((item) => item.iccid),
  ]).map((iccid) => ({ value: iccid, label: simProfileLabel(iccid, simProfiles.value, maskSensitive) })),
)
const filteredHistory = computed(() =>
  (calls.value?.history || []).filter((item) => !simFilter.value || item.iccid === simFilter.value),
)
const activeCallDuration = computed(() =>
  calls.value?.active?.connected_at ? formatDuration(calls.value.active.connected_at, undefined, now.value) : '',
)

onMounted(() => {
  durationTimer = window.setInterval(() => {
    now.value = Date.now()
  }, 1000)
})

onBeforeUnmount(() => {
  if (durationTimer !== undefined) window.clearInterval(durationTimer)
})

// 活动通话按钮按方向区分文案: 来电「拒接」, 去电/通话中「挂断」, 均走 AT+CHUP。
const activeCallActionLabel = computed(() =>
  calls.value?.active?.direction === 'incoming' ? t('calls.reject') : t('calls.hangUp'),
)

const numpadKeys = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '*', '0', '#']

// 输入长度与后端校验一致 (≤32 字符), 避免生成无意义长串。
function appendDialKey(key: string) {
  if (dialNumber.value.length < 32) dialNumber.value += key
}

function backspaceDial() {
  dialNumber.value = dialNumber.value.slice(0, -1)
}

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

function historyDuration(connectedAt?: string, endedAt?: string) {
  return connectedAt && endedAt ? formatDuration(connectedAt, endedAt) : ''
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
      <template #actions>
        <a-button
          class="dial-entry-button"
          type="primary"
          :disabled="!!calls?.active || !device.has('call_monitor')"
          @click="openCallsDial"
          ><PhoneOutlined />{{ t('calls.dial') }}</a-button
        >
      </template>
      <LoadingState v-if="!loadedViews.calls" />
      <div v-else-if="calls?.active" class="active-call">
        <div class="active-call-content">
          <span class="active-call-state">{{ callState(calls.active.state) }}</span
          ><strong class="active-call-number">{{ displayNumber(calls.active.number) }}</strong
          ><span v-if="calls.active.iccid" class="active-call-sim">{{
            simProfileLabel(calls.active.iccid, simProfiles, maskSensitive)
          }}</span
          ><span v-if="activeCallDuration" class="call-duration active-call-duration">
            <ClockCircleOutlined />{{ t('calls.duration', { duration: activeCallDuration }) }} </span
          ><time>{{ formatDateTime(calls.active.started_at) }}</time>
        </div>
        <a-button
          class="active-call-action"
          danger
          :disabled="!device.has('call_monitor')"
          @click="rejectCall"
          ><StopOutlined />{{ activeCallActionLabel }}</a-button
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
      :meta="String(filteredHistory.length)"
    >
      <template #actions>
        <a-select
          v-model:value="simFilter"
          class="sim-filter-select"
          :aria-label="t('calls.simFilter')"
          :options="[{ value: '', label: t('calls.allSimCards') }, ...simOptions]"
        />
      </template>
      <LoadingState v-if="!loadedViews.calls" />
      <a-list
        v-else-if="filteredHistory.length"
        class="call-history-list"
        size="small"
        :data-source="filteredHistory"
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
                <span v-if="item.iccid" class="call-history-sim">{{
                  simProfileLabel(item.iccid, simProfiles, maskSensitive)
                }}</span>
              </div>
            </div>
            <div class="call-history-timing">
              <time class="call-history-time">{{ formatDateTime(item.started_at) }}</time>
              <span v-if="historyDuration(item.connected_at, item.ended_at)" class="call-duration">
                <ClockCircleOutlined />{{
                  t('calls.duration', { duration: historyDuration(item.connected_at, item.ended_at) })
                }}
              </span>
            </div>
          </a-list-item>
        </template>
      </a-list>
      <EmptyState v-else class="call-empty-state" :title="t('calls.noHistory')" />
    </Panel>
    <a-modal
      v-model:open="callsDialOpen"
      class="call-dial-modal"
      :title="t('calls.dialTitle')"
      :footer="null"
      destroy-on-close
      @cancel="closeCallsDial"
    >
      <div v-if="dialWaiting" class="call-numpad-waiting">
        <a-spin size="small" />
        <span>{{ t('calls.dialingHint') }}</span>
      </div>
      <template v-else>
        <div class="call-numpad-display">{{ dialNumber || t('calls.dialEmpty') }}</div>
        <div class="call-numpad">
          <button
            v-for="key in numpadKeys"
            :key="key"
            class="call-numpad-key"
            type="button"
            @click="appendDialKey(key)"
          >
            {{ key }}
          </button>
          <button class="call-numpad-key" type="button" @click="appendDialKey('+')">+</button>
          <button
            class="call-numpad-key"
            type="button"
            :aria-label="t('calls.numpadBackspace')"
            @click="backspaceDial"
          >
            <DeleteOutlined />
          </button>
        </div>
      </template>
      <div class="modal-actions">
        <a-button @click="closeCallsDial">{{ t('common.cancel') }}</a-button>
        <a-button type="primary" :disabled="!dialNumber.trim()" :loading="dialCallBusy" @click="dialCall"
          ><PhoneOutlined />{{ t('calls.dial') }}</a-button
        >
      </div>
    </a-modal>
  </section>
</template>
