# third_party forks

Each directory here is a fork of an upstream Go module, wired in via a
`replace` directive in the root `go.mod`. Do not prune or bump any of them
without re-checking the fork against its upstream and updating the notes
below. If a fork ever becomes byte-identical to upstream, drop the `replace`
and resolve the upstream module directly.

## euicc-go (`github.com/damonto/euicc-go`)

- `driver/at/at.go`: `uiccat.Open` is called without `WithoutInit()` and
  without the context timeout — the device is already initialized by this
  product's own AT path, and the upstream init handshake would clash.
- `driver/http.go`: the SM-DP+ transport honours `HTTP_PROXY`/`HTTPS_PROXY`
  via `http.ProxyFromEnvironment`; upstream hardcodes a direct connection.
- `v2/profile.go`: adds `ProfileStatePresent` so a missing SGP.22 profile
  state decodes distinctly from an explicitly disabled state.
- `v2/rsp_header.go`: adds `RemoteExecutionError`, carrying the endpoint path
  and the ES9+/ES11 subject/reason codes. Request bodies are deliberately not
  retained — they can hold activation credentials and certificate material.
- `v2/types.go`: `InvokeHTTP` returns that structured `RemoteExecutionError`
  instead of upstream's flattened `errors.New`, so download failures stay
  diagnosable.
- `go.mod`: pins a newer `github.com/damonto/uicc-go`.

## gadb (`github.com/electricbubble/gadb`)

- `client.go`/`device.go`: `parseDeviceLine` handles `adb devices` entries
  without a serial number (shown as `(no serial number)`), and parses device
  attributes without mis-splitting the line.

## netlink (`github.com/iniwex5/netlink`, fork of `github.com/vishvananda/netlink`)

The `replace` here is load-bearing for a different reason than the others:
`github.com/iniwex5/netlink` is not a resolvable repository, so the module
cannot be fetched at all without it. The fork itself is a vishvananda master
snapshot with the module path rewritten.

It is pulled in transitively by `github.com/iniwex5/quectel-qmi-go`, which
only uses the stable surface (`LinkByName`, `AddrList`/`AddrAdd`/`AddrDel`,
`RouteAdd`/`RouteDel`/`RouteList`, `LinkSetUp`/`LinkSetDown`/`LinkSetMTU`).
None of the fork's additions over upstream are used. The way to remove this
fork is to change `quectel-qmi-go` to import `github.com/vishvananda/netlink`
directly; swapping the `replace` target here does not work, because upstream
imports its own `/nl` subpackage by real path and Go rejects one module
serving two paths.
