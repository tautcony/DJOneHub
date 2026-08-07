import { inject, type ComputedRef, type InjectionKey, type Ref } from 'vue'
import type { ATPreset, ParsedATResponse } from '../services/at'
import type { useDeviceStore } from '../stores/device'
import type {
  CallStatus,
  EsimHealth,
  EsimNotification,
  SimCard,
  EsimNotificationHistory,
  EsimOverview,
  FirmwareStatus,
  NetworkStatus,
  NetworkTrafficRange,
  NotificationDebugEvent,
  NotificationDebugInfo,
  NotificationPermissionStatus,
  NotificationPreferences,
  OperationStatus,
  SMSMessage,
  StartupStatus,
  VowifiStatus,
} from '../types'
import type {
  EsimDownloadPhase,
  EsimWorkspace,
  NotificationActionState,
  NotificationMode,
  ProfileStateFilter,
} from '../stores/esim'
import type { ViewID } from '../router'

// The shell owns device polling and actions; routed views consume this shared,
// fully typed surface instead of an untyped Record. Each member mirrors a
// value provided by App.vue; views never reach into the application root.
export type ViewContext = {
  device: ReturnType<typeof useDeviceStore>
  deviceCapabilities: ComputedRef<Record<string, string>>
  loadView: (view: ViewID) => Promise<void>
  loadedViews: Ref<Partial<Record<ViewID, boolean>>>

  // network / traffic
  networkTraffic: Ref<{
    rxRate: number
    txRate: number
    rxBytes: number
    txBytes: number
    dailyAvailable: boolean
    sampledAt: string
    date: string
  }>
  trafficHistory: Ref<Array<{ at: number; rxRate: number; txRate: number }>>
  trafficRangeData: Ref<NetworkTrafficRange | null>
  loadTrafficRange: (period: 'day' | 'week' | 'month') => Promise<void>
  overviewNetwork: Ref<NetworkStatus | null>
  network: Ref<NetworkStatus | null>
  networkMode: Ref<string>
  checkNetwork: () => Promise<void>
  loadNetwork: () => Promise<void>
  rebootModule: () => Promise<void>
  setNetworkMode: () => Promise<void>
  usbNetworkModeLabel: (mode?: string) => string
  usbNetworkModeOptions: ComputedRef<Array<{ value: string; label: string }>>

  // device / shell
  showSensitive: Ref<boolean>
  stateLabel: ComputedRef<string>
  stateTone: ComputedRef<'success' | 'warning' | 'danger' | 'info' | 'neutral'>
  maskSensitive: (value?: string) => string

  // calls
  calls: Ref<CallStatus | null>
  callsDialOpen: Ref<boolean>
  dialNumber: Ref<string>
  dialCall: () => Promise<void>
  dialCallBusy: Ref<boolean>
  dialWaiting: Ref<boolean>
  openCallsDial: () => void
  closeCallsDial: () => void
  rejectCall: () => Promise<void>

  // SMS
  clearModuleSMS: () => Promise<void>
  filteredSmsThreads: ComputedRef<SmsThread[]>
  refreshSMS: () => Promise<void>
  selectedSmsPeer: Ref<string>
  selectedSmsThread: ComputedRef<SmsThread | undefined>
  resetSMSOperation: () => void
  smsComposeNew: Ref<boolean>
  sendSMS: () => Promise<void>
  smsBody: Ref<string>
  smsOperation: ComputedRef<OperationStatus | undefined>
  smsQuery: Ref<string>
  smsSimFilter: Ref<string>
  smsThreads: ComputedRef<SmsThread[]>
  smsTo: Ref<string>
  startNewSMS: () => void

  // eSIM
  closeEsimDownload: () => void
  closeEsimSettings: () => void
  deleteEsim: (iccid?: string) => Promise<void>
  disableEsim: (iccid?: string) => Promise<void>
  downloadEsim: () => Promise<void>
  enableEsim: (iccid?: string) => Promise<void>
  esim: Ref<EsimOverview | null>
  esimOverviewError: Ref<string>
  esimNotesError: Ref<string>
  esimHealthError: Ref<string>
  esimNotificationsError: Ref<string>
  esimNotificationsLoading: Ref<boolean>
  esimWorkspace: Ref<EsimWorkspace>
  esimNotificationMode: Ref<NotificationMode>
  esimProfileQuery: Ref<string>
  esimProfileStateFilter: Ref<ProfileStateFilter>
  esimNotificationQuery: Ref<string>
  esimNotificationEventFilter: Ref<string>
  esimNotificationProfileFilter: Ref<string>
  esimNotificationStateFilter: Ref<string>
  esimFocusedICCID: Ref<string>
  esimFilteredProfiles: ComputedRef<EsimOverview['profiles']>
  esimFilteredNotifications: ComputedRef<EsimNotification[]>
  esimFilteredNotificationHistory: ComputedRef<EsimNotificationHistory[]>
  esimNotificationEvents: ComputedRef<string[]>
  esimOperationActive: ComputedRef<boolean>
  esimHealth: Ref<EsimHealth | null>
  esimActivationCode: Ref<string>
  esimConfirmationCode: Ref<string>
  esimDownloadOpen: Ref<boolean>
  esimLabels: Ref<Record<string, string>>
  esimMatchingID: Ref<string>
  esimOperation: ComputedRef<OperationStatus | undefined>
  esimSettingsOpen: Ref<boolean>
  esimSettingsICCID: Ref<string>
  esimNotifications: Ref<EsimNotification[]>
  esimNotificationHistory: Ref<EsimNotificationHistory[]>
  esimNotificationBusy: Ref<boolean>
  esimNotificationActionState: Ref<Record<number, NotificationActionState>>
  esimDownloadPhase: Ref<EsimDownloadPhase>
  esimConfirmationOpen: Ref<boolean>
  esimConfirmationOperationID: Ref<string>
  esimConfirmationInput: Ref<string>
  esimConfirmationBusy: Ref<boolean>
  loadNotifications: () => Promise<void>
  refreshEsimSnapshots: () => Promise<void>
  refreshEsimAfterOperation: () => Promise<void>
  processNotification: (sequence: number) => Promise<void>
  removeNotification: (sequence: number) => Promise<void>
  submitConfirmationCode: () => Promise<void>
  declineConfirmationCode: () => Promise<void>
  localProfileNote: (iccid?: string) => ProfileNote | undefined
  noteLabel: Ref<string>
  notePhone: Ref<string>
  noteTags: Ref<string>
  noteSummary: (note?: ProfileNote) => string
  openEsimDownload: () => void
  resetEsimDownloadForRetry: () => void
  openEsimSettings: (iccid?: string) => void
  showEsimWorkspace: (workspace: EsimWorkspace) => void
  showEsimProfileNotifications: (iccid?: string) => void
  showEsimNotificationProfile: (iccid?: string) => void
  clearEsimNotificationProfileFilter: () => void
  saveProfileNote: () => Promise<void>

  // SIM cards
  simCards: Ref<SimCard[]>
  simCardsBusy: Ref<boolean>
  createSimCard: (input: {
    iccid: string
    imsi: string
    msisdn: string
    name: string
    notes: string
  }) => Promise<void>
  updateSimCard: (iccid: string, input: { name: string; notes: string; msisdn: string }) => Promise<void>
  deleteSimCard: (iccid: string) => Promise<void>

  // VoWiFi
  loadVowifi: () => Promise<void>
  runVowifi: (action: 'enable' | 'disable' | 'reconnect') => Promise<void>
  vowifi: Ref<VowifiStatus | null>
  vowifiOperation: ComputedRef<OperationStatus | undefined>

  // raw AT
  AT_PRESETS: ATPreset[]
  applyATPreset: () => void
  executeRawAT: () => Promise<void>
  parsedATResponse: ComputedRef<ParsedATResponse | null>
  rawATCommand: Ref<string>
  rawATPreset: Ref<string>
  rawATResponse: Ref<string>

  // firmware
  firmware: Ref<FirmwareStatus | null>
  firmwareOperation: ComputedRef<OperationStatus | undefined>
  firmwareOperationLogs: ComputedRef<string[]>
  firmwareOperationModalOpen: Ref<boolean>
  refreshFirmware: () => Promise<void>
  runFirmwareAction: (action: 'unlock' | 'enable' | 'disable' | 'edl', serial?: string) => Promise<void>
  updateFirmwareUSBID: (vid: string, pid: string) => Promise<void>
  backupFirmware: (
    outputPath: string,
    loaderPath: string,
    edlPath: string,
    edlRunner: 'python' | 'uv',
  ) => Promise<void>
  selectFirmwareBackupDirectory: () => Promise<string>
  selectFirmwareEDLDirectory: () => Promise<string>
  selectFirmwareADBFile: () => Promise<string>
  saveFirmwareADBCommand: (command: string) => Promise<string>

  // notifications / settings
  debugEventData: (event: NotificationDebugEvent) => string
  loadNotificationPermissions: () => Promise<void>
  newNotifierCall: () => void
  notifierBody: Ref<string>
  notifierCallID: Ref<string>
  notifierEvents: Ref<NotificationDebugEvent[]>
  notifierInfo: Ref<NotificationDebugInfo | null>
  notifierNumber: Ref<string>
  notifierRecipient: Ref<string>
  notifierSender: Ref<string>
  triggerNotifierDebug: (action: string) => Promise<void>
  locale: Ref<string>
  notificationPermissionBusy: Ref<boolean>
  notificationPermissionLabel: (state?: NotificationPermissionStatus['state']) => string
  notificationPermissions: Ref<NotificationPermissionStatus | null>
  notificationPreferences: Ref<NotificationPreferences | null>
  notificationPreferencesBusy: Ref<boolean>
  openNotificationSettings: () => Promise<void>
  requestNotificationPermission: () => Promise<void>
  saveNotificationPreferences: () => Promise<void>
  startupBusy: Ref<boolean>
  startupSettings: Ref<StartupStatus | null>
  toggleStartup: (enabled: boolean) => Promise<void>
}

export interface SmsThread {
  key: string
  peer: string
  iccid: string
  items: SMSMessage[]
  latest?: SMSMessage
}

export interface ProfileNote {
  label?: string
  phone?: string
  tags?: string
  profile_class?: string
}

export const viewContextKey: InjectionKey<ViewContext> = Symbol('djonehub.view-context')

export function useViewContext() {
  const context = inject(viewContextKey)
  if (!context) throw new Error('View context is unavailable')
  return context
}
