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

package fsm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSM_GetStatusChan(t *testing.T) {
	t.Parallel()

	t.Run("Nil context", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		statusChan := fsm.GetStateChan(nil)
		require.NotNil(t, statusChan)

		select {
		case status, ok := <-statusChan:
			assert.Equal(t, StatusNew, status)
			assert.True(t, ok)
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for channel to close")
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
		case state, ok := <-statusChan:
			require.True(t, ok)
			assert.Equal(t, StatusNew, state)
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for initial state")
		}

		cancel()

		select {
		case _, ok := <-statusChan:
			assert.False(t, ok, "Channel should be closed after context is canceled")
		case <-time.After(time.Second):
			t.Fatal("Timed out waiting for channel to close")
		}

	})

	t.Run("State changes are emitted", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		statusChan := fsm.GetStateChan(ctx)

		var receivedStates []string
		done := make(chan struct{})
		ready := make(chan struct{})

		go func() {
			close(ready)
			for state := range statusChan {
				receivedStates = append(receivedStates, state)
				if state == StatusError {
					close(done)
					return
				}
			}
		}()

		// Wait for the goroutine to start
		<-ready

		// Perform rapid state transitions
		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		err = fsm.Transition(StatusRunning)
		require.NoError(t, err)

		err = fsm.Transition(StatusError)
		require.NoError(t, err)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Timed out waiting for state changes")
		}

		expectedStates := []string{StatusNew, StatusBooting, StatusRunning, StatusError}
		assert.Equal(t, expectedStates, receivedStates)
	})

	t.Run("Multiple subscribers receive updates", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		ctx1, cancel1 := context.WithCancel(context.Background())
		defer cancel1()
		ctx2, cancel2 := context.WithCancel(context.Background())
		defer cancel2()

		statusChan1 := fsm.GetStateChan(ctx1)
		statusChan2 := fsm.GetStateChan(ctx2)

		require.NotNil(t, statusChan1)
		require.NotNil(t, statusChan2)

		var wg sync.WaitGroup
		var mu sync.Mutex
		received1 := []string{}
		received2 := []string{}

		wg.Add(2)

		go func() {
			defer wg.Done()
			for state := range statusChan1 {
				mu.Lock()
				received1 = append(received1, state)
				mu.Unlock()
				if state == StatusRunning {
					return
				}
			}
		}()

		go func() {
			defer wg.Done()
			for state := range statusChan2 {
				mu.Lock()
				received2 = append(received2, state)
				mu.Unlock()
				if state == StatusRunning {
					return
				}
			}
		}()

		err = fsm.Transition(StatusBooting)
		require.NoError(t, err)

		err = fsm.Transition(StatusRunning)
		require.NoError(t, err)

		wg.Wait()

		mu.Lock()
		expectedStates := []string{StatusNew, StatusBooting, StatusRunning}
		assert.Equal(t, expectedStates, received1)
		assert.Equal(t, expectedStates, received2)
		mu.Unlock()
	})
}

func TestFSM_SetChanBufferSize(t *testing.T) {
	fsm, err := New(nil, StatusNew, TypicalTransitions)
	require.NoError(t, err)
	require.Equal(t, defaultStateChanBufferSize, fsm.channelBufferSize, "assume the default buffer size is set upon creation")

	// Change the buffer size before creating subscribers
	newBufferSize := 1
	fsm.SetChanBufferSize(newBufferSize)
	require.Equal(t, newBufferSize, fsm.channelBufferSize, "buffer size should be updated after running SetChanBufferSize")

	fsm.SetChanBufferSize(-1)
	require.Equal(t, newBufferSize, fsm.channelBufferSize, "buffer size remains the same if the new size is invalid")

	fsm.SetChanBufferSize(newBufferSize)
	require.Equal(t, newBufferSize, fsm.channelBufferSize, "buffer size remains the same if the new size is the same as the current size")

	// Create subscribers after changing buffer size
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	statusChan1 := fsm.GetStateChan(ctx1)
	require.NotNil(t, statusChan1)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	statusChan2 := fsm.GetStateChan(ctx2)
	require.NotNil(t, statusChan2)

	// Define a valid sequence of state transitions
	transitions := []string{
		StatusBooting,  // From StatusNew
		StatusRunning,  // From StatusBooting
		StatusStopping, // From StatusRunning
		StatusStopped,  // From StatusStopping
		StatusNew,      // From StatusStopped
	}

	// Collect states for the first subscriber
	var received1 []string
	done1 := make(chan struct{})
	go func() {
		for state := range statusChan1 {
			received1 = append(received1, state)
			if len(received1) >= newBufferSize {
				close(done1)
				return
			}
		}
	}()

	// Collect states for the second subscriber
	var received2 []string
	done2 := make(chan struct{})
	go func() {
		for state := range statusChan2 {
			received2 = append(received2, state)
			if len(received2) >= newBufferSize {
				close(done2)
				return
			}
		}
	}()

	// Perform multiple state transitions in the background
	go func() {
		for i := 0; i < newBufferSize; i++ {
			currentState := fsm.GetState()
			nextState := transitions[i%len(transitions)]

			// Check if transition is valid before attempting
			if _, ok := fsm.allowedTransitions[currentState][nextState]; !ok {
				// Reset FSM to a valid state
				err := fsm.SetState(StatusNew)
				require.NoError(t, err)
				nextState = StatusBooting
			}

			err := fsm.Transition(nextState)
			require.NoError(t, err)
		}
	}()

	select {
	case <-done1:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for states on subscriber 1")
	}

	select {
	case <-done2:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for states on subscriber 2")
	}

	// Verify the number of received states
	assert.Len(t, received1, newBufferSize)
	assert.Len(t, received2, newBufferSize)
}
