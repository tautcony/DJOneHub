<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { CheckOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons-vue'
import FieldRow from '../components/FieldRow.vue'
import LoadingState from '../components/LoadingState.vue'
import Panel from '../components/Panel.vue'
import { useViewContext } from './context'

const { t } = useI18n()
const { gps, loadedViews, formatSMSDate, refreshGPS, toggleGPS } = useViewContext()
</script>

<template>
  <section class="view-grid">
    <Panel :title="t('gps.title')"
      ><template #actions
        ><a-button :type="gps?.enabled ? 'default' : 'primary'" @click="toggleGPS"
          ><StopOutlined v-if="gps?.enabled" /><CheckOutlined v-else />{{
            gps?.enabled ? t('gps.stop') : t('gps.start')
          }}</a-button
        ></template
      ><LoadingState v-if="!loadedViews.gps" /><template v-else
        ><div class="detail-list">
          <FieldRow
            :label="t('gps.state')"
            :value="gps?.enabled ? t('gps.enabled') : t('gps.disabled')"
          /><FieldRow
            :label="t('gps.updated')"
            :value="gps?.last_fix?.timestamp ? formatSMSDate(gps.last_fix.timestamp) : undefined"
          /><FieldRow :label="t('gps.latitude')" :value="gps?.last_fix?.latitude" monospace /><FieldRow
            :label="t('gps.longitude')"
            :value="gps?.last_fix?.longitude"
            monospace
          /><FieldRow :label="t('gps.satellites')" :value="gps?.last_fix?.satellites" /><FieldRow
            :label="t('gps.altitude')"
            :value="gps?.last_fix?.altitude"
          />
        </div>
        <div class="panel-actions network-actions">
          <a-button :disabled="!gps?.enabled" @click="refreshGPS"
            ><ReloadOutlined />{{ t('gps.refresh') }}</a-button
          >
        </div></template
      ></Panel
    >
  </section>
</template>
