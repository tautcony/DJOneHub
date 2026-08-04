<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { CheckOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons-vue'
import FieldRow from '../components/FieldRow.vue'
import LoadingState from '../components/LoadingState.vue'
import Panel from '../components/Panel.vue'
import { useViewContext } from './context'

const { t } = useI18n()
const {
  checkNetwork,
  device,
  loadView,
  network,
  networkMode,
  setNetworkMode,
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
		  /><FieldRow
			:label="t('network.band')"
			:value="network?.radio_band"
			monospace
		  /><FieldRow :label="t('network.interface')" :value="network?.interface" monospace /><FieldRow
            :label="t('network.defaultRoute')"
            :value="network?.default_route"
          /><FieldRow
            :label="t('network.systemDefaultRoute')"
            :value="network?.system_default_route"
          /><FieldRow
            :label="t('network.addresses')"
            :value="network?.addresses?.join(', ')"
            monospace
          /><FieldRow
            :label="t('network.traffic')"
            :value="`${bytes(networkTraffic.rxBytes)} ${t('common.received')} · ${bytes(networkTraffic.txBytes)} ${t('common.sent')}`"
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
      </template></Panel
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
          :message="t('network.unavailableControl')" /></a-form
    ></Panel>
  </section>
</template>
