<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import LoadingState from '../components/LoadingState.vue'
import Panel from '../components/Panel.vue'
import type { SimProfile } from '../types'
import { useViewContext } from './context'

const { t } = useI18n()
const {
  createSimProfile,
  deleteSimProfile,
  loadView,
  loadedViews,
  maskSensitive,
  simProfiles,
  simProfilesBusy,
  updateSimProfile,
} = useViewContext()

const editorOpen = ref(false)
const editingICCID = ref('')
const formICCID = ref('')
const formIMSI = ref('')
const formMSISDN = ref('')
const formName = ref('')
const formLocalPhone = ref('')
const formNotes = ref('')
const formTags = ref('')
const formProfileType = ref<SimProfile['profile_type']>('unknown')

function openCreate() {
  editingICCID.value = ''
  formICCID.value = ''
  formIMSI.value = ''
  formMSISDN.value = ''
  formName.value = ''
  formLocalPhone.value = ''
  formNotes.value = ''
  formTags.value = ''
  formProfileType.value = 'unknown'
  editorOpen.value = true
}

function openEdit(profile: SimProfile) {
  editingICCID.value = profile.iccid
  formICCID.value = profile.iccid
  formIMSI.value = profile.imsi || ''
  formMSISDN.value = profile.msisdn || ''
  formName.value = profile.name || ''
  formLocalPhone.value = profile.local_phone || ''
  formNotes.value = profile.notes || ''
  formTags.value = profile.tags || ''
  formProfileType.value = profile.profile_type
  editorOpen.value = true
}

async function saveEditor() {
  if (editingICCID.value) {
    await updateSimProfile(editingICCID.value, {
      name: formName.value,
      local_phone: formLocalPhone.value,
      notes: formNotes.value,
      tags: formTags.value,
    })
  } else {
    await createSimProfile({
      iccid: formICCID.value,
      imsi: formIMSI.value,
      msisdn: formMSISDN.value,
      name: formName.value,
      local_phone: formLocalPhone.value,
      notes: formNotes.value,
      tags: formTags.value,
      profile_type: formProfileType.value,
    })
  }
  editorOpen.value = false
}

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString() : ''
}

const sortedProfiles = computed(() =>
  [...simProfiles.value].sort((a, b) => (b.last_seen_at || '').localeCompare(a.last_seen_at || '')),
)
</script>

<template>
  <section class="view-grid">
    <Panel
      class="sim-profiles-panel"
      :eyebrow="t('simProfiles.eyebrow')"
      :title="t('simProfiles.title')"
      :meta="simProfiles.length ? `${simProfiles.length} ${t('simProfiles.profiles')}` : undefined"
    >
      <template #actions>
        <a-space>
          <a-button @click="loadView('sim-profiles')"><ReloadOutlined />{{ t('common.refresh') }}</a-button>
          <a-button type="primary" :disabled="simProfilesBusy" @click="openCreate">
            <PlusOutlined />{{ t('simProfiles.add') }}
          </a-button>
        </a-space>
      </template>
      <LoadingState v-if="!loadedViews['sim-profiles']" />
      <div v-else>
        <div v-if="sortedProfiles.length" class="profile-grid sim-profile-grid">
          <article
            v-for="profile in sortedProfiles"
            :key="profile.iccid"
            class="profile-row sim-profile-card"
          >
            <div class="profile-card-head">
              <div>
                <span class="eyebrow">{{ t('simProfiles.cardEyebrow') }}</span>
                <h3>{{ profile.name || t('simProfiles.unnamed') }}</h3>
              </div>
              <span class="profile-state">{{ t(`simProfiles.types.${profile.profile_type}`) }}</span>
            </div>
            <div class="profile-fields">
              <div>
                <span>{{ t('simProfiles.iccid') }}</span
                ><strong>{{ maskSensitive(profile.iccid) }}</strong>
              </div>
              <div v-if="profile.local_phone">
                <span>{{ t('simProfiles.localPhone') }}</span
                ><strong>{{ profile.local_phone }}</strong>
              </div>
              <div v-if="profile.msisdn">
                <span>{{ t('simProfiles.msisdn') }}</span
                ><strong>{{ profile.msisdn }}</strong>
              </div>
              <div v-if="profile.imsi">
                <span>{{ t('simProfiles.imsi') }}</span
                ><strong>{{ maskSensitive(profile.imsi) }}</strong>
              </div>
              <div v-if="profile.tags">
                <span>{{ t('simProfiles.tags') }}</span
                ><span>{{ profile.tags }}</span>
              </div>
              <div v-if="profile.notes">
                <span>{{ t('simProfiles.notes') }}</span
                ><span>{{ profile.notes }}</span>
              </div>
              <div>
                <span>{{ t('simProfiles.lastSeen') }}</span
                ><span>{{ formatTime(profile.last_seen_at) }}</span>
              </div>
              <div v-if="profile.first_seen_at">
                <span>{{ t('simProfiles.firstSeen') }}</span
                ><span>{{ formatTime(profile.first_seen_at) }}</span>
              </div>
            </div>
            <div class="profile-actions">
              <a-button :disabled="simProfilesBusy" @click="openEdit(profile)">
                <EditOutlined />{{ t('simProfiles.edit') }}
              </a-button>
              <a-popconfirm
                :title="t('simProfiles.deleteConfirm')"
                @confirm="deleteSimProfile(profile.iccid)"
              >
                <a-button danger :disabled="simProfilesBusy">
                  <DeleteOutlined />{{ t('common.delete') }}
                </a-button>
              </a-popconfirm>
            </div>
          </article>
        </div>
        <EmptyState v-else :title="t('simProfiles.noProfiles')" />
      </div>
    </Panel>

    <a-modal
      v-model:open="editorOpen"
      :title="editingICCID ? t('simProfiles.editTitle') : t('simProfiles.addTitle')"
      :footer="null"
      destroy-on-close
    >
      <a-form layout="vertical" @submit.prevent="saveEditor">
        <a-form-item :label="t('simProfiles.iccid')" required>
          <a-input v-model:value="formICCID" required :disabled="!!editingICCID" placeholder="8986012..." />
        </a-form-item>
        <a-form-item v-if="!editingICCID" :label="t('simProfiles.type')">
          <a-select
            v-model:value="formProfileType"
            :options="[
              { value: 'unknown', label: t('simProfiles.types.unknown') },
              { value: 'physical', label: t('simProfiles.types.physical') },
              { value: 'esim', label: t('simProfiles.types.esim') },
            ]"
          />
        </a-form-item>
        <a-form-item v-if="!editingICCID" :label="t('simProfiles.imsi')"
          ><a-input v-model:value="formIMSI"
        /></a-form-item>
        <a-form-item v-if="!editingICCID" :label="t('simProfiles.msisdn')"
          ><a-input v-model:value="formMSISDN" placeholder="+86139..."
        /></a-form-item>
        <a-form-item :label="t('simProfiles.name')"
          ><a-input v-model:value="formName" :maxlength="80"
        /></a-form-item>
        <a-form-item :label="t('simProfiles.localPhone')"
          ><a-input v-model:value="formLocalPhone" :maxlength="80" placeholder="+86138..."
        /></a-form-item>
        <a-form-item :label="t('simProfiles.tags')"
          ><a-input v-model:value="formTags" :maxlength="200"
        /></a-form-item>
        <a-form-item :label="t('simProfiles.notes')"
          ><a-textarea v-model:value="formNotes" :rows="3" :maxlength="1000"
        /></a-form-item>
        <div class="modal-actions">
          <a-button @click="editorOpen = false">{{ t('common.cancel') }}</a-button>
          <a-button
            type="primary"
            html-type="submit"
            :loading="simProfilesBusy"
            :disabled="!formICCID.trim()"
            >{{ t('common.save') }}</a-button
          >
        </div>
      </a-form>
    </a-modal>
  </section>
</template>
