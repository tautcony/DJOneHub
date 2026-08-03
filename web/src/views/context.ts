import { inject, type InjectionKey } from 'vue'

// The shell owns device polling and actions; routed views consume this shared
// surface without coupling page templates to the application root.
/* eslint-disable @typescript-eslint/no-explicit-any */
export type ViewContext = Record<string, any>

export const viewContextKey: InjectionKey<ViewContext> = Symbol('djonehub.view-context')

export function useViewContext() {
  const context = inject(viewContextKey)
  if (!context) throw new Error('View context is unavailable')
  return context
}
