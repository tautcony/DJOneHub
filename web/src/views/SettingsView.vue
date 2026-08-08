<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Panel from '../components/Panel.vue'
import AppearanceSettings from './settings/AppearanceSettings.vue'
import StartupSettings from './settings/StartupSettings.vue'
import NotificationSettings from './settings/NotificationSettings.vue'
import ChannelSettings from './settings/ChannelSettings.vue'

const { t } = useI18n()
const activeTab = ref('appearance')
</script>

<template>
  <section>
    <Panel class="settings-panel" :eyebrow="t('settings.eyebrow')" :title="t('settings.title')">
      <a-tabs v-model:activeKey="activeTab" class="settings-tabs">
        <a-tab-pane key="appearance" :tab="t('settings.tab.appearance')">
          <AppearanceSettings />
        </a-tab-pane>
        <a-tab-pane key="startup" :tab="t('settings.tab.startup')">
          <StartupSettings />
        </a-tab-pane>
        <a-tab-pane key="notifications" :tab="t('settings.tab.notifications')">
          <NotificationSettings />
        </a-tab-pane>
        <a-tab-pane key="channels" :tab="t('settings.tab.channels')">
          <ChannelSettings />
        </a-tab-pane>
      </a-tabs>
    </Panel>
  </section>
</template>

<style scoped>
.settings-tabs :deep(.ant-tabs-nav) {
  margin-bottom: 18px;
}
</style>
