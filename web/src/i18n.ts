import { createI18n } from 'vue-i18n'

const messages = {
  'en-US': {
    language: { en: 'English', zh: 'Chinese', title: 'Language', current: 'Current language', english: 'English', chinese: 'Chinese' },
    brand: { subtitle: 'Single-device control' },
    nav: { primary: 'Primary', overview: 'Overview', sms: 'Messages', esim: 'eSIM', network: 'Network', rawAt: 'AT debug', vowifi: 'VoWiFi', settings: 'Settings' },
    settings: { title: 'Settings', appearance: 'Appearance', languageDetail: 'Choose the language used by the management interface.', saved: 'Language preference saved.' },
    common: {
      retry: 'Retry', refresh: 'Refresh', rescan: 'Rescan device', clear: 'Clear module SMS', send: 'Send message',
      cancel: 'Cancel', save: 'Save name', close: 'Close', download: 'Download profile', rename: 'Rename', enable: 'Enable',
      delete: 'Delete', execute: 'Execute', apply: 'Apply mode', check: 'Check connectivity', disable: 'Disable', reconnect: 'Reconnect',
      operation: 'Operation', unavailable: 'Unavailable', unknown: 'Unknown', notAvailable: 'Not available', empty: '—',
    },
    status: { live: 'Event stream live', reconnecting: 'Reconnecting', offline: 'Offline', inserted: 'Inserted', registered: 'Registered' },
    states: { absent: 'Absent', discovered: 'Discovered', connecting: 'Connecting', initializing: 'Initializing', ready: 'Ready', degraded: 'Degraded', disconnected: 'Disconnected' },
    header: { controlPlane: 'LOCAL CONTROL PLANE' },
    overview: {
      deviceStatus: 'DEVICE STATUS', noModem: 'No compatible modem detected', imei: 'IMEI', sim: 'SIM', registration: 'Registration',
      backend: 'Backend', radioNetwork: 'Radio and network', operator: 'Operator', signal: 'Signal', iccid: 'ICCID', capabilities: 'Capabilities',
      availableCapabilities: 'Available capabilities', serverReported: 'Server reported', capabilityReady: 'Capabilities appear when a device is ready.',
    },
    sms: {
      title: 'Messages', sendTitle: 'Send message', recipient: 'Recipient', message: 'Message', unknownSender: 'Unknown sender', copyCode: 'Copy', codeCopied: 'Verification code copied.', codeCopyFailed: 'Unable to copy the verification code.',
      backendContent: 'Message content is available from the device backend.', noMessages: 'No messages loaded. Refresh to read module history.',
      refreshed: 'Module history refreshed; inbox cache retained for this run', cleared: 'ME storage cleared; inbox cache was kept',
      accepted: 'Operation {id} accepted', sendStatus: 'Send status', code: 'Code {code}', part: 'Part {current}/{total}',
      unableLoad: 'Unable to load messages', unableRefresh: 'Unable to refresh messages', unableClear: 'Unable to clear module messages', unableSend: 'Unable to send message',
    },
    esim: {
      eyebrow: 'EUICC', profiles: 'Profiles', eid: 'EID', physical: 'Physical SIM detected', physicalDetail: 'The server did not expose an eUICC profile service.',
      unavailable: 'eSIM state unavailable', unavailableDetail: 'The eUICC profile service could not be read.', unnamed: 'Unnamed profile', enabled: 'Enabled', disabled: 'Disabled', stateUnavailable: 'State unavailable',
      unknownProvider: 'Unknown provider', unknownClass: 'Unknown class', downloadTitle: 'Download profile', activationCode: 'Activation code', confirmationCode: 'Confirmation code', matchingId: 'Matching ID',
      renameTitle: 'Rename profile', profileName: 'Profile name', operationAccepted: 'Operation {id} accepted', nameUpdated: 'Profile name updated',
      noProfiles: 'No profiles loaded.', unableLoad: 'Unable to load eSIM', unableDownload: 'Unable to download profile', unableEnable: 'Unable to enable profile',
      unableRename: 'Unable to rename profile', unableDelete: 'Unable to delete profile',
    },
    network: {
      title: 'Network status', usbMode: 'USB mode', radioMode: 'Radio mode', interface: 'Interface', defaultRoute: 'Default route', addresses: 'Addresses', traffic: 'Traffic', currentDownload: 'Current download', currentUpload: 'Current upload', modes: { rmnet: 'RMNET', ecm: 'ECM', mbim: 'MBIM', rndis: 'RNDIS' },
      received: 'received', sent: 'sent', usbNetworkMode: 'USB network mode', mode: 'Mode', unavailableControl: 'Network mode control is not available for the current backend.',
      accepted: 'Operation {id} accepted; the modem may reconnect.', reboot: 'Reboot modem', rebootConfirm: 'The modem will briefly disconnect and re-enumerate over USB. Continue?', rebooting: 'The modem is rebooting.', rebootAccepted: 'Reboot accepted; refreshing device status shortly.', unableReboot: 'Unable to reboot the modem', unableLoad: 'Unable to load network', unableCheck: 'Unable to check network', unableChange: 'Unable to change network mode',
    },
    rawAt: {
      ready: 'CAPABILITY READY', unavailable: 'CAPABILITY UNAVAILABLE', title: 'AT debug', command: 'Command', preset: 'Preset command', selectPreset: 'Choose a command', unavailableDetail: 'Raw AT is not available for the current backend.', unableExecute: 'Unable to execute AT command',
      parsedTitle: 'Parsed output', rawTitle: 'Raw output', noParsedFields: 'No structured fields were detected.', status: { ok: 'OK', error: 'Error', unknown: 'Unknown' },
      presets: { basic: 'Basic test', imei: 'Read IMEI', firmware: 'Read firmware', sim: 'Read SIM state', iccid: 'Read ICCID', phone: 'Read phone number', signal: 'Read signal quality', registration: 'Read network registration', operator: 'Read operator', attach: 'Read packet attachment', network: 'Read network information', servingCell: 'Read serving cell' },
      fields: { response: 'Response', responseLine: 'Response line', imei: 'IMEI', firmware: 'Firmware', rssi: 'RSSI index', ber: 'BER', signalDbm: 'Signal', reportMode: 'Report mode', registration: 'Registration', lac: 'LAC', cellId: 'Cell ID', operatorMode: 'Operator mode', operatorFormat: 'Operator format', operator: 'Operator', accessTechnology: 'Access technology', band: 'Band', channel: 'Channel', phone: 'Phone number', numberType: 'Number type', simState: 'SIM state', packetAttach: 'Packet attachment', iccid: 'ICCID', servingCell: 'Serving cell' },
      values: { registered: 'Registered', searching: 'Searching', rejected: 'Registration rejected', notRegistered: 'Not registered', attached: 'Attached', detached: 'Detached' },
    },
    vowifi: {
      title: 'VoWiFi', availability: 'Availability', state: 'State', reason: 'Reason', available: 'Available', unavailable: 'Unavailable',
      unavailableControl: 'VoWiFi control is not available for the current platform or backend.', operationStatus: 'Operation status',
      unableLoad: 'Unable to load VoWiFi', unableUpdate: 'Unable to update VoWiFi',
    },
    errors: {
      invalid_request: 'The request is invalid.', unauthenticated: 'Local authentication is required.', not_found: 'The requested resource was not found.',
      device_offline: 'The device is offline.', operation_conflict: 'The device is busy with another operation.', operation_cancelled: 'The operation was cancelled.',
      operation_timeout: 'The operation timed out.', backend_unavailable: 'The device backend is unavailable.', transport_unavailable: 'The device transport is unavailable.',
      capability_not_supported: 'This capability is not available.', packet_tunnel_not_supported: 'Packet tunneling is not supported.', internal_error: 'An internal error occurred.',
      generic: 'The request could not be completed.', apiUnavailable: 'API unavailable',
    },
  },
  'zh-CN': {
    language: { en: '英文', zh: '中文', title: '语言', current: '当前语言', english: '英文', chinese: '中文' },
    brand: { subtitle: '单设备控制' },
    nav: { primary: '主导航', overview: '概览', sms: '消息', esim: 'eSIM', network: '网络', rawAt: 'AT 调试', vowifi: 'VoWiFi', settings: '设置' },
    settings: { title: '设置', appearance: '外观', languageDetail: '选择管理界面使用的语言。', saved: '语言偏好已保存。' },
    common: {
      retry: '重试', refresh: '刷新', rescan: '重新扫描设备', clear: '清理模块短信', send: '发送消息', cancel: '取消', save: '保存名称', close: '关闭', download: '下载 Profile', rename: '重命名', enable: '启用',
      delete: '删除', execute: '执行', apply: '应用模式', check: '检查连通性', disable: '停用', reconnect: '重新连接', operation: '操作', unavailable: '不可用', unknown: '未知', notAvailable: '不可用', empty: '—',
    },
    status: { live: '事件流已连接', reconnecting: '正在重连', offline: '离线', inserted: '已插入', registered: '已注册' },
    states: { absent: '缺失', discovered: '已发现', connecting: '连接中', initializing: '初始化中', ready: '就绪', degraded: '降级', disconnected: '已断开' },
    header: { controlPlane: '本地控制面' },
    overview: {
      deviceStatus: '设备状态', noModem: '未检测到兼容的模块', imei: 'IMEI', sim: 'SIM', registration: '注册状态', backend: '后端', radioNetwork: '无线与网络', operator: '运营商', signal: '信号', iccid: 'ICCID', capabilities: '能力', availableCapabilities: '可用能力', serverReported: '服务器报告', capabilityReady: '设备就绪后显示能力。',
    },
    sms: {
      title: '消息', sendTitle: '发送消息', recipient: '收件人', message: '消息', unknownSender: '未知发件人', copyCode: '复制', codeCopied: '验证码已复制。', codeCopyFailed: '无法复制验证码。', backendContent: '消息内容由设备后端提供。', noMessages: '暂无消息。刷新以读取模块历史记录。', refreshed: '模块历史已刷新；本次运行仍保留收件箱缓存', cleared: '已清理 ME 存储；收件箱缓存仍保留', accepted: '操作 {id} 已接受', sendStatus: '发送状态', code: '验证码 {code}', part: '第 {current}/{total} 段', unableLoad: '无法加载消息', unableRefresh: '无法刷新消息', unableClear: '无法清理模块短信', unableSend: '无法发送消息',
    },
    esim: {
      eyebrow: 'EUICC', profiles: 'Profiles', eid: 'EID', physical: '检测到实体 SIM', physicalDetail: '服务器未提供 eUICC Profile 服务。', unavailable: 'eSIM 状态不可用', unavailableDetail: '无法读取 eUICC Profile 服务。', unnamed: '未命名 Profile', enabled: '已启用', disabled: '已停用', stateUnavailable: '状态不可用', unknownProvider: '未知供应商', unknownClass: '未知类别', downloadTitle: '下载 Profile', activationCode: '激活码', confirmationCode: '确认码', matchingId: '匹配 ID', renameTitle: '重命名 Profile', profileName: 'Profile 名称', operationAccepted: '操作 {id} 已接受', nameUpdated: 'Profile 名称已更新', noProfiles: '暂无 Profile。', unableLoad: '无法加载 eSIM', unableDownload: '无法下载 Profile', unableEnable: '无法启用 Profile', unableRename: '无法重命名 Profile', unableDelete: '无法删除 Profile',
    },
    network: {
      title: '网络状态', usbMode: 'USB 模式', radioMode: '无线模式', interface: '接口', defaultRoute: '默认路由', addresses: '地址', traffic: '流量', currentDownload: '当前下载', currentUpload: '当前上传', modes: { rmnet: 'RMNET', ecm: 'ECM', mbim: 'MBIM', rndis: 'RNDIS' }, received: '已接收', sent: '已发送', usbNetworkMode: 'USB 网络模式', mode: '模式', unavailableControl: '当前后端不支持网络模式控制。', accepted: '操作 {id} 已接受；模块可能会重新连接。', reboot: '重启模块', rebootConfirm: '模块会短暂断开并重新枚举 USB，是否继续？', rebooting: '模块正在重启。', rebootAccepted: '重启请求已接受，稍后刷新设备状态。', unableReboot: '无法重启模块', unableLoad: '无法加载网络', unableCheck: '无法检查网络', unableChange: '无法更改网络模式',
    },
    rawAt: {
      ready: '能力可用', unavailable: '能力不可用', title: 'AT 调试', command: '命令', preset: '预置命令', selectPreset: '选择命令', unavailableDetail: '当前后端不支持 Raw AT。', unableExecute: '无法执行 AT 命令',
      parsedTitle: '解析结果', rawTitle: '原始输出', noParsedFields: '未检测到结构化字段。', status: { ok: '成功', error: '错误', unknown: '未知' },
      presets: { basic: '基础测试', imei: '读取 IMEI', firmware: '读取固件版本', sim: '读取 SIM 状态', iccid: '读取 ICCID', phone: '读取本机号码', signal: '读取信号质量', registration: '读取网络注册状态', operator: '读取运营商', attach: '读取分组域附着状态', network: '读取网络信息', servingCell: '读取当前小区' },
      fields: { response: '响应', responseLine: '响应行', imei: 'IMEI', firmware: '固件版本', rssi: 'RSSI 索引', ber: '误码率', signalDbm: '信号', reportMode: '报告模式', registration: '注册状态', lac: '位置区代码', cellId: '小区 ID', operatorMode: '运营商模式', operatorFormat: '运营商格式', operator: '运营商', accessTechnology: '接入技术', band: '频段', channel: '信道', phone: '本机号码', numberType: '号码类型', simState: 'SIM 状态', packetAttach: '分组域附着', iccid: 'ICCID', servingCell: '当前小区' },
      values: { registered: '已注册', searching: '搜索中', rejected: '注册被拒绝', notRegistered: '未注册', attached: '已附着', detached: '未附着' },
    },
    vowifi: { title: 'VoWiFi', availability: '可用性', state: '状态', reason: '原因', available: '可用', unavailable: '不可用', unavailableControl: '当前平台或后端不支持 VoWiFi 控制。', operationStatus: '操作状态', unableLoad: '无法加载 VoWiFi', unableUpdate: '无法更新 VoWiFi' },
    errors: { invalid_request: '请求无效。', unauthenticated: '需要本地身份验证。', not_found: '未找到请求的资源。', device_offline: '设备处于离线状态。', operation_conflict: '设备正在执行其他操作。', operation_cancelled: '操作已取消。', operation_timeout: '操作超时。', backend_unavailable: '设备后端不可用。', transport_unavailable: '设备传输不可用。', capability_not_supported: '当前能力不可用。', packet_tunnel_not_supported: '不支持数据包隧道。', internal_error: '发生内部错误。', generic: '请求无法完成。', apiUnavailable: 'API 不可用' },
  },
} as const

export type SupportedLocale = 'en-US' | 'zh-CN'

const localeStorageKey = 'djonehub.locale'

function browserLocale(): SupportedLocale {
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-US'
}

function storedLocale(): SupportedLocale | undefined {
  const value = localStorage.getItem(localeStorageKey)
  return value === 'en-US' || value === 'zh-CN' ? value : undefined
}

export function persistLocale(value: string) {
  if (value === 'en-US' || value === 'zh-CN') localStorage.setItem(localeStorageKey, value)
}

export const i18n = createI18n({
  legacy: false,
  locale: storedLocale() || browserLocale(),
  fallbackLocale: 'en-US',
  messages,
})
