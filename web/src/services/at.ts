export interface ATField {
  labelKey: string
  value: string
  valueKey?: string
}

export interface ParsedATResponse {
  statusKey: 'rawAt.status.ok' | 'rawAt.status.error' | 'rawAt.status.unknown'
  fields: ATField[]
}

export interface RawATSMSDiagnostic {
  index: number
  indexes?: number[]
  status?: string
  tpdu_length?: number
  sender?: string
  body?: string
  received_at?: string
  concat_ref?: number
  part_number?: number
  total_parts?: number
  missing_parts?: number[]
  decode_error?: string
}

export interface ATPreset {
  id: string
  command: string
  labelKey: string
}

export const AT_PRESETS: ATPreset[] = [
  { id: 'basic', command: 'AT', labelKey: 'rawAt.presets.basic' },
  { id: 'imei', command: 'AT+CGSN', labelKey: 'rawAt.presets.imei' },
  { id: 'firmware', command: 'AT+QGMR', labelKey: 'rawAt.presets.firmware' },
  { id: 'sim', command: 'AT+CPIN?', labelKey: 'rawAt.presets.sim' },
  { id: 'iccid', command: 'AT+QCCID', labelKey: 'rawAt.presets.iccid' },
  { id: 'phone', command: 'AT+CNUM', labelKey: 'rawAt.presets.phone' },
  { id: 'signal', command: 'AT+CSQ', labelKey: 'rawAt.presets.signal' },
  { id: 'registration', command: 'AT+CREG?', labelKey: 'rawAt.presets.registration' },
  { id: 'operator', command: 'AT+COPS?', labelKey: 'rawAt.presets.operator' },
  { id: 'attach', command: 'AT+CGATT?', labelKey: 'rawAt.presets.attach' },
  { id: 'network', command: 'AT+QNWINFO', labelKey: 'rawAt.presets.network' },
  { id: 'servingCell', command: 'AT+QENG="servingcell"', labelKey: 'rawAt.presets.servingCell' },
  { id: 'smsStorage', command: 'AT+CPMS?', labelKey: 'rawAt.presets.smsStorage' },
  { id: 'smsFormat', command: 'AT+CMGF?', labelKey: 'rawAt.presets.smsFormat' },
  { id: 'smsNotifications', command: 'AT+CNMI?', labelKey: 'rawAt.presets.smsNotifications' },
  { id: 'smsList', command: 'AT+CMGL=4', labelKey: 'rawAt.presets.smsList' },
  { id: 'smsCenter', command: 'AT+CSCA?', labelKey: 'rawAt.presets.smsCenter' },
]

function linesOf(response: string): string[] {
  return response
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .filter((line) => !/^AT(?:\+|$)/i.test(line))
}

function quotedCSV(value: string): string[] {
  const values: string[] = []
  let field = ''
  let quoted = false
  for (let index = 0; index < value.length; index += 1) {
    const char = value[index]
    if (char === '"') {
      if (quoted && value[index + 1] === '"') {
        field += '"'
        index += 1
      } else {
        quoted = !quoted
      }
    } else if (char === ',' && !quoted) {
      values.push(field.trim())
      field = ''
    } else {
      field += char
    }
  }
  values.push(field.trim())
  return values
}

function responseValue(lines: string[], prefix: RegExp): string | undefined {
  const line = lines.find((item) => prefix.test(item))
  return line?.replace(prefix, '').trim()
}

function statusValue(value: string): string {
  return value === '1' || value === '5'
    ? 'rawAt.values.registered'
    : value === '2'
      ? 'rawAt.values.searching'
      : value === '3'
        ? 'rawAt.values.rejected'
        : value === '0' || value === '4'
          ? 'rawAt.values.notRegistered'
          : value
}

function parseCSQ(lines: string[]): ATField[] {
  const value = responseValue(lines, /^\+CSQ:\s*/)
  if (!value) return []
  const [rssi, ber] = value.split(',').map((item) => item.trim())
  const fields: ATField[] = [
    { labelKey: 'rawAt.fields.rssi', value: rssi || '—' },
    { labelKey: 'rawAt.fields.ber', value: ber || '—' },
  ]
  const numericRSSI = Number(rssi)
  if (Number.isInteger(numericRSSI) && numericRSSI >= 0 && numericRSSI <= 31) {
    fields.push({ labelKey: 'rawAt.fields.signalDbm', value: String(-113 + numericRSSI * 2) + ' dBm' })
  }
  return fields
}

function parseCREG(lines: string[]): ATField[] {
  const value = responseValue(lines, /^\+(?:C|CG)REG:\s*/)
  if (!value) return []
  const [mode, registration, lac, cellID] = value.split(',').map((item) => item.trim().replace(/^"|"$/g, ''))
  return [
    { labelKey: 'rawAt.fields.reportMode', value: mode || '—' },
    {
      labelKey: 'rawAt.fields.registration',
      value: registration || '—',
      valueKey: statusValue(registration || ''),
    },
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
  const scalar = lines.find((line) => line !== 'OK' && line !== 'ERROR' && !/^\+C(?:ME|MS) ERROR/i.test(line))
  return scalar ? [{ labelKey, value: scalar }] : []
}

function parsePacketAttach(lines: string[]): ATField[] {
  const fields = parseNamedValue(lines, /^\+CGATT:\s*/, 'rawAt.fields.packetAttach')
  const field = fields[0]
  if (field)
    field.valueKey =
      field.value === '1'
        ? 'rawAt.values.attached'
        : field.value === '0'
          ? 'rawAt.values.detached'
          : undefined
  return fields
}

function parseCPMS(lines: string[]): ATField[] {
  const value = responseValue(lines, /^\+CPMS:\s*/)
  if (!value) return []
  const values = quotedCSV(value)
  const labels = [
    'rawAt.fields.smsReadStorage',
    'rawAt.fields.smsWriteStorage',
    'rawAt.fields.smsReceiveStorage',
  ]
  const fields: ATField[] = []
  for (let index = 0; index < 3 && index * 3 < values.length; index += 1) {
    const [storage, used, total] = values.slice(index * 3, index * 3 + 3)
    if (!storage && used === undefined && total === undefined) continue
    const usage = used !== undefined && total !== undefined ? `${used || '0'} / ${total || '0'}` : ''
    fields.push({ labelKey: labels[index], value: [storage || '—', usage].filter(Boolean).join(' · ') })
  }
  return fields
}

function parseCMGF(lines: string[]): ATField[] {
  const value = responseValue(lines, /^\+CMGF:\s*/)
  if (!value) return []
  return [
    {
      labelKey: 'rawAt.fields.smsFormat',
      value,
      valueKey:
        value === '0' ? 'rawAt.values.smsPduMode' : value === '1' ? 'rawAt.values.smsTextMode' : undefined,
    },
  ]
}

function parseCNMI(lines: string[]): ATField[] {
  const value = responseValue(lines, /^\+CNMI:\s*/)
  if (!value) return []
  const [mode, message, cellBroadcast, statusReport, buffer] = quotedCSV(value)
  return [
    { labelKey: 'rawAt.fields.smsNotificationMode', value: mode || '—' },
    { labelKey: 'rawAt.fields.smsMessageNotification', value: message || '—' },
    { labelKey: 'rawAt.fields.smsCellBroadcast', value: cellBroadcast || '—' },
    { labelKey: 'rawAt.fields.smsStatusReport', value: statusReport || '—' },
    { labelKey: 'rawAt.fields.smsNotificationBuffer', value: buffer || '—' },
  ]
}

function smsStatusValue(status: string): string | undefined {
  const normalized = status.trim().toUpperCase()
  if (normalized === '0' || normalized === 'REC UNREAD') return 'rawAt.values.smsUnread'
  if (normalized === '1' || normalized === 'REC READ') return 'rawAt.values.smsRead'
  if (normalized === '2' || normalized === 'STO UNSENT') return 'rawAt.values.smsUnsent'
  if (normalized === '3' || normalized === 'STO SENT') return 'rawAt.values.smsSent'
  return undefined
}

function parseCMGL(lines: string[]): ATField[] {
  let count = 0
  for (const line of lines) if (/^\+CMGL:/i.test(line)) count += 1
  return [{ labelKey: 'rawAt.fields.smsRecordCount', value: String(count) }]
}

function parseCMGR(lines: string[]): ATField[] {
  const headerIndex = lines.findIndex((line) => /^\+CMGR:/i.test(line))
  if (headerIndex < 0) return []
  const [status, alpha, length] = quotedCSV(lines[headerIndex].replace(/^\+CMGR:\s*/i, ''))
  const fields: ATField[] = [
    {
      labelKey: 'rawAt.fields.smsRecordStatus',
      value: status || '—',
      valueKey: smsStatusValue(status || ''),
    },
  ]
  if (alpha) fields.push({ labelKey: 'rawAt.fields.smsAlpha', value: alpha })
  if (length) fields.push({ labelKey: 'rawAt.fields.smsPduLength', value: length })
  return fields
}

function parseCSCA(lines: string[]): ATField[] {
  const value = responseValue(lines, /^\+CSCA:\s*/)
  if (!value) return []
  const [address, type] = quotedCSV(value)
  return [
    { labelKey: 'rawAt.fields.smsCenter', value: address || '—' },
    { labelKey: 'rawAt.fields.numberType', value: type || '—' },
  ]
}

function parseGeneric(lines: string[]): ATField[] {
  return lines
    .filter(
      (line) =>
        line !== 'OK' && line !== 'ERROR' && !/^\+CME ERROR/i.test(line) && !/^\+CMS ERROR/i.test(line),
    )
    .map((value, index) => ({
      labelKey: index === 0 ? 'rawAt.fields.response' : 'rawAt.fields.responseLine',
      value,
    }))
}

function decodedSMSFields(messages: RawATSMSDiagnostic[]): ATField[] {
  const fields: ATField[] = []
  for (const [position, message] of messages.entries()) {
    const indexes = message.indexes?.length ? message.indexes : [message.index]
    fields.push({
      labelKey: 'rawAt.fields.smsDecodedMessage',
      value: `#${position + 1} · ${indexes.join(', ')}`,
    })
    if (message.sender) fields.push({ labelKey: 'rawAt.fields.smsSender', value: message.sender })
    if (message.received_at)
      fields.push({
        labelKey: 'rawAt.fields.smsReceivedAt',
        value: new Date(message.received_at).toLocaleString(),
      })
    if (message.body) fields.push({ labelKey: 'rawAt.fields.smsBody', value: message.body })
    if (message.total_parts)
      fields.push({
        labelKey: 'rawAt.fields.smsConcat',
        value: message.missing_parts?.length
          ? `${message.total_parts - message.missing_parts.length}/${message.total_parts} · ref ${message.concat_ref || '—'} · missing: ${message.missing_parts.join(', ')}`
          : `${message.total_parts}/${message.total_parts} · ref ${message.concat_ref || '—'}`,
      })
    if (message.decode_error)
      fields.push({ labelKey: 'rawAt.fields.smsDecodeError', value: message.decode_error })
  }
  return fields
}

export function parseATResponse(
  command: string,
  response: string,
  smsMessages: RawATSMSDiagnostic[] = [],
): ParsedATResponse {
  const lines = linesOf(response)
  const normalized = command.trim().toUpperCase()
  const hasError = lines.some((line) => line === 'ERROR' || /^\+C(?:ME|MS) ERROR/i.test(line))
  const statusKey = hasError
    ? 'rawAt.status.error'
    : lines.includes('OK')
      ? 'rawAt.status.ok'
      : 'rawAt.status.unknown'
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
  else if (normalized === 'AT+QGMR' || normalized === 'AT+CGMR')
    fields = parseNamedValue(lines, /^\+(?:QGMR|CGMR):\s*/, 'rawAt.fields.firmware')
  else if (normalized.startsWith('AT+QENG='))
    fields = parseNamedValue(lines, /^\+QENG:\s*/, 'rawAt.fields.servingCell')
  else if (normalized === 'AT+CPMS?') fields = parseCPMS(lines)
  else if (normalized === 'AT+CMGF?') fields = parseCMGF(lines)
  else if (normalized === 'AT+CNMI?') fields = parseCNMI(lines)
  else if (/^AT\+CMGL=/.test(normalized)) fields = parseCMGL(lines)
  else if (/^AT\+CMGR=\d+$/.test(normalized)) fields = parseCMGR(lines)
  else if (normalized === 'AT+CSCA?') fields = parseCSCA(lines)
  if (!fields.length) fields = parseGeneric(lines)
  if (smsMessages.length) fields = [...fields, ...decodedSMSFields(smsMessages)]

  return { statusKey, fields }
}
