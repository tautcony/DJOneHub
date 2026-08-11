import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useEsimStore } from './esim'

const apiMocks = vi.hoisted(() => ({
  esimNotifications: vi.fn(),
  esimNotificationHistory: vi.fn(),
}))

vi.mock('../services/api', () => ({
  api: apiMocks,
  APIError: class APIError extends Error {
    code = 'test'
  },
}))

vi.mock('../i18n', () => ({
  i18n: { global: { te: () => false, t: (key: string) => key } },
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

describe('eSIM notification loading', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('coalesces concurrent pending and history refreshes', async () => {
    const pending = deferred<{ notifications: [] }>()
    const history = deferred<{ history: [] }>()
    apiMocks.esimNotifications.mockReturnValue(pending.promise)
    apiMocks.esimNotificationHistory.mockReturnValue(history.promise)
    const store = useEsimStore()

    const first = store.loadNotifications(true)
    const second = store.loadNotifications(true)

    expect(apiMocks.esimNotifications).toHaveBeenCalledTimes(1)
    expect(apiMocks.esimNotificationHistory).toHaveBeenCalledTimes(1)
    pending.resolve({ notifications: [] })
    history.resolve({ history: [] })
    await Promise.all([first, second])
    expect(store.notificationsLoading).toBe(false)
  })
})
