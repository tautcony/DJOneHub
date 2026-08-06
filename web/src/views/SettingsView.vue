<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { BellOutlined, ReloadOutlined, SettingOutlined } from '@ant-design/icons-vue'
import Panel from '../components/Panel.vue'
import { useViewContext } from './context'

const { t } = useI18n()
const {
  loadNotificationPermissions,
  locale,
  notificationPermissionBusy,
  notificationPermissionLabel,
  notificationPermissions,
  notificationPreferences,
  notificationPreferencesBusy,
  openNotificationSettings,
  requestNotificationPermission,
  saveNotificationPreferences,
  showSensitive,
  startupBusy,
  startupSettings,
  toggleStartup,
} = useViewContext()
</script>

<template>
  <section>
    <Panel class="settings-panel" :eyebrow="t('settings.appearance')" :title="t('settings.title')"
      ><a-form class="form settings-form" layout="vertical"
        ><a-form-item :label="t('language.title')"
          ><a-select v-model:value="locale"
            ><a-select-option value="en-US">{{ t('language.english') }}</a-select-option
            ><a-select-option value="zh-CN">{{ t('language.chinese') }}</a-select-option></a-select
          ></a-form-item
        >
        <p class="settings-detail">{{ t('settings.languageDetail') }}</p>
        <div class="setting-toggle">
          <a-switch v-model:checked="showSensitive" /><span
            ><strong>{{ t('settings.showSensitive') }}</strong
            ><small>{{
              showSensitive ? t('settings.sensitiveVisible') : t('settings.sensitiveMasked')
            }}</small></span
          >
        </div></a-form
      >
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
      <div class="settings-section">
        <div class="settings-section-heading">
          <div>
            <span class="eyebrow">{{ t('settings.notificationsEyebrow') }}</span>
            <h3>{{ t('settings.notificationsTitle') }}</h3>
          </div>
          <a-tag
            :color="
              notificationPermissions?.state === 'authorized' ||
              notificationPermissions?.state === 'provisional'
                ? 'green'
                : notificationPermissions?.state === 'denied'
                  ? 'red'
                  : 'default'
            "
            >{{ notificationPermissionLabel(notificationPermissions?.state) }}</a-tag
          >
        </div>
        <p class="settings-detail">
          {{
            notificationPermissions?.state === 'denied'
              ? t('settings.notificationsDeniedDetail')
              : notificationPermissions?.state === 'not_determined'
                ? t('settings.notificationsNotDeterminedDetail')
                : notificationPermissions?.state === 'authorized' ||
                    notificationPermissions?.state === 'provisional'
                  ? t('settings.notificationsAuthorizedDetail')
                  : t('settings.notificationsUnknownDetail')
          }}
        </p>
        <div class="panel-actions settings-actions">
          <a-button
            v-if="notificationPermissions?.can_request"
            type="primary"
            :loading="notificationPermissionBusy"
            @click="requestNotificationPermission"
            ><BellOutlined />{{ t('settings.requestNotifications') }}</a-button
          ><a-button
            v-if="notificationPermissions?.can_open_settings"
            :loading="notificationPermissionBusy"
            @click="openNotificationSettings"
            ><SettingOutlined />{{ t('settings.openNotificationSettings') }}</a-button
          ><a-button
            v-if="
              notificationPermissions?.state === 'unknown' || notificationPermissions?.state === 'unsupported'
            "
            :loading="notificationPermissionBusy"
            @click="loadNotificationPermissions"
            ><ReloadOutlined />{{ t('common.refresh') }}</a-button
          >
        </div>
      </div>
      <div class="settings-section">
        <div class="settings-section-heading">
          <div>
            <span class="eyebrow">{{ t('settings.presentationEyebrow') }}</span>
            <h3>{{ t('settings.presentationTitle') }}</h3>
          </div>
          <a-tag v-if="notificationPreferences" color="blue">{{ t('settings.presentationSaved') }}</a-tag>
        </div>
        <p class="settings-detail">{{ t('settings.presentationDetail') }}</p>
        <div v-if="notificationPreferences" class="notification-preference-list">
          <div class="setting-toggle notification-debug-toggle">
            <a-switch
              v-model:checked="notificationPreferences.show_debug"
              :loading="notificationPreferencesBusy"
              @change="saveNotificationPreferences"
            /><span
              ><strong>{{ t('settings.showNotificationDebug') }}</strong
              ><small>{{ t('settings.showNotificationDebugDetail') }}</small></span
            >
          </div>
          <div class="setting-toggle notification-sender-only-toggle">
            <a-switch
              :checked="notificationPreferences?.sender_only ?? true"
              :loading="notificationPreferencesBusy"
              @change="(value: boolean) => { if (notificationPreferences) { notificationPreferences.sender_only = value; saveNotificationPreferences() } }"
            /><span
              ><strong>{{ t('settings.senderOnly') }}</strong
              ><small>{{ t('settings.senderOnlyDetail') }}</small></span
            >
          </div>
          <div class="notification-preference-row">
            <div>
              <strong>{{ t('settings.incomingCall') }}</strong
              ><small>{{ t('settings.incomingCallDetail') }}</small>
            </div>
            <a-radio-group
              v-model:value="notificationPreferences.incoming_call"
              option-type="button"
              button-style="solid"
              :disabled="notificationPreferencesBusy"
              @change="saveNotificationPreferences"
              ><a-radio-button value="system">{{ t('settings.presentationModes.system') }}</a-radio-button
              ><a-radio-button value="custom">{{
                t('settings.presentationModes.custom')
              }}</a-radio-button></a-radio-group
            >
          </div>
          <div class="notification-preference-row">
            <div>
              <strong>{{ t('settings.missedCall') }}</strong
              ><small>{{ t('settings.missedCallDetail') }}</small>
            </div>
            <a-radio-group
              v-model:value="notificationPreferences.missed_call"
              option-type="button"
              button-style="solid"
              :disabled="notificationPreferencesBusy"
              @change="saveNotificationPreferences"
              ><a-radio-button value="system">{{ t('settings.presentationModes.system') }}</a-radio-button
              ><a-radio-button value="custom">{{
                t('settings.presentationModes.custom')
              }}</a-radio-button></a-radio-group
            >
          </div>
          <div class="notification-preference-row">
            <div>
              <strong>{{ t('settings.smsNotifications') }}</strong
              ><small>{{ t('settings.smsNotificationsDetail') }}</small>
            </div>
            <a-radio-group
              v-model:value="notificationPreferences.sms"
              option-type="button"
              button-style="solid"
              :disabled="notificationPreferencesBusy"
              @change="saveNotificationPreferences"
              ><a-radio-button value="system">{{ t('settings.presentationModes.system') }}</a-radio-button
              ><a-radio-button value="custom">{{
                t('settings.presentationModes.custom')
              }}</a-radio-button></a-radio-group
            >
          </div>
          <div class="notification-preference-row">
            <div>
              <strong>{{ t('settings.offlineNotifications') }}</strong
              ><small>{{ t('settings.offlineNotificationsDetail') }}</small>
            </div>
            <a-radio-group
              v-model:value="notificationPreferences.device_offline"
              option-type="button"
              button-style="solid"
              :disabled="notificationPreferencesBusy"
              @change="saveNotificationPreferences"
              ><a-radio-button value="system">{{ t('settings.presentationModes.system') }}</a-radio-button
              ><a-radio-button value="custom">{{
                t('settings.presentationModes.custom')
              }}</a-radio-button></a-radio-group
            >
          </div>
        </div>
        <p v-else class="settings-detail">{{ t('settings.notificationsUnknownDetail') }}</p>
      </div></Panel
    >
  </section>
</template>
