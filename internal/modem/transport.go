package modem

import "time"

// ATTransport is the byte-stream contract used by the shared AT command
// session. Platform adapters own discovery and opening. Manager owns the
// transport after construction and closes it during shutdown.
type ATTransport interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}

type atReadTimeoutSetter interface {
	SetReadTimeout(time.Duration) error
}
