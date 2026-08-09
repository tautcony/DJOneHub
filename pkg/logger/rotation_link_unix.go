//go:build !windows

package logger

import rotatelogs "github.com/lestrrat-go/file-rotatelogs"

func currentLogOptions(filename string) []rotatelogs.Option {
	return []rotatelogs.Option{rotatelogs.WithLinkName(filename)}
}

func newPlatformRotationHandler(string) rotatelogs.Handler {
	return nil
}
