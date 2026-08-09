<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import dagre from '@dagrejs/dagre'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import {
  MarkerType,
  Position,
  VueFlow,
  type Edge,
  type EdgeMouseEvent,
  type Node,
  type NodeMouseEvent,
  type VueFlowStore,
} from '@vue-flow/core'
import { MiniMap } from '@vue-flow/minimap'
import {
  ApartmentOutlined,
  ArrowRightOutlined,
  ClockCircleOutlined,
  LoadingOutlined,
  PauseOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
} from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import RuntimeTraceEdge from '../components/RuntimeTraceEdge.vue'
import StatusLight from '../components/StatusLight.vue'
import { api } from '../services/api'
import { formatBytes } from '../utils/format'
import type {
  RuntimeDiagnostics,
  RuntimeMessageTrace,
  RuntimeTopologyEdge,
  RuntimeTopologyNode,
} from '../types'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import '@vue-flow/minimap/dist/style.css'

const { t, te } = useI18n()
interface RuntimeEventStream {
  readyState: number
  addEventListener(type: string, listener: (event: { data?: string }) => void): void
  close(): void
  onerror: (() => void) | null
}
interface RuntimeEventStreamConstructor {
  new (url: string): RuntimeEventStream
  OPEN: number
  CLOSED: number
}
const eventStreamAPI = Reflect.get(window, 'EventSource') as RuntimeEventStreamConstructor
const diagnostics = ref<RuntimeDiagnostics | null>(null)
const traces = ref<RuntimeMessageTrace[]>([])
const loading = ref(false)
const error = ref('')
const streamState = ref<'connecting' | 'live' | 'offline'>('connecting')
const paused = ref(false)
const eventType = ref('all')
const traceStatus = ref('all')
const selectedTraceID = ref<number | null>(null)
const selectedNodeID = ref<string | null>(null)
const selectedEdgeID = ref<string | null>(null)
const drawerOpen = ref(false)
const animationClock = ref(Date.now())
const replayVersion = ref(0)
let flowInstance: VueFlowStore | null = null
let refreshTimer: number | undefined
let animationTimer: number | undefined
let streamErrorTimer: number | undefined
let stream: RuntimeEventStream | undefined

const dropped = computed(() => diagnostics.value?.event_bus.cumulative_drops || 0)
const runningWorkers = computed(
  () => diagnostics.value?.workers.filter((worker) => worker.state === 'running').length || 0,
)
const eventSources = computed(() => diagnostics.value?.workers.filter((worker) => worker.event_source) || [])
const selectedTrace = computed(() => traces.value.find((trace) => trace.id === selectedTraceID.value) || null)
const selectedNode = computed(
  () => diagnostics.value?.topology.nodes.find((node) => node.id === selectedNodeID.value) || null,
)
const selectedEdge = computed(
  () => diagnostics.value?.topology.edges.find((edge) => edge.id === selectedEdgeID.value) || null,
)
const eventTypeOptions = computed(() => [
  { label: t('runtime.allEvents'), value: 'all' },
  ...Array.from(new Set(traces.value.map((trace) => trace.type)))
    .sort()
    .map((value) => ({ label: value, value })),
])
const filteredTraces = computed(() =>
  [...traces.value]
    .reverse()
    .filter((trace) => eventType.value === 'all' || trace.type === eventType.value)
    .filter((trace) => traceStatus.value === 'all' || trace.status === traceStatus.value),
)
const animatedTraces = computed(() => {
  const visible = traces.value.filter(
    (trace) =>
      (eventType.value === 'all' || trace.type === eventType.value) &&
      (traceStatus.value === 'all' || trace.status === traceStatus.value),
  )
  const recent = visible.filter((trace) => animationClock.value - new Date(trace.updated_at).getTime() < 8000)
  if (selectedTrace.value && !recent.some((trace) => trace.id === selectedTrace.value?.id)) {
    recent.push(selectedTrace.value)
  }
  return recent.slice(-12)
})
const selectedPathNodes = computed(() => new Set(selectedTrace.value?.hops.map((hop) => hop.node_id) || []))
const selectedPathEdges = computed(() => {
  const result = new Set<string>()
  const hops = selectedTrace.value?.hops || []

  for (const hop of hops) {
    if (hop.from_node_id) result.add(`${hop.from_node_id}--${hop.node_id}`)
  }
  return result
})

function translatedNode(node: RuntimeTopologyNode) {
  for (const scope of ['workerNames', 'channelNames', 'nodeNames']) {
    const key = `runtime.${scope}.${node.id}`
    if (te(key)) return t(key)
  }
  return node.name
}

function translatedWorker(worker: { id: string; name: string }) {
  const key = `runtime.workerNames.${worker.id}`
  return te(key) ? t(key) : worker.name
}

function translatedWorkerDetail(worker: { id: string; detail: string }) {
  const key = `runtime.workerDetails.${worker.id}`
  return te(key) ? t(key) : worker.detail
}

function statusTone(state: string) {
  if (state === 'running' || state === 'success') return 'success'
  if (state === 'failed' || state === 'dropped') return 'danger'
  if (state === 'idle' || state === 'attempt' || state === 'recovering') return 'info'
  return 'neutral'
}

function statusLabel(state: string) {
  const key = `runtime.${state}`
  return te(key) ? t(key) : state
}

function localTime(value?: string) {
  return value ? new Date(value).toLocaleTimeString([], { hour12: false }) : '—'
}

function duration(seconds: number) {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return [h ? `${h}h` : '', `${m}m`].filter(Boolean).join(' ')
}

function traceLatency(trace: RuntimeMessageTrace) {
  return `${Math.max(0, new Date(trace.updated_at).getTime() - new Date(trace.started_at).getTime())}ms`
}

function traceStateLabel(value: unknown) {
  const state = String(value || '')
  const deviceKey = `status.deviceStates.${state}`
  if (te(deviceKey)) return t(deviceKey)
  const operationKey = `operation.states.${state}`
  if (te(operationKey)) return t(operationKey)
  return statusLabel(state)
}

function traceStateColor(value: unknown) {
  const state = String(value || '')
  if (state === 'ready' || state === 'succeeded') return 'green'
  if (state === 'absent' || state === 'disconnected' || state === 'failed') return 'red'
  if (state === 'degraded' || state === 'cancelled') return 'orange'
  return 'blue'
}

function traceStateChange(trace: RuntimeMessageTrace) {
  const previous = trace.fields?.previous_state
  const current = trace.fields?.state
  return previous !== undefined && current !== undefined ? { previous, current } : null
}

function traceFieldLabel(key: string) {
  const translationKey = `runtime.traceFields.${key}`
  return te(translationKey) ? t(translationKey) : key.replaceAll('_', ' ')
}

function traceFieldValue(key: string, value: unknown) {
  if (key === 'state') return traceStateLabel(value)
  if (key === 'update' && value === 'backend_metadata') return t('runtime.traceValues.backendMetadata')
  if (typeof value === 'boolean') return t(`runtime.boolean.${value}`)
  if (typeof value === 'number' && key.endsWith('_bytes')) return formatBytes(value)
  if (typeof value === 'number' && key === 'progress') return `${value}%`
  if (typeof value === 'number' && key === 'signal_dbm') return `${value} dBm`
  if (typeof value === 'string' && key.endsWith('_at')) {
    const date = new Date(value)
    if (!Number.isNaN(date.getTime())) return date.toLocaleString([], { hour12: false })
  }
  if (value && typeof value === 'object') return JSON.stringify(value)
  return String(value ?? '—')
}

function traceFieldEntries(trace: RuntimeMessageTrace) {
  const hasChange = !!traceStateChange(trace)
  const priority = [
    'update',
    'derived_from',
    'backend_event',
    'state',
    'operation',
    'type',
    'progress',
    'index',
    'count',
  ]
  return Object.entries(trace.fields || {})
    .filter(([key]) => key !== 'previous_state' && (!hasChange || key !== 'state'))
    .sort(([left], [right]) => {
      const leftRank = priority.indexOf(left)
      const rightRank = priority.indexOf(right)
      return (leftRank < 0 ? priority.length : leftRank) - (rightRank < 0 ? priority.length : rightRank)
    })
}

function traceSummary(trace: RuntimeMessageTrace) {
  const change = traceStateChange(trace)
  if (change) return `${traceStateLabel(change.previous)} → ${traceStateLabel(change.current)}`
  if (trace.type === 'network.updated') {
    const fields = trace.fields || {}
    const summaryKeys = ['network_mode', 'mode', 'signal_dbm', 'registered'].filter(
      (key) => fields[key] !== undefined,
    )
    if (summaryKeys.length) {
      return summaryKeys
        .map((key) => `${traceFieldLabel(key)}: ${traceFieldValue(key, fields[key])}`)
        .join(' · ')
    }
  }
  const fields = traceFieldEntries(trace)
  if (!fields.length) return t('runtime.noContent')
  return fields
    .slice(0, 2)
    .map(([key, value]) => `${traceFieldLabel(key)}: ${traceFieldValue(key, value)}`)
    .join(' · ')
}

function hopDelta(trace: RuntimeMessageTrace, index: number) {
  if (index === 0) return '+0ms'
  const previous = new Date(trace.hops[index - 1].at).getTime()
  return `+${Math.max(0, new Date(trace.hops[index].at).getTime() - previous)}ms`
}

function upsertTrace(trace: RuntimeMessageTrace) {
  const index = traces.value.findIndex((item) => item.id === trace.id)
  if (index >= 0) traces.value.splice(index, 1, trace)
  else traces.value.push(trace)
  if (traces.value.length > 200) traces.value.splice(0, traces.value.length - 200)
}

function layoutNodes(topologyNodes: RuntimeTopologyNode[], topologyEdges: RuntimeTopologyEdge[]) {
  const graph = new dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}))
  graph.setGraph({ rankdir: 'LR', ranksep: 92, nodesep: 32, marginx: 32, marginy: 72 })
  topologyNodes.forEach((node) => graph.setNode(node.id, { width: 184, height: 72 }))
  topologyEdges.forEach((edge) => graph.setEdge(edge.source, edge.target))
  dagre.layout(graph)

  const processorOrder = ['vowifi-runtime', 'browser-websocket', 'notification-policy']
  const processorSlots = processorOrder
    .map((id) => graph.node(id))
    .filter(Boolean)
    .map((point) => point.y)
    .sort((left, right) => left - right)
  processorOrder.forEach((id, index) => {
    const point = graph.node(id)
    if (point && processorSlots[index] !== undefined) point.y = processorSlots[index]
  })
  const notificationPolicy = graph.node('notification-policy')
  const notificationQueue = graph.node('notification-queue')
  if (notificationPolicy && notificationQueue) notificationQueue.y = notificationPolicy.y

  const nodes = topologyNodes.map((node): Node => {
    const point = graph.node(node.id)
    const selected = selectedPathNodes.value.has(node.id)
    return {
      id: node.id,
      position: { x: point.x - 92, y: point.y - 36 },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
      data: { ...node, label: translatedNode(node), selected },
      class: [
        `runtime-node-${node.kind}`,
        `runtime-node-${node.state}`,
        selectedTrace.value && !selected ? 'runtime-node-muted' : '',
        selected ? 'runtime-node-selected' : '',
      ].filter(Boolean),
    }
  })

  const laneKeys = ['source', 'channel', 'processor', 'queue', 'destination']
  const rankCenters = Array.from(
    new Set(topologyNodes.map((node) => Math.round(graph.node(node.id).x))),
  ).sort((left, right) => left - right)
  const laneNodes = rankCenters.map((x, index): Node => ({
    id: `runtime-lane-${index}`,
    type: 'lane',
    position: { x: x - 92, y: 20 },
    selectable: false,
    focusable: false,
    connectable: false,
    draggable: false,
    data: { label: t(`runtime.lanes.${laneKeys[index] || 'processor'}`) },
  }))

  return [...nodes, ...laneNodes]
}

const graphNodes = computed(() => {
  const topology = diagnostics.value?.topology
  return topology ? layoutNodes(topology.nodes, topology.edges) : []
})
const topologySignature = computed(() => {
  const topology = diagnostics.value?.topology
  if (!topology) return ''
  return [
    ...topology.nodes.map((node) => node.id),
    ...topology.edges.map((edge) => `${edge.source}>${edge.target}`),
  ].join('|')
})

const graphEdges = computed((): Edge[] => {
  const edges = diagnostics.value?.topology.edges || []
  const latest = traces.value.at(-1)
  const liveNodes = new Set(!paused.value && latest ? latest.hops.map((hop) => hop.node_id) : [])
  const baseEdges: Edge[] = edges.map((edge) => {
    const selected = selectedPathEdges.value.has(edge.id)
    const live = liveNodes.has(edge.source) && liveNodes.has(edge.target)
    const muted = !!selectedTrace.value && !selected
    return {
      ...edge,
      type: 'smoothstep',
      animated: selected || live,
      markerEnd: { type: MarkerType.ArrowClosed, width: 17, height: 17 },
      label: edge.event_types?.length === 1 ? edge.event_types[0] : undefined,
      class: [
        selected ? 'runtime-edge-selected' : '',
        live ? 'runtime-edge-live' : '',
        muted ? 'runtime-edge-muted' : '',
      ]
        .filter(Boolean)
        .join(' '),
    }
  })
  const topologyEdgeIDs = new Set(edges.map((edge) => edge.id))
  const traceEdges: Edge[] = []
  for (const trace of animatedTraces.value) {
    const seenEdges = new Set<string>()
    const nodeDepth = new Map<string, number>()
    if (trace.hops[0]) nodeDepth.set(trace.hops[0].node_id, 0)
    trace.hops.forEach((hop, index) => {
      if (!hop.from_node_id) return
      const edgeID = `${hop.from_node_id}--${hop.node_id}`
      if (!topologyEdgeIDs.has(edgeID) || seenEdges.has(edgeID)) return
      seenEdges.add(edgeID)
      const depth = (nodeDepth.get(hop.from_node_id) || 0) + 1
      nodeDepth.set(hop.node_id, Math.max(nodeDepth.get(hop.node_id) || 0, depth))
      const replay = selectedTraceID.value === trace.id ? replayVersion.value : 0
      traceEdges.push({
        id: `trace-${trace.id}-${replay}-${index}-${edgeID}`,
        source: hop.from_node_id,
        target: hop.node_id,
        type: 'trace',
        selectable: true,
        focusable: true,
        zIndex: 20,
        data: {
          traceID: trace.id,
          eventType: trace.type,
          status: hop.state,
          showLabel: hop.node_id === 'domain-events',
          delay: Math.max(0, depth - 1) * 0.86,
          duration: 0.76,
        },
      })
    })
  }
  return [...baseEdges, ...traceEdges]
})

async function refresh() {
  if (loading.value) return
  loading.value = true
  try {
    const next = await api.runtimeDiagnostics()
    diagnostics.value = next
    if (!paused.value) traces.value = next.traces || []
    error.value = ''
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : t('runtime.unavailable')
  } finally {
    loading.value = false
  }
}

function connectStream() {
  stream?.close()
  if (streamErrorTimer) window.clearTimeout(streamErrorTimer)
  streamState.value = 'connecting'
  stream = new eventStreamAPI('/api/v1/runtime/traces/stream')
  stream.addEventListener('ready', () => {
    if (streamErrorTimer) window.clearTimeout(streamErrorTimer)
    streamState.value = 'live'
  })
  stream.addEventListener('trace', (message) => {
    streamState.value = 'live'
    if (paused.value) return
    try {
      if (message.data) upsertTrace(JSON.parse(message.data) as RuntimeMessageTrace)
    } catch {
      // A malformed diagnostic update is ignored; periodic refresh repairs it.
    }
  })
  stream.onerror = () => {
    if (streamErrorTimer) window.clearTimeout(streamErrorTimer)
    streamErrorTimer = window.setTimeout(() => {
      if (stream?.readyState !== eventStreamAPI.OPEN) streamState.value = 'offline'
    }, 750)
  }
}

function togglePause() {
  paused.value = !paused.value
  if (!paused.value) void refresh()
}

function onNodeClick(event: NodeMouseEvent) {
  selectedTraceID.value = null
  selectedEdgeID.value = null
  selectedNodeID.value = event.node.id
  drawerOpen.value = true
}

function onEdgeClick(event: EdgeMouseEvent) {
  const traceID = Number(event.edge.data?.traceID || 0)
  if (traceID) {
    const trace = traces.value.find((item) => item.id === traceID)
    if (trace) selectTrace(trace)
    return
  }
  selectedTraceID.value = null
  selectedNodeID.value = null
  selectedEdgeID.value = event.edge.id
  drawerOpen.value = true
}

function selectTrace(trace: RuntimeMessageTrace) {
  selectedNodeID.value = null
  selectedEdgeID.value = null
  selectedTraceID.value = trace.id
  replayVersion.value += 1
  drawerOpen.value = true
}

function clearSelection() {
  selectedTraceID.value = null
  selectedNodeID.value = null
  selectedEdgeID.value = null
}

function onPaneReady(instance: VueFlowStore) {
  flowInstance = instance
}

function topologyNodeName(id: string) {
  return diagnostics.value?.topology.nodes.find((node) => node.id === id)?.name || id
}

function nodeDetail(node: RuntimeTopologyNode) {
  const recovery = diagnostics.value?.channel_recovery?.find((item) => item.channel === node.id)
  if (!recovery) return node.detail || t('runtime.noDetail')
  if (recovery.retryable) {
    return t('runtime.channelRetrying', {
      attempts: recovery.attempts,
      time: localTime(recovery.next_retry),
    })
  }
  return t('runtime.channelFailed', { error: recovery.last_error })
}

function sourceInterval(interval?: number) {
  if (!interval) return t('runtime.onEvent')
  if (interval % 1000 === 0) return t('runtime.every', { value: `${interval / 1000}s` })
  return t('runtime.every', { value: `${interval}ms` })
}

function tracesForNode(id: string) {
  return traces.value.filter((trace) => trace.hops.some((hop) => hop.node_id === id))
}

function tracesForEdge(id: string) {
  return traces.value.filter((trace) =>
    trace.hops.some((hop) => `${hop.from_node_id}--${hop.node_id}` === id),
  )
}

function handleVisibility() {
  if (document.visibilityState === 'visible') {
    if (!stream || stream.readyState === eventStreamAPI.CLOSED) connectStream()
    void refresh()
  }
}

watch(topologySignature, async () => {
  await nextTick()
  void flowInstance?.fitView({ padding: 0.12, duration: 250 })
})

onMounted(() => {
  void refresh()
  connectStream()
  refreshTimer = window.setInterval(() => {
    if (document.visibilityState === 'visible') void refresh()
  }, 10000)
  animationTimer = window.setInterval(() => {
    animationClock.value = Date.now()
  }, 500)
  document.addEventListener('visibilitychange', handleVisibility)
})

onBeforeUnmount(() => {
  if (refreshTimer) window.clearInterval(refreshTimer)
  if (animationTimer) window.clearInterval(animationTimer)
  if (streamErrorTimer) window.clearTimeout(streamErrorTimer)
  stream?.close()
  document.removeEventListener('visibilitychange', handleVisibility)
})
</script>

<template>
  <section class="runtime-view">
    <a-alert v-if="error" type="error" show-icon :message="error" />

    <header class="runtime-header">
      <div>
        <span class="eyebrow">{{ t('runtime.eyebrow') }}</span>
        <h2>{{ t('runtime.title') }}</h2>
        <p v-if="diagnostics">
          {{ t('runtime.updated', { time: localTime(diagnostics.generated_at) }) }} ·
          {{ t('runtime.uptime') }} {{ duration(diagnostics.uptime_seconds) }}
        </p>
      </div>
      <div class="runtime-header-actions">
        <StatusLight
          :tone="streamState === 'live' ? 'success' : streamState === 'connecting' ? 'info' : 'warning'"
          :label="t(`runtime.stream.${streamState}`)"
          :pulse="streamState === 'live' && !paused"
        />
        <a-button :title="paused ? t('runtime.resume') : t('runtime.pause')" @click="togglePause">
          <PlayCircleOutlined v-if="paused" /><PauseOutlined v-else />
        </a-button>
        <a-button :disabled="loading" :title="t('common.refresh')" @click="refresh">
          <LoadingOutlined v-if="loading" spin /><ReloadOutlined v-else />
        </a-button>
      </div>
    </header>

    <div v-if="diagnostics" class="runtime-metrics">
      <article>
        <span>{{ t('runtime.workers') }}</span
        ><strong>{{ runningWorkers }}/{{ diagnostics.workers.length }}</strong>
      </article>
      <article>
        <span>{{ t('runtime.goroutines') }}</span
        ><strong>{{ diagnostics.goroutines }}</strong>
      </article>
      <article>
        <span>{{ t('runtime.published') }}</span
        ><strong>{{ diagnostics.event_bus.published }}</strong>
      </article>
      <article>
        <span>{{ t('runtime.dropped') }}</span
        ><strong :class="{ danger: dropped > 0 }">{{ dropped }}</strong>
      </article>
    </div>

    <section v-if="diagnostics" class="runtime-sources-section">
      <div class="runtime-section-title">
        <div>
          <ClockCircleOutlined /><strong>{{ t('runtime.eventSources') }}</strong>
        </div>
        <span>{{ eventSources.length }}</span>
      </div>
      <div v-if="eventSources.length" class="runtime-source-list">
        <article v-for="source in eventSources" :key="source.id" class="runtime-source">
          <div class="runtime-source-head">
            <StatusLight :tone="statusTone(source.state)" :pulse="source.state === 'running'" />
            <strong>{{ translatedWorker(source) }}</strong>
            <span>{{ statusLabel(source.state) }}</span>
          </div>
          <small>{{ sourceInterval(source.interval_ms) }} · {{ translatedWorkerDetail(source) }}</small>
          <a-space v-if="source.event_types?.length" :size="[4, 4]" wrap>
            <a-tag v-for="eventName in source.event_types" :key="eventName" :bordered="false">
              {{ eventName }}
            </a-tag>
          </a-space>
        </article>
      </div>
      <EmptyState v-else :title="t('runtime.noEventSources')" />
    </section>

    <section v-if="diagnostics" class="runtime-canvas-section">
      <div class="runtime-toolbar">
        <div>
          <ApartmentOutlined />
          <strong>{{ t('runtime.messageTopology') }}</strong>
          <span>{{ diagnostics.topology.nodes.length }} {{ t('runtime.nodes') }}</span>
        </div>
        <div class="runtime-filters">
          <a-select v-model:value="eventType" :options="eventTypeOptions" />
          <a-select
            v-model:value="traceStatus"
            :options="[
              { label: t('runtime.allStates'), value: 'all' },
              { label: t('runtime.success'), value: 'success' },
              { label: t('runtime.failed'), value: 'failed' },
              { label: t('runtime.dropped'), value: 'dropped' },
            ]"
          />
          <a-button v-if="selectedTrace" @click="clearSelection">{{ t('runtime.showAll') }}</a-button>
        </div>
      </div>

      <div class="runtime-canvas">
        <VueFlow
          :nodes="graphNodes"
          :edges="graphEdges"
          :nodes-draggable="false"
          :nodes-connectable="false"
          :min-zoom="0.25"
          :max-zoom="1.8"
          fit-view-on-init
          @pane-ready="onPaneReady"
          @node-click="onNodeClick"
          @edge-click="onEdgeClick"
        >
          <Background :gap="20" :size="1" pattern-color="var(--ui-border)" />
          <MiniMap pannable zoomable />
          <Controls :show-interactive="false" />
          <template #edge-trace="edgeProps">
            <RuntimeTraceEdge v-bind="edgeProps" />
          </template>
          <template #node-default="{ data }">
            <div class="runtime-graph-node">
              <div class="runtime-node-head">
                <StatusLight :tone="statusTone(data.state)" :pulse="data.state === 'running'" />
                <span>{{ t(`runtime.nodeKinds.${data.kind}`) }}</span>
              </div>
              <strong>{{ data.label }}</strong>
              <small>{{ statusLabel(data.state) }}</small>
            </div>
          </template>
          <template #node-lane="{ data }">
            <div class="runtime-lane-label">{{ data.label }}</div>
          </template>
        </VueFlow>
      </div>
    </section>

    <section v-if="diagnostics" class="runtime-traces-section">
      <div class="runtime-section-title">
        <div>
          <ClockCircleOutlined /><strong>{{ t('runtime.messageTraces') }}</strong>
        </div>
        <span>{{ filteredTraces.length }}/{{ traces.length }}</span>
      </div>
      <div v-if="filteredTraces.length" class="runtime-trace-list">
        <button
          v-for="trace in filteredTraces.slice(0, 80)"
          :key="trace.id"
          type="button"
          :class="{ selected: trace.id === selectedTraceID }"
          @click="selectTrace(trace)"
        >
          <StatusLight :tone="statusTone(trace.status)" />
          <time>{{ localTime(trace.started_at) }}</time>
          <code>{{ trace.type }}</code>
          <span>#{{ trace.id }}</span>
          <span>{{ trace.hops.length }} {{ t('runtime.hops') }}</span>
          <span>{{ traceLatency(trace) }}</span>
          <span class="runtime-trace-summary">{{ traceSummary(trace) }}</span>
          <ArrowRightOutlined />
        </button>
      </div>
      <EmptyState v-else :title="t('runtime.noEvents')" />
    </section>

    <a-drawer v-model:open="drawerOpen" :width="460" :title="t('runtime.details')" destroy-on-close>
      <div v-if="selectedTrace" class="runtime-detail">
        <div class="runtime-detail-heading">
          <StatusLight :tone="statusTone(selectedTrace.status)" />
          <div>
            <code>{{ selectedTrace.type }}</code
            ><span>#{{ selectedTrace.id }}</span>
          </div>
          <a-tag :color="selectedTrace.status === 'success' ? 'green' : 'red'">{{
            statusLabel(selectedTrace.status)
          }}</a-tag>
        </div>
        <a-descriptions
          v-if="traceStateChange(selectedTrace) || traceFieldEntries(selectedTrace).length"
          :title="t('runtime.eventContent')"
          :column="1"
          size="small"
          bordered
        >
          <a-descriptions-item v-if="traceStateChange(selectedTrace)" :label="t('runtime.stateChange')">
            <a-space :size="6">
              <a-tag :color="traceStateColor(traceStateChange(selectedTrace)?.previous)">
                {{ traceStateLabel(traceStateChange(selectedTrace)?.previous) }}
              </a-tag>
              <ArrowRightOutlined />
              <a-tag :color="traceStateColor(traceStateChange(selectedTrace)?.current)">
                {{ traceStateLabel(traceStateChange(selectedTrace)?.current) }}
              </a-tag>
            </a-space>
          </a-descriptions-item>
          <a-descriptions-item
            v-for="[key, value] in traceFieldEntries(selectedTrace)"
            :key="key"
            :label="traceFieldLabel(key)"
          >
            {{ traceFieldValue(key, value) }}
          </a-descriptions-item>
        </a-descriptions>
        <dl class="runtime-detail-metrics">
          <div>
            <dt>{{ t('runtime.started') }}</dt>
            <dd>{{ localTime(selectedTrace.started_at) }}</dd>
          </div>
          <div>
            <dt>{{ t('runtime.latency') }}</dt>
            <dd>{{ traceLatency(selectedTrace) }}</dd>
          </div>
          <div>
            <dt>{{ t('runtime.hops') }}</dt>
            <dd>{{ selectedTrace.hops.length }}</dd>
          </div>
        </dl>
        <div class="runtime-hop-list">
          <article v-for="(hop, index) in selectedTrace.hops" :key="`${hop.node_id}-${index}-${hop.at}`">
            <div class="runtime-hop-rail"><StatusLight :tone="statusTone(hop.state)" /><i /></div>
            <div>
              <strong>{{ topologyNodeName(hop.node_id) }}</strong>
              <span>{{ hop.action }} · {{ statusLabel(hop.state) }}</span>
              <small v-if="hop.detail">{{ hop.detail }}</small>
            </div>
            <time>{{ hopDelta(selectedTrace, index) }}</time>
          </article>
        </div>
      </div>

      <div v-else-if="selectedNode" class="runtime-detail">
        <div class="runtime-detail-heading">
          <StatusLight :tone="statusTone(selectedNode.state)" />
          <div>
            <strong>{{ translatedNode(selectedNode) }}</strong
            ><span>{{ t(`runtime.nodeKinds.${selectedNode.kind}`) }}</span>
          </div>
        </div>
        <p>{{ nodeDetail(selectedNode) }}</p>
        <dl class="runtime-detail-metrics">
          <div>
            <dt>{{ t('runtime.state') }}</dt>
            <dd>{{ statusLabel(selectedNode.state) }}</dd>
          </div>
          <div>
            <dt>{{ t('runtime.recent') }}</dt>
            <dd>{{ tracesForNode(selectedNode.id).length }}</dd>
          </div>
        </dl>
        <button
          v-for="trace in tracesForNode(selectedNode.id).slice(-12).reverse()"
          :key="trace.id"
          type="button"
          class="runtime-related-trace"
          @click="selectTrace(trace)"
        >
          <code>{{ trace.type }}</code
          ><span>#{{ trace.id }}</span
          ><ArrowRightOutlined />
        </button>
      </div>

      <div v-else-if="selectedEdge" class="runtime-detail">
        <div class="runtime-edge-heading">
          <strong>{{ selectedEdge.source }}</strong
          ><ArrowRightOutlined /><strong>{{ selectedEdge.target }}</strong>
        </div>
        <a-space v-if="selectedEdge.event_types?.length" :size="[4, 4]" wrap>
          <a-tag v-for="type in selectedEdge.event_types" :key="type" color="blue">
            {{ type }}
          </a-tag>
        </a-space>
        <dl class="runtime-detail-metrics">
          <div>
            <dt>{{ t('runtime.recent') }}</dt>
            <dd>{{ tracesForEdge(selectedEdge.id).length }}</dd>
          </div>
        </dl>
      </div>
    </a-drawer>
  </section>
</template>

<style scoped>
.runtime-view {
  display: grid;
  gap: 16px;
}
.runtime-sources-section {
  display: grid;
  gap: 12px;
}
.runtime-source-list {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 10px;
}
.runtime-source {
  display: grid;
  gap: 8px;
  padding: 12px 14px;
  border: 1px solid var(--ui-border);
  background: var(--ui-surface);
}
.runtime-source-head {
  display: flex;
  align-items: center;
  gap: 8px;
}
.runtime-source-head span {
  margin-left: auto;
  color: var(--ui-text-secondary);
  font-size: 12px;
}
.runtime-source > small {
  color: var(--ui-text-secondary);
  line-height: 1.45;
}
.runtime-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 8px 4px;
}
.runtime-header h2 {
  margin: 4px 0 2px;
  font-size: 24px;
  letter-spacing: 0;
}
.runtime-header p {
  margin: 0;
  color: var(--ui-text-secondary);
  font-size: 12px;
}
.runtime-header-actions,
.runtime-filters,
.runtime-section-title,
.runtime-section-title > div {
  display: flex;
  align-items: center;
  gap: 10px;
}
.runtime-header-actions {
  min-width: 190px;
  justify-content: flex-end;
}
.runtime-header-actions :deep(.status-indicator) {
  width: 94px;
  justify-content: flex-start;
}
.runtime-header-actions :deep(.ant-btn) {
  display: inline-grid;
  width: 32px;
  min-width: 32px;
  height: 32px;
  padding: 0;
  place-items: center;
}
.runtime-header-actions :deep(.ant-btn > .anticon) {
  width: 14px;
  height: 14px;
  margin: 0;
  line-height: 14px;
}
.runtime-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  border: 1px solid var(--ui-border);
  background: var(--ui-surface);
}
.runtime-metrics article {
  display: grid;
  gap: 4px;
  padding: 14px 18px;
  border-right: 1px solid var(--ui-border);
}
.runtime-metrics article:last-child {
  border-right: 0;
}
.runtime-metrics span {
  color: var(--ui-text-secondary);
  font-size: 11px;
}
.runtime-metrics strong {
  font-size: 21px;
  font-variant-numeric: tabular-nums;
}
.runtime-metrics .danger {
  color: var(--ui-danger);
}
.runtime-canvas-section,
.runtime-traces-section {
  overflow: hidden;
  border: 1px solid var(--ui-border);
  background: var(--ui-surface);
}
.runtime-toolbar,
.runtime-section-title {
  min-height: 54px;
  padding: 9px 14px;
  border-bottom: 1px solid var(--ui-border);
}
.runtime-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.runtime-toolbar > div:first-child {
  display: flex;
  align-items: center;
  gap: 9px;
}
.runtime-toolbar > div:first-child span,
.runtime-section-title > span {
  color: var(--ui-text-tertiary);
  font-size: 11px;
}
.runtime-filters :deep(.ant-select) {
  min-width: 150px;
}
.runtime-canvas {
  position: relative;
  height: clamp(500px, 64vh, 720px);
}
.runtime-canvas :deep(.vue-flow__pane) {
  cursor: grab;
}
.runtime-canvas :deep(.vue-flow__node) {
  width: 184px;
  border: 0;
  border-radius: 6px;
  padding: 0;
  background: transparent;
  box-shadow: none;
  text-align: left;
}
.runtime-graph-node {
  display: grid;
  min-height: 72px;
  gap: 3px;
  padding: 10px 12px;
  border: 1px solid var(--ui-border);
  border-left: 4px solid #64748b;
  border-radius: 6px;
  background: var(--ui-surface);
  box-shadow: 0 3px 12px rgb(15 23 42 / 8%);
  transition:
    opacity 0.18s,
    box-shadow 0.18s,
    border-color 0.18s;
}
.runtime-node-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: var(--ui-text-tertiary);
  font-size: 9px;
  text-transform: uppercase;
}
.runtime-graph-node strong {
  overflow: hidden;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.runtime-graph-node small {
  color: var(--ui-text-secondary);
  font-size: 10px;
}
.runtime-canvas :deep(.runtime-node-channel .runtime-graph-node) {
  border-left-color: #1677ff;
}
.runtime-canvas :deep(.runtime-node-processor .runtime-graph-node) {
  border-left-color: #d97706;
}
.runtime-canvas :deep(.runtime-node-destination .runtime-graph-node) {
  border-left-color: #15803d;
}
.runtime-canvas :deep(.runtime-node-stopped .runtime-graph-node) {
  opacity: 0.58;
}
.runtime-canvas :deep(.runtime-node-muted .runtime-graph-node) {
  opacity: 0.18;
}
.runtime-canvas :deep(.runtime-node-selected .runtime-graph-node) {
  border-color: #1677ff;
  box-shadow:
    0 0 0 3px rgb(22 119 255 / 14%),
    0 6px 18px rgb(15 23 42 / 12%);
  opacity: 1;
}
.runtime-canvas :deep(.vue-flow__edge-path) {
  stroke: #94a3b8;
  stroke-width: 1.4;
  transition:
    opacity 0.18s,
    stroke 0.18s,
    stroke-width 0.18s;
}
.runtime-canvas :deep(.runtime-edge-live .vue-flow__edge-path),
.runtime-canvas :deep(.runtime-edge-selected .vue-flow__edge-path) {
  stroke: #1677ff;
  stroke-width: 2.5;
}
.runtime-canvas :deep(.runtime-edge-muted) {
  opacity: 0.1;
}
.runtime-canvas :deep(.vue-flow__edge-text) {
  font-size: 8px;
  fill: var(--ui-text-secondary);
}
.runtime-canvas :deep(.vue-flow__edge-textbg) {
  fill: var(--ui-surface);
}
.runtime-canvas :deep(.vue-flow__minimap) {
  width: 150px;
  height: 90px;
  border: 1px solid var(--ui-border);
  border-radius: 4px;
  background: var(--ui-surface);
}
.runtime-canvas :deep(.vue-flow__node-lane) {
  pointer-events: none;
}
.runtime-lane-label {
  width: 184px;
  color: var(--ui-text-tertiary);
  font-size: 10px;
  font-weight: 600;
  text-align: center;
  text-transform: uppercase;
}
.runtime-trace-list {
  max-height: 360px;
  overflow-y: auto;
}
.runtime-trace-list button {
  display: grid;
  grid-template-columns: 18px 84px minmax(180px, 1fr) 64px 76px 68px minmax(160px, 0.9fr) 18px;
  gap: 10px;
  align-items: center;
  width: 100%;
  min-height: 43px;
  padding: 7px 14px;
  border: 0;
  border-bottom: 1px solid var(--ui-border);
  background: transparent;
  color: var(--ui-text-secondary);
  font-size: 11px;
  text-align: left;
  cursor: pointer;
}
.runtime-trace-list button:hover,
.runtime-trace-list button.selected {
  background: var(--ui-surface-muted);
  color: var(--ui-text);
}
.runtime-trace-list time,
.runtime-trace-list span {
  font-variant-numeric: tabular-nums;
}
.runtime-trace-summary {
  overflow: hidden;
  color: var(--ui-text);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.runtime-trace-list code,
.runtime-detail code,
.runtime-related-trace code {
  color: var(--ui-primary-active);
  font-size: 11px;
}
.runtime-detail {
  display: grid;
  gap: 18px;
}
.runtime-detail > p {
  margin: 0;
  color: var(--ui-text-secondary);
  line-height: 1.6;
}
.runtime-detail-heading {
  display: grid;
  grid-template-columns: 18px minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
}
.runtime-detail-heading > div {
  display: grid;
  gap: 3px;
}
.runtime-detail-heading span {
  color: var(--ui-text-secondary);
  font-size: 11px;
}
.runtime-detail-metrics {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  margin: 0;
  border: 1px solid var(--ui-border);
}
.runtime-detail-metrics div {
  display: grid;
  gap: 3px;
  padding: 10px;
  border-right: 1px solid var(--ui-border);
}
.runtime-detail-metrics div:last-child {
  border-right: 0;
}
.runtime-detail-metrics dt {
  color: var(--ui-text-tertiary);
  font-size: 10px;
}
.runtime-detail-metrics dd {
  margin: 0;
  font-size: 12px;
  font-weight: 600;
}
.runtime-hop-list article {
  display: grid;
  grid-template-columns: 18px minmax(0, 1fr) 50px;
  gap: 10px;
  min-height: 60px;
}
.runtime-hop-rail {
  display: grid;
  grid-template-rows: 18px 1fr;
  justify-items: center;
}
.runtime-hop-rail i {
  width: 1px;
  height: 100%;
  background: var(--ui-border);
}
.runtime-hop-list article:last-child .runtime-hop-rail i {
  display: none;
}
.runtime-hop-list article > div:nth-child(2) {
  display: grid;
  align-content: start;
  gap: 2px;
}
.runtime-hop-list strong {
  font-size: 12px;
}
.runtime-hop-list span,
.runtime-hop-list small,
.runtime-hop-list time {
  color: var(--ui-text-secondary);
  font-size: 10px;
}
.runtime-hop-list time {
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.runtime-related-trace {
  display: grid;
  grid-template-columns: 1fr auto 18px;
  gap: 8px;
  width: 100%;
  padding: 10px 0;
  border: 0;
  border-bottom: 1px solid var(--ui-border);
  background: transparent;
  text-align: left;
  cursor: pointer;
}
.runtime-edge-heading {
  display: flex;
  align-items: center;
  gap: 10px;
}
@media (max-width: 900px) {
  .runtime-header,
  .runtime-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }
  .runtime-header-actions,
  .runtime-filters {
    width: 100%;
  }
  .runtime-filters {
    flex-wrap: wrap;
  }
  .runtime-filters :deep(.ant-select) {
    flex: 1;
    min-width: 130px;
  }
  .runtime-canvas {
    height: 560px;
  }
  .runtime-trace-list button {
    grid-template-columns: 18px 76px minmax(0, 1fr) minmax(120px, auto) 18px;
  }
  .runtime-trace-list button > span:not(.runtime-trace-summary) {
    display: none;
  }
}
@media (max-width: 560px) {
  .runtime-metrics {
    grid-template-columns: repeat(2, 1fr);
  }
  .runtime-metrics article:nth-child(2) {
    border-right: 0;
  }
  .runtime-metrics article:nth-child(-n + 2) {
    border-bottom: 1px solid var(--ui-border);
  }
  .runtime-header-actions {
    min-width: 0;
    justify-content: flex-end;
  }
  .runtime-canvas {
    height: 500px;
  }
  .runtime-canvas :deep(.vue-flow__minimap) {
    display: none;
  }
  .runtime-detail-metrics {
    grid-template-columns: 1fr;
  }
  .runtime-detail-metrics div {
    border-right: 0;
    border-bottom: 1px solid var(--ui-border);
  }
  .runtime-detail-metrics div:last-child {
    border-bottom: 0;
  }
}
</style>
