/*
Copyright 2024 Robert Terhaar <robbyt@robbyt.net>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/robbyt/go-fsm"
	"github.com/robbyt/go-fsm/hooks"
	"github.com/robbyt/go-fsm/transitions"
)

// Define custom states
const (
	StatusOnline  = "StatusOnline"
	StatusOffline = "StatusOffline"
	StatusUnknown = "StatusUnknown"
)

// newLogger creates a new logger with time attribute omitted
func newLogger() *slog.Logger {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{} // Omit the time attribute
			}
			return a
		},
	})
	return slog.New(handler).WithGroup("example")
}

// getTransitionsConfig returns the allowed state transition configuration
func getTransitionsConfig() *transitions.Config {
	return transitions.MustNew(map[string][]string{
		StatusOnline:  {StatusOffline, StatusUnknown},
		StatusOffline: {StatusOnline, StatusUnknown},
		StatusUnknown: {},
	})
}

// newStateMachine creates a new FSM with the given logger and initial state
func newStateMachine(logger *slog.Logger, initialState string) (*fsm.Machine, error) {
	transitionsConfig := getTransitionsConfig()

	// Create and configure callback registry
	registry, err := hooks.NewSynchronousCallbackRegistry(
		hooks.WithLogger(logger),
		hooks.WithTransitions(transitionsConfig),
	)
	if err != nil {
		return nil, err
	}

	// Add transition action for going offline
	err = registry.RegisterPreTransitionHook([]string{StatusOnline}, []string{StatusOffline}, func(ctx context.Context, from, to string) error {
		logger.Info("Shutting down services", "from", from, "to", to)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Add transition action for going online
	err = registry.RegisterPreTransitionHook([]string{StatusOffline}, []string{StatusOnline}, func(ctx context.Context, from, to string) error {
		logger.Info("Starting services", "from", from, "to", to)
		return nil
	})
	if err != nil {
		return nil, err
	}

	machine, err := fsm.New(
		logger.Handler(),
		initialState,
		transitionsConfig,
		fsm.WithCallbackRegistry(registry),
	)
	if err != nil {
		return nil, err
	}

	// Register broadcast hook to enable state change notifications
	err = registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, func(ctx context.Context, from, to string) {
		machine.Broadcast.Broadcast(to)
	})
	if err != nil {
		return nil, err
	}

	return machine, nil
}

// listenForStateChanges starts a goroutine that listens for state changes
// Returns a channel that will be closed when the listener exits
func listenForStateChanges(
	ctx context.Context,
	logger *slog.Logger,
	machine *fsm.Machine,
) chan struct{} {
	done := make(chan struct{})
	listener := machine.GetStateChan(ctx)

	go func() {
		defer close(done)
		for {
			select {
			case state, ok := <-listener:
				if !ok {
					logger.Debug("State channel closed")
					return
				}

				logger.Info("State change received", "state", state)

			case <-ctx.Done():
				logger.Debug("Context done, exiting listener")
				return
			}
		}
	}()

	return done
}

// waitForOfflineState waits for the machine to transition to StatusOffline and then cancels the context
func waitForOfflineState(
	ctx context.Context,
	cancel context.CancelFunc,
	logger *slog.Logger,
	machine *fsm.Machine,
) {
	stateChan := machine.GetStateChan(ctx)

	go func() {
		for state := range stateChan {
			if state == StatusOffline {
				logger.Info("Received offline state, canceling context...")
				cancel()
				return
			}
		}
	}()
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := newLogger()

	// Create a new FSM
	machine, err := newStateMachine(logger, StatusOnline)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	// Listen and log all state changes
	done := listenForStateChanges(ctx, logger, machine)

	// Set up a 2nd listener specifically for acting on the offline state
	waitForOfflineState(ctx, cancel, logger, machine)

	// Transition to StatusOffline after a small delay
	time.Sleep(100 * time.Millisecond)
	logger.Debug("Transitioning to offline state")
	err = machine.Transition(StatusOffline)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	// Wait for the done signal from the listner goroutine
	<-done
	logger.Info("Done.")
}
