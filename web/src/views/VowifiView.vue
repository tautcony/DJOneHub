<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ReloadOutlined } from '@ant-design/icons-vue'
import FieldRow from '../components/FieldRow.vue'
import LoadingState from '../components/LoadingState.vue'
import OperationStatusView from '../components/OperationStatus.vue'
import Panel from '../components/Panel.vue'
import { useViewContext } from './context'

const { t } = useI18n()
const { device, loadVowifi, loadedViews, runVowifi, vowifi, vowifiOperation } = useViewContext()
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
        <div class="panel-actions vowifi-actions">
          <a-button type="primary" :disabled="!device.has('vowifi_control')" @click="runVowifi('enable')">{{
            t('common.enable')
          }}</a-button
          ><a-button :disabled="!device.has('vowifi_control')" @click="runVowifi('disable')">{{
            t('common.disable')
          }}</a-button
          ><a-button :disabled="!device.has('vowifi_control')" @click="runVowifi('reconnect')">{{
            t('common.reconnect')
          }}</a-button>
        </div>
        <a-alert
          v-if="!device.has('vowifi_control')"
          class="inline-alert vowifi-control-alert"
          type="warning"
          show-icon
          :message="t('vowifi.unavailableControl')" /><OperationStatusView
          :operation="vowifiOperation"
          :label="t('vowifi.operationStatus')" /></template
    ></Panel>
  </section>
</template>
