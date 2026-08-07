<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import jsQR from 'jsqr'
import {
  CheckOutlined,
  DeleteOutlined,
  DownloadOutlined,
  ReloadOutlined,
  SettingOutlined,
} from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import FieldRow from '../components/FieldRow.vue'
import LoadingState from '../components/LoadingState.vue'
import OperationStatusView from '../components/OperationStatus.vue'
import Panel from '../components/Panel.vue'
import { useViewContext } from './context'

const { t } = useI18n()
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
  esimLabels,
  esimMatchingID,
  esimNotificationBusy,
  esimNotificationHistory,
  esimNotifications,
  esimOperation,
  esimSettingsOpen,
  esimSettingsICCID: settingsICCID,
  loadNotifications,
  loadView,
  loadedViews,
  localProfileNote,
  maskSensitive,
  noteLabel,
  notePhone,
  noteTags,
  noteSummary,
  openEsimDownload,
  openEsimSettings,
  processNotification,
  removeNotification,
  saveProfileNote,
  submitConfirmationCode,
} = useViewContext()

// 二维码扫描：文件选择或剪贴板粘贴图片 → jsqr 解码 → 填入激活码输入框。
const qrInput = ref<HTMLInputElement | null>(null)
const qrError = ref('')

function pickQRImage() {
  qrError.value = ''
  qrInput.value?.click()
}

async function decodeQRFromFile(file: File) {
  const url = URL.createObjectURL(file)
  try {
    const image = new Image()
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve()
      image.onerror = () => reject(new Error('image load failed'))
      image.src = url
    })
    const canvas = document.createElement('canvas')
    canvas.width = image.naturalWidth
    canvas.height = image.naturalHeight
    const context = canvas.getContext('2d')
    if (!context) return false
    context.drawImage(image, 0, 0)
    const imageData = context.getImageData(0, 0, canvas.width, canvas.height)
    const result = jsQR(imageData.data, imageData.width, imageData.height)
    if (result?.data) {
      esimActivationCode.value = result.data
      return true
    }
    return false
  } catch {
    return false
  } finally {
    URL.revokeObjectURL(url)
  }
}

async function onQRFileSelected(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  const ok = await decodeQRFromFile(file)
  if (!ok) qrError.value = t('esim.qrDecodeFailed')
}

function historyStateLabel(state?: string) {
  switch (state) {
    case 'pending':
      return t('esim.statePending')
    case 'processed':
      return t('esim.stateProcessed')
    case 'failed':
      return t('esim.stateFailed')
    case 'removed':
      return t('esim.stateRemoved')
    default:
      return state || ''
  }
}

function onDownloadPaste(event: ClipboardEvent) {
  const item = Array.from(event.clipboardData?.items || []).find((entry) => entry.type.startsWith('image/'))
  const file = item?.getAsFile()
  if (!file) return
  void (async () => {
    const ok = await decodeQRFromFile(file)
    if (!ok) qrError.value = t('esim.qrDecodeFailed')
  })()
}
</script>

<template>
  <section class="view-grid">
    <Panel
      class="esim-panel"
      :eyebrow="t('esim.eyebrow')"
      :title="t('esim.profiles')"
      :meta="esim ? `${esim.profiles.length} ${t('esim.profiles')}` : undefined"
      ><template #actions
        ><a-space
          ><a-button @click="loadView('esim')"><ReloadOutlined />{{ t('common.refresh') }}</a-button
          ><a-button
            type="primary"
            shape="circle"
            :title="t('common.download')"
            :aria-label="t('common.download')"
            :disabled="!device.has('esim')"
            @click="openEsimDownload"
            ><DownloadOutlined /></a-button></a-space></template
      ><LoadingState v-if="!loadedViews.esim" /><template v-else
        ><div class="detail-list">
          <FieldRow :label="t('esim.eid')" :value="maskSensitive(esim?.eid)" monospace /><FieldRow
            :label="t('esim.profiles')"
            :value="esim?.profiles?.length || 0"
          />
        </div>
        <a-alert
          v-if="esim?.card_type === 'physical_sim'"
          class="esim-state-alert"
          type="info"
          show-icon
          :message="t('esim.physical')"
          :description="t('esim.physicalDetail')" /><a-alert
          v-else-if="esim?.card_type === 'unknown'"
          class="esim-state-alert"
          type="warning"
          show-icon
          :message="t('esim.unavailable')"
          :description="t('esim.unavailableDetail')" />
        <div v-else class="profile-grid">
          <div
            v-for="profile in esim?.profiles"
            :key="profile.aid + ':' + profile.iccid"
            class="message-row profile-row"
          >
            <div class="profile-card-head">
              <div>
                <span class="eyebrow">{{ t('esim.profileCard') }}</span>
                <h3>{{ profile.label || t('esim.unnamed') }}</h3>
              </div>
              <a-tag
                :color="
                  profile.state === 'enabled'
                    ? 'success'
                    : profile.state === 'disabled'
                      ? 'warning'
                      : 'default'
                "
                >{{
                  profile.state === 'enabled'
                    ? t('esim.enabled')
                    : profile.state === 'disabled'
                      ? t('esim.disabled')
                      : t('esim.stateUnavailable')
                }}</a-tag
              >
            </div>
            <div class="profile-fields">
              <div>
                <span>{{ t('esim.iccid') }}</span
                ><strong>{{ maskSensitive(profile.iccid) }}</strong>
              </div>
              <div>
                <span>{{ t('esim.provider') }}</span
                ><strong>{{ profile.service_provider_name || t('esim.unknownProvider') }}</strong>
              </div>
              <div>
                <span>{{ t('esim.profileClass') }}</span
                ><strong>{{ profile.profile_class || t('esim.unknownClass') }}</strong>
              </div>
              <div v-if="profile.aid">
                <span>{{ t('esim.aid') }}</span
                ><strong>{{ maskSensitive(profile.aid) }}</strong>
              </div>
            </div>
            <div v-if="noteSummary(localProfileNote(profile.iccid))" class="profile-notes">
              <div class="profile-note local-note">
                <b>{{ t('esim.localNote') }}</b
                >{{ noteSummary(localProfileNote(profile.iccid)) }}
              </div>
            </div>
            <div class="profile-actions">
              <a-button @click="openEsimSettings(profile.iccid)"
                ><SettingOutlined />{{ t('common.settings') }}</a-button
              ><a-popconfirm
                v-if="profile.state === 'enabled'"
                :title="t('esim.disableConfirm')"
                @confirm="disableEsim(profile.iccid)"
                ><a-button>{{ t('esim.disable') }}</a-button></a-popconfirm
              ><a-button v-if="profile.state === 'disabled'" type="primary" @click="enableEsim(profile.iccid)"
                ><CheckOutlined />{{ t('common.enable') }}</a-button
              ><a-popconfirm
                v-if="profile.state !== 'enabled'"
                :title="t('esim.deleteConfirm')"
                @confirm="deleteEsim(profile.iccid)"
                ><a-button danger><DeleteOutlined />{{ t('common.delete') }}</a-button></a-popconfirm
              >
            </div>
          </div>
          <EmptyState v-if="!esim?.profiles?.length" :title="t('esim.noProfiles')" />
        </div>
        <OperationStatusView :operation="esimOperation" :label="t('common.operation')" /></template
    ></Panel>
    <Panel
      class="esim-notifications-panel"
      :eyebrow="t('esim.eyebrow')"
      :title="t('esim.notifications')"
      :meta="esimNotifications.length ? `${esimNotifications.length}` : undefined"
      ><template #actions
        ><a-button size="small" :disabled="esimNotificationBusy" @click="loadNotifications"
          ><ReloadOutlined /></a-button></template
      ><div v-if="!esimNotifications.length" class="notifications-empty">{{
        t('esim.noNotifications')
      }}</div>
      <div v-else class="notification-list">
        <div
          v-for="item in esimNotifications"
          :key="'pending-' + item.sequence_number"
          class="message-row notification-row"
        >
          <div class="notification-meta">
            <b>{{ item.event }}</b>
            <span v-if="item.iccid">{{ maskSensitive(item.iccid) }}</span>
            <span v-if="item.address" class="notification-address">{{ item.address }}</span>
          </div>
          <a-space>
            <a-button
              size="small"
              :disabled="esimNotificationBusy"
              @click="processNotification(item.sequence_number)"
              >{{ t('esim.retry') }}</a-button
            ><a-button
              size="small"
              danger
              :disabled="esimNotificationBusy"
              @click="removeNotification(item.sequence_number)"
              >{{ t('common.delete') }}</a-button
            >
          </a-space>
        </div>
      </div>
      <h3 class="notification-history-title">{{ t('esim.historyTitle') }}</h3>
      <div v-if="!esimNotificationHistory.length" class="notifications-empty">{{
        t('esim.noHistory')
      }}</div>
      <div v-else class="notification-list">
        <div
          v-for="item in esimNotificationHistory"
          :key="'history-' + item.sequence_number + '-' + item.event + '-' + (item.iccid || '')"
          class="message-row notification-row"
        >
          <div class="notification-meta">
            <b>{{ item.event }}</b>
            <a-tag
              :color="
                item.state === 'processed'
                  ? 'success'
                  : item.state === 'failed'
                    ? 'error'
                    : item.state === 'removed'
                      ? 'default'
                      : 'warning'
              "
              >{{ historyStateLabel(item.state) }}</a-tag
            >
            <span v-if="item.iccid">{{ maskSensitive(item.iccid) }}</span>
            <span v-if="item.updated_at" class="notification-address">{{
              new Date(item.updated_at).toLocaleString()
            }}</span>
          </div>
        </div>
      </div></Panel
    >
    <a-modal
      v-model:open="esimDownloadOpen"
      :title="t('esim.downloadTitle')"
      :footer="null"
      destroy-on-close
      @cancel="closeEsimDownload"
      ><a-form layout="vertical" @submit.prevent="downloadEsim" @paste="onDownloadPaste"
        ><a-form-item :label="t('esim.activationCode')"
          ><a-input v-model:value="esimActivationCode" required placeholder="LPA:1$..." /></a-form-item
        ><div class="qr-entry">
          <input
            ref="qrInput"
            type="file"
            accept="image/*"
            class="qr-file-input"
            @change="onQRFileSelected"
          />
          <a-button size="small" @click="pickQRImage">{{ t('esim.scanQR') }}</a-button>
          <span class="qr-hint">{{ t('esim.scanQRHint') }}</span>
        </div>
        <p v-if="qrError" class="qr-error">{{ qrError }}</p>
        <a-form-item :label="t('esim.confirmationCode')"
          ><a-input v-model:value="esimConfirmationCode" /></a-form-item
        ><a-form-item :label="t('esim.matchingId')"><a-input v-model:value="esimMatchingID" /></a-form-item>
        <div class="modal-actions">
          <a-button @click="closeEsimDownload">{{ t('common.cancel') }}</a-button
          ><a-button
            type="primary"
            html-type="submit"
            :disabled="!device.has('esim') || !esimActivationCode.trim()"
            ><DownloadOutlined />{{ t('common.download') }}</a-button
          >
        </div></a-form
      ></a-modal
    >
    <a-modal
      v-model:open="esimConfirmationOpen"
      :title="t('esim.confirmationTitle')"
      :footer="null"
      :closable="false"
      ><p class="confirmation-hint">{{ t('esim.confirmationHint') }}</p>
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
      </div></a-modal
    >
    <a-modal
      v-model:open="esimSettingsOpen"
      :title="t('esim.settings')"
      :width="680"
      :footer="null"
      destroy-on-close
      @cancel="closeEsimSettings"
      ><a-form layout="vertical" @submit.prevent="saveProfileNote"
        ><a-form-item :label="t('esim.profileName')"
          ><a-input v-model:value="esimLabels[settingsICCID]" required :maxlength="64"
        /></a-form-item>
        <div class="settings-note-fields">
          <h3>{{ t('esim.localNote') }}</h3>
          <p>{{ t('esim.localNoteHint') }}</p>
          <a-form-item :label="t('esim.noteLabel')"
            ><a-input v-model:value="noteLabel" :maxlength="80" /></a-form-item
          ><a-form-item :label="t('esim.notePhone')"
            ><a-input v-model:value="notePhone" :maxlength="80" /></a-form-item
          ><a-form-item :label="t('esim.noteTags')"
            ><a-input v-model:value="noteTags" :maxlength="200"
          /></a-form-item>
        </div>
        <div class="modal-actions">
          <a-button @click="closeEsimSettings">{{ t('common.cancel') }}</a-button
          ><a-button type="primary" html-type="submit">{{ t('esim.saveLocal') }}</a-button>
        </div></a-form
      >
    </a-modal>
  </section>
</template>
