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

package broadcast_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/robbyt/go-fsm/v2/hooks/broadcast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

func TestNewManager(t *testing.T) {
	t.Parallel()

	manager := broadcast.NewManager(newTestLogger())
	assert.NotNil(t, manager)
}

func TestGetStateChan(t *testing.T) {
	t.Parallel()

	manager := broadcast.NewManager(newTestLogger())

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch, err := manager.GetStateChan(ctx)
	require.NoError(t, err)
	assert.NotNil(t, ch)

	// Send initial state
	manager.Broadcast("StateA")

	// Should receive initial state immediately
	require.Eventually(t, func() bool {
		select {
		case state := <-ch:
			return assert.Equal(t, "StateA", state)
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "Expected to receive initial state")

	// Broadcast new state
	manager.Broadcast("StateB")

	require.Eventually(t, func() bool {
		select {
		case state := <-ch:
			return assert.Equal(t, "StateB", state)
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "Expected to receive broadcast state")

	// Cancel context and verify channel closes
	cancel()
	assert.Eventually(t, func() bool {
		_, ok := <-ch
		return !ok
	}, 100*time.Millisecond, 10*time.Millisecond)
}

func TestGetStateChan_WithBufferSize(t *testing.T) {
	t.Parallel()

	manager := broadcast.NewManager(newTestLogger())

	ch, err := manager.GetStateChan(t.Context(), broadcast.WithBufferSize(5))
	require.NoError(t, err)
	assert.NotNil(t, ch)
	assert.Equal(t, 5, cap(ch))
}

func TestGetStateChan_WithCustomChannel(t *testing.T) {
	t.Parallel()

	manager := broadcast.NewManager(newTestLogger())

	customCh := make(chan string, 3)
	ch, err := manager.GetStateChan(t.Context(), broadcast.WithCustomChannel(customCh))
	require.NoError(t, err)
	// Channel is returned as receive-only, so we check capacity instead of equality
	assert.NotNil(t, ch)
	assert.Equal(t, 3, cap(ch))

	// Send initial state
	manager.Broadcast("StateA")

	// Should receive initial state
	require.Eventually(t, func() bool {
		select {
		case state := <-ch:
			return assert.Equal(t, "StateA", state)
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "Expected initial state")
}

func TestBroadcast_AsyncMode(t *testing.T) {
	t.Parallel()

	manager := broadcast.NewManager(newTestLogger())

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Async mode (default timeout=0)
	ch, err := manager.GetStateChan(ctx)
	require.NoError(t, err)

	// Send initial state
	manager.Broadcast("StateA")

	// Drain initial state
	<-ch

	// Broadcast to non-full channel
	manager.Broadcast("StateB")
	require.Eventually(t, func() bool {
		select {
		case state := <-ch:
			return assert.Equal(t, "StateB", state)
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "Expected broadcast")

	// Fill the channel buffer
	manager.Broadcast("StateC")

	// Next broadcast should be dropped (channel full, async mode)
	manager.Broadcast("StateD")

	// Drain channel
	state1 := <-ch
	assert.Equal(t, "StateC", state1)

	// No StateD should be present
	require.Never(t, func() bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, 10*time.Millisecond, "StateD should have been dropped")
}

func TestBroadcast_SyncMode(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		manager := broadcast.NewManager(newTestLogger())

		// Timeout mode with 500ms timeout
		ch, err := manager.GetStateChan(t.Context(),
			broadcast.WithTimeout(500*time.Millisecond),
			broadcast.WithBufferSize(1),
		)
		require.NoError(t, err)

		// Send initial state
		manager.Broadcast("StateA")

		// Drain initial state
		<-ch

		// Fill the channel buffer
		manager.Broadcast("StateB")

		// Start goroutine to broadcast StateC (will block because channel is full)
		broadcastDone := false
		go func() {
			manager.Broadcast("StateC")
			broadcastDone = true
		}()

		// Wait for broadcast to block
		synctest.Wait()
		require.False(t, broadcastDone, "Broadcast should be blocked until channel is consumed")

		// Consume StateB from the channel in a goroutine
		consumedState := ""
		go func() {
			consumedState = <-ch
		}()

		// Wait for consumption and broadcast to complete
		synctest.Wait()

		// Verify StateB was consumed
		assert.Equal(t, "StateB", consumedState)

		// Broadcast should now have completed
		require.True(t, broadcastDone, "Broadcast should have completed after channel was consumed")

		// Verify StateC is in the channel
		state := <-ch
		assert.Equal(t, "StateC", state)
	})
}

func TestBroadcast_TimeoutMode(t *testing.T) {
	t.Parallel()

	manager := broadcast.NewManager(newTestLogger())

	// Timeout mode with short timeout
	ch, err := manager.GetStateChan(t.Context(),
		broadcast.WithTimeout(50*time.Millisecond),
		broadcast.WithBufferSize(1),
	)
	require.NoError(t, err)

	// Send initial state
	manager.Broadcast("StateA")

	// Drain initial state
	<-ch

	// Fill buffer
	manager.Broadcast("StateB")

	// Next broadcast should timeout (channel full, no reader)
	start := time.Now()
	manager.Broadcast("StateC")
	elapsed := time.Since(start)

	// Should have waited for timeout
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
	assert.Less(t, elapsed, 150*time.Millisecond)
}

func TestBroadcast_GuaranteedDeliveryMode(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		manager := broadcast.NewManager(newTestLogger())

		// Guaranteed delivery mode (negative timeout)
		ch, err := manager.GetStateChan(t.Context(),
			broadcast.WithTimeout(-1),
			broadcast.WithBufferSize(1),
		)
		require.NoError(t, err)

		// Send initial state
		manager.Broadcast("StateA")

		// Drain initial state
		<-ch

		// Fill the buffer with first broadcast
		manager.Broadcast("StateB")

		// Start goroutine to broadcast StateC (will block because channel is full)
		broadcastDone := false
		go func() {
			manager.Broadcast("StateC")
			broadcastDone = true
		}()

		// Wait for all goroutines to block - broadcast should be blocked on full channel
		synctest.Wait()

		// Broadcast should NOT have completed yet
		require.False(t, broadcastDone, "Broadcast should be blocked until channel is consumed")

		// Consume StateB from the channel in a goroutine
		consumedState := ""
		go func() {
			consumedState = <-ch
		}()

		// Wait for consume and broadcast to complete
		synctest.Wait()

		// Verify StateB was consumed
		assert.Equal(t, "StateB", consumedState)

		// Broadcast should now have completed
		require.True(t, broadcastDone, "Broadcast should have completed after channel was consumed")

		// Verify StateC is in the channel
		state := <-ch
		assert.Equal(t, "StateC", state)
	})
}

func TestBroadcast_MultipleSubscribers(t *testing.T) {
	t.Parallel()

	manager := broadcast.NewManager(newTestLogger())

	ctx := t.Context()

	// Create multiple subscribers with different modes
	ch1, err := manager.GetStateChan(ctx) // Best-effort
	require.NoError(t, err)
	ch2, err := manager.GetStateChan(ctx, broadcast.WithTimeout(10*time.Second)) // With timeout
	require.NoError(t, err)
	ch3, err := manager.GetStateChan(ctx, broadcast.WithBufferSize(10)) // Best-effort with big buffer
	require.NoError(t, err)
	ch4, err := manager.GetStateChan(ctx, broadcast.WithBufferSize(5)) // Best-effort with medium buffer
	require.NoError(t, err)

	// Send initial state
	manager.Broadcast("StateA")

	// Drain initial states
	<-ch1
	<-ch2
	<-ch3
	<-ch4

	// Broadcast to all
	manager.Broadcast("StateB")

	// All should receive
	assert.Eventually(t, func() bool {
		return <-ch1 == "StateB"
	}, 100*time.Millisecond, 10*time.Millisecond)

	assert.Eventually(t, func() bool {
		return <-ch2 == "StateB"
	}, 100*time.Millisecond, 10*time.Millisecond)

	assert.Eventually(t, func() bool {
		return <-ch3 == "StateB"
	}, 100*time.Millisecond, 10*time.Millisecond)

	assert.Eventually(t, func() bool {
		return <-ch4 == "StateB"
	}, 100*time.Millisecond, 10*time.Millisecond)
}

func TestBroadcast_ContextCancellation(t *testing.T) {
	t.Parallel()

	manager := broadcast.NewManager(newTestLogger())

	ctx1, cancel1 := context.WithCancel(t.Context())
	ctx2, cancel2 := context.WithCancel(t.Context())
	defer cancel2()

	ch1, err := manager.GetStateChan(ctx1)
	require.NoError(t, err)
	ch2, err := manager.GetStateChan(ctx2)
	require.NoError(t, err)

	// Send initial state
	manager.Broadcast("StateA")

	// Drain initial states
	<-ch1
	<-ch2

	// Cancel first subscriber
	cancel1()

	// Wait for ch1 to close
	assert.Eventually(t, func() bool {
		_, ok := <-ch1
		return !ok
	}, 100*time.Millisecond, 10*time.Millisecond)

	// Broadcast should only go to ch2
	manager.Broadcast("StateB")

	// ch2 should receive
	require.Eventually(t, func() bool {
		select {
		case state := <-ch2:
			return assert.Equal(t, "StateB", state)
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "ch2 should receive broadcast")
}

func TestGetStateChan_CustomChannelNotClosedOnContextCancel(t *testing.T) {
	t.Parallel()

	manager := broadcast.NewManager(newTestLogger())

	// Test 1: Default internal channel should be closed when context is cancelled
	ctx1, cancel1 := context.WithCancel(t.Context())
	defaultCh, err := manager.GetStateChan(ctx1)
	require.NoError(t, err)

	manager.Broadcast("StateA")
	<-defaultCh

	cancel1()
	assert.Eventually(t, func() bool {
		_, ok := <-defaultCh
		return !ok
	}, 100*time.Millisecond, 10*time.Millisecond, "Default channel should be closed")

	// Test 2: WithBufferSize channel should be closed when context is cancelled
	ctx2, cancel2 := context.WithCancel(t.Context())
	bufferedCh, err := manager.GetStateChan(ctx2, broadcast.WithBufferSize(10))
	require.NoError(t, err)
	assert.Equal(t, 10, cap(bufferedCh))

	manager.Broadcast("StateB")
	<-bufferedCh

	cancel2()
	assert.Eventually(t, func() bool {
		_, ok := <-bufferedCh
		return !ok
	}, 100*time.Millisecond, 10*time.Millisecond, "WithBufferSize channel should be closed")

	// Test 3: WithCustomChannel should NOT be closed when context is cancelled
	ctx3, cancel3 := context.WithCancel(t.Context())
	customCh := make(chan string, 1)
	ch, err := manager.GetStateChan(ctx3, broadcast.WithCustomChannel(customCh))
	require.NoError(t, err)
	assert.Equal(t, 1, cap(ch))

	manager.Broadcast("StateC")
	state := <-ch
	assert.Equal(t, "StateC", state)

	// Cancel context
	cancel3()

	// Wait for unsubscribe to complete by verifying broadcasts no longer reach the channel
	assert.Eventually(t, func() bool {
		manager.Broadcast("StateD")
		select {
		case <-customCh:
			return false
		case <-time.After(10 * time.Millisecond):
			return true
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "Channel should be unsubscribed")

	// Verify custom channel is still open by sending directly to it
	customCh <- "DirectSend"
	received := <-customCh
	assert.Equal(t, "DirectSend", received, "Custom channel should still be open")

	// Clean up: close the custom channel since we own it
	close(customCh)
}
