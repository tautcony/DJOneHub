export function formatDateTime(value?: string) {
  return value ? new Date(value).toLocaleString() : ''
}

export function formatDuration(startedAt?: string, endedAt?: string, now = Date.now()) {
  if (!startedAt) return ''
  const start = Date.parse(startedAt)
  const end = endedAt ? Date.parse(endedAt) : now
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return ''

  const totalSeconds = Math.floor((end - start) / 1000)
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const minuteText = String(minutes).padStart(2, '0')
  const secondText = String(seconds).padStart(2, '0')
  return hours > 0 ? `${hours}:${minuteText}:${secondText}` : `${minuteText}:${secondText}`
}
