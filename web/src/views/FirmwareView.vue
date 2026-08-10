<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import {
  CloudDownloadOutlined,
  CodeOutlined,
  FolderOpenOutlined,
  LockOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import Panel from '../components/Panel.vue'
import StatusLight from '../components/StatusLight.vue'
import type { OperationStatus } from '../types'
import { confirmDanger } from '../utils/confirm'
import { useViewContext } from './context'

const { t } = useI18n()
const {
  backupFirmware,
  device,
  firmware,
  firmwareOperation,
  firmwareOperationLogs,
  firmwareOperationModalOpen,
  refreshFirmware,
  runFirmwareAction,
  saveFirmwareADBCommand,
  saveFirmwareDeviceControlSettings,
  selectFirmwareADBFile,
  selectFirmwareBackupDirectory,
  selectFirmwareEDLDirectory,
  selectFirmwareLoaderFile,
  updateFirmwareUSBID,
} = useViewContext()

type EDLRunner = 'python' | 'uv'
const edlPath = ref('')
const edlRunner = ref<EDLRunner>('uv')
const backupDirectory = ref('')
const loaderPath = ref('')
const backupFileName = ref(makeBackupFileName())
let lastGeneratedBackupFileName = backupFileName.value
const vid = ref('')
const pid = ref('')
const selectedADBSerial = ref('')
const selectedADBAction = ref<string | undefined>(undefined)
const shellOpen = ref(false)
const shellTerminalElement = ref(null)
const operationTerminalElement = ref(null)
const shellConnecting = ref(false)
const shellConnected = ref(false)
let shellSocket: WebSocket | undefined
let shellTerminal: Terminal | undefined
let operationTerminal: Terminal | undefined
let renderedOperationLogCount = 0
let operationTerminalOpening = false
const outputPath = computed(() => {
  const directory = backupDirectory.value.trim().replace(/\/$/, '')
  return directory ? `${directory}/${backupFileName.value}` : ''
})
const edlAvailable = computed(() => firmware.value?.backup.available === true)
const resetAvailable = computed(() => firmware.value?.backup.reset_available === true)
const busy = computed(() => {
  const state = firmwareOperation.value?.state
  return state === 'pending' || state === 'running'
})
const recoveryDetail = computed(() => {
  const details = firmwareOperationSnapshot.value?.error?.details
  if (details?.backup_valid === true && details?.reconnect_required === true)
    return t('firmware.backupValidResetFailed')
  return ''
})
const modeLabel = computed(() => {
  const mode = firmware.value?.mode
  if (mode === 'adb') return t('firmware.modes.adb')
  if (mode === 'edl') return t('firmware.modes.edl')
  if (mode === 'normal') return t('firmware.modes.normal')
  return t('firmware.modes.unknown')
})
const modeColor = computed(() => {
  if (firmware.value?.mode === 'adb') return 'blue'
  if (firmware.value?.mode === 'edl') return 'orange'
  if (firmware.value?.mode === 'normal') return 'green'
  return 'default'
})
type ADBDeviceStatus = { serial: string; state: string; online: boolean }
const adbDevices = computed<ADBDeviceStatus[]>(() => firmware.value?.adb.devices || [])
const selectedADBDevice = computed(() =>
  adbDevices.value.find((item) => item.serial === selectedADBSerial.value),
)
const selectedADBOnline = computed(() => selectedADBDevice.value?.online === true)
const directEDLAvailable = computed(() => firmware.value?.entry_methods?.includes('direct') === true)
const adbEDLAvailable = computed(() => firmware.value?.entry_methods?.includes('adb') === true)
const directEDLReason = computed(() => firmware.value?.entry_method_reasons?.direct || '')
const canEnterEDL = computed(() => !busy.value && directEDLAvailable.value)
const adbModeAction = computed<'enable' | 'disable' | undefined>(() => {
  if (!firmware.value?.adb.enabled_known) return undefined
  return firmware.value.adb.enabled ? 'disable' : 'enable'
})
const canToggleADB = computed(() => !busy.value && device.has('raw_at') && adbModeAction.value !== undefined)
const canShowEnterEDL = computed(
  () => !busy.value && ['normal', 'adb'].includes(firmware.value?.mode || '') && directEDLAvailable.value,
)
const canShowResetEDL = computed(
  () => !busy.value && firmware.value?.mode === 'edl' && resetAvailable.value,
)
const adbConfigLabel = computed(() => {
  if (firmware.value?.adb.enabled_known) {
    return firmware.value.adb.enabled ? t('firmware.adb.enabled') : t('firmware.adb.disabled')
  }
  return t('firmware.adb.configurationUnknown')
})
const adbClientLabel = computed(() => {
  if (firmware.value?.adb.server_available) return t('firmware.adb.serverReady')
  return t('firmware.adb.serverUnavailable')
})
const adbCommand = ref('')
let lastPersistedCommand = ''
let adbCommandSaveTimer: ReturnType<typeof setTimeout> | undefined
// Fill the draft once from the effective command reported by the backend,
// without clobbering edits while status keeps polling.
watch(
  () => firmware.value?.adb.command,
  (command) => {
    if (command && !adbCommand.value) {
      adbCommand.value = command
      lastPersistedCommand = command
    }
  },
  { immediate: true },
)

watch(
  () => firmware.value?.settings,
  (settings) => {
    if (!settings) return
    if (!edlPath.value && settings.edl_path) edlPath.value = settings.edl_path
    if (settings.edl_runner === 'python' || settings.edl_runner === 'uv') edlRunner.value = settings.edl_runner
    if (!loaderPath.value && settings.loader_path) loaderPath.value = settings.loader_path
    if (!backupDirectory.value && settings.backup_directory) backupDirectory.value = settings.backup_directory
  },
  { immediate: true },
)
watch(
  () => firmware.value?.firmware,
  (version) => {
    if (backupFileName.value !== lastGeneratedBackupFileName) return
    backupFileName.value = makeBackupFileName(version)
    lastGeneratedBackupFileName = backupFileName.value
  },
)
// The command saves automatically after a short pause: choosing a file or
// finishing a manual edit persists without a separate save button.
watch(adbCommand, (value) => {
  const next = value.trim()
  if (next === lastPersistedCommand) return
  if (adbCommandSaveTimer !== undefined) window.clearTimeout(adbCommandSaveTimer)
  adbCommandSaveTimer = window.setTimeout(async () => {
    const effective = await saveFirmwareADBCommand(next)
    if (effective) {
      lastPersistedCommand = effective
      adbCommand.value = effective
    }
  }, 600)
})
const adbCommandSourceLabel = computed(() => {
  const source = firmware.value?.adb.command_source
  if (source === 'env') return t('firmware.adb.commandFromEnv')
  if (source === 'saved') return t('firmware.adb.commandFromSaved')
  return t('firmware.adb.commandFromDefault')
})
async function chooseADBFile() {
  const path = await selectFirmwareADBFile()
  if (path) adbCommand.value = path // the watcher auto-saves
}
const adbAccessibleLabel = computed(() => {
  if (adbDevices.value.some((item) => item.online)) return t('firmware.adb.accessible')
  return t('firmware.adb.inaccessible')
})
const usbConfigFields = computed(() => firmware.value?.usb_config_fields || [])

function closeOperationModal() {
  firmwareOperationModalOpen.value = false
}

async function openOperationTerminal() {
  if (operationTerminalOpening || operationTerminal || !firmwareOperationLogs.value.length) return
  operationTerminalOpening = true
  try {
    await nextTick()
    const element = operationTerminalElement.value as Parameters<Terminal['open']>[0] | null
    if (!element || !firmwareOperationModalOpen.value) return
    operationTerminal = new Terminal({
      convertEol: true,
      cursorBlink: false,
      disableStdin: true,
      fontFamily: 'SFMono-Regular, Menlo, Consolas, monospace',
      fontSize: 12,
      scrollback: 5000,
      theme: { background: '#111315', foreground: '#e6e8ea', cursor: '#111315' },
    })
    operationTerminal.open(element)
    resizeOperationTerminal()
    renderedOperationLogCount = 0
    syncOperationTerminal(firmwareOperationLogs.value)
    window.requestAnimationFrame(resizeOperationTerminal)
  } finally {
    operationTerminalOpening = false
  }
}

function resizeOperationTerminal() {
  const element = operationTerminalElement.value as Parameters<Terminal['open']>[0] | null
  if (!operationTerminal || !element) return
  const columns = Math.max(40, Math.floor((element.clientWidth - 20) / 7.3))
  const rows = Math.max(8, Math.floor((element.clientHeight - 20) / 16))
  operationTerminal.resize(columns, rows)
}

function syncOperationTerminal(logs: string[]) {
  if (!operationTerminal) return
  if (logs.length < renderedOperationLogCount) {
    operationTerminal.reset()
    renderedOperationLogCount = 0
  }
  for (let index = renderedOperationLogCount; index < logs.length; index++) {
    operationTerminal.write(logs[index])
  }
  renderedOperationLogCount = logs.length
  operationTerminal.scrollToBottom()
}

function usbConfigFieldLabel(key: string) {
  return t(`firmware.usbFields.${key}.label`)
}

function usbConfigFieldDetail(key: string) {
  return t(`firmware.usbFields.${key}.detail`)
}

function usbConfigFieldState(value: string) {
  if (value === '1') return t('firmware.usbFieldState.enabled')
  if (value === '0') return t('firmware.usbFieldState.disabled')
  return t('firmware.usbFieldState.unknown')
}

function usbConfigTooltip(key: string, value: string) {
  return `${usbConfigFieldLabel(key)} · ${value || '?'} · ${usbConfigFieldState(value)} · ${usbConfigFieldDetail(key)}`
}

watch(
  () => firmware.value?.backup.default_dir,
  (directory) => {
    if (!backupDirectory.value && directory) backupDirectory.value = directory
  },
  { immediate: true },
)

watch(
  () => [firmware.value?.usb_vid, firmware.value?.usb_pid] as const,
  ([currentVID, currentPID]) => {
    vid.value = currentVID ? currentVID.replace(/^0x/i, '') : ''
    pid.value = currentPID ? currentPID.replace(/^0x/i, '') : ''
  },
  { immediate: true },
)

watch(
  adbDevices,
  (devices: ADBDeviceStatus[]) => {
    if (!devices.some((item) => item.serial === selectedADBSerial.value)) {
      selectedADBSerial.value = devices.find((item) => item.online)?.serial || ''
    }
  },
  { immediate: true },
)

watch(
  () => firmware.value,
  (value) => {
    if (!value) closeShell()
  },
)

watch(
  () => firmwareOperationModalOpen.value,
  (open) => {
    if (open && firmwareOperationLogs.value.length) void openOperationTerminal()
    else {
      operationTerminal?.dispose()
      operationTerminal = undefined
      renderedOperationLogCount = 0
    }
  },
)

// 弹窗打开期间保留 operation 快照, 避免底层 operation 在 5 分钟 TTL 清理后
// 弹窗内容突然变空白 (不动 client operations map 的有界清理策略)。
const firmwareOperationSnapshot = ref<OperationStatus | undefined>(undefined)
watch(
  firmwareOperation,
  (operation) => {
    if (operation) firmwareOperationSnapshot.value = operation
  },
  { immediate: true },
)

watch(firmwareOperationLogs, (logs) => {
  if (!firmwareOperationModalOpen.value || !logs.length) return
  if (!operationTerminal) void openOperationTerminal()
  else syncOperationTerminal(logs)
})

watch(vid, (value) => {
  const normalized = value
    .toUpperCase()
    .replace(/[^0-9A-F]/g, '')
    .slice(0, 4)
  if (value !== normalized) vid.value = normalized
})
watch(pid, (value) => {
  const normalized = value
    .toUpperCase()
    .replace(/[^0-9A-F]/g, '')
    .slice(0, 4)
  if (value !== normalized) pid.value = normalized
})

function makeBackupFileName(version = firmware.value?.firmware) {
  const now = new Date()
  const part = (value: number) => String(value).padStart(2, '0')
  const safeVersion = (version || 'unknown')
    .trim()
    .replace(/[^A-Za-z0-9._-]+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '') || 'unknown'
  return `full-nand-${safeVersion}-${now.getFullYear()}${part(now.getMonth() + 1)}${part(now.getDate())}-${part(now.getHours())}${part(now.getMinutes())}${part(now.getSeconds())}.bin`
}

function startBackup() {
  if (!outputPath.value || !edlAvailable.value || busy.value) return
  void backupFirmware(outputPath.value, loaderPath.value.trim(), edlPath.value.trim(), edlRunner.value)
  backupFileName.value = makeBackupFileName()
  lastGeneratedBackupFileName = backupFileName.value
}

async function chooseBackupDirectory() {
  if (busy.value) return
  const directory = await selectFirmwareBackupDirectory()
  if (directory) {
    backupDirectory.value = directory
    await saveFirmwareDeviceControlSettings({ backup_directory: directory })
  }
}

async function chooseEDLDirectory() {
  if (busy.value) return
  const directory = await selectFirmwareEDLDirectory()
  if (directory) {
    edlPath.value = directory
    await saveFirmwareDeviceControlSettings({ edl_path: directory, edl_runner: edlRunner.value })
  }
}

async function saveEDLPath() {
  await saveFirmwareDeviceControlSettings({ edl_path: edlPath.value.trim(), edl_runner: edlRunner.value })
}

async function saveEDLRunner() {
  if (!edlPath.value.trim()) return
  await saveFirmwareDeviceControlSettings({ edl_path: edlPath.value.trim(), edl_runner: edlRunner.value })
}

async function chooseLoaderFile() {
  if (busy.value) return
  const path = await selectFirmwareLoaderFile()
  if (path) {
    loaderPath.value = path
    await saveFirmwareDeviceControlSettings({ loader_path: path })
  }
}

async function saveLoaderPath() {
  await saveFirmwareDeviceControlSettings({ loader_path: loaderPath.value.trim() })
}

async function enterEDL() {
  if (!canEnterEDL.value) return
  const confirmed = await confirmDanger({
    title: t('firmware.mode.edlConfirm'),
    confirmLabel: t('common.confirm'),
    cancelLabel: t('common.cancel'),
  })
  if (confirmed) await runFirmwareAction('edl', '', 'direct')
}

async function enterEDLFromADB() {
  if (!adbEDLAvailable.value || !selectedADBOnline.value || busy.value) return
  const confirmed = await confirmDanger({
    title: t('firmware.mode.adbEDLConfirm'),
    confirmLabel: t('common.confirm'),
    cancelLabel: t('common.cancel'),
  })
  if (confirmed) await runFirmwareAction('edl', selectedADBSerial.value, 'adb')
}

async function resetEDL() {
  if (!resetAvailable.value || busy.value) return
  const confirmed = await confirmDanger({
    title: t('firmware.mode.resetConfirm'),
    confirmLabel: t('common.confirm'),
    cancelLabel: t('common.cancel'),
  })
  if (confirmed) await runFirmwareAction('reset')
}

async function toggleADBMode(action: 'enable' | 'disable') {
  if (!canToggleADB.value || action !== adbModeAction.value) return
  const confirmed = await confirmDanger({
    title: t('firmware.mode.adbModeConfirm'),
    confirmLabel: t('common.confirm'),
    cancelLabel: t('common.cancel'),
  })
  if (confirmed) await runFirmwareAction(action)
}

async function rebootADBDevice() {
  if (busy.value || !selectedADBOnline.value) return
  const confirmed = await confirmDanger({
    title: t('firmware.mode.adbRebootConfirm'),
    confirmLabel: t('common.confirm'),
    cancelLabel: t('common.cancel'),
  })
  if (confirmed) await runFirmwareAction('adb-reboot', selectedADBSerial.value)
}

async function runADBCommandAction(action?: string) {
  selectedADBAction.value = undefined
  if (action === 'reboot') {
    await rebootADBDevice()
    return
  }
  if (action === 'edl') await enterEDLFromADB()
}

async function openShell() {
  if (!selectedADBOnline.value || busy.value) return
  shellOpen.value = true
  shellConnecting.value = true
  shellConnected.value = false
  await nextTick()
  shellTerminal?.dispose()
  shellTerminal = new Terminal({
    cursorBlink: true,
    cursorStyle: 'bar',
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
    fontSize: 13,
    scrollback: 5000,
    theme: { background: '#111315', foreground: '#e6e8ea', cursor: '#ffffff' },
  })
  const terminalElement = shellTerminalElement.value as Parameters<Terminal['open']>[0] | null
  if (terminalElement) {
    shellTerminal.open(terminalElement)
    resizeShellTerminal()
    shellTerminal.focus()
  }
  const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws'
  const socket = new WebSocket(
    `${protocol}://${window.location.host}/api/v1/device-control/actions/adb/shell/ws?serial=${encodeURIComponent(selectedADBSerial.value)}`,
  )
  shellSocket = socket
  socket.binaryType = 'arraybuffer'
  shellTerminal.onData((data) => {
    if (socket.readyState === WebSocket.OPEN) socket.send(data)
  })
  socket.onopen = () => {
    if (shellSocket !== socket) return
    shellConnecting.value = false
    shellConnected.value = true
    shellTerminal?.focus()
  }
  socket.onmessage = (event) => {
    if (typeof event.data === 'string') {
      shellTerminal?.write(event.data)
      return
    }
    if (event.data instanceof ArrayBuffer) {
      shellTerminal?.write(new Uint8Array(event.data))
      return
    }
    if (event.data instanceof window.Blob) {
      void event.data.arrayBuffer().then((value) => {
        shellTerminal?.write(new Uint8Array(value))
      })
    }
  }
  socket.onclose = () => {
    if (shellSocket !== socket) return
    closeShell()
  }
  socket.onerror = () => {
    if (shellSocket !== socket) return
    closeShell()
  }
}

function resizeShellTerminal() {
  const terminalElement = shellTerminalElement.value as Parameters<Terminal['open']>[0] | null
  if (!shellTerminal || !terminalElement) return
  const width = terminalElement.clientWidth
  shellTerminal.resize(Math.max(40, Math.floor(width / 8.1)), 24)
}

function focusShellTerminal() {
  shellTerminal?.focus()
}

function closeShell() {
  shellSocket?.close()
  shellSocket = undefined
  shellTerminal?.dispose()
  shellTerminal = undefined
  shellOpen.value = false
  shellConnecting.value = false
  shellConnected.value = false
}

function chooseUSBID(nextVID: string, nextPID: string) {
  vid.value = nextVID
  pid.value = nextPID
}

async function updateUSBID() {
  if (!vid.value.trim() || !pid.value.trim() || busy.value) return
  const nextVID = vid.value.trim().toUpperCase()
  const nextPID = pid.value.trim().toUpperCase()
  const confirmed = await confirmDanger({
    title: t('firmware.usbIDConfirm', { vid: nextVID, pid: nextPID }),
    confirmLabel: t('common.confirm'),
    cancelLabel: t('common.cancel'),
  })
  if (confirmed) await updateFirmwareUSBID(nextVID, nextPID)
}

window.addEventListener('resize', resizeShellTerminal)
window.addEventListener('resize', resizeOperationTerminal)
onBeforeUnmount(() => {
  window.removeEventListener('resize', resizeShellTerminal)
  window.removeEventListener('resize', resizeOperationTerminal)
  closeShell()
  operationTerminal?.dispose()
})
</script>

<template>
  <section class="firmware-view">
    <Panel :eyebrow="t('firmware.infoEyebrow')" :title="t('firmware.infoTitle')">
      <template #actions>
        <div class="panel-actions">
          <a-tag :color="modeColor">{{ modeLabel }}</a-tag>
          <a-button :loading="device.error === '' && !firmware" @click="refreshFirmware">
            <ReloadOutlined />{{ t('common.refresh') }}
          </a-button>
        </div>
      </template>
      <EmptyState
        v-if="!firmware"
        :title="t('firmware.unavailable')"
        :detail="t('firmware.unavailableDetail')"
      />
      <div v-else class="firmware-status-grid">
        <dl class="firmware-fields">
          <div>
            <dt>{{ t('firmware.manufacturer') }}</dt>
            <dd>{{ firmware.manufacturer || t('common.empty') }}</dd>
          </div>
          <div>
            <dt>{{ t('firmware.model') }}</dt>
            <dd>{{ firmware.model || t('common.empty') }}</dd>
          </div>
          <div>
            <dt>{{ t('firmware.version') }}</dt>
            <dd>
              {{ firmware.firmware || t('common.empty') }}
              <small v-if="firmware.firmware_version_source" class="firmware-version-source">
                {{ firmware.firmware_version_source }}{{ firmware.firmware_version_live === false ? ` (${t('firmware.cached')})` : '' }}
              </small>
              <small v-else-if="firmware.firmware_version_reason" class="firmware-version-source">
                {{ firmware.firmware_version_reason }}
              </small>
            </dd>
          </div>
          <div>
            <dt>{{ t('firmware.usbId') }}</dt>
            <dd class="mono">{{ firmware.usb_id || t('common.empty') }}</dd>
          </div>
        </dl>
        <div v-if="usbConfigFields.length" class="firmware-usb-config">
          <div class="firmware-usb-config-heading">
            <div class="firmware-subheading">{{ t('firmware.usbConfigFields') }}</div>
            <small>{{ t('firmware.usbConfigLegend') }}</small>
          </div>
          <div class="firmware-usb-config-strip" role="list">
            <a-tooltip
              v-for="field in usbConfigFields"
              :key="field.key"
              :title="usbConfigTooltip(field.key, field.value)"
            >
              <span
                :class="[
                  'firmware-usb-config-item',
                  field.value === '1' ? 'enabled' : field.value === '0' ? 'disabled' : 'unknown',
                ]"
                role="listitem"
                tabindex="0"
              >
                <span class="firmware-usb-config-dot" aria-hidden="true" />
                <span class="mono">{{ field.key.toUpperCase() }}</span>
                <strong class="mono">{{ field.value || '?' }}</strong>
              </span>
            </a-tooltip>
          </div>
        </div>
      </div>
    </Panel>

    <div class="firmware-columns">
      <Panel :eyebrow="t('firmware.adbEyebrow')" :title="t('firmware.adbTitle')">
        <div class="firmware-adb-summary">
          <div class="firmware-adb-status">
            <StatusLight :tone="firmware?.adb.enabled ? 'success' : 'neutral'" />
            <div class="firmware-adb-status-copy">
              <span>{{ t('firmware.adb.usbComposition') }}</span>
              <strong>{{ adbConfigLabel }}</strong>
            </div>
          </div>
          <div class="firmware-adb-status">
            <StatusLight :tone="firmware?.adb.server_available ? 'success' : 'neutral'" />
            <div class="firmware-adb-status-copy">
              <span>{{ t('firmware.adb.adbClient') }}</span>
              <strong>{{ adbClientLabel }}</strong>
            </div>
          </div>
          <div class="firmware-adb-status">
            <StatusLight :tone="adbDevices.some((item) => item.online) ? 'success' : 'neutral'" />
            <div class="firmware-adb-status-copy">
              <span>{{ t('firmware.adb.adbDevice') }}</span>
              <strong>{{ adbAccessibleLabel }}</strong>
            </div>
          </div>
        </div>
        <a-alert v-if="firmware?.adb.error" type="warning" show-icon :message="firmware.adb.error" />
        <div class="firmware-adb-command">
          <a-input v-model:value="adbCommand" allow-clear :placeholder="t('firmware.adb.commandPlaceholder')">
            <template #suffix>
              <a-tooltip :title="t('firmware.adb.chooseFile')">
                <button
                  type="button"
                  class="firmware-directory-button"
                  :aria-label="t('firmware.adb.chooseFile')"
                  @click="chooseADBFile"
                >
                  <FolderOpenOutlined />
                </button>
              </a-tooltip>
            </template>
          </a-input>
          <div class="firmware-adb-command-source">{{ adbCommandSourceLabel }}</div>
        </div>
        <div class="firmware-adb-device-controls">
          <a-select
            v-model:value="selectedADBSerial"
            :disabled="busy || !adbDevices.length"
            :placeholder="t('firmware.adb.selectDevice')"
          >
            <a-select-option v-for="item in adbDevices" :key="item.serial" :value="item.serial">
              {{ item.serial }} · {{ item.state }}
            </a-select-option>
          </a-select>
        </div>
        <div class="firmware-control-groups">
          <div class="firmware-control-group">
            <div class="firmware-control-label">{{ t('firmware.adb.commands') }}</div>
            <div class="firmware-command-buttons">
              <a-button :disabled="!selectedADBOnline || busy" @click="openShell">
                <CodeOutlined />{{ t('firmware.adb.shell') }}
              </a-button>
              <a-select
                v-model:value="selectedADBAction"
                class="firmware-adb-command-select"
                :disabled="!selectedADBOnline || busy"
                :placeholder="t('firmware.adb.selectAction')"
                @change="runADBCommandAction"
              >
                <a-select-option value="reboot">{{ t('firmware.adb.reboot') }}</a-select-option>
                <a-select-option v-if="adbEDLAvailable" value="edl">
                  {{ t('firmware.adb.enterEDL') }}
                </a-select-option>
              </a-select>
            </div>
          </div>
          <div class="firmware-control-group">
            <div class="firmware-control-label">{{ t('firmware.adb.configuration') }}</div>
            <div class="firmware-adb-mode-controls">
              <a-button
                v-if="adbModeAction === 'disable'"
                type="primary"
                class="firmware-mode-action"
                :loading="busy"
                :disabled="!canToggleADB"
                @click="toggleADBMode('disable')"
              >
                <LockOutlined />{{ t('firmware.disableADBReboot') }}
              </a-button>
              <a-button
                v-else-if="adbModeAction === 'enable'"
                type="primary"
                class="firmware-mode-action"
                :loading="busy"
                :disabled="!canToggleADB"
                @click="toggleADBMode('enable')"
              >
                <ThunderboltOutlined />{{ t('firmware.enableADBReboot') }}
              </a-button>
            </div>
          </div>
        </div>
      </Panel>

      <Panel :eyebrow="t('firmware.edlEyebrow')" :title="t('firmware.edlTitle')">
        <div class="firmware-edl-capabilities">
          <div class="firmware-edl-capability">
            <StatusLight :tone="directEDLAvailable ? 'success' : 'neutral'" />
            <div>
              <strong>{{ t('firmware.entryDirect') }}</strong>
              <span>{{ directEDLAvailable ? t('firmware.edlAvailable') : directEDLReason || t('common.unavailable') }}</span>
            </div>
          </div>
        </div>

        <a-form layout="vertical" class="form firmware-form">
          <a-form-item :label="t('firmware.edlPath')">
            <a-input
              v-model:value="edlPath"
              readonly
              allow-clear
              :placeholder="t('firmware.edlPathPlaceholder')"
              :disabled="busy"
              @change="saveEDLPath"
            >
              <template #suffix>
                <a-tooltip :title="t('firmware.selectEDLDirectory')">
                  <button
                    type="button"
                    class="firmware-directory-button"
                    :aria-label="t('firmware.selectEDLDirectory')"
                    :disabled="busy"
                    @click="chooseEDLDirectory"
                  >
                    <FolderOpenOutlined />
                  </button>
                </a-tooltip>
              </template>
            </a-input>
          </a-form-item>
          <a-form-item :label="t('firmware.edlRunner')">
            <a-segmented
              v-model:value="edlRunner"
              block
              :disabled="busy"
              :options="[
                { label: t('firmware.edlRunnerPython'), value: 'python' },
                { label: t('firmware.edlRunnerUV'), value: 'uv' },
              ]"
              @change="saveEDLRunner"
            />
          </a-form-item>
          <a-button
            v-if="canShowEnterEDL"
            type="primary"
            class="firmware-mode-action"
            @click="enterEDL"
          >
            <ThunderboltOutlined />{{ t('firmware.enterEDL') }}
          </a-button>
          <a-button
            v-else-if="canShowResetEDL"
            type="primary"
            class="firmware-mode-action"
            @click="resetEDL"
          >
            <ReloadOutlined />{{ t('firmware.resetEDL') }}
          </a-button>
        </a-form>
      </Panel>

      <Panel :eyebrow="t('firmware.backupEyebrow')" :title="t('firmware.backupTitle')">
        <a-alert
          v-if="firmware && !firmware.backup.available"
          type="warning"
          show-icon
          :message="firmware.backup.reason || t('firmware.edlUnavailable')"
        />
        <a-form layout="vertical" class="form firmware-form" @submit.prevent="startBackup">
          <a-form-item :label="t('firmware.loaderFile')">
            <a-input
              v-model:value="loaderPath"
              readonly
              allow-clear
              :placeholder="t('firmware.loaderOptional')"
              :disabled="busy"
              @change="saveLoaderPath"
            >
              <template #suffix>
                <a-tooltip :title="t('firmware.selectLoaderFile')">
                  <button
                    type="button"
                    class="firmware-directory-button"
                    :aria-label="t('firmware.selectLoaderFile')"
                    :disabled="busy"
                    @click="chooseLoaderFile"
                  >
                    <FolderOpenOutlined />
                  </button>
                </a-tooltip>
              </template>
            </a-input>
          </a-form-item>
          <a-form-item :label="t('firmware.outputDirectory')">
            <a-input
              :value="backupDirectory"
              readonly
              :placeholder="t('firmware.outputDirectoryPlaceholder')"
              :disabled="busy"
            >
              <template #suffix>
                <a-tooltip :title="t('firmware.selectDirectory')">
                  <button
                    type="button"
                    class="firmware-directory-button"
                    :aria-label="t('firmware.selectDirectory')"
                    :disabled="busy"
                    @click="chooseBackupDirectory"
                  >
                    <FolderOpenOutlined />
                  </button>
                </a-tooltip>
              </template>
            </a-input>
          </a-form-item>
          <a-form-item :label="t('firmware.outputFile')">
            <a-input :value="backupFileName" readonly />
          </a-form-item>
          <a-button
            type="primary"
            html-type="submit"
            :loading="busy"
            :disabled="!edlAvailable || !backupDirectory.trim()"
          >
            <CloudDownloadOutlined />{{ t('firmware.startBackup') }}
          </a-button>
        </a-form>
      </Panel>

      <Panel :eyebrow="t('firmware.usbIDEyebrow')" :title="t('firmware.usbIDTitle')">
        <div class="firmware-usb-presets">
          <button
            type="button"
            :class="[
              'firmware-preset',
              { active: vid.toUpperCase() === '2CA3' && pid.toUpperCase() === '4006' },
            ]"
            @click="chooseUSBID('2CA3', '4006')"
          >
            <strong>{{ t('firmware.djiMode') }}</strong
            ><span>2CA3:4006</span>
          </button>
          <button
            type="button"
            :class="[
              'firmware-preset',
              { active: vid.toUpperCase() === '2C7C' && pid.toUpperCase() === '0125' },
            ]"
            @click="chooseUSBID('2C7C', '0125')"
          >
            <strong>{{ t('firmware.ec25Mode') }}</strong
            ><span>2C7C:0125</span>
          </button>
        </div>
        <a-form layout="vertical" class="form firmware-form" @submit.prevent="updateUSBID">
          <div class="firmware-usb-fields">
            <a-form-item label="VID"><a-input v-model:value="vid" :disabled="busy" /></a-form-item>
            <a-form-item label="PID"><a-input v-model:value="pid" :disabled="busy" /></a-form-item>
          </div>
          <a-button type="primary" html-type="submit" :loading="busy" :disabled="!vid.trim() || !pid.trim()">
            <ReloadOutlined />{{ t('firmware.updateUSBID') }}
          </a-button>
        </a-form>
      </Panel>
    </div>

    <a-modal
      v-model:open="firmwareOperationModalOpen"
      :title="t('firmware.operationTitle')"
      :width="760"
      destroy-on-close
      @ok="closeOperationModal"
    >
      <div class="firmware-operation">
        <div v-if="firmwareOperationSnapshot" class="firmware-operation-heading">
          <strong>{{ firmwareOperationSnapshot.message || t('firmware.operationWaiting') }}</strong
          ><span>{{ firmwareOperationSnapshot.progress }}%</span>
        </div>
        <a-progress
          v-if="firmwareOperationSnapshot"
          :percent="firmwareOperationSnapshot.progress"
          :status="
            firmwareOperationSnapshot.state === 'failed'
              ? 'exception'
              : firmwareOperationSnapshot.state === 'succeeded'
                ? 'success'
                : 'active'
          "
          :show-info="false"
        />
        <div
          v-if="firmwareOperationLogs.length"
          ref="operationTerminalElement"
          class="firmware-operation-terminal"
        />
        <a-alert
          v-if="firmwareOperationSnapshot?.error"
          type="error"
          show-icon
          :message="firmwareOperationSnapshot.error.message"
          :description="recoveryDetail || undefined"
        />
      </div>
    </a-modal>

    <a-modal
      v-model:open="shellOpen"
      :title="t('firmware.adb.shellTitle')"
      :footer="null"
      :width="840"
      destroy-on-close
      @cancel="closeShell"
    >
      <div class="firmware-shell">
        <div class="firmware-shell-status">
          <StatusLight :tone="shellConnected ? 'success' : 'info'" :pulse="!shellConnected" />
          <span>{{
            shellConnected ? t('firmware.adb.shellConnected') : t('firmware.adb.shellConnecting')
          }}</span>
        </div>
        <div ref="shellTerminalElement" class="firmware-shell-terminal" @click="focusShellTerminal" />
      </div>
    </a-modal>
  </section>
</template>
