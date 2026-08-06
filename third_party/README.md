# third_party forks

Each directory here is a fork of an upstream Go module, wired in via a
`replace` directive in the root `go.mod`. All four are genuine patches —
do not prune or bump any of them without re-checking the fork against its
upstream and updating the notes below. If a fork ever becomes byte-identical
to upstream, drop the `replace` and resolve the upstream module directly.

## euicc-go (`github.com/damonto/euicc-go`)

- `driver/at/at.go`: `uiccat.Open` is called without `WithoutInit()` and
  without the context timeout — the device is already initialized by this
  product's own AT path, and the upstream init handshake would clash.
- `v2/profile.go`: adds `ProfileStatePresent` so a missing SGP.22 profile
  state decodes distinctly from an explicitly disabled state.
- `go.mod`: pins a newer `github.com/damonto/uicc-go`.

## gadb (`github.com/electricbubble/gadb`)

- `client.go`/`device.go`: `parseDeviceLine` handles `adb devices` entries
  without a serial number (shown as `(no serial number)`), and parses device
  attributes without mis-splitting the line.

## x-sys (`golang.org/x/sys`)

- `windows/syscall_windows.go`: `RtlInitString` input is length-checked
  against `MAX_USHORT` (the source string plus its NUL must fit in the
  USHORT length field) and built from a byte slice instead of a NUL-expecting
  pointer, avoiding overflow for long strings.
- `windows/types_windows.go`: adds the `MAX_USHORT` constant.
- `windows/security_windows.go` + tests: documents that `TrusteeValue`
  helpers require `runtime.Pinner` pinning.

## netlink (`github.com/iniwex5/netlink`, fork of `github.com/vishvananda/netlink`)

- `addr.go`/`addr_linux.go`: adds `IFA_PROTO` / `IFAPROT_*` constants and an
  `Addr.Protocol` field (address protocol/origin, kernel 5.18+).
- `link.go`: adds newer vlan/qdisc fields (`Headroom`/`Tailroom`, `ParentDev`,
  `IngressQosMap`/`EgressQosMap`, `ReorderHdr`/`Gvrp`).
