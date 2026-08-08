<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useViewContext } from '../context'

const { t } = useI18n()
const { startupSettings, startupBusy, toggleStartup } = useViewContext()
</script>

<template>
  <div class="settings-section">
    <div class="settings-section-heading">
      <div>
        <span class="eyebrow">{{ t('settings.startupEyebrow') }}</span>
        <h3>{{ t('settings.startupTitle') }}</h3>
      </div>
      <a-tag :color="startupSettings?.enabled ? 'green' : 'default'">
        {{ startupSettings?.enabled ? t('settings.startupEnabled') : t('settings.startupDisabled') }}
      </a-tag>
    </div>
    <p class="settings-detail">
      {{ startupSettings?.supported ? t('settings.startupDetail') : t('settings.startupUnavailable') }}
    </p>
    <div class="setting-toggle">
      <a-switch
        :checked="startupSettings?.enabled"
        :disabled="!startupSettings?.supported"
        :loading="startupBusy"
        @change="toggleStartup"
      /><span
        ><strong>{{ t('settings.startupToggle') }}</strong
        ><small>{{ t('settings.startupToggleDetail') }}</small></span
      >
    </div>
  </div>
</template>
