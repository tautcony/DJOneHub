<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { CheckOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons-vue'
import FieldRow from '../components/FieldRow.vue'
import LoadingState from '../components/LoadingState.vue'
import Panel from '../components/Panel.vue'
import { useViewContext } from './context'

const { t } = useI18n()
const {
  cellularPolicy,
  checkNetwork,
  device,
  loadView,
  network,
  networkChecks,
  networkMode,
  runNetworkCheck,
  setNetworkMode,
  toggleCellularPolicy,
  usbNetworkModeLabel,
  usbNetworkModeOptions,
  loadedViews,
  networkTraffic,
  rebootModule,
} = useViewContext()
function bytes(value: number) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let amount = Math.max(0, value)
  let unit = 0
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024
    unit++
  }
  return `${amount.toFixed(unit === 0 ? 0 : amount >= 100 ? 0 : 1)} ${units[unit]}`
}
</script>

<template>
  <section class="view-grid network-view">
    <Panel class="network-status-panel" :eyebrow="t('network.statusEyebrow')" :title="t('network.title')"
      ><template #actions
        ><a-button @click="loadView('network')"
          ><ReloadOutlined />{{ t('common.refresh') }}</a-button
        ></template
      ><LoadingState v-if="!loadedViews.network" /><template v-else
        ><div class="detail-list">
          <FieldRow :label="t('network.usbMode')" :value="usbNetworkModeLabel(network?.mode)" /><FieldRow
            :label="t('network.radioMode')"
            :value="network?.network_mode"
          /><FieldRow :label="t('network.interface')" :value="network?.interface" monospace /><FieldRow
            :label="t('network.defaultRoute')"
            :value="network?.default_route"
          /><FieldRow
            :label="t('network.addresses')"
            :value="network?.addresses?.join(', ')"
            monospace
          /><FieldRow
            :label="t('network.traffic')"
            :value="`${bytes(networkTraffic.rxBytes)} ${t('network.received')} · ${bytes(networkTraffic.txBytes)} ${t('network.sent')}`"
          /><FieldRow
            :label="t('network.currentDownload')"
            :value="`${bytes(networkTraffic.rxRate)}/s`"
          /><FieldRow :label="t('network.currentUpload')" :value="`${bytes(networkTraffic.txRate)}/s`" />
        </div>
        <div class="panel-actions network-actions">
          <a-button :disabled="!device.has('network_status')" @click="checkNetwork"
            ><CheckOutlined />{{ t('common.check') }}</a-button
          ><a-button danger :disabled="!device.has('device_control')" @click="rebootModule"
            ><StopOutlined />{{ t('network.reboot') }}</a-button
          >
        </div>
        <div class="network-checks">
          <div class="panel-heading">
            <h3>{{ t('network.checks') }}</h3>
            <span>{{ t('network.checksHint') }}</span>
          </div>
          <div class="panel-actions">
            <a-button @click="runNetworkCheck('4g')">{{ t('network.check4g') }}</a-button
            ><a-button @click="runNetworkCheck('proxy')">{{ t('network.checkProxy') }}</a-button>
          </div>
          <div class="network-check-results">
            <div v-for="item in networkChecks" :key="item.label" class="check-result">
              <a-tag :color="item.ok ? 'success' : 'error'">{{
                item.ok ? t('status.supported') : t('status.notSupported')
              }}</a-tag
              ><strong>{{ item.label }}</strong
              ><span>{{ item.summary }}</span
              ><small>{{ item.detail }}</small>
            </div>
          </div>
        </div></template
      ></Panel
    ><Panel
      :eyebrow="t('network.modeEyebrow')"
      :title="t('network.usbNetworkMode')"
      :meta="usbNetworkModeLabel(networkMode)"
      ><a-form class="form" layout="vertical" @submit.prevent="setNetworkMode"
        ><a-form-item :label="t('network.mode')"
          ><a-select v-model:value="networkMode" :options="usbNetworkModeOptions" /></a-form-item
        ><a-button type="primary" html-type="submit" :disabled="!device.has('network_control')">{{
          t('common.apply')
        }}</a-button
        ><a-alert
          v-if="!device.has('network_control')"
          class="inline-alert"
          type="warning"
          show-icon
          :message="t('network.unavailableControl')"
      /></a-form>
      <div class="policy-block">
        <div>
          <span>{{ t('network.policy') }}</span
          ><a-switch
            :checked="!cellularPolicy?.force_off"
            :disabled="!cellularPolicy"
            @change="toggleCellularPolicy"
          /><strong>{{ cellularPolicy?.force_off ? t('network.policyOff') : t('network.policyOn') }}</strong>
        </div>
      </div></Panel
    >
  </section>
</template>
