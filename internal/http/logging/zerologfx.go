package logging

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.uber.org/fx/fxevent"
)

func NewZerologfx() fxevent.Logger {
	return &zerologfx{logger: log.Logger}
}

type zerologfx struct {
	logger zerolog.Logger
}

func (l *zerologfx) LogEvent(event fxevent.Event) {
	switch e := event.(type) {
	case *fxevent.OnStartExecuted:
		l.level(e.Err, "OnStart", func(ev *zerolog.Event) *zerolog.Event {
			return ev.Str("callee", e.FunctionName).Str("caller", e.CallerName)
		})
	case *fxevent.OnStopExecuted:
		l.level(e.Err, "OnStop", func(ev *zerolog.Event) *zerolog.Event {
			return ev.Str("callee", e.FunctionName).Str("caller", e.CallerName)
		})
	case *fxevent.Provided:
		l.level(e.Err, "Provided", func(ev *zerolog.Event) *zerolog.Event {
			return ev.Str("constructor", e.ConstructorName).Strs("types", e.OutputTypeNames)
		})
	case *fxevent.Invoked:
		l.level(e.Err, "Invoked", func(ev *zerolog.Event) *zerolog.Event {
			return ev.Str("function", e.FunctionName)
		})
	case *fxevent.Supplied:
		l.level(e.Err, "Supplied", func(ev *zerolog.Event) *zerolog.Event {
			return ev.Str("type", e.TypeName)
		})
	}
}

func (l *zerologfx) level(err error, fxevent string, fields func(*zerolog.Event) *zerolog.Event) {
	if err != nil {
		fields(l.logger.Error().Err(err)).Msg(fxevent + " failed")
	}

	fields(l.logger.Info()).Msg(fxevent + " succeeded")
}
