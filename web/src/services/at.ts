export interface ATField {
  labelKey: string
  value: string
  valueKey?: string
}

export interface ParsedATResponse {
  statusKey: 'rawAt.status.ok' | 'rawAt.status.error' | 'rawAt.status.unknown'
  fields: ATField[]
}

export interface ATPreset {
  id: string
  command: string
  labelKey: string
}

export const AT_PRESETS: ATPreset[] = [
  { id: 'basic', command: 'AT', labelKey: 'rawAt.presets.basic' },
  { id: 'imei', command: 'AT+CGSN', labelKey: 'rawAt.presets.imei' },
  { id: 'firmware', command: 'AT+CGMR', labelKey: 'rawAt.presets.firmware' },
  { id: 'sim', command: 'AT+CPIN?', labelKey: 'rawAt.presets.sim' },
  { id: 'iccid', command: 'AT+QCCID', labelKey: 'rawAt.presets.iccid' },
  { id: 'phone', command: 'AT+CNUM', labelKey: 'rawAt.presets.phone' },
  { id: 'signal', command: 'AT+CSQ', labelKey: 'rawAt.presets.signal' },
  { id: 'registration', command: 'AT+CREG?', labelKey: 'rawAt.presets.registration' },
  { id: 'operator', command: 'AT+COPS?', labelKey: 'rawAt.presets.operator' },
  { id: 'attach', command: 'AT+CGATT?', labelKey: 'rawAt.presets.attach' },
  { id: 'network', command: 'AT+QNWINFO', labelKey: 'rawAt.presets.network' },
  { id: 'servingCell', command: 'AT+QENG="servingcell"', labelKey: 'rawAt.presets.servingCell' },
]

function linesOf(response: string): string[] {
  return response
    .split(/\r?\n/)
    .map(line => line.trim())
    .filter(Boolean)
    .filter(line => !/^AT(?:\+|$)/i.test(line))
}

function quotedCSV(value: string): string[] {
  const values: string[] = []
  const pattern = /"([^"]*)"|([^,\s]+)(?=\s*,|\s*$)/g
  let match: RegExpExecArray | null
  while ((match = pattern.exec(value)) !== null) values.push(match[1] ?? match[2])
  return values
}

function responseValue(lines: string[], prefix: RegExp): string | undefined {
  const line = lines.find(item => prefix.test(item))
  return line?.replace(prefix, '').trim()
}

function statusValue(value: string): string {
  return value === '1' || value === '5' ? 'rawAt.values.registered' : value === '2' ? 'rawAt.values.searching' : value === '3' ? 'rawAt.values.rejected' : value === '0' || value === '4' ? 'rawAt.values.notRegistered' : value
}

function parseCSQ(lines: string[]): ATField[] {
  const value = responseValue(lines, /^\+CSQ:\s*/)
  if (!value) return []
  const [rssi, ber] = value.split(',').map(item => item.trim())
  const fields: ATField[] = [{ labelKey: 'rawAt.fields.rssi', value: rssi || '—' }, { labelKey: 'rawAt.fields.ber', value: ber || '—' }]
  const numericRSSI = Number(rssi)
  if (Number.isInteger(numericRSSI) && numericRSSI >= 0 && numericRSSI <= 31) {
    fields.push({ labelKey: 'rawAt.fields.signalDbm', value: String(-113 + numericRSSI * 2) + ' dBm' })
  }
  return fields
}

function parseCREG(lines: string[]): ATField[] {
  const value = responseValue(lines, /^\+(?:C|CG)REG:\s*/)
  if (!value) return []
  const [mode, registration, lac, cellID] = value.split(',').map(item => item.trim().replace(/^"|"$/g, ''))
  return [
    { labelKey: 'rawAt.fields.reportMode', value: mode || '—' },
    { labelKey: 'rawAt.fields.registration', value: registration || '—', valueKey: statusValue(registration || '') },
    { labelKey: 'rawAt.fields.lac', value: lac || '—' },
    { labelKey: 'rawAt.fields.cellId', value: cellID || '—' },
  ]
}

function parseCOPS(lines: string[]): ATField[] {
  const value = responseValue(lines, /^\+COPS:\s*/)
  if (!value) return []
  const [mode, format, operator, accessTechnology] = quotedCSV(value)
  return [
    { labelKey: 'rawAt.fields.operatorMode', value: mode || '—' },
    { labelKey: 'rawAt.fields.operatorFormat', value: format || '—' },
    { labelKey: 'rawAt.fields.operator', value: operator || '—' },
    { labelKey: 'rawAt.fields.accessTechnology', value: accessTechnology || '—' },
  ]
}

function parseQNWINFO(lines: string[]): ATField[] {
  const value = responseValue(lines, /^\+QNWINFO:\s*/)
  if (!value) return []
  const [technology, operator, band, channel] = quotedCSV(value)
  return [
    { labelKey: 'rawAt.fields.accessTechnology', value: technology || '—' },
    { labelKey: 'rawAt.fields.operator', value: operator || '—' },
    { labelKey: 'rawAt.fields.band', value: band || '—' },
    { labelKey: 'rawAt.fields.channel', value: channel || '—' },
  ]
}

function parseCNUM(lines: string[]): ATField[] {
  const value = responseValue(lines, /^\+CNUM:\s*/)
  if (!value) return []
  const [, number, type] = quotedCSV(value)
  return [
    { labelKey: 'rawAt.fields.phone', value: number || '—' },
    { labelKey: 'rawAt.fields.numberType', value: type || '—' },
  ]
}

function parseNamedValue(lines: string[], prefix: RegExp, labelKey: string): ATField[] {
  const value = responseValue(lines, prefix)
  if (value) return [{ labelKey, value: value.replace(/^"|"$/g, '') }]
  const scalar = lines.find(line => line !== 'OK' && line !== 'ERROR' && !/^\+C(?:ME|MS) ERROR/i.test(line))
  return scalar ? [{ labelKey, value: scalar }] : []
}

function parsePacketAttach(lines: string[]): ATField[] {
  const fields = parseNamedValue(lines, /^\+CGATT:\s*/, 'rawAt.fields.packetAttach')
  const field = fields[0]
  if (field) field.valueKey = field.value === '1' ? 'rawAt.values.attached' : field.value === '0' ? 'rawAt.values.detached' : undefined
  return fields
}

function parseGeneric(lines: string[]): ATField[] {
  return lines
    .filter(line => line !== 'OK' && line !== 'ERROR' && !/^\+CME ERROR/i.test(line) && !/^\+CMS ERROR/i.test(line))
    .map((value, index) => ({ labelKey: index === 0 ? 'rawAt.fields.response' : 'rawAt.fields.responseLine', value }))
}

export function parseATResponse(command: string, response: string): ParsedATResponse {
  const lines = linesOf(response)
  const normalized = command.trim().toUpperCase()
  const hasError = lines.some(line => line === 'ERROR' || /^\+C(?:ME|MS) ERROR/i.test(line))
  const statusKey = hasError ? 'rawAt.status.error' : lines.includes('OK') ? 'rawAt.status.ok' : 'rawAt.status.unknown'
  let fields: ATField[] = []

  if (normalized === 'AT+CSQ') fields = parseCSQ(lines)
  else if (normalized === 'AT+CREG?') fields = parseCREG(lines)
  else if (normalized === 'AT+COPS?') fields = parseCOPS(lines)
  else if (normalized === 'AT+QNWINFO') fields = parseQNWINFO(lines)
  else if (normalized === 'AT+CNUM') fields = parseCNUM(lines)
  else if (normalized === 'AT+CPIN?') fields = parseNamedValue(lines, /^\+CPIN:\s*/, 'rawAt.fields.simState')
  else if (normalized === 'AT+CGATT?') fields = parsePacketAttach(lines)
  else if (normalized === 'AT+QCCID') fields = parseNamedValue(lines, /^\+QCCID:\s*/, 'rawAt.fields.iccid')
  else if (normalized === 'AT+CGSN') fields = parseNamedValue(lines, /^\+CGSN:\s*/, 'rawAt.fields.imei')
  else if (normalized === 'AT+CGMR') fields = parseNamedValue(lines, /^\+CGMR:\s*/, 'rawAt.fields.firmware')
  else if (normalized.startsWith('AT+QENG=')) fields = parseNamedValue(lines, /^\+QENG:\s*/, 'rawAt.fields.servingCell')
  if (!fields.length) fields = parseGeneric(lines)

  return { statusKey, fields }
}
