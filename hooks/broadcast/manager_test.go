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

	"github.com/robbyt/go-fsm/hooks/broadcast"
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

	currentState := "initial"
	getState := func() string { return currentState }

	manager := broadcast.NewManager(newTestLogger(), getState)
	assert.NotNil(t, manager)
}

func TestGetStateChan(t *testing.T) {
	t.Parallel()

	currentState := "StateA"
	getState := func() string { return currentState }
	manager := broadcast.NewManager(newTestLogger(), getState)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch := manager.GetStateChan(ctx)
	assert.NotNil(t, ch)

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
	currentState = "StateB"
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

func TestGetStateChanBuffer(t *testing.T) {
	t.Parallel()

	currentState := "StateA"
	getState := func() string { return currentState }
	manager := broadcast.NewManager(newTestLogger(), getState)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch := manager.GetStateChanBuffer(ctx, 10)
	assert.NotNil(t, ch)
	assert.Equal(t, 10, cap(ch))

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

func TestGetStateChanWithOptions_WithoutInitialState(t *testing.T) {
	t.Parallel()

	currentState := "StateA"
	getState := func() string { return currentState }
	manager := broadcast.NewManager(newTestLogger(), getState)

	ch := manager.GetStateChanWithOptions(t.Context(), broadcast.WithoutInitialState())
	assert.NotNil(t, ch)

	// Should NOT receive initial state
	require.Never(t, func() bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, 10*time.Millisecond, "Should not receive initial state")

	// But should receive broadcasts
	manager.Broadcast("StateB")
	require.Eventually(t, func() bool {
		select {
		case state := <-ch:
			return assert.Equal(t, "StateB", state)
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "Expected broadcast state")
}

func TestGetStateChanWithOptions_WithBufferSize(t *testing.T) {
	t.Parallel()

	currentState := "StateA"
	getState := func() string { return currentState }
	manager := broadcast.NewManager(newTestLogger(), getState)

	ch := manager.GetStateChanWithOptions(t.Context(), broadcast.WithBufferSize(5))
	assert.NotNil(t, ch)
	assert.Equal(t, 5, cap(ch))
}

func TestGetStateChanWithOptions_WithCustomChannel(t *testing.T) {
	t.Parallel()

	currentState := "StateA"
	getState := func() string { return currentState }
	manager := broadcast.NewManager(newTestLogger(), getState)

	customCh := make(chan string, 3)
	ch := manager.GetStateChanWithOptions(t.Context(), broadcast.WithCustomChannel(customCh))
	// Channel is returned as receive-only, so we check capacity instead of equality
	assert.NotNil(t, ch)
	assert.Equal(t, 3, cap(ch))

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

func TestAddSubscriber(t *testing.T) {
	t.Parallel()

	currentState := "StateA"
	getState := func() string { return currentState }
	manager := broadcast.NewManager(newTestLogger(), getState)

	ch := make(chan string, 5)
	unsubscribe := manager.AddSubscriber(ch)
	assert.NotNil(t, unsubscribe)

	// Should receive initial state
	require.Eventually(t, func() bool {
		select {
		case state := <-ch:
			return assert.Equal(t, "StateA", state)
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "Expected initial state")

	// Should receive broadcast
	manager.Broadcast("StateB")
	require.Eventually(t, func() bool {
		select {
		case state := <-ch:
			return assert.Equal(t, "StateB", state)
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "Expected broadcast state")

	// Unsubscribe
	unsubscribe()

	// Should not receive future broadcasts
	manager.Broadcast("StateC")
	require.Never(t, func() bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, 10*time.Millisecond, "Should not receive broadcast after unsubscribe")
}

func TestBroadcast_AsyncMode(t *testing.T) {
	t.Parallel()

	currentState := "StateA"
	getState := func() string { return currentState }
	manager := broadcast.NewManager(newTestLogger(), getState)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Async mode (default timeout=0)
	ch := manager.GetStateChan(ctx)

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

	currentState := "StateA"
	getState := func() string { return currentState }
	manager := broadcast.NewManager(newTestLogger(), getState)

	// Sync mode with 500ms timeout
	ch := manager.GetStateChanWithOptions(t.Context(),
		broadcast.WithSyncTimeout(500*time.Millisecond),
		broadcast.WithBufferSize(1),
	)

	// Drain initial state
	<-ch

	// Start goroutine to consume after delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		<-ch // Consume StateB
		<-ch // Consume StateC
	}()

	// Broadcast should block briefly until consumed
	start := time.Now()
	manager.Broadcast("StateB")
	manager.Broadcast("StateC")
	elapsed := time.Since(start)

	// Should have blocked for at least 100ms (consumer delay)
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
}

func TestBroadcast_SyncMode_Timeout(t *testing.T) {
	t.Parallel()

	currentState := "StateA"
	getState := func() string { return currentState }
	manager := broadcast.NewManager(newTestLogger(), getState)

	// Sync mode with short timeout
	ch := manager.GetStateChanWithOptions(t.Context(),
		broadcast.WithSyncTimeout(50*time.Millisecond),
		broadcast.WithBufferSize(1),
	)

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

func TestBroadcast_InfiniteBlockingMode(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		currentState := "StateA"
		getState := func() string { return currentState }
		manager := broadcast.NewManager(newTestLogger(), getState)

		// Infinite blocking mode (negative timeout)
		ch := manager.GetStateChanWithOptions(t.Context(),
			broadcast.WithSyncTimeout(-1),
			broadcast.WithBufferSize(1),
		)

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

	currentState := "StateA"
	getState := func() string { return currentState }
	manager := broadcast.NewManager(newTestLogger(), getState)

	ctx := t.Context()

	// Create multiple subscribers with different modes
	ch1 := manager.GetStateChan(ctx)                                             // Async
	ch2 := manager.GetStateChanWithOptions(ctx, broadcast.WithSyncBroadcast())   // Sync 10s
	ch3 := manager.GetStateChanWithOptions(ctx, broadcast.WithBufferSize(10))    // Async big buffer
	ch4 := manager.GetStateChanWithOptions(ctx, broadcast.WithoutInitialState()) // Async no initial

	// Drain initial states
	<-ch1
	<-ch2
	<-ch3
	// ch4 has no initial state

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

	currentState := "StateA"
	getState := func() string { return currentState }
	manager := broadcast.NewManager(newTestLogger(), getState)

	ctx1, cancel1 := context.WithCancel(t.Context())
	ctx2, cancel2 := context.WithCancel(t.Context())
	defer cancel2()

	ch1 := manager.GetStateChan(ctx1)
	ch2 := manager.GetStateChan(ctx2)

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
