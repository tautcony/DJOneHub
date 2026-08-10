<script setup lang="ts">
import { computed } from 'vue'
import { BaseEdge, getSmoothStepPath, type EdgeProps } from '@vue-flow/core'

const props = defineProps<EdgeProps>()
const path = computed(() =>
  getSmoothStepPath({
    sourceX: props.sourceX,
    sourceY: props.sourceY,
    targetX: props.targetX,
    targetY: props.targetY,
    sourcePosition: props.sourcePosition,
    targetPosition: props.targetPosition,
    borderRadius: 8,
  }),
)
const eventType = computed(() => String(props.data?.eventType || 'event'))
const traceID = computed(() => String(props.data?.traceID || ''))
const showLabel = computed(() => Boolean(props.data?.showLabel))
const failed = computed(() => ['failed', 'dropped'].includes(String(props.data?.status || '')))
const chipWidth = computed(() =>
  showLabel.value ? Math.min(156, Math.max(78, eventType.value.length * 6.4 + 38)) : 38,
)
const delay = computed(() => Number(props.data?.delay || 0))
const duration = computed(() => Number(props.data?.duration || 0.78))
</script>

<template>
  <BaseEdge :id="id" :path="path[0]" :style="{ stroke: 'transparent', strokeWidth: 14 }" />
  <g :class="['runtime-trace-particle', { failed, branch: !showLabel }]" opacity="0">
    <set attributeName="opacity" to="1" :begin="`${delay}s`" fill="freeze" />
    <animateMotion :path="path[0]" :dur="`${duration}s`" :begin="`${delay}s`" repeatCount="1" fill="freeze" />
    <animate
      attributeName="opacity"
      from="1"
      to="0"
      :begin="`${delay + duration}s`"
      dur="0.18s"
      fill="freeze"
    />
    <rect :x="-chipWidth / 2" y="-10" :width="chipWidth" height="20" rx="4" />
    <circle :cx="-chipWidth / 2 + 9" cy="0" r="3" />
    <text :x="showLabel ? 3 : 4" y="3.5" text-anchor="middle">{{
      showLabel ? `${eventType} #${traceID}` : `#${traceID}`
    }}</text>
  </g>
</template>

<style scoped>
.runtime-trace-particle {
  pointer-events: all;
  filter: drop-shadow(0 2px 4px rgb(15 23 42 / 18%));
}
.runtime-trace-particle.branch {
  filter: drop-shadow(0 1px 3px rgb(15 23 42 / 16%));
}
.runtime-trace-particle rect {
  fill: var(--ui-info-bg);
  stroke: var(--ui-primary);
  stroke-width: 1;
}
.runtime-trace-particle circle {
  fill: var(--ui-primary);
}
.runtime-trace-particle text {
  fill: var(--ui-info-text);
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0;
}
.runtime-trace-particle.failed rect {
  fill: var(--ui-danger-bg);
  stroke: var(--ui-danger-text);
}
.runtime-trace-particle.failed circle {
  fill: var(--ui-danger-text);
}
.runtime-trace-particle.failed text {
  fill: var(--ui-danger-text);
}
</style>
