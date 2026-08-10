## Context

The frontend uses Vue 3, Pinia, Ant Design Vue, and project CSS tokens. The audit found contract conflicts and several sources of visual drift.

The server capability snapshot remains authoritative. The application must work without hardware and must not present actions that the active device cannot perform.

## Goals / Non-Goals

**Goals:**

- Make capability navigation match the current specification.
- Add persistent light, dark, and system appearance modes.
- Use the same resolved theme for Ant Design Vue and project CSS.
- Use shared status and danger-confirmation behavior in affected pages.
- Enforce clean type, lint, format, test, and build results.
- Remove the SMS store dependency on the view layer.

**Non-Goals:**

- Do not change an HTTP or WebSocket schema.
- Do not change device capability declarations.
- Do not complete the full `App.vue` or `style.css` decomposition in this change.
- Do not change the special layout of the SMS, eSIM, firmware, or runtime workbenches.

## Decisions

### Store the appearance preference in Pinia

A dedicated appearance store owns `light`, `dark`, and `system`. The store persists the preference in local storage.

The store listens to `prefers-color-scheme`. The system preference changes the resolved value only in system mode.

This choice keeps appearance state out of `App.vue`. A component-local preference would not control the complete application.

### Apply one resolved theme to two rendering systems

The application wraps its shell in Ant Design Vue `ConfigProvider`. The resolved mode selects the Ant Design light or dark algorithm.

The same resolved mode sets `data-theme` on the document root. Project CSS reads semantic tokens from that attribute.

This choice retains Ant Design Vue. A replacement component library would increase risk without solving the current contract conflict.

### Filter navigation before rendering

A pure navigation helper removes items whose required capability is absent. Items without a capability requirement remain visible.

The helper is independent of Vue. Focused tests cover ready and empty capability snapshots.

### Use one application confirmation helper

A shared helper uses Ant Design Vue `Modal.confirm`. The helper returns a promise and supports localized button text.

Affected firmware and network actions use this helper. Existing `Popconfirm` controls remain valid for row-level actions.

### Reuse semantic status tones

The firmware page replaces private status dots with `StatusLight`. The page maps boolean state to success or neutral.

Special labels can retain their shape. They must use the shared semantic token colors.

### Add focused frontend tests

Vitest tests pure navigation and appearance resolution behavior. The tests do not require a device or a backend.

The continuous integration workflow runs type, lint, format, test, and build checks.

## Risks / Trade-offs

- [Dark theme can expose hard-coded page colors] -> Add dark semantic tokens and targeted overrides for all current workbenches.
- [Capability navigation can shrink while a device is not ready] -> Keep entries without capability requirements visible and keep the overview available.
- [System-theme listeners can leak] -> Register one listener in the appearance store and remove obsolete listeners before replacement.
- [Static confirmation dialogs can outlive a route] -> Use them only for immediate user actions and resolve cancellation without side effects.
- [New tests add a development dependency] -> Use Vitest only and keep the test surface limited to pure logic.

## Migration Plan

1. Add appearance and navigation utilities with tests.
2. Apply the provider, tokens, controls, and capability filtering.
3. Replace affected confirmation and status implementations.
4. Fix dependency declarations and code-format findings.
5. Add continuous integration checks.
6. Run all frontend checks and inspect light and dark layouts.

The appearance preference defaults to `system`. Existing users do not need a data migration.

## Open Questions

None.
