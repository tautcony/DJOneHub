<script setup lang="ts">
export type StatusTone = 'success' | 'warning' | 'danger' | 'info' | 'neutral'

withDefaults(
  defineProps<{
    tone?: StatusTone
    label?: string
    pulse?: boolean
  }>(),
  {
    tone: 'neutral',
    label: '',
    pulse: false,
  },
)
</script>

<template>
  <span :class="['status-indicator', `status-${tone}`, { pulse }]">
    <i aria-hidden="true" />
    <span v-if="label">{{ label }}</span>
  </span>
</template>
