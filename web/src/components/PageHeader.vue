<script setup lang="ts">
withDefaults(
  defineProps<{
    eyebrow?: string
    title: string
    subtitle?: string
  }>(),
  {
    eyebrow: '',
    subtitle: '',
  },
)
</script>

<template>
  <a-page-header class="page-header" :title="title" :sub-title="subtitle || undefined" :ghost="false">
    <template v-if="eyebrow" #tags
      ><a-tag color="blue">{{ eyebrow }}</a-tag></template
    >
    <template v-if="$slots.actions" #extra><slot name="actions" /></template>
  </a-page-header>
</template>
