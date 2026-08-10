<script setup lang="ts">
import { ref, watch } from 'vue'
import type { Component } from 'vue'
import {
  ApiOutlined,
  CloudDownloadOutlined,
  BellOutlined,
  CloseOutlined,
  CodeOutlined,
  CreditCardOutlined,
  DashboardOutlined,
  GlobalOutlined,
  MailOutlined,
  MenuOutlined,
  PhoneOutlined,
  ReloadOutlined,
  SettingOutlined,
  DeploymentUnitOutlined,
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
}>()

const emit = defineEmits<{
  'select-view': [id: string]
  'toggle-mobile': []
  rescan: []
}>()

const openKeys = ref<string[]>(props.navGroups.map((group) => group.id))

watch(
  () => props.navGroups,
  (groups) => {
    const available = new Set(groups.map((group) => group.id))
    openKeys.value = openKeys.value.filter((key) => available.has(key))
  },
)

watch(
  () => props.active,
  (active) => {
    const group = props.navGroups.find((item) => item.items.some((item) => item.id === active))
    if (group && !openKeys.value.includes(group.id)) openKeys.value = [...openKeys.value, group.id]
  },
)

function handleOpenChange(keys: string[]) {
  openKeys.value = keys
}

const iconMap: Record<string, Component> = {
  overview: DashboardOutlined,
  sms: MailOutlined,
  esim: CreditCardOutlined,
  network: GlobalOutlined,
  calls: PhoneOutlined,
  vowifi: WifiOutlined,
  'raw-at': CodeOutlined,
  notifications: BellOutlined,
  settings: SettingOutlined,
  firmware: CloudDownloadOutlined,
  runtime: DeploymentUnitOutlined,
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
        </div>
      </div>

      <a-menu
        id="primary-nav"
        class="nav-menu"
        mode="inline"
        theme="dark"
        :selected-keys="[props.active]"
        :open-keys="openKeys"
        @open-change="handleOpenChange"
      >
        <a-sub-menu v-for="group in props.navGroups" :key="group.id">
          <template #title>{{ group.label }}</template>
          <a-menu-item v-for="item in group.items" :key="item.id" @click="emit('select-view', item.id)">
            <template #icon><component :is="navIcon(item.id)" /></template>
            <span>{{ item.label }}</span>
          </a-menu-item>
        </a-sub-menu>
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
      <a-button
        type="text"
        shape="circle"
        class="mobile-menu-toggle mobile-menu-toggle-main"
        :aria-label="props.mobileOpen ? props.closeLabel : props.menuLabel"
        aria-controls="primary-nav"
        :aria-expanded="props.mobileOpen"
        @click="emit('toggle-mobile')"
      >
        <CloseOutlined v-if="props.mobileOpen" aria-hidden="true" />
        <MenuOutlined v-else aria-hidden="true" />
      </a-button>
      <slot />
    </a-layout-content>
  </a-layout>
</template>
