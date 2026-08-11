<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons-vue'
import { api } from '../../services/api'
import type { CountryTableStatus, UpstreamProxy } from '../../types'

const { t } = useI18n()

// —— 国家前置代理 ——
const proxies = ref<UpstreamProxy[]>([])
const proxyForm = reactive({ id: '', name: '', addr: '', username: '', password: '', enabled: true })
const proxyError = ref('')
const proxiesLoading = ref(false)
const proxySaving = ref(false)

async function loadProxies() {
  proxiesLoading.value = true
  proxyError.value = ''
  try {
    proxies.value = await api.vowifiProxies()
  } catch (cause) {
    proxyError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    proxiesLoading.value = false
  }
}

async function saveProxy() {
  if (!proxyForm.id.trim() || !proxyForm.addr.trim()) return
  proxySaving.value = true
  proxyError.value = ''
  try {
    await api.vowifiProxyUpsert({
      id: proxyForm.id.trim(),
      name: proxyForm.name.trim(),
      addr: proxyForm.addr.trim(),
      username: proxyForm.username.trim(),
      password: proxyForm.password,
      enabled: proxyForm.enabled,
    })
    proxyForm.id = ''
    proxyForm.name = ''
    proxyForm.addr = ''
    proxyForm.username = ''
    proxyForm.password = ''
    proxyForm.enabled = true
    await loadProxies()
  } catch (cause) {
    proxyError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    proxySaving.value = false
  }
}

async function deleteProxy(id: string) {
  proxyError.value = ''
  try {
    await api.vowifiProxyDelete(id)
    await loadProxies()
  } catch (cause) {
    proxyError.value = cause instanceof Error ? cause.message : String(cause)
  }
}

// —— MCC 国家表状态（只读，随代理 tab 展示） ——
const countryTable = ref<CountryTableStatus | null>(null)
const countryTableError = ref('')

async function loadCountryTable() {
  countryTableError.value = ''
  try {
    countryTable.value = await api.vowifiCountryTable()
  } catch (cause) {
    countryTableError.value = cause instanceof Error ? cause.message : String(cause)
  }
}

onMounted(() => {
  void loadProxies()
  void loadCountryTable()
})
</script>

<template>
  <div>
    <!-- 国家表状态：代理命中依赖 MCC→国家码解析，未就绪时给出提示。 -->
    <div v-if="countryTable" class="country-table-status">
      <a-tag :color="countryTable.ready ? 'green' : 'orange'">{{
        countryTable.ready ? t('vowifi.countryTableReady') : t('vowifi.countryTableNotReady')
      }}</a-tag>
      <span v-if="countryTable.ready" class="country-table-meta">
        {{
          [
            countryTable.row_count !== undefined
              ? `${countryTable.row_count} ${t('vowifi.countryTableRows')}`
              : '',
            countryTable.countries !== undefined
              ? `${countryTable.countries} ${t('vowifi.countryTableCountries')}`
              : '',
            countryTable.source ? t('vowifi.countryTableSource') + ': ' + countryTable.source : '',
          ]
            .filter(Boolean)
            .join(' · ')
        }}
      </span>
      <span v-else class="country-table-meta">{{ t('vowifi.countryTableHint') }}</span>
    </div>
    <a-alert
      v-if="countryTableError"
      class="inline-alert"
      type="error"
      show-icon
      :message="countryTableError"
    />

    <a-alert
      v-if="proxyError"
      class="inline-alert"
      type="error"
      show-icon
      :message="proxyError"
    /><LoadingState v-if="proxiesLoading" />
    <div v-else class="proxy-list">
      <div v-for="proxy in proxies" :key="proxy.id" class="proxy-item">
        <div class="proxy-item-main">
          <span class="proxy-item-id">{{ proxy.id }}</span>
          <span class="proxy-item-meta">{{ proxy.addr }}{{ proxy.name ? ` · ${proxy.name}` : '' }}</span>
          <a-tag :color="proxy.enabled ? 'green' : 'default'">{{
            proxy.enabled ? t('common.enable') : t('common.disable')
          }}</a-tag>
        </div>
        <a-button size="small" danger @click="deleteProxy(proxy.id)"><DeleteOutlined /></a-button>
      </div>
      <div v-if="!proxies.length" class="proxy-empty">{{ t('vowifi.proxyEmpty') }}</div>
    </div>
    <a-form layout="inline" class="proxy-form" @submit.prevent="saveProxy">
      <a-form-item><a-input v-model:value="proxyForm.id" :placeholder="t('vowifi.proxyId')" /></a-form-item>
      <a-form-item
        ><a-input v-model:value="proxyForm.addr" :placeholder="t('vowifi.proxyAddr')"
      /></a-form-item>
      <a-form-item
        ><a-input v-model:value="proxyForm.name" :placeholder="t('vowifi.proxyName')"
      /></a-form-item>
      <a-form-item
        ><a-input v-model:value="proxyForm.username" :placeholder="t('vowifi.proxyUsername')"
      /></a-form-item>
      <a-form-item
        ><a-input-password v-model:value="proxyForm.password" :placeholder="t('vowifi.proxyPassword')"
      /></a-form-item>
      <a-form-item
        ><a-checkbox v-model:checked="proxyForm.enabled">{{
          t('vowifi.proxyEnabled')
        }}</a-checkbox></a-form-item
      >
      <a-form-item
        ><a-button
          type="primary"
          :loading="proxySaving"
          :disabled="!proxyForm.id.trim() || !proxyForm.addr.trim()"
          @click="saveProxy"
          ><PlusOutlined />{{ t('vowifi.proxyAdd') }}</a-button
        ></a-form-item
      >
    </a-form>
  </div>
</template>

<style scoped>
.country-table-status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}
.country-table-meta {
  color: var(--text-secondary, rgba(0, 0, 0, 0.65));
}
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
