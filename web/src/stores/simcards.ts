import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '../services/api'
import type { SimCard } from '../types'

// SIM 卡档案域：以 ICCID 为主键的卡片维护（名称/备注/号码）与插卡记录。
export const useSimCardsStore = defineStore('simcards', () => {
  const cards = ref<SimCard[]>([])
  const busy = ref(false)

  async function load(): Promise<void> {
    try {
      const result = await api.simCards()
      cards.value = Array.isArray(result.cards) ? result.cards : []
    } catch {
      cards.value = []
    }
  }

  async function create(input: {
    iccid: string
    imsi: string
    msisdn: string
    name: string
    notes: string
  }): Promise<void> {
    busy.value = true
    try {
      await api.simCardCreate(input.iccid, input.imsi, input.msisdn, input.name, input.notes)
      await load()
    } finally {
      busy.value = false
    }
  }

  async function update(
    iccid: string,
    input: { name: string; notes: string; msisdn: string },
  ): Promise<void> {
    busy.value = true
    try {
      await api.simCardUpdate(iccid, input.name, input.notes, input.msisdn)
      await load()
    } finally {
      busy.value = false
    }
  }

  async function remove(iccid: string): Promise<void> {
    busy.value = true
    try {
      await api.simCardDelete(iccid)
      await load()
    } finally {
      busy.value = false
    }
  }

  return { cards, busy, load, create, update, remove }
})

export type SimCardsStore = ReturnType<typeof useSimCardsStore>
