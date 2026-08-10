## Why

The frontend has a consistent framework baseline, but its visual and code rules are not fully enforced. Capability navigation, appearance support, shared feedback, and quality checks must use one verified contract.

## What Changes

- Hide a navigation entry when the active capability snapshot does not include its required capability.
- Add light, dark, and system appearance modes.
- Apply one theme source to Ant Design Vue and project CSS tokens.
- Use shared status and danger-confirmation behavior in the affected workflows.
- Make the page-header eyebrow visible and remove the hidden component state.
- Remove the store-to-view type dependency for SMS threads.
- Declare direct frontend dependencies as direct package dependencies.
- Make lint and format checks pass without warnings.
- Add frontend quality checks to continuous integration.
- Add focused tests for capability navigation and appearance resolution.
- Record later work for root-component, context, style, and bundle decomposition.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `vue-management-ui`: Add the appearance-mode contract and clarify shared interaction and verification behavior.

## Impact

The change affects the Vue application shell, appearance settings, shared UI utilities, frontend styles, dependencies, tests, and continuous integration. The HTTP API and device protocol contracts do not change.
