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
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/robbyt/go-fsm/v2/transitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))

		var output bytes.Buffer
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := run(ctx, logger, &output)
		require.NoError(t, err)
		require.NotNil(t, machine)

		// Wait for all transitions and broadcasts to complete
		synctest.Wait()

		// Cancel context to stop the state broadcast
		cancel()

		// Wait for goroutine to finish
		synctest.Wait()

		// Verify all three states were printed
		outputStr := output.String()
		assert.Contains(t, outputStr, "State: New")
		assert.Contains(t, outputStr, "State: Booting")
		assert.Contains(t, outputStr, "State: Running")

		// Verify states appear in order
		lines := strings.Split(strings.TrimSpace(outputStr), "\n")
		require.Len(t, lines, 3)
		assert.Equal(t, "State: New", lines[0])
		assert.Equal(t, "State: Booting", lines[1])
		assert.Equal(t, "State: Running", lines[2])

		// Verify final state
		assert.Equal(t, transitions.StatusRunning, machine.GetState())
	})
}
