<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppearanceStore, type AppearanceMode } from '../../stores/appearance'
import { useViewContext } from '../context'

const { t } = useI18n()
const { locale, showSensitive } = useViewContext()
const appearance = useAppearanceStore()
const appearanceOptions = computed<Array<{ label: string; value: AppearanceMode }>>(() => [
  { label: t('settings.appearanceModes.light'), value: 'light' },
  { label: t('settings.appearanceModes.dark'), value: 'dark' },
  { label: t('settings.appearanceModes.system'), value: 'system' },
])
</script>

<template>
  <a-form class="form settings-form" layout="vertical">
    <a-form-item :label="t('settings.appearanceMode')">
      <a-segmented v-model:value="appearance.mode" block :options="appearanceOptions" />
    </a-form-item>
    <p class="settings-detail">{{ t('settings.appearanceDetail') }}</p>
    <a-form-item :label="t('language.title')" html-for="settings-language">
      <a-select id="settings-language" v-model:value="locale" :aria-label="t('language.title')">
        <a-select-option value="en-US">{{ t('language.english') }}</a-select-option>
        <a-select-option value="zh-CN">{{ t('language.chinese') }}</a-select-option>
      </a-select>
    </a-form-item>
    <p class="settings-detail">{{ t('settings.languageDetail') }}</p>
    <div class="setting-toggle">
      <a-switch v-model:checked="showSensitive" :aria-label="t('settings.showSensitive')" /><span
        ><strong>{{ t('settings.showSensitive') }}</strong
        ><small>{{
          showSensitive ? t('settings.sensitiveVisible') : t('settings.sensitiveMasked')
        }}</small></span
      >
    </div>
  </a-form>
</template>
