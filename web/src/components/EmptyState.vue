<script setup lang="ts">
withDefaults(
  defineProps<{
    eyebrow?: string
    title: string
    detail?: string
  }>(),
  {
    eyebrow: '',
    detail: '',
  },
)
</script>

<template>
  <a-empty class="empty-state" :description="title">
    <p v-if="eyebrow" class="eyebrow">{{ eyebrow }}</p>
    <p v-if="detail" class="empty-state-detail">{{ detail }}</p>
    <div v-if="$slots.actions" class="empty-state-actions"><slot name="actions" /></div>
  </a-empty>
</template>
