<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CheckOutlined,
  DeleteOutlined,
  DownloadOutlined,
  LinkOutlined,
  ReloadOutlined,
  SearchOutlined,
  SettingOutlined,
} from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import LoadingState from '../components/LoadingState.vue'
import OperationStatusView from '../components/OperationStatus.vue'
import EsimOperationDock from '../components/esim/EsimOperationDock.vue'
import { useViewContext } from './context'
import type { EsimNotification, EsimNotificationHistory } from '../types'
import { decodeEsimActivationImage } from '../services/esimQr'

const { t, te } = useI18n()
const {
  closeEsimDownload,
  closeEsimSettings,
  declineConfirmationCode,
  deleteEsim,
  device,
  disableEsim,
  downloadEsim,
  enableEsim,
  esim,
  esimActivationCode,
  esimConfirmationBusy,
  esimConfirmationCode,
  esimConfirmationInput,
  esimConfirmationOpen,
  esimDownloadOpen,
  esimDownloadPhase,
  esimFilteredNotificationHistory,
  esimFilteredNotifications,
  esimFilteredProfiles,
  esimFocusedICCID,
  esimHealth,
  esimHealthError,
  esimLabels,
  esimMatchingID,
  esimMetadataError,
  esimNotificationActionState,
  esimNotificationEventFilter,
  esimNotificationEvents,
  esimNotificationMode,
  esimNotificationHistory,
  esimNotificationProfileFilter,
  esimNotificationQuery,
  esimNotificationStateFilter,
  esimNotificationsError,
  esimNotificationsLoading,
  esimNotifications,
  esimOperation,
  esimOperationActive,
  esimOverviewError,
  esimProfileQuery,
  esimProfileStateFilter,
  esimSettingsOpen,
  esimSettingsICCID: settingsICCID,
  esimWorkspace,
  loadedViews,
  loadNotifications,
  loadView,
  localSimProfile,
  maskSensitive,
  simProfileSummary,
  openEsimDownload,
  openEsimSettings,
  processNotification,
  removeNotification,
  resetEsimDownloadForRetry,
  saveEsimProfileName,
  showEsimNotificationProfile,
  showEsimProfileNotifications,
  showEsimWorkspace,
  submitConfirmationCode,
  clearEsimNotificationProfileFilter,
} = useViewContext()

const qrInput = ref<HTMLInputElement | null>(null)
const qrError = ref('')
const qrPreviewURL = ref('')
const qrPreviewName = ref('')
const dismissedOperationID = ref('')

const activeProfile = computed(() => esim.value?.profiles.find((profile) => profile.state === 'enabled'))
const showOperationDock = computed(
  () => !!esimOperation.value && esimOperation.value.operation_id !== dismissedOperationID.value,
)
const healthLabel = computed(() => {
  if (esimHealthError.value) return t('esim.healthUnavailable')
  if (!esimHealth.value) return t('esim.healthLoading')
  return esimHealth.value.ok ? t('esim.healthReady') : t('esim.healthDegraded')
})

function eventLabel(event?: string) {
  if (!event) return t('esim.unknownEvent')
  const key = `esim.events.${event}`
  return te(key) ? t(key) : t('esim.unknownEvent')
}

function historyStateLabel(state?: string) {
  if (!state) return t('esim.stateUnknown')
  const key = `esim.states.${state}`
  return te(key) ? t(key) : t('esim.stateUnknown')
}

function formatTime(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleString()
}

function notificationAction(sequence: number, action: 'process' | 'remove') {
  return esimNotificationActionState.value[sequence]?.action === action
    ? esimNotificationActionState.value[sequence]
    : undefined
}

function dismissOperationDock() {
  dismissedOperationID.value = esimOperation.value?.operation_id || ''
}

function setQRPreview(file: File) {
  if (qrPreviewURL.value) URL.revokeObjectURL(qrPreviewURL.value)
  qrPreviewURL.value = URL.createObjectURL(file)
  qrPreviewName.value = file.name || t('esim.pastedQRImage')
}

async function decodeQRFromFile(file: File) {
  qrError.value = ''
  setQRPreview(file)
  const result = await decodeEsimActivationImage(file)
  if (result) {
    esimActivationCode.value = result
  } else {
    qrError.value = t('esim.qrDecodeFailed')
  }
}

onBeforeUnmount(() => {
  if (qrPreviewURL.value) URL.revokeObjectURL(qrPreviewURL.value)
})

function pickQRImage() {
  qrError.value = ''
  qrInput.value?.click()
}

async function onQRFileSelected(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (file) await decodeQRFromFile(file)
}

function onDownloadPaste(event: ClipboardEvent) {
  const item = Array.from(event.clipboardData?.items || []).find((entry) => entry.type.startsWith('image/'))
  const file = item?.getAsFile()
  if (file) {
    event.preventDefault()
    void decodeQRFromFile(file)
  }
}

function onDownloadDrop(event: globalThis.DragEvent) {
  event.preventDefault()
  const file = event.dataTransfer?.files?.[0]
  if (file) {
    void decodeQRFromFile(file)
    return
  }
  const text = event.dataTransfer?.getData('text/plain')?.trim()
  if (text) esimActivationCode.value = text
}

function profileName(profile: { iccid?: string; label?: string }) {
  return profile.label || t('esim.unnamed')
}

function isBootstrapProfile(profile: {
  profile_class?: string
  label?: string
  service_provider_name?: string
}) {
  return [profile.profile_class, profile.label, profile.service_provider_name]
    .filter(Boolean)
    .some((value) =>
      ['bootstrap', 'bootstrap profile', 'provisioning'].includes(String(value).trim().toLocaleLowerCase()),
    )
}

function notificationProfile(item: EsimNotification | EsimNotificationHistory) {
  return item.iccid ? maskSensitive(item.iccid) : t('esim.profileNotSpecified')
}
</script>

<template>
  <section class="esim-workbench">
    <section class="esim-summary" aria-labelledby="esim-summary-title">
      <div class="esim-summary-heading">
        <div>
          <span class="eyebrow">{{ t('esim.eyebrow') }}</span>
          <h2 id="esim-summary-title">{{ t('esim.workbenchTitle') }}</h2>
          <p>{{ t('esim.workbenchDetail') }}</p>
        </div>
        <div class="esim-summary-actions">
          <a-button @click="loadView('esim')"><ReloadOutlined />{{ t('common.refresh') }}</a-button>
          <a-button type="primary" :disabled="!device.has('esim')" @click="openEsimDownload">
            <DownloadOutlined />{{ t('common.download') }}
          </a-button>
        </div>
      </div>
      <LoadingState v-if="!loadedViews.esim" :label="t('esim.loading')" />
      <template v-else-if="!device.has('esim')">
        <a-alert
          class="esim-state-alert esim-unavailable-alert"
          type="warning"
          show-icon
          :message="`${t('esim.unavailable')}: ${t('esim.unavailableDetail')}`"
        />
      </template>
      <template v-else-if="esim">
        <div class="esim-summary-grid">
          <div class="esim-summary-item">
            <span>{{ t('esim.eid') }}</span
            ><strong class="mono">{{ maskSensitive(esim.eid) }}</strong>
          </div>
          <div class="esim-summary-item">
            <span>{{ t('esim.deviceSKU') }}</span
            ><strong>{{ esim.device_info?.sku_name || t('common.unknown') }}</strong>
          </div>
          <div class="esim-summary-item">
            <span>{{ t('esim.deviceSerial') }}</span
            ><strong class="mono">{{ maskSensitive(esim.device_info?.serial_number) }}</strong>
          </div>
          <div class="esim-summary-item">
            <span>{{ t('esim.deviceFirmware') }}</span
            ><strong>{{ esim.device_info?.firmware || t('common.unknown') }}</strong>
          </div>
          <div class="esim-summary-item">
            <span>{{ t('esim.activeProfile') }}</span
            ><strong>{{ activeProfile ? profileName(activeProfile) : t('esim.none') }}</strong>
          </div>
          <div class="esim-summary-item">
            <span>{{ t('esim.health') }}</span
            ><strong>{{ healthLabel }}</strong>
          </div>
          <div class="esim-summary-item">
            <span>{{ t('esim.freeCardSpace') }}</span
            ><strong>{{ esim.free_nvram || t('esim.spaceUnavailable') }}</strong>
          </div>
        </div>
        <a-alert
          v-if="esim.card_type === 'physical_sim'"
          class="esim-state-alert"
          type="info"
          show-icon
          :message="t('esim.physical')"
          :description="t('esim.physicalDetail')"
        />
        <a-alert
          v-else-if="esim.card_type === 'unknown'"
          class="esim-state-alert esim-unavailable-alert"
          type="warning"
          show-icon
          :message="`${t('esim.unavailable')}: ${t('esim.unavailableDetail')}`"
        />
        <a-alert
          v-else-if="!esim.profiles.length"
          class="esim-state-alert"
          type="info"
          show-icon
          :message="t('esim.noProfiles')"
          :description="t('esim.noProfilesDetail')"
        />
        <a-alert
          v-if="esimHealthError"
          class="esim-state-alert"
          type="warning"
          show-icon
          :message="t('esim.healthUnavailable')"
          :description="t('esim.healthUnavailableDetail')"
        />
      </template>
      <a-alert
        v-if="esimOverviewError"
        class="esim-state-alert"
        type="error"
        show-icon
        :message="esimOverviewError"
      />
    </section>

    <EsimOperationDock
      v-if="showOperationDock && esimOperation"
      :operation="esimOperation"
      @close="dismissOperationDock"
    />

    <div class="esim-workspace-tabs" role="tablist" :aria-label="t('esim.workspaces')">
      <button
        type="button"
        role="tab"
        :aria-selected="esimWorkspace === 'profiles'"
        :class="{ active: esimWorkspace === 'profiles' }"
        @click="showEsimWorkspace('profiles')"
      >
        {{ t('esim.profilesWorkspace') }}
        <span class="esim-tab-count">{{ esim?.profiles.length || 0 }}</span>
      </button>
      <button
        type="button"
        role="tab"
        :aria-selected="esimWorkspace === 'notifications'"
        :class="{ active: esimWorkspace === 'notifications' }"
        @click="showEsimWorkspace('notifications')"
      >
        {{ t('esim.notificationsWorkspace') }}
        <span class="esim-tab-count">{{ esimNotifications.length }}</span>
      </button>
    </div>

    <section
      v-if="esimWorkspace === 'profiles'"
      class="esim-workspace"
      aria-labelledby="profiles-workspace-title"
    >
      <div class="esim-workspace-heading">
        <div>
          <span class="eyebrow">{{ t('esim.profileCard') }}</span>
          <h2 id="profiles-workspace-title">{{ t('esim.profilesWorkspace') }}</h2>
        </div>
        <div class="esim-filter-row">
          <a-input
            v-model:value="esimProfileQuery"
            class="esim-search"
            allow-clear
            :placeholder="t('esim.searchProfiles')"
            :aria-label="t('esim.searchProfiles')"
            ><template #prefix><SearchOutlined /></template
          ></a-input>
          <a-segmented
            v-model:value="esimProfileStateFilter"
            :options="[
              { value: 'all', label: t('esim.allStates') },
              { value: 'enabled', label: t('esim.enabled') },
              { value: 'disabled', label: t('esim.disabled') },
            ]"
          />
        </div>
      </div>
      <a-alert
        v-if="esimMetadataError"
        type="warning"
        show-icon
        :message="t('esim.unableMetadata')"
        :description="t('esim.metadataManagedHint')"
      />
      <div
        v-if="esim?.card_type === 'euicc' && esimFilteredProfiles.length"
        class="profile-grid esim-profile-grid"
      >
        <article
          v-for="profile in esimFilteredProfiles"
          :key="profile.aid + ':' + profile.iccid"
          class="profile-row esim-profile-card"
          :class="{ focused: esimFocusedICCID === profile.iccid }"
        >
          <div class="profile-card-head">
            <div>
              <span class="eyebrow">{{ t('esim.profileCard') }}</span>
              <h3>{{ profileName(profile) }}</h3>
            </div>
            <span class="profile-state" :class="profile.state">{{
              profile.state === 'enabled'
                ? t('esim.enabled')
                : profile.state === 'disabled'
                  ? t('esim.disabled')
                  : t('esim.stateUnavailable')
            }}</span>
          </div>
          <div class="profile-fields">
            <div>
              <span>{{ t('esim.iccid') }}</span
              ><strong class="mono">{{ maskSensitive(profile.iccid) }}</strong>
            </div>
            <div>
              <span>{{ t('esim.provider') }}</span
              ><strong>{{ profile.service_provider_name || t('esim.unknownProvider') }}</strong>
            </div>
            <div>
              <span>{{ t('esim.profileClass') }}</span
              ><strong>{{ profile.profile_class || t('esim.unknownClass') }}</strong>
            </div>
            <div>
              <span>{{ t('esim.localMetadata') }}</span
              ><strong>{{
                simProfileSummary(localSimProfile(profile.iccid)) || t('esim.noLocalNote')
              }}</strong>
            </div>
          </div>
          <div class="profile-actions">
            <a-button
              v-if="profile.state === 'enabled'"
              :disabled="esimOperationActive"
              @click="disableEsim(profile.iccid)"
              >{{ t('esim.disable') }}</a-button
            >
            <a-button
              v-else-if="profile.state === 'disabled'"
              type="primary"
              :disabled="esimOperationActive"
              @click="enableEsim(profile.iccid)"
              ><CheckOutlined />{{ t('esim.enable') }}</a-button
            >
            <span v-else class="esim-action-note">{{ t('esim.stateUnavailable') }}</span>
            <a-button :disabled="esimOperationActive" @click="openEsimSettings(profile.iccid)"
              ><SettingOutlined />{{ t('esim.editProfile') }}</a-button
            >
            <a-button @click="showEsimProfileNotifications(profile.iccid)"
              ><LinkOutlined />{{ t('esim.relatedNotifications') }}</a-button
            >
            <a-popconfirm
              v-if="profile.state === 'disabled' && !isBootstrapProfile(profile)"
              :title="t('esim.deleteConfirmTarget', { name: profileName(profile) })"
              :description="t('esim.deleteConfirmDetail')"
              @confirm="deleteEsim(profile.iccid)"
              ><a-button danger :disabled="esimOperationActive"
                ><DeleteOutlined />{{ t('common.delete') }}</a-button
              ></a-popconfirm
            >
            <span v-else-if="isBootstrapProfile(profile)" class="esim-action-note">{{
              t('esim.bootstrapCannotDelete')
            }}</span>
            <span v-else-if="profile.state === 'enabled'" class="esim-action-note">{{
              t('esim.disableBeforeDelete')
            }}</span>
          </div>
        </article>
      </div>
      <EmptyState
        v-else-if="esim && esim.profiles.length && !esimFilteredProfiles.length"
        :title="t('esim.noProfileMatches')"
        :detail="t('esim.noProfileMatchesDetail')"
      />
      <EmptyState
        v-else-if="esim?.card_type === 'euicc'"
        :title="t('esim.noProfiles')"
        :detail="t('esim.noProfilesDetail')"
        ><template #actions
          ><a-button type="primary" @click="openEsimDownload"
            ><DownloadOutlined />{{ t('common.download') }}</a-button
          ></template
        ></EmptyState
      >
    </section>

    <section v-else class="esim-workspace" aria-labelledby="notifications-workspace-title">
      <div class="esim-workspace-heading">
        <div>
          <span class="eyebrow">{{ t('esim.notifications') }}</span>
          <h2 id="notifications-workspace-title">{{ t('esim.notificationsWorkspace') }}</h2>
        </div>
        <a-button :loading="esimNotificationsLoading" @click="loadNotifications"
          ><ReloadOutlined />{{ t('common.refresh') }}</a-button
        >
      </div>
      <div class="notification-mode-row">
        <a-segmented
          v-model:value="esimNotificationMode"
          :options="[
            { value: 'pending', label: `${t('esim.pending')} (${esimNotifications.length})` },
            { value: 'history', label: `${t('esim.history')} (${esimNotificationHistory.length})` },
          ]"
        />
      </div>
      <div class="esim-filter-row notification-filters">
        <a-input
          v-model:value="esimNotificationQuery"
          allow-clear
          :placeholder="t('esim.searchNotifications')"
          :aria-label="t('esim.searchNotifications')"
          ><template #prefix><SearchOutlined /></template></a-input
        ><a-select
          v-model:value="esimNotificationEventFilter"
          allow-clear
          :placeholder="t('esim.filterEvent')"
          :options="esimNotificationEvents.map((event) => ({ value: event, label: eventLabel(event) }))"
        /><a-select
          v-if="esimNotificationMode === 'history'"
          v-model:value="esimNotificationStateFilter"
          allow-clear
          :placeholder="t('esim.filterState')"
          :options="[
            { value: 'pending', label: historyStateLabel('pending') },
            { value: 'processed', label: historyStateLabel('processed') },
            { value: 'failed', label: historyStateLabel('failed') },
            { value: 'removed', label: historyStateLabel('removed') },
          ]"
        />
      </div>
      <div v-if="esimNotificationProfileFilter" class="esim-filter-context">
        <span>{{ t('esim.filteredProfile', { id: maskSensitive(esimNotificationProfileFilter) }) }}</span
        ><a-button type="link" size="small" @click="clearEsimNotificationProfileFilter">{{
          t('esim.clearFilter')
        }}</a-button>
      </div>
      <a-alert
        v-if="esimNotificationsError"
        type="error"
        show-icon
        :message="t('esim.unableNotifications')"
        :description="t('esim.notificationsErrorDetail')"
      />
      <LoadingState v-else-if="esimNotificationsLoading" />
      <template v-else-if="esimNotificationMode === 'pending'">
        <div v-if="esimFilteredNotifications.length" class="notification-list">
          <article
            v-for="item in esimFilteredNotifications"
            :key="'pending-' + item.sequence_number"
            class="message-row notification-row"
          >
            <div class="notification-meta">
              <b>{{ eventLabel(item.event) }}</b
              ><span>{{ notificationProfile(item) }}</span
              ><span v-if="item.address" class="notification-address">{{ item.address }}</span>
            </div>
            <div class="notification-actions">
              <a-button
                v-if="item.can_retry"
                size="small"
                :loading="notificationAction(item.sequence_number, 'process')?.busy"
                @click="processNotification(item.sequence_number)"
                >{{ t('esim.process') }}</a-button
              ><a-popconfirm
                :title="t('esim.removeConfirm')"
                :description="t('esim.removeConfirmDetail')"
                @confirm="removeNotification(item.sequence_number)"
                ><a-button
                  size="small"
                  danger
                  :loading="notificationAction(item.sequence_number, 'remove')?.busy"
                  >{{ t('esim.remove') }}</a-button
                ></a-popconfirm
              ><a-button v-if="item.iccid" size="small" @click="showEsimNotificationProfile(item.iccid)">{{
                t('esim.viewProfile')
              }}</a-button>
            </div>
            <p
              v-if="
                notificationAction(item.sequence_number, 'process')?.error ||
                notificationAction(item.sequence_number, 'remove')?.error
              "
              class="notification-error"
            >
              {{ t('esim.notificationActionFailed') }}
            </p>
          </article>
        </div>
        <EmptyState
          v-else
          :title="
            esimNotificationProfileFilter || esimNotificationQuery || esimNotificationEventFilter
              ? t('esim.noNotificationMatches')
              : t('esim.noPendingNotifications')
          "
          :detail="t('esim.noPendingNotificationsDetail')"
        />
      </template>
      <template v-else>
        <div v-if="esimFilteredNotificationHistory.length" class="notification-list">
          <article
            v-for="item in esimFilteredNotificationHistory"
            :key="'history-' + item.sequence_number + '-' + item.event + '-' + item.iccid"
            class="message-row notification-row"
          >
            <div class="notification-meta">
              <b>{{ eventLabel(item.event) }}</b
              ><span class="profile-state">{{ historyStateLabel(item.state) }}</span
              ><span>{{ notificationProfile(item) }}</span
              ><span v-if="item.updated_at" class="notification-address">{{
                formatTime(item.updated_at)
              }}</span>
            </div>
            <a-button v-if="item.iccid" size="small" @click="showEsimNotificationProfile(item.iccid)">{{
              t('esim.viewProfile')
            }}</a-button>
          </article>
        </div>
        <EmptyState v-else :title="t('esim.noHistory')" :detail="t('esim.noHistoryDetail')" />
      </template>
    </section>

    <a-modal
      :open="esimDownloadOpen || esimConfirmationOpen"
      :title="t('esim.downloadTitle')"
      :footer="null"
      :closable="!esimConfirmationOpen"
      :mask-closable="!esimConfirmationOpen"
      destroy-on-close
      @cancel="closeEsimDownload"
    >
      <template v-if="esimDownloadPhase === 'input'">
        <a-form layout="vertical" @submit.prevent="downloadEsim" @paste="onDownloadPaste">
          <a-form-item :label="t('esim.activationCode')"
            ><a-input v-model:value="esimActivationCode" required placeholder="LPA:1$..."
          /></a-form-item>
          <div class="qr-drop-zone" @dragover.prevent @drop="onDownloadDrop">
            <input
              ref="qrInput"
              type="file"
              accept="image/*"
              class="qr-file-input"
              @change="onQRFileSelected"
            /><a-button size="small" @click="pickQRImage">{{ t('esim.scanQR') }}</a-button
            ><span>{{ t('esim.dropQR') }}</span>
          </div>
          <figure v-if="qrPreviewURL" class="qr-preview">
            <img :src="qrPreviewURL" :alt="t('esim.qrPreview')" />
            <figcaption>{{ qrPreviewName }}</figcaption>
          </figure>
          <p v-if="qrError" class="qr-error">{{ qrError }}</p>
          <a-form-item :label="t('esim.confirmationCode')"
            ><a-input
              v-model:value="esimConfirmationCode"
              :placeholder="t('esim.confirmationCodePlaceholder')" /></a-form-item
          ><a-form-item :label="t('esim.matchingId')"
            ><a-input v-model:value="esimMatchingID" :placeholder="t('esim.matchingIdPlaceholder')"
          /></a-form-item>
          <div class="modal-actions">
            <a-button @click="closeEsimDownload">{{ t('common.cancel') }}</a-button
            ><a-button
              type="primary"
              html-type="submit"
              :disabled="!device.has('esim') || !esimActivationCode.trim()"
              ><DownloadOutlined />{{ t('common.download') }}</a-button
            >
          </div>
        </a-form>
      </template>
      <template v-else>
        <p class="confirmation-hint">{{ t('esim.downloadProgressDetail') }}</p>
        <OperationStatusView
          v-if="esimOperation"
          :operation="esimOperation"
          :label="t('esim.downloadProgress')"
        />
        <a-alert
          v-if="esimConfirmationOpen"
          class="esim-confirmation-inline"
          type="info"
          show-icon
          :message="t('esim.confirmationTitle')"
          :description="t('esim.confirmationHint')"
          ><template #description
            ><div>{{ t('esim.confirmationHint') }}</div>
            <a-input
              v-model:value="esimConfirmationInput"
              autofocus
              :disabled="esimConfirmationBusy"
              @press-enter="submitConfirmationCode"
            />
            <div class="modal-actions">
              <a-button :disabled="esimConfirmationBusy" @click="declineConfirmationCode">{{
                t('common.cancel')
              }}</a-button
              ><a-button
                type="primary"
                :loading="esimConfirmationBusy"
                :disabled="!esimConfirmationInput.trim()"
                @click="submitConfirmationCode"
                >{{ t('common.confirm') }}</a-button
              >
            </div></template
          ></a-alert
        >
        <div
          v-if="
            esimDownloadPhase === 'terminal' &&
            esimOperation &&
            ['failed', 'cancelled'].includes(esimOperation.state)
          "
          class="modal-actions"
        >
          <a-button @click="resetEsimDownloadForRetry">{{ t('esim.retryDownload') }}</a-button
          ><a-button @click="closeEsimDownload">{{ t('common.close') }}</a-button>
        </div>
      </template>
    </a-modal>

    <a-modal
      v-model:open="esimSettingsOpen"
      :title="t('esim.editProfile')"
      :footer="null"
      :width="520"
      destroy-on-close
      @cancel="closeEsimSettings"
    >
      <a-form layout="vertical" @submit.prevent="saveEsimProfileName"
        ><a-form-item :label="t('esim.profileNameCard')"
          ><a-input v-model:value="esimLabels[settingsICCID]" :maxlength="64"
        /></a-form-item>
        <p>{{ t('esim.metadataManagedHint') }}</p>
        <div class="modal-actions">
          <a-button @click="closeEsimSettings">{{ t('common.cancel') }}</a-button
          ><a-button type="primary" html-type="submit">{{ t('common.save') }}</a-button>
        </div></a-form
      >
    </a-modal>
  </section>
</template>
