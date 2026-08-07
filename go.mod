module github.com/iniwex5/vohive

go 1.26.3

// Fork patch note: each replace below points at a genuine fork under
// third_party/ that differs from its upstream module. Before bumping or
// pruning any of these, re-check the fork against upstream and update the
// note to match. See third_party/README.md for what each fork patches.
replace github.com/damonto/euicc-go => ./third_party/euicc-go

replace github.com/electricbubble/gadb => ./third_party/gadb

replace github.com/iniwex5/netlink => ./third_party/netlink

require (
	github.com/damonto/euicc-go v1.1.3-0.20260628013808-8d873a2dfc98
	github.com/electricbubble/gadb v0.1.0
	github.com/gorilla/websocket v1.5.3
	github.com/iniwex5/quectel-qmi-go v0.6.0
	github.com/lestrrat-go/file-rotatelogs v2.4.0+incompatible
	github.com/warthog618/sms v0.3.0
	go.bug.st/serial v1.6.4
	go.uber.org/zap v1.27.1
	go.yaml.in/yaml/v3 v3.0.4
	golang.org/x/sync v0.20.0
	golang.org/x/sys v0.46.0
	modernc.org/sqlite v1.23.1
)

require (
	github.com/creack/goselect v0.1.2 // indirect
	github.com/damonto/uicc-go v0.0.0-20260629073618-7ddada6bb13e // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/iniwex5/netlink v1.3.3 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/lestrrat-go/strftime v1.2.0 // indirect
	github.com/mattn/go-isatty v0.0.16 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/mod v0.34.0 // indirect
	golang.org/x/tools v0.43.0 // indirect
	lukechampine.com/uint128 v1.2.0 // indirect
	modernc.org/cc/v3 v3.40.0 // indirect
	modernc.org/ccgo/v3 v3.16.13 // indirect
	modernc.org/libc v1.22.5 // indirect
	modernc.org/mathutil v1.5.0 // indirect
	modernc.org/memory v1.5.0 // indirect
	modernc.org/opt v0.1.3 // indirect
	modernc.org/strutil v1.1.3 // indirect
	modernc.org/token v1.0.1 // indirect
)
