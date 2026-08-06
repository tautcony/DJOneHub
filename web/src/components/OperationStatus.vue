<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OperationStatus } from '../types'
import StatusLight, { type StatusTone } from './StatusLight.vue'

const props = withDefaults(
  defineProps<{
    operation?: OperationStatus | null
    label: string
  }>(),
  {
    operation: null,
  },
)

const { t, te } = useI18n()

// 操作状态经 i18n 目录解析, 缺失时渲染回退文案而非原始 key。
function operationStateLabel(state: string) {
  const key = `operation.states.${state}`
  return te(key) ? t(key) : t('operation.states.unknown')
}

const tone = computed<StatusTone>(() => {
  if (!props.operation) return 'neutral'
  if (props.operation.state === 'succeeded') return 'success'
  if (props.operation.state === 'failed' || props.operation.state === 'cancelled') return 'danger'
  return 'info'
})
</script>

<template>
  <a-card v-if="props.operation" class="operation-status" size="small" :bordered="true">
    <div class="operation-topline">
      <div class="operation-label">
        <StatusLight
          :tone="tone"
          :pulse="props.operation.state === 'running' || props.operation.state === 'pending'"
        />
        <span>{{ props.label }}</span>
      </div>
      <strong>{{ operationStateLabel(props.operation.state) }} · {{ props.operation.progress }}%</strong>
    </div>
    <a-progress
      :percent="props.operation.progress"
      :show-info="false"
      size="small"
      :status="props.operation.state === 'failed' ? 'exception' : undefined"
    />
    <p>{{ props.operation.message || props.operation.operation_id }}</p>
  </a-card>
</template>
