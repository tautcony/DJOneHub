<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons-vue'
import { api } from '../../services/api'
import type { UpstreamProxy, UpstreamProxyCountryRule } from '../../types'

const { t } = useI18n()

const rules = ref<UpstreamProxyCountryRule[]>([])
const proxies = ref<UpstreamProxy[]>([])
const ruleForm = reactive({ country_code: '', upstream_proxy_id: '', enabled: true })
const ruleError = ref('')
const rulesLoading = ref(false)
const ruleSaving = ref(false)

async function loadRules() {
  rulesLoading.value = true
  ruleError.value = ''
  try {
    rules.value = await api.vowifiCountryRules()
  } catch (cause) {
    ruleError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    rulesLoading.value = false
  }
}

// 规则表单的代理下拉；进入 tab 时随规则一起加载。
async function loadProxies() {
  try {
    proxies.value = await api.vowifiProxies()
  } catch {
    proxies.value = []
  }
}

async function saveRule() {
  if (!ruleForm.country_code.trim() || !ruleForm.upstream_proxy_id) return
  ruleSaving.value = true
  ruleError.value = ''
  try {
    await api.vowifiCountryRuleUpsert({
      country_code: ruleForm.country_code.trim().toUpperCase(),
      upstream_proxy_id: ruleForm.upstream_proxy_id,
      enabled: ruleForm.enabled,
    })
    ruleForm.country_code = ''
    ruleForm.upstream_proxy_id = ''
    ruleForm.enabled = true
    await loadRules()
  } catch (cause) {
    ruleError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    ruleSaving.value = false
  }
}

async function deleteRule(countryCode: string) {
  ruleError.value = ''
  try {
    await api.vowifiCountryRuleDelete(countryCode)
    await loadRules()
  } catch (cause) {
    ruleError.value = cause instanceof Error ? cause.message : String(cause)
  }
}

onMounted(() => {
  void loadRules()
  void loadProxies()
})
</script>

<template>
  <div>
    <a-alert v-if="ruleError" class="inline-alert" type="error" show-icon :message="ruleError" /><LoadingState
      v-if="rulesLoading"
    />
    <div v-else class="proxy-list">
      <div v-for="rule in rules" :key="rule.country_code" class="proxy-item">
        <div class="proxy-item-main">
          <span class="proxy-item-id">{{ rule.country_code }}</span>
          <span class="proxy-item-meta">→ {{ rule.upstream_proxy_id }}</span>
          <a-tag :color="rule.enabled ? 'green' : 'default'">{{
            rule.enabled ? t('common.enable') : t('common.disable')
          }}</a-tag>
        </div>
        <a-button size="small" danger @click="deleteRule(rule.country_code)"><DeleteOutlined /></a-button>
      </div>
      <div v-if="!rules.length" class="proxy-empty">{{ t('vowifi.countryRuleEmpty') }}</div>
    </div>
    <a-form layout="inline" class="proxy-form" @submit.prevent="saveRule">
      <a-form-item
        ><a-input
          v-model:value="ruleForm.country_code"
          :placeholder="t('vowifi.countryRuleCountry')"
          maxlength="2"
      /></a-form-item>
      <a-form-item
        ><a-select
          v-model:value="ruleForm.upstream_proxy_id"
          :placeholder="t('vowifi.countryRuleProxy')"
          style="width: 200px"
          ><a-select-option v-for="proxy in proxies" :key="proxy.id" :value="proxy.id">{{
            proxy.id
          }}</a-select-option
          ><a-select-option v-if="!proxies.length" disabled :value="undefined">{{
            t('vowifi.countryRuleNoProxy')
          }}</a-select-option></a-select
        ></a-form-item
      >
      <a-form-item
        ><a-checkbox v-model:checked="ruleForm.enabled">{{
          t('vowifi.proxyEnabled')
        }}</a-checkbox></a-form-item
      >
      <a-form-item
        ><a-button
          type="primary"
          :loading="ruleSaving"
          :disabled="!ruleForm.country_code.trim() || !ruleForm.upstream_proxy_id"
          @click="saveRule"
          ><PlusOutlined />{{ t('vowifi.countryRuleAdd') }}</a-button
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
.proxy-item-meta {
  color: var(--text-secondary, rgba(0, 0, 0, 0.65));
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.proxy-empty {
  color: var(--text-secondary, rgba(0, 0, 0, 0.65));
  padding: 8px 0;
}
.proxy-form {
  row-gap: 8px;
}
</style>
