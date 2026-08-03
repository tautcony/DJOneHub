<script setup lang="ts">
withDefaults(
  defineProps<{
    eyebrow?: string
    title?: string
    meta?: string
    compact?: boolean
  }>(),
  {
    eyebrow: '',
    title: '',
    meta: '',
    compact: false,
  },
)
</script>

<template>
  <a-card :class="['panel', { compact }]" :bordered="true">
    <template v-if="eyebrow || title || meta || $slots.actions || $slots.header" #title>
      <slot name="header">
        <div class="panel-heading-copy">
          <span v-if="eyebrow" class="eyebrow">{{ eyebrow }}</span>
          <h2 v-if="title">{{ title }}</h2>
        </div>
      </slot>
    </template>
    <template v-if="meta || $slots.actions" #extra>
      <div class="panel-heading-side">
        <span v-if="meta" class="panel-meta">{{ meta }}</span>
        <div v-if="$slots.actions" class="panel-actions"><slot name="actions" /></div>
      </div>
    </template>
    <slot />
  </a-card>
</template>
