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

	// Create a done channel for synchronization
	done := make(chan struct{})

	// Listen for state changes, and print them
	listener := fsm.GetStateChan(ctx)
	go func() {
		defer close(done)
		for {
			select {
			case state := <-listener:
				logger.Info("State change received", "state", state)

				if state == StatusOffline {
					logger.Info("Received offline state, canceling context...")
					cancel()
					return
				}

			case <-ctx.Done():
				logger.Debug("Context done, exiting listener")
				return
			}
		}
	}()

	// Transition to a new state after a small delay
	// to ensure the goroutine is running
	time.Sleep(50 * time.Millisecond)
	logger.Debug("Transitioning to offline state")
	err = fsm.Transition(StatusOffline)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	// Wait for the done signal from the goroutine
	<-done
	logger.Info("Done.")
}
