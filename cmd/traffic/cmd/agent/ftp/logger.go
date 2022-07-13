package ftp

import (
	"context"
	"fmt"

	ftplog "github.com/fclairamb/go-log"

	"github.com/datawire/dlib/dlog"
	"github.com/telepresenceio/telepresence/v2/pkg/log"
)

func Logger(ctx context.Context) ftplog.Logger {
	return &logger{Context: ctx, maxLevel: log.MaxLogLevel(ctx)}
}

type logger struct {
	context.Context
	maxLevel dlog.LogLevel
}

func (d *logger) Debug(event string, keyvals ...any) {
	d.log(dlog.LogLevelDebug, event, keyvals)
}

func (d *logger) Info(event string, keyvals ...any) {
	d.log(dlog.LogLevelInfo, event, keyvals)
}

func (d *logger) Warn(event string, keyvals ...any) {
	d.log(dlog.LogLevelWarn, event, keyvals)
}

func (d *logger) Error(event string, keyvals ...any) {
	d.log(dlog.LogLevelError, event, keyvals)
}

func (d *logger) Panic(event string, keyvals ...any) {
	d.log(dlog.LogLevelError, event, keyvals)
	panic(event)
}

func (d *logger) With(keyvals ...any) ftplog.Logger {
	return d.with(keyvals)
}

func (d *logger) log(level dlog.LogLevel, event string, keyvals []any) {
	if d.maxLevel >= level {
		dlog.Log(d.with(keyvals), level, event)
	}
}

func (d *logger) with(keyvals []any) *logger {
	l := len(keyvals)
	if l == 0 {
		return d
	}
	if l%2 != 0 {
		keyvals = append(keyvals, "*** missing value ***")
		l++
	}
	c := d.Context
	for i := 0; i < l; i += 2 {
		c = dlog.WithField(c, fmt.Sprint(keyvals[i]), keyvals[i+1])
	}
	return &logger{Context: c, maxLevel: d.maxLevel}
}
