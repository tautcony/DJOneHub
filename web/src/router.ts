import { createRouter, createWebHistory, type RouteComponent } from 'vue-router'

export type ViewID =
  | 'overview'
  | 'calls'
  | 'sms'
  | 'esim'
  | 'sim-profiles'
  | 'network'
  | 'raw-at'
  | 'vowifi'
  | 'notifications'
  | 'settings'
  | 'firmware'
  | 'runtime'

export const viewPaths: Record<ViewID, string> = {
  overview: '/overview',
  calls: '/calls',
  sms: '/sms',
  esim: '/esim',
  'sim-profiles': '/sim-profiles',
  network: '/network',
  'raw-at': '/raw-at',
  vowifi: '/vowifi',
  notifications: '/notifications',
  settings: '/settings',
  firmware: '/firmware',
  runtime: '/runtime',
}

const viewComponents: Record<ViewID, RouteComponent> = {
  overview: () => import('./views/OverviewView.vue'),
  calls: () => import('./views/CallsView.vue'),
  sms: () => import('./views/SmsView.vue'),
  esim: () => import('./views/EsimView.vue'),
  'sim-profiles': () => import('./views/SimProfilesView.vue'),
  network: () => import('./views/NetworkView.vue'),
  'raw-at': () => import('./views/RawAtView.vue'),
  vowifi: () => import('./views/VowifiView.vue'),
  notifications: () => import('./views/NotificationsView.vue'),
  settings: () => import('./views/SettingsView.vue'),
  firmware: () => import('./views/FirmwareView.vue'),
  runtime: () => import('./views/RuntimeView.vue'),
}

export function viewFromRoute(value: unknown): ViewID {
  return (Object.entries(viewPaths).find(([, path]) => path === value)?.[0] as ViewID) || 'overview'
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: viewPaths.overview },
    ...Object.entries(viewPaths).map(([name, path]) => ({
      path,
      name,
      component: viewComponents[name as ViewID],
    })),
    { path: '/:pathMatch(.*)*', redirect: viewPaths.overview },
  ],
})
