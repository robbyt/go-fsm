package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/robbyt/go-fsm"
)

// Define custom states
const (
	StatusOnline  = "StatusOnline"
	StatusOffline = "StatusOffline"
	StatusUnknown = "StatusUnknown"
)

func main() {
	// Define allowed transitions
	allowed := fsm.TransitionsConfig{
		StatusOnline:  []string{StatusOffline, StatusUnknown},
		StatusOffline: []string{StatusOnline, StatusUnknown},
		StatusUnknown: []string{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a new logger that omits the time attribute
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{} // Omit the time attribute
			}
			return a
		},
	})
	logger := slog.New(handler).WithGroup("example")

	// Create a new FSM
	fsm, err := fsm.New(logger.Handler(), StatusOnline, allowed)

	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	// Listen for state changes, and print them
	listener := fsm.GetStateChan(ctx)
	go func() {
		for {
			select {
			case state := <-listener:
				logger.Info("State change received", "state", state)

				if state == StatusOffline {
					cancel()
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	// Transition to a new state
	err = fsm.Transition(StatusOffline)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	<-ctx.Done()
	logger.Info("Done.")
}
