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

import "context"

// defaultStateChanBufferSize is the default buffer size used for the state change channel.
const defaultStateChanBufferSize = 5

// SetChanBufferSize sets the buffer size for the state change channel.
func (fsm *Machine) SetChanBufferSize(size int) {
	if size < 1 {
		fsm.logger.Warn("Invalid channel buffer size; must be at least 1", "size", size)
		return
	}

	fsm.mutex.RLock()
	currentBuffer := fsm.channelBufferSize
	fsm.mutex.RUnlock()

	if size == currentBuffer {
		fsm.logger.Debug("Channel buffer size is already set to the requested size", "size", size)
		return
	}

	fsm.mutex.Lock()
	fsm.channelBufferSize = size
	fsm.mutex.Unlock()
}

// GetStateChan returns a channel that emits the FSM's state whenever it changes.
// The current state is sent immediately upon subscription. If the channel is not being read,
// state changes will be dropped, and a warning will be logged.
func (fsm *Machine) GetStateChan(ctx context.Context) <-chan string {
	logger := fsm.logger.WithGroup("GetStateChan")
	if ctx == nil {
		logger.Warn("Context is nil; this will cause a goroutine leak")
		ctx = context.Background()
	}

	// Read lock to get a copy of the current state and buffer size, for use later
	fsm.mutex.RLock()
	bufferSize := fsm.channelBufferSize
	initialState := fsm.state
	fsm.mutex.RUnlock()

	// Send the current state immediately
	ch := make(chan string, bufferSize)
	ch <- initialState

	// Add the channel to the subscribers list
	fsm.subscriberMutex.Lock()
	fsm.subscribers[ch] = struct{}{}
	fsm.subscriberMutex.Unlock()

	// Start a goroutine to monitor the context
	go func() {
		logger.Debug("State listener channel started", "bufferSize", bufferSize, "state", initialState)
		// Wait for context cancellation
		<-ctx.Done()
		// Clean up: remove the subscriber and close the channel
		fsm.unsubscribe(ch)
		logger.Debug("State listener channel closed")
	}()

	return ch
}

// unsubscribe removes a subscriber channel and closes it.
func (fsm *Machine) unsubscribe(ch chan string) {
	fsm.subscriberMutex.Lock()
	defer fsm.subscriberMutex.Unlock()
	delete(fsm.subscribers, ch)
	close(ch)
}

// broadcast sends the new state to all subscriber channels.
// If a channel is full, the state change is skipped for that channel, and a warning is logged.
// This, and the other subscriber-related methods, use a standard mutex instead of an RWMutex,
// because the broadcast sends should always be serial, and never concurrent, otherwise the order
// of state change notifications could be unpredictable.
func (fsm *Machine) broadcast(newState string) {
	logger := fsm.logger.WithGroup("broadcast").With("state", newState)
	fsm.subscriberMutex.Lock()
	defer fsm.subscriberMutex.Unlock()

	for ch := range fsm.subscribers {
		select {
		case ch <- newState:
			logger.Debug("Sent state to channel")
		default:
			logger.Warn("Channel is full; skipping broadcast")
		}
	}
}
