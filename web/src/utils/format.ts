export function formatBytes(value: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let amount = Math.max(0, value)
  let unit = 0
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024
    unit++
  }
  return `${amount.toFixed(unit === 0 ? 0 : amount >= 100 ? 0 : 1)} ${units[unit]}`
}

export function formatRate(value: number): string {
  return `${formatBytes(value)}/s`
}
