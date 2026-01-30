package klog

import (
	"io"

	"github.com/ccontavalli/enkit/lib/logger"
)

// Tee forwards log messages to both primary and secondary loggers.
type Tee struct {
	Primary   logger.Logger
	Secondary logger.Logger
}

// NewTee returns a Logger that forwards to both loggers.
func NewTee(primary, secondary logger.Logger) logger.Logger {
	return &Tee{Primary: primary, Secondary: secondary}
}

func (t *Tee) Debugf(format string, args ...interface{}) {
	t.forward(func(log logger.Logger) { log.Debugf(format, args...) })
}

func (t *Tee) Infof(format string, args ...interface{}) {
	t.forward(func(log logger.Logger) { log.Infof(format, args...) })
}

func (t *Tee) Warnf(format string, args ...interface{}) {
	t.forward(func(log logger.Logger) { log.Warnf(format, args...) })
}

func (t *Tee) Errorf(format string, args ...interface{}) {
	t.forward(func(log logger.Logger) { log.Errorf(format, args...) })
}

func (t *Tee) SetOutput(writer io.Writer) {
	t.forward(func(log logger.Logger) { log.SetOutput(writer) })
}

func (t *Tee) forward(fn func(logger.Logger)) {
	if t.Primary != nil {
		fn(t.Primary)
	}
	if t.Secondary != nil && t.Secondary != t.Primary {
		fn(t.Secondary)
	}
}
