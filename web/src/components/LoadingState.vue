<script setup lang="ts">
withDefaults(
  defineProps<{
    label?: string
    rows?: number
  }>(),
  {
    label: '',
    rows: 3,
  },
)
</script>

<template>
  <div class="loading-state" role="status" aria-live="polite">
    <a-spin size="large" />
    <span v-if="label">{{ label }}</span>
    <a-skeleton active :paragraph="{ rows }" />
  </div>
</template>
