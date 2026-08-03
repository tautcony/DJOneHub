<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    label: string
    value?: unknown
    placeholder?: string
    monospace?: boolean
  }>(),
  {
    placeholder: '—',
    monospace: false,
  },
)

const displayValue = computed(() => {
  if (props.value === null || props.value === undefined || props.value === '') return props.placeholder
  return String(props.value)
})
</script>

<template>
  <div class="field-row">
    <span>{{ props.label }}</span>
    <strong :class="{ monospace: props.monospace }" :title="displayValue">
      <slot>{{ displayValue }}</slot>
    </strong>
  </div>
</template>
