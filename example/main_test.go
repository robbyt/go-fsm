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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTransitionsConfig(t *testing.T) {
	t.Parallel()

	config := getTransitionsConfig()

	// Test that the transition config has the expected states
	assert.Contains(t, config, StatusOnline, "Config should contain StatusOnline")
	assert.Contains(t, config, StatusOffline, "Config should contain StatusOffline")
	assert.Contains(t, config, StatusUnknown, "Config should contain StatusUnknown")

	// Test that each state has the expected transitions
	assert.ElementsMatch(t, []string{StatusOffline, StatusUnknown}, config[StatusOnline],
		"StatusOnline should transition to StatusOffline and StatusUnknown")
	assert.ElementsMatch(t, []string{StatusOnline, StatusUnknown}, config[StatusOffline],
		"StatusOffline should transition to StatusOnline and StatusUnknown")
	assert.Empty(t, config[StatusUnknown],
		"StatusUnknown should not have any allowed transitions")
}

func TestNewLogger(t *testing.T) {
	t.Parallel()

	logger := newLogger()

	// Check that the logger is not nil
	assert.NotNil(t, logger, "NewLogger should return a non-nil logger")
}

func TestNewStateMachine(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		initialState  string
		expectedError bool
	}{
		{
			name:          "Valid initial state",
			initialState:  StatusOnline,
			expectedError: false,
		},
		{
			name:          "Invalid initial state",
			initialState:  "InvalidState",
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger := newLogger()
			machine, err := newStateMachine(logger, tc.initialState)

			if tc.expectedError {
				require.Error(t, err, "Expected an error with invalid initial state")
				assert.Nil(t, machine, "Machine should be nil when error occurs")
			} else {
				require.NoError(t, err, "Should not error with valid initial state")
				assert.NotNil(t, machine, "Machine should not be nil with valid initial state")
				assert.Equal(t, tc.initialState, machine.GetState(),
					"Machine should have the correct initial state")
			}
		})
	}
}

func TestListenForStateChanges(t *testing.T) {
	t.Parallel()

	t.Run("Receives state changes", func(t *testing.T) {
		logger := newLogger()
		machine, err := newStateMachine(logger, StatusOnline)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		done := listenForStateChanges(ctx, logger, machine)

		// Wait a moment for the goroutine to start
		time.Sleep(50 * time.Millisecond)

		// Transition to a new state
		err = machine.Transition(StatusOffline)
		require.NoError(t, err)

		// Cancel context to trigger exit of the listener goroutine
		cancel()

		// Wait for the done signal from the goroutine
		require.Eventually(t, func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Timed out waiting for done signal")
	})

	t.Run("Handles context cancellation", func(t *testing.T) {
		logger := newLogger()
		machine, err := newStateMachine(logger, StatusOnline)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())

		done := listenForStateChanges(ctx, logger, machine)

		// Wait a moment for the goroutine to start
		time.Sleep(50 * time.Millisecond)

		// Cancel the context
		cancel()

		// Wait for the done signal from the goroutine
		require.Eventually(t, func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Timed out waiting for done signal after context cancellation")
	})
}

func TestWaitForOfflineState(t *testing.T) {
	t.Parallel()

	t.Run("Cancels context when state changes to offline", func(t *testing.T) {
		logger := newLogger()
		machine, err := newStateMachine(logger, StatusOnline)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())

		// Set up the wait for offline state
		waitForOfflineState(ctx, cancel, logger, machine)

		// Wait a moment for the goroutine to start
		time.Sleep(50 * time.Millisecond)

		// Transition to offline state
		err = machine.Transition(StatusOffline)
		require.NoError(t, err)

		// The context should be canceled shortly
		require.Eventually(t, func() bool {
			select {
			case <-ctx.Done():
				return true
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Timed out waiting for context to be cancelled")
	})
}

func TestIntegration(t *testing.T) {
	// This test mimics the flow in main() but in a testable way
	logger := newLogger()

	// Create a new FSM
	machine, err := newStateMachine(logger, StatusOnline)
	require.NoError(t, err)

	// Check initial state
	assert.Equal(t, StatusOnline, machine.GetState())

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Set up both listeners
	done := listenForStateChanges(ctx, logger, machine)
	waitForOfflineState(ctx, cancel, logger, machine)

	// Wait a moment for the goroutines to start
	time.Sleep(50 * time.Millisecond)

	// Transition to offline state
	err = machine.Transition(StatusOffline)
	require.NoError(t, err)

	// Wait for the done signal
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond, "Timed out waiting for done signal")

	// Verify final state
	assert.Equal(t, StatusOffline, machine.GetState())
}
