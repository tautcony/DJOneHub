# Third-Party Notices

DJOneHub contains code derived from the upstream VoHive project and retains the license and required notice provided in the repository root [`LICENSE`](LICENSE):

```text
Required Notice: Copyright iniwex5 (https://github.com/iniwex5/vohive)
```

## Release Runtime

The macOS release package includes **libusb 1.0.30**, distributed under the GNU Lesser General Public License, version 2.1 or later.

- Project: <https://libusb.info/>
- Source: <https://github.com/libusb/libusb/releases/tag/v1.0.30>
- License text in the release package: `licenses/libusb-COPYING`

## Vendored Source Dependencies

The source repository includes selected source dependencies under `third_party/` so project-specific changes remain reproducible. Their original copyright notices and license texts are retained in the corresponding directories.

| Component | License file |
| --- | --- |
| euicc-go | `third_party/euicc-go/LICENSE` |
| gadb | `third_party/gadb/LICENSE` |
| github.com/iniwex5/netlink | `third_party/netlink/LICENSE` |
| github.com/iniwex5/vowifi-go | `third_party/vowifi-go/LICENSE` (GNU Affero General Public License v3) |

`internal/upstreamproxy`（国家前置代理的 MCC 国家表与 SOCKS5 自检）逐字复制自上游 VoHive 项目（<https://github.com/iniwex5/vohive>），随 VoHive 的 AGPL-3.0 许可使用。

The following dependencies are fetched directly through Go modules and retain their upstream licenses and copyright notices: `github.com/damonto/uicc-go`, `github.com/iniwex5/quectel-qmi-go`, `github.com/lestrrat-go/strftime`, `github.com/pkg/errors`, `golang.org/x/sys`, `golang.org/x/text`, `go.uber.org/multierr`, `github.com/emiago/sipgo`, and the `github.com/pion/*` WebRTC stack (brought in by vowifi-go).

This file is informational and does not replace any component's full license text.
