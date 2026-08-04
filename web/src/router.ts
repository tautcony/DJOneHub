import { createRouter, createWebHistory } from 'vue-router'

export type ViewID =
  | 'overview'
  | 'calls'
  | 'sms'
  | 'esim'
  | 'network'
  | 'raw-at'
  | 'vowifi'
  | 'notifications'
  | 'settings'

export const viewPaths: Record<ViewID, string> = {
  overview: '/overview',
  calls: '/calls',
  sms: '/sms',
  esim: '/esim',
  network: '/network',
  'raw-at': '/raw-at',
  vowifi: '/vowifi',
  notifications: '/notifications',
  settings: '/settings',
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
      component: () =>
        import(`./views/${name === 'raw-at' ? 'RawAt' : name[0].toUpperCase() + name.slice(1)}View.vue`),
    })),
    { path: '/:pathMatch(.*)*', redirect: viewPaths.overview },
  ],
})
