<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { PlusOutlined } from '@ant-design/icons-vue'
import { api } from '../../services/api'
import type { CardPolicy } from '../../types'

const { t } = useI18n()

const policies = ref<CardPolicy[]>([])
const policyForm = reactive({ iccid: '', vowifi_enabled: true })
const policyError = ref('')
const policiesLoading = ref(false)
const policySaving = ref(false)

async function loadPolicies() {
  policiesLoading.value = true
  policyError.value = ''
  try {
    policies.value = await api.vowifiCardPolicies()
  } catch (cause) {
    policyError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    policiesLoading.value = false
  }
}

async function savePolicy() {
  if (!policyForm.iccid.trim()) return
  policySaving.value = true
  policyError.value = ''
  try {
    await api.vowifiCardPolicySet(policyForm.iccid.trim(), policyForm.vowifi_enabled)
    policyForm.iccid = ''
    policyForm.vowifi_enabled = true
    await loadPolicies()
  } catch (cause) {
    policyError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    policySaving.value = false
  }
}

async function togglePolicy(policy: CardPolicy) {
  policyError.value = ''
  try {
    await api.vowifiCardPolicySet(policy.iccid, !policy.vowifi_enabled)
    await loadPolicies()
  } catch (cause) {
    policyError.value = cause instanceof Error ? cause.message : String(cause)
  }
}

onMounted(() => {
  void loadPolicies()
})
</script>

<template>
  <div>
    <a-alert
      v-if="policyError"
      class="inline-alert"
      type="error"
      show-icon
      :message="policyError"
    /><LoadingState v-if="policiesLoading" />
    <div v-else class="proxy-list">
      <div v-for="policy in policies" :key="policy.iccid" class="proxy-item">
        <div class="proxy-item-main">
          <span class="proxy-item-id">{{ policy.iccid }}</span>
          <a-switch :checked="policy.vowifi_enabled" @change="togglePolicy(policy)" />
        </div>
      </div>
      <div v-if="!policies.length" class="proxy-empty">{{ t('vowifi.cardPolicyEmpty') }}</div>
    </div>
    <a-form layout="inline" class="proxy-form" @submit.prevent="savePolicy">
      <a-form-item style="width: 340px"
        ><a-input v-model:value="policyForm.iccid" :placeholder="t('vowifi.cardPolicyIccid')"
      /></a-form-item>
      <a-form-item
        ><a-switch v-model:checked="policyForm.vowifi_enabled" />{{
          t('vowifi.cardPolicyVoWiFi')
        }}</a-form-item
      >
      <a-form-item
        ><a-button
          type="primary"
          :loading="policySaving"
          :disabled="!policyForm.iccid.trim()"
          @click="savePolicy"
          ><PlusOutlined />{{ t('vowifi.cardPolicySave') }}</a-button
        ></a-form-item
      >
    </a-form>
  </div>
</template>

<style scoped>
.proxy-list {
  margin-bottom: 8px;
}
.proxy-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 4px 0;
}
.proxy-item-main {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.proxy-item-id {
  font-weight: 600;
}
.proxy-empty {
  color: var(--text-secondary, rgba(0, 0, 0, 0.65));
  padding: 8px 0;
}
.proxy-form {
  row-gap: 8px;
}
</style>
