module github.com/iniwex5/vohive

go 1.26.4

// Fork patch note: each replace below points at a genuine fork under
// third_party/ that differs from its upstream module. Before bumping or
// pruning any of these, re-check the fork against upstream and update the
// note to match. See third_party/README.md for what each fork patches.
replace github.com/damonto/euicc-go => ./third_party/euicc-go

replace github.com/electricbubble/gadb => ./third_party/gadb

replace github.com/iniwex5/netlink => ./third_party/netlink

replace github.com/iniwex5/vowifi-go => ./third_party/vowifi-go

require (
	github.com/damonto/euicc-go v1.1.3-0.20260628013808-8d873a2dfc98
	github.com/electricbubble/gadb v0.1.0
	github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1
	github.com/gorilla/websocket v1.5.3
	github.com/iniwex5/quectel-qmi-go v0.6.0
	github.com/iniwex5/vowifi-go v1.1.2
	github.com/larksuite/oapi-sdk-go/v3 v3.9.10
	github.com/lestrrat-go/file-rotatelogs v2.4.0+incompatible
	github.com/warthog618/sms v0.3.0
	go.bug.st/serial v1.8.0
	go.uber.org/zap v1.28.0
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/sync v0.22.0
	golang.org/x/sys v0.47.0
	modernc.org/sqlite v1.56.0
)

require (
	github.com/damonto/uicc-go v0.0.0-20260629073618-7ddada6bb13e // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/iniwex5/netlink v1.3.3 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/lestrrat-go/strftime v1.2.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	modernc.org/libc v1.75.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.0 // indirect
)
