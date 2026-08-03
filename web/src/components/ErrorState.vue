<script setup lang="ts">
withDefaults(
  defineProps<{
    message: string
    retryLabel?: string
  }>(),
  {
    retryLabel: '',
  },
)

const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <a-alert class="error-state" type="error" show-icon :message="message">
    <template v-if="retryLabel" #action
      ><a-button type="link" size="small" @click="emit('retry')">{{ retryLabel }}</a-button></template
    >
  </a-alert>
</template>
