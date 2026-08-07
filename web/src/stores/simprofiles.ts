import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '../services/api'
import type { SimProfile } from '../types'

export const useSimProfilesStore = defineStore('sim-profiles', () => {
  const profiles = ref<SimProfile[]>([])
  const busy = ref(false)

  async function load(): Promise<void> {
    const result = await api.simProfiles()
    profiles.value = Array.isArray(result.profiles) ? result.profiles : []
  }

  async function create(input: Omit<SimProfile, 'first_seen_at' | 'last_seen_at'>): Promise<void> {
    busy.value = true
    try {
      await api.simProfileCreate(input)
      await load()
    } finally {
      busy.value = false
    }
  }

  async function update(
    iccid: string,
    input: Pick<SimProfile, 'name' | 'local_phone' | 'notes' | 'tags'>,
  ): Promise<void> {
    busy.value = true
    try {
      await api.simProfileUpdate(iccid, input)
      await load()
    } finally {
      busy.value = false
    }
  }

  async function remove(iccid: string): Promise<void> {
    busy.value = true
    try {
      await api.simProfileDelete(iccid)
      await load()
    } finally {
      busy.value = false
    }
  }

  function find(iccid?: string): SimProfile | undefined {
    return iccid ? profiles.value.find((profile) => profile.iccid === iccid) : undefined
  }

  return { profiles, busy, load, create, update, remove, find }
})

export type SimProfilesStore = ReturnType<typeof useSimProfilesStore>
