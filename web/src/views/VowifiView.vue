<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ReloadOutlined } from '@ant-design/icons-vue'
import FieldRow from '../components/FieldRow.vue'
import LoadingState from '../components/LoadingState.vue'
import OperationStatusView from '../components/OperationStatus.vue'
import Panel from '../components/Panel.vue'
import { useViewContext } from './context'
import CardPolicyTab from './vowifi/CardPolicyTab.vue'
import CountryRuleTab from './vowifi/CountryRuleTab.vue'
import ProxyTab from './vowifi/ProxyTab.vue'

const { t } = useI18n()
const { device, loadVowifi, loadedViews, runVowifi, vowifi, vowifiOperation } = useViewContext()

const activeManagementTab = ref('proxies')
</script>

<template>
  <section>
    <Panel :title="t('vowifi.title')"
      ><template #actions
        ><a-button @click="loadVowifi"><ReloadOutlined />{{ t('common.refresh') }}</a-button></template
      ><LoadingState v-if="!loadedViews.vowifi" /><template v-else
        ><div class="detail-list">
          <FieldRow
            :label="t('vowifi.availability')"
            :value="
              vowifi?.available === false
                ? t('vowifi.unavailable')
                : vowifi?.available
                  ? t('vowifi.available')
                  : undefined
            "
          /><FieldRow :label="t('vowifi.state')" :value="vowifi?.state" /><FieldRow
            :label="t('vowifi.reason')"
            :value="vowifi?.reason"
          />
        </div>
        <a-alert
          v-if="vowifi?.last_error"
          class="inline-alert vowifi-error-alert"
          type="error"
          show-icon
          :message="vowifi.last_error"
        />
        <div class="panel-actions vowifi-actions">
          <!-- VoWiFi 可用性 = 设备就绪且后端有 SIM APDU（AKA）能力（has 已含 ready 检查）。 -->
          <a-button type="primary" :disabled="!device.has('apdu')" @click="runVowifi('enable')">{{
            t('common.enable')
          }}</a-button
          ><a-button :disabled="!device.has('apdu')" @click="runVowifi('disable')">{{
            t('common.disable')
          }}</a-button
          ><a-button :disabled="!device.has('apdu')" @click="runVowifi('reconnect')">{{
            t('common.reconnect')
          }}</a-button>
        </div>
        <a-alert
          v-if="device.ready && !device.has('apdu')"
          class="inline-alert vowifi-control-alert"
          type="warning"
          show-icon
          :message="t('vowifi.unavailableApdu')" /><OperationStatusView
          :operation="vowifiOperation"
          :label="t('vowifi.operationStatus')" /></template
    ></Panel>

    <Panel class="vowifi-management-panel" :title="t('vowifi.management')">
      <a-tabs v-model:active-key="activeManagementTab" class="vowifi-management-tabs">
        <a-tab-pane key="proxies" :tab="t('vowifi.tab.proxies')">
          <ProxyTab />
        </a-tab-pane>
        <a-tab-pane key="rules" :tab="t('vowifi.tab.rules')">
          <CountryRuleTab />
        </a-tab-pane>
        <a-tab-pane key="cardPolicy" :tab="t('vowifi.tab.cardPolicy')">
          <CardPolicyTab />
        </a-tab-pane>
      </a-tabs>
    </Panel>
  </section>
</template>

<style scoped>
section {
  display: grid;
  gap: 18px;
  align-items: start;
}
.vowifi-management-tabs :deep(.ant-tabs-nav) {
  margin-bottom: 18px;
}
</style>
