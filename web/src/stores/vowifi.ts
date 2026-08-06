import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '../services/api'
import { useDeviceStore } from './device'
import type { VowifiStatus } from '../types'

// VoWiFi 域状态: 状态与操作追踪。
// 视图经 typed ViewContext 读取本 store 暴露的 refs/actions。
export const useVowifiStore = defineStore('vowifi', () => {
  const device = useDeviceStore()
  const status = ref<VowifiStatus | null>(null)
  const operationID = ref('')

  const operation = computed(() => (operationID.value ? device.operations[operationID.value] : undefined))

  async function load(): Promise<void> {
    status.value = await api.vowifi()
  }

  async function run(action: 'enable' | 'disable' | 'reconnect'): Promise<{ operation_id: string }> {
    const result =
      action === 'enable'
        ? await api.vowifiEnable()
        : action === 'disable'
          ? await api.vowifiDisable()
          : await api.vowifiReconnect()
    operationID.value = result.operation_id
    return result
  }

  return {
    status,
    operationID,
    operation,
    load,
    run,
  }
})

export type VowifiStore = ReturnType<typeof useVowifiStore>
