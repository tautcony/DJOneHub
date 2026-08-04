export function formatDateTime(value?: string) {
  return value ? new Date(value).toLocaleString() : ''
}
