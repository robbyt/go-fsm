package fsm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSM_GetStatusChan(t *testing.T) {
	t.Parallel()

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
}
