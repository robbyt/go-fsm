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
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/hooks/broadcast"
	"github.com/robbyt/go-fsm/v2/transitions"
)

func run(parentCtx context.Context, logger *slog.Logger, output io.Writer) (*fsm.Machine, error) {
	// Create callback registry
	registry, err := hooks.NewRegistry(
		hooks.WithLogger(logger),
		hooks.WithTransitions(transitions.Typical),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry: %w", err)
	}

	// Create FSM with typical transitions
	machine, err := fsm.New(
		transitions.StatusNew,
		transitions.Typical,
		fsm.WithLogger(logger),
		fsm.WithCallbackRegistry(registry),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create FSM: %w", err)
	}

	// Create standalone broadcast manager
	broadcastManager := broadcast.NewManager(logger)

	// Register broadcast hook to enable state change notifications
	err = registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, broadcastManager.BroadcastHook())
	if err != nil {
		return nil, fmt.Errorf("failed to register broadcast hook: %w", err)
	}

	// Start goroutine to print state changes
	stateChan, err := broadcastManager.GetStateChan(parentCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to get state channel: %w", err)
	}

	// Send initial state to subscribers
	broadcastManager.Broadcast(machine.GetState())

	go func() {
		for state := range stateChan {
			// Writing to output writer; errors are acceptable in this demo
			//nolint:errcheck
			fmt.Fprintf(output, "State: %s\n", state)
		}
	}()

	// Perform state transitions
	time.Sleep(100 * time.Millisecond)
	if err := machine.Transition(transitions.StatusBooting); err != nil {
		return nil, fmt.Errorf("transition to Booting failed: %w", err)
	}

	time.Sleep(100 * time.Millisecond)
	if err := machine.Transition(transitions.StatusRunning); err != nil {
		return nil, fmt.Errorf("transition to Running failed: %w", err)
	}

	return machine, nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Set up signal handling for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	machine, err := run(ctx, logger, os.Stdout)
	if err != nil {
		logger.Error("application failed", "error", err)
		os.Exit(1)
	}

	logger.Info("Application running", "state", machine.GetState())
	logger.Info("Press Ctrl+C to shutdown...")

	// Wait for signal
	<-ctx.Done()

	// Print final state
	finalState := machine.GetState()
	logger.Info("Shutting down", "final_state", finalState)
	fmt.Printf("Final state: %s\n", finalState)
}
