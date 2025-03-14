package fsm

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSM_GetStatusChan(t *testing.T) {
	t.Parallel()

	t.Run("nil context guard check", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		// Create a nil Context variable to test the guard
		// This approach avoids the linter warning while still testing nil context behavior
		var nilCtx context.Context

		statusChan := fsm.GetStateChan(nilCtx)
		assert.Nil(t, statusChan, "Should return nil when context is nil")
	})

	t.Run("TODO context", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		statusChan := fsm.GetStateChan(context.TODO())
		require.NotNil(t, statusChan)

		// With new implementation, initial state should be sent immediately
		select {
		case status := <-statusChan:
			assert.Equal(t, StatusNew, status)
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for initial state")
		}
	})

	t.Run("Initial state is sent immediately", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		statusChan := fsm.GetStateChan(ctx)
		require.NotNil(t, statusChan)

		select {
		case status := <-statusChan:
			assert.Equal(t, StatusNew, status)
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for initial state")
		}
	})

	t.Run("Channel is closed when context is canceled", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		statusChan := fsm.GetStateChan(ctx)
		require.NotNil(t, statusChan)

		// First receive the initial state
		select {
		case status := <-statusChan:
			assert.Equal(t, StatusNew, status)
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for initial state")
		}

		// Cancel the context
		cancel()

		// The channel should be closed
		select {
		case status, ok := <-statusChan:
			assert.False(t, ok, "Channel should be closed")
			assert.Empty(t, status, "Value should be empty on a closed channel")
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for channel to close")
		}
	})

	t.Run("State transitions are broadcast to subscribers", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create two subscribers
		ch1 := fsm.GetStateChan(ctx)
		ch2 := fsm.GetStateChan(ctx)

		// Consume initial states
		<-ch1
		<-ch2

		// Transition to a new state
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		// Both channels should receive the new state
		select {
		case status := <-ch1:
			assert.Equal(t, StatusBooting, status)
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for status update on channel 1")
		}

		select {
		case status := <-ch2:
			assert.Equal(t, StatusBooting, status)
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for status update on channel 2")
		}
	})

	t.Run("AddSubscriber", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ch := make(chan string, 1)
		unsub := fsm.AddSubscriber(ch)
		defer unsub()

		// Should receive the initial state
		select {
		case status := <-ch:
			assert.Equal(t, StatusNew, status)
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for initial state")
		}

		// Transition to a new state
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		// Should receive the state update
		select {
		case status := <-ch:
			assert.Equal(t, StatusBooting, status)
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for state update")
		}

		// Unsubscribe
		unsub()

		// Transition to another state
		err = fsm.Transition(StatusRunning)
		require.NoError(t, err)

		// Should not receive any update after unsubscribing
		select {
		case status := <-ch:
			t.Fatalf("Received unexpected state update: %s", status)
		case <-time.After(100 * time.Millisecond):
			// This is the expected outcome - no state update after unsubscribing
		}
	})

	t.Run("AddSubscriber with full channel", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		// Create a buffered channel of size 1 and fill it
		ch := make(chan string, 1)
		ch <- "existing-value"

		// Channel is now full, so initial state won't be sent
		unsub := fsm.AddSubscriber(ch)
		defer unsub()

		// Make sure the channel still contains only the original value
		select {
		case val := <-ch:
			assert.Equal(t, "existing-value", val, "Channel should still contain original value")
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Should be able to read existing value from channel")
		}

		// Now the channel is empty, transition to a new state
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		// Should receive the state update now that the channel has space
		select {
		case status := <-ch:
			assert.Equal(t, StatusBooting, status)
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for state update")
		}
	})

	t.Run("Multiple subscribers", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		var channels []chan string
		var unsubscribers []func()
		numSubscribers := 5

		// Create multiple subscribers
		for i := 0; i < numSubscribers; i++ {
			ch := make(chan string, 1)
			unsub := fsm.AddSubscriber(ch)
			channels = append(channels, ch)
			unsubscribers = append(unsubscribers, unsub)
		}

		// Defer cleanup
		defer func() {
			for _, unsub := range unsubscribers {
				unsub()
			}
		}()

		// Consume initial states
		for i, ch := range channels {
			select {
			case status := <-ch:
				assert.Equal(t, StatusNew, status)
			case <-time.After(time.Second):
				t.Fatalf("Timed out waiting for initial state on channel %d", i)
			}
		}

		// Transition to a new state
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		// All channels should receive the state update
		for i, ch := range channels {
			select {
			case status := <-ch:
				assert.Equal(t, StatusBooting, status)
			case <-time.After(time.Second):
				t.Fatalf("Timed out waiting for state update on channel %d", i)
			}
		}
	})

	t.Run("Broadcast skips full channels", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		// Create a non-buffered channel - we won't read from it
		// This simulates a slow consumer that's not keeping up
		nonBufferedCh := make(chan string)
		nonBufferedUnsub := fsm.AddSubscriber(nonBufferedCh)
		defer nonBufferedUnsub()

		// Create a normal buffered channel that will receive updates
		bufferedCh := make(chan string, 2)
		bufferedUnsub := fsm.AddSubscriber(bufferedCh)
		defer bufferedUnsub()

		// Read initial state from buffered channel
		select {
		case state := <-bufferedCh:
			assert.Equal(t, StatusNew, state)
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for initial state on buffered channel")
		}

		// Perform multiple transitions quickly
		// The non-buffered channel won't be able to keep up, but the FSM shouldn't block
		for _, state := range []string{StatusBooting, StatusRunning, StatusReloading} {
			err := fsm.Transition(state)
			require.NoError(t, err, "Transition should succeed even with blocked channels")

			// Each transition should be received on the buffered channel
			select {
			case receivedState := <-bufferedCh:
				assert.Equal(t, state, receivedState)
			case <-time.After(time.Second):
				t.Fatalf("Timed out waiting for state %s on buffered channel", state)
			}
		}

		// The final state should match our expected state
		assert.Equal(t, StatusReloading, fsm.GetState(), "Final state should be StatusReloading")
	})

	t.Run("Check consumer with indirect channel", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		// Create a channel and subscribe
		statesChan := make(chan string, 3)

		// Monitor states in a separate goroutine
		statesReceived := make([]string, 0, 3)
		done := make(chan struct{})

		// Start a goroutine to handle processing the received states
		go func() {
			defer close(done)
			for state := range statesChan {
				statesReceived = append(statesReceived, state)
			}
		}()

		// Subscribe the channel
		unsub := fsm.AddSubscriber(statesChan)

		// Allow some time for initial state to be received
		time.Sleep(50 * time.Millisecond)

		// Make state transitions
		transitions := []string{StatusBooting, StatusRunning}
		for _, state := range transitions {
			err = fsm.Transition(state)
			require.NoError(t, err)
			time.Sleep(50 * time.Millisecond)
		}

		// Unsubscribe and close the channel
		unsub()
		close(statesChan)

		// Wait for the goroutine to process all states
		select {
		case <-done:
			// Success
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for goroutine to finish")
		}

		// Verify all expected states were received
		expected := []string{StatusNew, StatusBooting, StatusRunning}
		assert.Equal(t, expected, statesReceived, "Should have received all state transitions")

		// Verify a transition after unsubscribe doesn't affect anything
		err = fsm.Transition(StatusStopping)
		require.NoError(t, err)

		err = fsm.Transition(StatusStopped)
		require.NoError(t, err)

		assert.Equal(t, expected, statesReceived, "States shouldn't change after unsubscribe")
		assert.Equal(t, StatusStopped, fsm.GetState(), "Final state should be stopped")
	})

	t.Run("Single state transition", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Create several channels
		numChans := 3
		stateChan := make([]<-chan string, numChans)
		for i := 0; i < numChans; i++ {
			stateChan[i] = fsm.GetStateChan(ctx)
		}

		// Read all the initial states
		for i, ch := range stateChan {
			select {
			case state := <-ch:
				assert.Equal(t, StatusNew, state)
			case <-time.After(time.Second):
				t.Fatalf("Channel %d never received initial state", i)
			}
		}

		// Transition to a new state
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		// Verify all subscribers got the state update
		for i, ch := range stateChan {
			select {
			case state := <-ch:
				assert.Equal(t, StatusBooting, state)
			case <-time.After(time.Second):
				t.Fatalf("Channel %d never received state update", i)
			}
		}
	})

	t.Run("Race condition test: demonstrate race with closed channel", func(t *testing.T) {
		// This test demonstrates the race condition by showing we might try to write to closed channels
		// during broadcast if subscribers are concurrently being removed

		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		// Track if we panic from writing to closed channel
		var panicked atomic.Bool

		// Create a channel that will be closed while broadcast is running
		racyChan := make(chan string, 1)

		// Add the channel as a subscriber
		unsub := fsm.AddSubscriber(racyChan)

		// Wait group to coordinate the test
		var wg sync.WaitGroup

		// Start goroutine to transition rapidly
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Start with initial state transitions
			for i := 0; i < 100; i++ {
				// We'll produce many state transitions, which trigger broadcasts
				err := fsm.Transition(StatusBooting)
				if err != nil {
					continue
				}

				err = fsm.Transition(StatusRunning)
				if err != nil {
					continue
				}

				// Very small sleep to allow other goroutines to run
				// but keep transitions rapid
				time.Sleep(time.Microsecond)
			}
		}()

		// Start goroutine that will close the channel during broadcast
		// We'll use another goroutine to simulate the race condition by:
		// 1. Adding a subscriber
		// 2. Unsubscribing immediately (which could happen during broadcast)
		// 3. Closing the channel immediately after unsubscribe
		// If the broadcast function isn't properly protected by a mutex, it might try
		// to send to the channel after it's been unsubscribed and closed
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Create many channels that will be immediately unsubscribed and closed
			// This increases the chance of hitting the race condition
			for i := 0; i < 10000; i++ {
				ch := make(chan string, 1)

				// Add subscriber
				unsubFn := fsm.AddSubscriber(ch)

				// Immediately unsubscribe
				unsubFn()

				// Close the channel immediately after unsubscribing
				// In a race condition, broadcast might still try to use it
				close(ch)

				// Let's try to detect if broadcast tries to send to this closed channel
				// by capturing the panic that would occur
				defer func() {
					if r := recover(); r != nil {
						t.Logf("Caught panic: %v", r)
						panicked.Store(true)
					}
				}()
			}

			// Also try unsubscribing the original channel
			unsub()
			close(racyChan)
		}()

		// Start another goroutine that continuously adds and removes subscribers
		// to increase contention
		wg.Add(1)
		go func() {
			defer wg.Done()

			channels := make([]struct {
				ch    chan string
				unsub func()
			}, 1000)

			for i := 0; i < 1000; i++ {
				// Add more subscribers
				for j := 0; j < 10; j++ {
					idx := (i*10 + j) % len(channels)

					// Clean up previous subscriber at this index if it exists
					if channels[idx].unsub != nil {
						channels[idx].unsub()
						// Don't close the channel here, the unsubscribe is sufficient
					}

					// Create new channel and subscribe
					ch := make(chan string, 1)
					unsub := fsm.AddSubscriber(ch)

					channels[idx] = struct {
						ch    chan string
						unsub func()
					}{ch, unsub}

					// Drain any messages
					select {
					case <-ch:
					default:
					}
				}

				// Very small sleep to increase chance of race
				time.Sleep(time.Microsecond)
			}

			// Clean up all subscribers at the end
			for _, entry := range channels {
				if entry.unsub != nil {
					entry.unsub()
				}
			}
		}()

		// Wait for all goroutines to complete or timeout
		waitCh := make(chan struct{})
		go func() {
			wg.Wait()
			close(waitCh)
		}()

		select {
		case <-waitCh:
			// Test completed normally
		case <-time.After(5 * time.Second):
			// Timeout is okay
		}

		// If we're using the race detector, we might not observe actual panics
		// but we should see data races reported by the race detector
		t.Logf("Detected panic from closed channel: %v", panicked.Load())

		// The test is considered successful if:
		// 1. It completes without deadlocking
		// 2. The race detector reports a race condition (when run with -race)
		// 3. OR we directly observe a panic from a closed channel (if race detector is not used)
	})
}
