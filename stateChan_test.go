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
		assert.Eventually(t, func() bool {
			select {
			case status := <-statusChan:
				return status == StatusNew
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive initial state")
	})

	t.Run("Initial state is sent immediately", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		statusChan := fsm.GetStateChan(ctx)
		require.NotNil(t, statusChan)

		assert.Eventually(t, func() bool {
			select {
			case status := <-statusChan:
				return status == StatusNew
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive initial state")
	})

	t.Run("Channel is closed when context is canceled", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		statusChan := fsm.GetStateChan(ctx)
		require.NotNil(t, statusChan)

		// First receive the initial state
		assert.Eventually(t, func() bool {
			select {
			case status := <-statusChan:
				return status == StatusNew
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive initial state")

		// Cancel the context
		cancel()

		// The channel should be closed
		assert.Eventually(t, func() bool {
			_, ok := <-statusChan
			return !ok
		}, time.Second, 10*time.Millisecond, "Channel should be closed")
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
		assert.Eventually(t, func() bool {
			select {
			case status := <-ch1:
				return status == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Channel 1 should receive status update")

		assert.Eventually(t, func() bool {
			select {
			case status := <-ch2:
				return status == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Channel 2 should receive status update")
	})

	t.Run("AddSubscriber", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ch := make(chan string, 1)
		unsub := fsm.AddSubscriber(ch)
		defer unsub()

		// Should receive the initial state
		assert.Eventually(t, func() bool {
			select {
			case status := <-ch:
				return status == StatusNew
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive initial state")

		// Transition to a new state
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		// Should receive the state update
		assert.Eventually(t, func() bool {
			select {
			case status := <-ch:
				return status == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive state update")

		// Unsubscribe
		unsub()

		// Transition to another state
		err = fsm.Transition(StatusRunning)
		require.NoError(t, err)

		// Should not receive any update after unsubscribing
		assert.Never(t, func() bool {
			select {
			case <-ch:
				return true
			default:
				return false
			}
		}, 100*time.Millisecond, 10*time.Millisecond, "Should not receive updates after unsubscribing")
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
		assert.Eventually(t, func() bool {
			select {
			case val := <-ch:
				return val == "existing-value"
			default:
				return false
			}
		}, 100*time.Millisecond, 10*time.Millisecond, "Channel should still contain original value")

		// Now the channel is empty, transition to a new state
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		// Should receive the state update now that the channel has space
		assert.Eventually(t, func() bool {
			select {
			case status := <-ch:
				return status == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive state update after channel has space")
	})

	t.Run("Multiple subscribers", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		var channels []chan string
		var unsubs []func()
		numSubscribers := 5

		// Create multiple subscribers
		for range numSubscribers {
			ch := make(chan string, 1)
			unsub := fsm.AddSubscriber(ch)
			channels = append(channels, ch)
			unsubs = append(unsubs, unsub)
		}

		// Defer cleanup
		defer func() {
			for _, unsub := range unsubs {
				unsub()
			}
		}()

		// Consume initial states
		for i, ch := range channels {
			chCopy := ch // Capture for closure
			idx := i     // Capture for closure
			assert.Eventually(t, func() bool {
				select {
				case status := <-chCopy:
					return status == StatusNew
				default:
					return false
				}
			}, time.Second, 10*time.Millisecond, "Channel %d should receive initial state", idx)
		}

		// Transition to a new state
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		// All channels should receive the state update
		for i, ch := range channels {
			chCopy := ch // Capture for closure
			idx := i     // Capture for closure
			assert.Eventually(t, func() bool {
				select {
				case status := <-chCopy:
					return status == StatusBooting
				default:
					return false
				}
			}, time.Second, 10*time.Millisecond, "Channel %d should receive state update", idx)
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
		assert.Eventually(t, func() bool {
			select {
			case state := <-bufferedCh:
				return state == StatusNew
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive initial state on buffered channel")

		// Perform multiple transitions quickly
		// The non-buffered channel won't be able to keep up, but the FSM shouldn't block
		for _, state := range []string{StatusBooting, StatusRunning, StatusReloading} {
			err := fsm.Transition(state)
			require.NoError(t, err, "Transition should succeed even with blocked channels")

			// Each transition should be received on the buffered channel
			expectedState := state // Capture for closure
			assert.Eventually(t, func() bool {
				select {
				case receivedState := <-bufferedCh:
					return receivedState == expectedState
				default:
					return false
				}
			}, time.Second, 10*time.Millisecond, "Should receive state %s on buffered channel", expectedState)
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
		assert.Eventually(t, func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Goroutine should finish processing states")

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
		for i := range numChans {
			stateChan[i] = fsm.GetStateChan(ctx)
		}

		// Read all the initial states
		for i, ch := range stateChan {
			chCopy := ch // Capture for closure
			idx := i     // Capture for closure
			assert.Eventually(t, func() bool {
				select {
				case state := <-chCopy:
					return state == StatusNew
				default:
					return false
				}
			}, time.Second, 10*time.Millisecond, "Channel %d should receive initial state", idx)
		}

		// Transition to a new state
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		// Verify all subscribers got the state update
		for i, ch := range stateChan {
			chCopy := ch // Capture for closure
			idx := i     // Capture for closure
			assert.Eventually(t, func() bool {
				select {
				case state := <-chCopy:
					return state == StatusBooting
				default:
					return false
				}
			}, time.Second, 10*time.Millisecond, "Channel %d should receive state update", idx)
		}
	})

	t.Run("GetStateChan uses buffer size 1", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// GetStateChan should behave like GetStateChanBuffer with size 1
		ch := fsm.GetStateChan(ctx)
		require.NotNil(t, ch)

		// Should receive initial state immediately (buffer size 1)
		assert.Eventually(t, func() bool {
			select {
			case status := <-ch:
				return status == StatusNew
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive initial state with default GetStateChan")

		// Transition and verify buffer behavior
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			select {
			case status := <-ch:
				return status == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive state transition")
	})

	t.Run("GetStateChanBuffer with various buffer sizes", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Test buffer size 0 (unbuffered)
		ch0 := fsm.GetStateChanBuffer(ctx, 0)
		require.NotNil(t, ch0)

		// For unbuffered channel, the initial state send will be skipped
		// because nobody is reading when AddSubscriber tries to send
		assert.Never(t, func() bool {
			select {
			case <-ch0:
				return true
			default:
				return false
			}
		}, 100*time.Millisecond, 10*time.Millisecond, "Unbuffered channel should not have initial state")

		// Test buffer size 5
		ch5 := fsm.GetStateChanBuffer(ctx, 5)
		require.NotNil(t, ch5)

		// Should receive initial state immediately
		assert.Eventually(t, func() bool {
			select {
			case status := <-ch5:
				return status == StatusNew
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive initial state on buffered channel")

		// Test buffer size 100
		ch100 := fsm.GetStateChanBuffer(ctx, 100)
		require.NotNil(t, ch100)

		assert.Eventually(t, func() bool {
			select {
			case status := <-ch100:
				return status == StatusNew
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive initial state on large buffered channel")
	})

	t.Run("GetStateChanBuffer nil context", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		var nilCtx context.Context
		ch := fsm.GetStateChanBuffer(nilCtx, 10)
		assert.Nil(t, ch, "Should return nil when context is nil")
	})

	t.Run("GetStateChanBuffer with rapid transitions", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create channel with buffer size 3
		ch := fsm.GetStateChanBuffer(ctx, 3)
		require.NotNil(t, ch)

		// Read initial state
		<-ch

		// Perform rapid transitions
		transitions := []string{StatusBooting, StatusRunning, StatusReloading}
		for _, state := range transitions {
			err := fsm.Transition(state)
			require.NoError(t, err)
		}

		// Should be able to read all transitions from buffer
		for _, expectedState := range transitions {
			assert.Eventually(t, func() bool {
				select {
				case state := <-ch:
					return state == expectedState
				default:
					return false
				}
			}, time.Second, 10*time.Millisecond, "Should receive state %s", expectedState)
		}
	})

	t.Run("GetStateChanBuffer with small buffer overflow", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create channel with buffer size 2
		ch := fsm.GetStateChanBuffer(ctx, 2)
		require.NotNil(t, ch)

		// Don't read anything - let buffer fill up
		// Initial state should be in buffer

		// Perform more transitions than buffer can hold
		// Use valid transitions according to TypicalTransitions
		transitions := []string{StatusBooting, StatusRunning, StatusReloading, StatusRunning}
		for _, state := range transitions {
			err := fsm.Transition(state)
			require.NoError(t, err)
		}

		// Now read from channel - should get initial state first
		assert.Eventually(t, func() bool {
			select {
			case state := <-ch:
				return state == StatusNew
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should read initial state first")

		// Should get at least one more state (buffer was full)
		assert.Eventually(t, func() bool {
			select {
			case state := <-ch:
				return assert.Contains(t, transitions, state)
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should read at least one transition state")
	})

	t.Run("GetStateChanBuffer context cancellation", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())

		// Create channels with different buffer sizes
		ch1 := fsm.GetStateChanBuffer(ctx, 1)
		ch10 := fsm.GetStateChanBuffer(ctx, 10)
		ch0 := fsm.GetStateChanBuffer(ctx, 0)

		// Read initial states from buffered channels
		<-ch1
		<-ch10

		// For unbuffered, start a goroutine to read
		go func() {
			for range ch0 {
				// Drain the channel
			}
		}()

		// Allow goroutine to start
		time.Sleep(50 * time.Millisecond)

		// Cancel context
		cancel()

		// All channels should eventually be closed
		channels := []<-chan string{ch1, ch10, ch0}
		for i, ch := range channels {
			assert.Eventually(t, func() bool {
				_, ok := <-ch
				return !ok
			}, time.Second, 10*time.Millisecond, "Channel %d should be closed after context cancellation", i)
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
			for range 100 {
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

			for i := range 1000 {
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

		assert.Eventually(t, func() bool {
			select {
			case <-waitCh:
				return true
			default:
				return false
			}
		}, 10*time.Second, 10*time.Millisecond, "Test should complete within timeout")

		// If we're using the race detector, we might not observe actual panics
		// but we should see data races reported by the race detector
		t.Logf("Detected panic from closed channel: %v", panicked.Load())

		// The test is considered successful if:
		// 1. It completes without deadlocking
		// 2. The race detector reports a race condition (when run with -race)
		// 3. OR we directly observe a panic from a closed channel (if race detector is not used)
	})
}

func TestFSM_GetStateChanWithOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithBufferSize option", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch := fsm.GetStateChanWithOptions(ctx, WithBufferSize(5))
		require.NotNil(t, ch)
		assert.Equal(t, 5, cap(ch))

		assert.Eventually(t, func() bool {
			select {
			case state := <-ch:
				return state == StatusNew
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive initial state")
	})

	t.Run("WithCustomChannel option", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		customCh := make(chan string, 3)
		ch := fsm.GetStateChanWithOptions(ctx, WithCustomChannel(customCh))

		// Verify it's the same channel by checking capacity
		assert.Equal(t, cap(customCh), cap(ch))

		// First consume the initial state that was automatically sent
		select {
		case initialState := <-ch:
			assert.Equal(t, StatusNew, initialState)
		default:
			t.Fatal("Should receive initial state")
		}

		// Now test that it's the same channel by sending a test message
		customCh <- "test-message"
		select {
		case msg := <-ch:
			assert.Equal(t, "test-message", msg)
		default:
			t.Fatal("Should be able to read test message from returned channel")
		}
	})

	t.Run("WithoutInitialState option", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch := fsm.GetStateChanWithOptions(ctx, WithoutInitialState())
		require.NotNil(t, ch)

		assert.Never(t, func() bool {
			select {
			case <-ch:
				return true
			default:
				return false
			}
		}, 100*time.Millisecond, 10*time.Millisecond, "Should not receive initial state")

		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			select {
			case state := <-ch:
				return state == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive state transition")
	})

	t.Run("Option precedence", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		t.Run("WithCustomChannel overrides WithBufferSize", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			customCh := make(chan string, 10)
			ch := fsm.GetStateChanWithOptions(ctx, WithBufferSize(5), WithCustomChannel(customCh))

			// Verify it's the same channel by checking capacity
			assert.Equal(t, cap(customCh), cap(ch))
			assert.Equal(t, 10, cap(ch))
		})

		t.Run("WithBufferSize overrides WithCustomChannel", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			customCh := make(chan string, 2)
			ch := fsm.GetStateChanWithOptions(ctx, WithCustomChannel(customCh), WithBufferSize(7))
			assert.NotEqual(t, customCh, ch)
			assert.Equal(t, 7, cap(ch))
		})

		t.Run("WithoutInitialState persists with other options", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ch := fsm.GetStateChanWithOptions(ctx, WithoutInitialState(), WithBufferSize(1))

			assert.Never(t, func() bool {
				select {
				case <-ch:
					return true
				default:
					return false
				}
			}, 100*time.Millisecond, 10*time.Millisecond, "Should not receive initial state")
		})
	})

	t.Run("No options - default behavior", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch := fsm.GetStateChanWithOptions(ctx)
		require.NotNil(t, ch)
		assert.Equal(t, 1, cap(ch))

		assert.Eventually(t, func() bool {
			select {
			case state := <-ch:
				return state == StatusNew
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive initial state")
	})

	t.Run("Nil context guard", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		var nilCtx context.Context
		ch := fsm.GetStateChanWithOptions(nilCtx, WithBufferSize(10))
		assert.Nil(t, ch)
	})

	t.Run("Combined options", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		customCh := make(chan string, 2)
		ch := fsm.GetStateChanWithOptions(ctx, WithCustomChannel(customCh), WithoutInitialState())

		// Verify it's the same channel by checking capacity
		assert.Equal(t, cap(customCh), cap(ch))

		assert.Never(t, func() bool {
			select {
			case <-ch:
				return true
			default:
				return false
			}
		}, 100*time.Millisecond, 10*time.Millisecond, "Should not receive initial state")

		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			select {
			case state := <-ch:
				return state == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive state transition")
	})

	t.Run("WithSyncBroadcast option", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create a very small buffered channel to test sync behavior
		ch := fsm.GetStateChanWithOptions(ctx, WithSyncBroadcast(), WithBufferSize(1))
		require.NotNil(t, ch)

		// Consume initial state
		select {
		case state := <-ch:
			assert.Equal(t, StatusNew, state)
		case <-time.After(time.Second):
			t.Fatal("Should receive initial state")
		}

		// Fill the channel buffer
		ch2 := make(chan string, 1)
		ch2 <- "blocking-message"

		// Test that sync broadcast waits (doesn't drop messages)
		syncCh := fsm.GetStateChanWithOptions(ctx, WithSyncBroadcast(), WithCustomChannel(ch2))

		// Start a goroutine that will read from the channel after a delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			<-syncCh // Remove the blocking message
		}()

		// This transition should succeed even though channel was initially full
		// because sync broadcast waits
		start := time.Now()
		err = fsm.Transition(StatusBooting)
		duration := time.Since(start)

		require.NoError(t, err)
		// Should have taken some time (at least 50ms) because it waited
		assert.Greater(t, duration, 40*time.Millisecond, "Sync broadcast should have waited")

		// Should eventually receive the state
		assert.Eventually(t, func() bool {
			select {
			case state := <-syncCh:
				return state == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive state after channel was unblocked")
	})

	t.Run("WithSyncTimeout option", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Test custom short timeout
		shortTimeout := 100 * time.Millisecond

		// Create a channel that will be permanently blocked
		blockedCh := make(chan string) // Unbuffered, no readers
		ch := fsm.GetStateChanWithOptions(ctx,
			WithSyncBroadcast(),
			WithCustomChannel(blockedCh),
			WithSyncTimeout(shortTimeout),
			WithoutInitialState(), // Don't send initial state to avoid blocking
		)
		require.NotNil(t, ch)

		// This transition should timeout after shortTimeout
		start := time.Now()
		err = fsm.Transition(StatusBooting)
		duration := time.Since(start)

		require.NoError(t, err) // Transition itself should succeed
		// Should have taken approximately the timeout duration
		assert.Greater(
			t,
			duration,
			shortTimeout-10*time.Millisecond,
			"Should have waited for timeout",
		)
		assert.Less(
			t,
			duration,
			shortTimeout+50*time.Millisecond,
			"Should not wait much longer than timeout",
		)
	})

	t.Run("Mixed sync and async subscribers", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create async subscriber (default)
		asyncCh := fsm.GetStateChanWithOptions(ctx, WithBufferSize(2))

		// Create sync subscriber
		syncCh := fsm.GetStateChanWithOptions(ctx, WithSyncBroadcast(), WithBufferSize(2))

		// Consume initial states
		<-asyncCh
		<-syncCh

		// Transition - both should receive it
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		// Both channels should receive the update
		assert.Eventually(t, func() bool {
			select {
			case state := <-asyncCh:
				return state == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Async subscriber should receive state")

		assert.Eventually(t, func() bool {
			select {
			case state := <-syncCh:
				return state == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Sync subscriber should receive state")
	})

	t.Run("Sync broadcast with blocked subscriber doesn't block others", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create one permanently blocked sync subscriber
		blockedSyncCh := make(chan string) // Unbuffered, no readers
		fsm.GetStateChanWithOptions(ctx,
			WithSyncBroadcast(),
			WithCustomChannel(blockedSyncCh),
			WithoutInitialState(),
			WithSyncTimeout(50*time.Millisecond), // Short timeout for faster test
		)

		// Create a normal async subscriber that should not be blocked
		asyncCh := fsm.GetStateChanWithOptions(ctx, WithBufferSize(2))
		<-asyncCh // Consume initial state

		// This should complete after the sync timeout (50ms) since we wait for all sync broadcasts
		start := time.Now()
		err = fsm.Transition(StatusBooting)
		transitionDuration := time.Since(start)

		require.NoError(t, err)
		// Transition should complete around the timeout duration (sync broadcasts complete in parallel)
		assert.Greater(t, transitionDuration, 40*time.Millisecond, "Should wait for sync timeout")
		assert.Less(
			t,
			transitionDuration,
			70*time.Millisecond,
			"Should not wait much longer than timeout",
		)

		// Async subscriber should still receive the update
		assert.Eventually(t, func() bool {
			select {
			case state := <-asyncCh:
				return state == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Async subscriber should receive state despite blocked sync subscriber")
	})

	t.Run("Default sync timeout", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Test that default timeout is 10 seconds by creating sync subscriber without explicit timeout
		syncCh := fsm.GetStateChanWithOptions(ctx, WithSyncBroadcast(), WithBufferSize(1))
		require.NotNil(t, syncCh)

		// We can't easily test a 10-second timeout in a unit test, but we can verify
		// that the subscriber was created successfully and behaves synchronously
		// for unblocked channels

		// Consume initial state
		<-syncCh

		// Quick transition should work fine
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			select {
			case state := <-syncCh:
				return state == StatusBooting
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond, "Should receive state with default timeout")
	})
}
