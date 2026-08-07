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

The following dependencies are fetched directly through Go modules and retain their upstream licenses and copyright notices: `github.com/damonto/uicc-go`, `github.com/iniwex5/quectel-qmi-go`, `github.com/lestrrat-go/strftime`, `github.com/pkg/errors`, `golang.org/x/sys`, `golang.org/x/text`, and `go.uber.org/multierr`.

This file is informational and does not replace any component's full license text.
