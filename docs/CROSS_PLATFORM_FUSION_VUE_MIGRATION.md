# Vue Migration Compatibility

The DJOneHub Vue application is the only maintained management surface for the
single-device API at `/api/v1`. The macOS entry no longer embeds a separate
management page; its legacy `/api/*` routes remain available only for
integration compatibility.

The legacy entry can be removed only after all of the following are true:

- `/api/v1` has passed the macOS DJI/Quectel regression record.
- SMS, eSIM, network diagnostics, raw AT, and the verified VoWiFi controls are
  usable from the Vue application.
- Existing users have a documented migration path from the legacy `/api/*`
  endpoints and no supported installer starts the legacy entry by default.
New feature work belongs in `web/` and the versioned application services.
