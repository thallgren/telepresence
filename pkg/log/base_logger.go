package log

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/datawire/dlib/dlog"
)

func MakeBaseLogger(ctx context.Context, logLevel string) context.Context {
	logrusLogger := logrus.New()
	logrusFormatter := NewFormatter("2006-01-02 15:04:05.0000")
	logrusLogger.SetFormatter(logrusFormatter)

	SetLogrusLevel(logrusLogger, logLevel)

	ctx = WithLogrusLogger(ctx, logrusLogger)
	logger := dlog.WrapLogrus(logrusLogger)
	dlog.SetFallbackLogger(logger)
	ctx = dlog.WithLogger(ctx, logger)
	var lvl dlog.LogLevel
	switch logrusLogger.GetLevel() {
	case logrus.ErrorLevel:
		lvl = dlog.LogLevelError
	case logrus.WarnLevel:
		lvl = dlog.LogLevelWarn
	case logrus.InfoLevel:
		lvl = dlog.LogLevelInfo
	case logrus.DebugLevel:
		lvl = dlog.LogLevelDebug
	case logrus.TraceLevel:
		lvl = dlog.LogLevelTrace
	default:
		panic("invalid loglevel")
	}
	ctx = context.WithValue(ctx, maxLevelKey{}, lvl)
	return WithLevelSetter(ctx, logrusLogger)
}

type logrusLoggerKey struct{}

func WithLogrusLogger(ctx context.Context, logger *logrus.Logger) context.Context {
	return context.WithValue(ctx, logrusLoggerKey{}, logger)
}

type maxLevelKey struct{}

func MaxLogLevel(ctx context.Context) dlog.LogLevel {
	if lr, ok := ctx.Value(maxLevelKey{}).(dlog.LogLevel); ok {
		return lr
	}
	return dlog.LogLevelTrace
}
