<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { CloseOutlined } from '@ant-design/icons-vue'
import OperationStatusView from '../OperationStatus.vue'
import type { OperationStatus } from '../../types'

defineProps<{
  operation: OperationStatus
}>()

defineEmits<{
  close: []
}>()

const { t } = useI18n()
</script>

<template>
  <section class="esim-operation-dock" aria-live="polite" :aria-label="t('esim.operationDock')">
    <a-button
      class="esim-operation-close"
      type="text"
      shape="circle"
      size="small"
      :aria-label="t('common.close')"
      :title="t('common.close')"
      @click="$emit('close')"
    >
      <CloseOutlined />
    </a-button>
    <OperationStatusView :operation="operation" :label="t('esim.operationDock')" />
    <a-alert
      v-if="operation.error"
      class="esim-operation-error"
      type="error"
      show-icon
      :message="t('esim.operationFailed')"
      :description="operation.error.message || t('common.error')"
    />
  </section>
</template>
