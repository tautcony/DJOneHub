<script setup lang="ts">
import { useI18n } from 'vue-i18n'
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
  deleteEsim,
  device,
  downloadEsim,
  enableEsim,
  esim,
  esimActivationCode,
  esimConfirmationCode,
  esimDownloadOpen,
  esimLabels,
  esimMatchingID,
  esimOperation,
  esimSettingsOpen,
  esimSettingsICCID: settingsICCID,
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
  saveProfileNote,
} = useViewContext()
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
    <a-modal
      v-model:open="esimDownloadOpen"
      :title="t('esim.downloadTitle')"
      :footer="null"
      destroy-on-close
      @cancel="closeEsimDownload"
      ><a-form layout="vertical" @submit.prevent="downloadEsim"
        ><a-form-item :label="t('esim.activationCode')"
          ><a-input v-model:value="esimActivationCode" required placeholder="LPA:1$..." /></a-form-item
        ><a-form-item :label="t('esim.confirmationCode')"
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
      v-model:open="esimSettingsOpen"
      :title="t('esim.settings')"
      :width="680"
      :footer="null"
      destroy-on-close
      @cancel="closeEsimSettings"
      ><a-form layout="vertical" @submit.prevent="saveProfileNote"
        ><a-form-item :label="t('esim.profileName')"
          ><a-input v-model:value="esimLabels[settingsICCID]" required maxlength="64"
        /></a-form-item>
        <div class="settings-note-fields">
          <h3>{{ t('esim.localNote') }}</h3>
          <p>{{ t('esim.localNoteHint') }}</p>
          <a-form-item :label="t('esim.noteLabel')"
            ><a-input v-model:value="noteLabel" maxlength="80" /></a-form-item
          ><a-form-item :label="t('esim.notePhone')"
            ><a-input v-model:value="notePhone" maxlength="80" /></a-form-item
          ><a-form-item :label="t('esim.noteTags')"
            ><a-input v-model:value="noteTags" maxlength="200"
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
