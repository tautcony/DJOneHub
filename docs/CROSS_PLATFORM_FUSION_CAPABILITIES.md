# Cross-Platform Capability Matrix

This matrix records what the repository implements and what still requires
platform or real-device evidence. A cross-compiled binary is not hardware
verification.

| Platform | Discovery | Serial/USB | Network status | QMI/MBIM | Packet tunnel | Service install |
| --- | --- | --- | --- | --- | --- | --- |
| Linux | sysfs USB discovery; fake-sysfs tested | AT path is exposed to the AT backend; real serial evidence pending | interface/address/counter read | candidate mode detection only; backend wiring pending | Linux TUN open adapter; XFRM/VoWiFi hardware evidence pending | structured unsupported result |
| macOS | DJI/Quectel IOKit-style `ioreg` discovery; regression recorded | libusb AT path and serial fallback | legacy route has hardware evidence; new adapter control pending | MBIM/QMI hardware evidence pending | structured unsupported result | structured unsupported result |
| Windows | explicit structured unsupported result | SetupAPI/COM/WinUSB pending | explicit structured unsupported result | explicit structured unsupported result | structured unsupported result | structured unsupported result |

## Runtime directories

Platform adapters register log, configuration, data, and permission guidance
through `internal/platform/unsupported.PathsFor`. Installers must create these
directories with the service account permissions described by the selected
adapter.

## Build and release

Run `scripts/build-platforms.sh` on a host with the required Go toolchain. The
script emits SHA-256 files and generates an SPDX SBOM when `syft` is installed.
Signing, notarization, and Windows service packaging remain release-owner
steps until platform credentials and installers are available.

## Known gaps and rollback

- Linux QMI/MBIM control-device construction is not enabled by the generic app
  bootstrap yet; a discovered control-only device remains degraded rather than
  being treated as AT.
- macOS and Windows do not claim VoWiFi data-plane support.
- The legacy macOS entry remains available while route migration is incomplete.
- Rollback uses the single `cmd/djonehub` entry and `/api/*` routes;
  no device, SMS, eSIM, or local data is deleted by switching entry points.
