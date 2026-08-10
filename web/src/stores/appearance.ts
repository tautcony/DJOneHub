import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'

export type AppearanceMode = 'light' | 'dark' | 'system'
export type ResolvedAppearance = Exclude<AppearanceMode, 'system'>

export const appearanceStorageKey = 'djonehub.appearance'

export function parseAppearanceMode(value: string | null | undefined): AppearanceMode {
  return value === 'light' || value === 'dark' || value === 'system' ? value : 'system'
}

export function resolveAppearanceMode(mode: AppearanceMode, systemPrefersDark: boolean): ResolvedAppearance {
  if (mode === 'system') return systemPrefersDark ? 'dark' : 'light'
  return mode
}

export function initialResolvedAppearance(): ResolvedAppearance {
  const mode = parseAppearanceMode(localStorage.getItem(appearanceStorageKey))
  return resolveAppearanceMode(mode, window.matchMedia('(prefers-color-scheme: dark)').matches)
}

function applyAppearance(value: ResolvedAppearance) {
  document.documentElement.dataset.theme = value
  document.documentElement.style.colorScheme = value
}

export const useAppearanceStore = defineStore('appearance', () => {
  const mode = ref<AppearanceMode>(parseAppearanceMode(localStorage.getItem(appearanceStorageKey)))
  const systemPrefersDark = ref(false)
  const resolved = computed(() => resolveAppearanceMode(mode.value, systemPrefersDark.value))
  let mediaQuery: MediaQueryList | undefined

  function handleSystemAppearance(event: MediaQueryListEvent) {
    systemPrefersDark.value = event.matches
  }

  function initialize() {
    if (!mediaQuery) {
      mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
      systemPrefersDark.value = mediaQuery.matches
      mediaQuery.addEventListener('change', handleSystemAppearance)
    }
    applyAppearance(resolved.value)
  }

  watch(mode, (value) => {
    localStorage.setItem(appearanceStorageKey, value)
  })
  watch(resolved, applyAppearance)

  return { mode, resolved, initialize }
})
