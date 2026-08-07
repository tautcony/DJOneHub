<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import LoadingState from '../components/LoadingState.vue'
import Panel from '../components/Panel.vue'
import { useViewContext } from './context'

const { t } = useI18n()
const {
  createSimCard,
  deleteSimCard,
  loadView,
  loadedViews,
  maskSensitive,
  simCards,
  simCardsBusy,
  updateSimCard,
} = useViewContext()

// 编辑/新建表单状态。
const editorOpen = ref(false)
const editingICCID = ref('')
const formICCID = ref('')
const formIMSI = ref('')
const formMSISDN = ref('')
const formName = ref('')
const formNotes = ref('')

function openCreate() {
  editingICCID.value = ''
  formICCID.value = ''
  formIMSI.value = ''
  formMSISDN.value = ''
  formName.value = ''
  formNotes.value = ''
  editorOpen.value = true
}

function openEdit(card: (typeof simCards.value)[number]) {
  editingICCID.value = card.iccid
  formICCID.value = card.iccid
  formIMSI.value = card.imsi || ''
  formMSISDN.value = card.msisdn || ''
  formName.value = card.name || ''
  formNotes.value = card.notes || ''
  editorOpen.value = true
}

async function saveEditor() {
  if (editingICCID.value) {
    await updateSimCard(editingICCID.value, {
      name: formName.value,
      notes: formNotes.value,
      msisdn: formMSISDN.value,
    })
  } else {
    await createSimCard({
      iccid: formICCID.value,
      imsi: formIMSI.value,
      msisdn: formMSISDN.value,
      name: formName.value,
      notes: formNotes.value,
    })
  }
  editorOpen.value = false
}

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString() : ''
}

const sortedCards = computed(() =>
  [...simCards.value].sort((a, b) => (b.last_seen_at || '').localeCompare(a.last_seen_at || '')),
)
</script>

<template>
  <section class="view-grid">
    <Panel
      class="simcards-panel"
      :eyebrow="t('simcards.eyebrow')"
      :title="t('simcards.title')"
      :meta="simCards.length ? `${simCards.length} ${t('simcards.cards')}` : undefined"
      ><template #actions
        ><a-space
          ><a-button @click="loadView('simcards')"><ReloadOutlined />{{ t('common.refresh') }}</a-button
          ><a-button type="primary" :disabled="simCardsBusy" @click="openCreate"
            ><PlusOutlined />{{ t('simcards.add') }}</a-button
          ></a-space></template
      ><LoadingState v-if="!loadedViews.simcards" /><div v-else class="detail-list">
        <p v-if="!simCards.length" class="simcards-hint">{{ t('simcards.emptyHint') }}</p>
        <div
          v-for="card in sortedCards"
          :key="card.iccid"
          class="message-row simcard-row"
        >
          <div class="simcard-main">
            <h3>{{ card.name || t('simcards.unnamed') }}</h3>
            <div class="simcard-fields">
              <div>
                <span>{{ t('simcards.iccid') }}</span
                ><strong>{{ maskSensitive(card.iccid) }}</strong>
              </div>
              <div v-if="card.msisdn">
                <span>{{ t('simcards.msisdn') }}</span
                ><strong>{{ card.msisdn }}</strong>
              </div>
              <div v-if="card.notes">
                <span>{{ t('simcards.notes') }}</span
                ><span>{{ card.notes }}</span>
              </div>
              <div>
                <span>{{ t('simcards.lastSeen') }}</span
                ><span>{{ formatTime(card.last_seen_at) }}</span>
              </div>
              <div v-if="card.first_seen_at">
                <span>{{ t('simcards.firstSeen') }}</span
                ><span>{{ formatTime(card.first_seen_at) }}</span>
              </div>
            </div>
          </div>
          <div class="simcard-actions">
            <a-button size="small" @click="openEdit(card)"
              ><template #icon><EditOutlined /></template
            ></a-button
            ><a-popconfirm
              :title="t('simcards.deleteConfirm')"
              @confirm="deleteSimCard(card.iccid)"
              ><a-button size="small" danger :disabled="simCardsBusy"
                ><template #icon><DeleteOutlined /></template
              ></a-button></a-popconfirm
            >
          </div>
        </div>
        <EmptyState v-if="!simCards.length" :title="t('simcards.noCards')" />
      </div></Panel
    >
    <a-modal
      v-model:open="editorOpen"
      :title="editingICCID ? t('simcards.editTitle') : t('simcards.addTitle')"
      :footer="null"
      destroy-on-close
      ><a-form layout="vertical" @submit.prevent="saveEditor"
        ><a-form-item :label="t('simcards.iccid')" required
          ><a-input
            v-model:value="formICCID"
            required
            :disabled="!!editingICCID"
            placeholder="8986012..."
        /></a-form-item>
        <a-form-item :label="t('simcards.msisdn')"
          ><a-input v-model:value="formMSISDN" placeholder="+86138..." /></a-form-item
        ><a-form-item v-if="!editingICCID" :label="t('simcards.imsi')"
          ><a-input v-model:value="formIMSI" /></a-form-item
        ><a-form-item :label="t('simcards.name')"
          ><a-input v-model:value="formName" maxlength="64" /></a-form-item
        ><a-form-item :label="t('simcards.notes')"
          ><a-textarea v-model:value="formNotes" :rows="3" maxlength="500" /></a-form-item
        >
        <div class="modal-actions">
          <a-button @click="editorOpen = false">{{ t('common.cancel') }}</a-button
          ><a-button
            type="primary"
            html-type="submit"
            :loading="simCardsBusy"
            :disabled="!formICCID.trim()"
            >{{ t('common.save') }}</a-button
          >
        </div></a-form
      ></a-modal
    >
  </section>
</template>
