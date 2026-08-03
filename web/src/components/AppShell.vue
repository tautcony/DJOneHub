<script setup lang="ts">
import type { Component } from 'vue'
import {
  ApiOutlined,
  BellOutlined,
  CloseOutlined,
  CodeOutlined,
  CreditCardOutlined,
  DashboardOutlined,
  DownOutlined,
  EnvironmentOutlined,
  GlobalOutlined,
  MailOutlined,
  MenuOutlined,
  PhoneOutlined,
  ReloadOutlined,
  SettingOutlined,
  UpOutlined,
  WifiOutlined,
} from '@ant-design/icons-vue'

export interface ShellNavItem {
  id: string
  label: string
  capability?: string
}

export interface ShellNavGroup {
  id: string
  label: string
  items: ShellNavItem[]
}

const props = defineProps<{
  brandSubtitle: string
  navGroups: ShellNavGroup[]
  active: string
  connected: boolean
  connectionLabel: string
  deviceReady: boolean
  deviceLabel: string
  rescanLabel: string
  primaryLabel: string
  menuLabel: string
  closeLabel: string
  mobileOpen: boolean
  mobileExpanded: Record<string, boolean>
}>()

const emit = defineEmits<{
  'select-view': [id: string]
  'toggle-mobile': []
  'toggle-group': [id: string]
  rescan: []
}>()

const iconMap: Record<string, Component> = {
  overview: DashboardOutlined,
  sms: MailOutlined,
  esim: CreditCardOutlined,
  network: GlobalOutlined,
  gps: EnvironmentOutlined,
  calls: PhoneOutlined,
  vowifi: WifiOutlined,
  'raw-at': CodeOutlined,
  notifications: BellOutlined,
  settings: SettingOutlined,
}

function navIcon(id: string) {
  return iconMap[id] || ApiOutlined
}
</script>

<template>
  <a-layout :class="['app-shell', { 'mobile-nav-visible': props.mobileOpen }]">
    <a-layout-sider
      :class="['sidebar', { 'mobile-nav-open': props.mobileOpen }]"
      :width="248"
      :collapsed-width="0"
      :trigger="null"
    >
      <div class="brand">
        <span class="brand-mark" aria-hidden="true"><ApiOutlined /></span>
        <div class="brand-copy">
          <strong>DJOneHub</strong>
          <small>{{ props.brandSubtitle }}</small>
        </div>
      </div>

      <a-menu id="primary-nav" class="nav-menu" mode="inline" theme="dark" :selected-keys="[props.active]">
        <a-menu-item-group v-for="group in props.navGroups" :key="group.id">
          <template #title>
            <button
              class="menu-group-title"
              type="button"
              :aria-label="group.label"
              @click.stop="emit('toggle-group', group.id)"
            >
              <span>{{ group.label }}</span>
              <span class="mobile-nav-chevron" aria-hidden="true">
                <UpOutlined v-if="props.mobileExpanded[group.id]" />
                <DownOutlined v-else />
              </span>
            </button>
          </template>
          <a-menu-item
            v-for="item in group.items"
            v-if="props.mobileExpanded[group.id]"
            :key="item.id"
            @click="emit('select-view', item.id)"
          >
            <template #icon><component :is="navIcon(item.id)" /></template>
            <span>{{ item.label }}</span>
          </a-menu-item>
        </a-menu-item-group>
      </a-menu>

      <div class="sidebar-footer">
        <div class="ws-status">
          <span :class="['dot', { live: props.connected }]" aria-hidden="true" />
          <span>{{ props.connectionLabel }}</span>
        </div>
        <div class="ws-status">
          <span :class="['dot', { live: props.deviceReady }]" aria-hidden="true" />
          <span>{{ props.deviceLabel }}</span>
        </div>
        <button class="sidebar-rescan" type="button" :title="props.rescanLabel" @click="emit('rescan')">
          <span class="sidebar-rescan-icon" aria-hidden="true"><ReloadOutlined /></span>
          <span>{{ props.rescanLabel }}</span>
        </button>
      </div>
    </a-layout-sider>

    <button
      v-if="props.mobileOpen"
      class="mobile-nav-scrim"
      type="button"
      :aria-label="props.closeLabel"
      @click="emit('toggle-mobile')"
    />

    <a-layout-content class="main-content">
      <button
        class="mobile-menu-toggle mobile-menu-toggle-main"
        type="button"
        :aria-label="props.mobileOpen ? props.closeLabel : props.menuLabel"
        aria-controls="primary-nav"
        :aria-expanded="props.mobileOpen"
        @click="emit('toggle-mobile')"
      >
        <CloseOutlined v-if="props.mobileOpen" aria-hidden="true" />
        <MenuOutlined v-else aria-hidden="true" />
      </button>
      <slot />
    </a-layout-content>
  </a-layout>
</template>
