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
	"time"
)

const (
	defaultSyncTimeout = 10 * time.Second
)

// SubscriberOption configures subscriber behavior
type SubscriberOption func(*subscriberConfig)

// subscriberConfig holds configuration for subscriber channels
type subscriberConfig struct {
	sendInitial   bool
	customChannel chan string
	syncBroadcast bool
	syncTimeout   time.Duration
}

// WithBufferSize creates a channel with the specified buffer size
func WithBufferSize(size int) SubscriberOption {
	return func(config *subscriberConfig) {
		config.customChannel = make(chan string, size)
	}
}

// WithoutInitialState prevents sending the current state of the FSM immediately after creation
func WithoutInitialState() SubscriberOption {
	return func(config *subscriberConfig) {
		config.sendInitial = false
	}
}

// WithCustomChannel uses an external channel instead of creating a new one
func WithCustomChannel(ch chan string) SubscriberOption {
	return func(config *subscriberConfig) {
		config.customChannel = ch
	}
}

// WithSyncBroadcast enables synchronous broadcasting that blocks until the message
// is delivered to the channel, rather than dropping messages for full channels
func WithSyncBroadcast() SubscriberOption {
	return func(config *subscriberConfig) {
		config.syncBroadcast = true
	}
}

// WithSyncTimeout sets the timeout for synchronous broadcast operations
func WithSyncTimeout(timeout time.Duration) SubscriberOption {
	return func(config *subscriberConfig) {
		config.syncTimeout = timeout
	}
}

// GetStateChan returns a channel that will receive the current state of the FSM immediately.
func (fsm *Machine) GetStateChan(ctx context.Context) <-chan string {
	return fsm.GetStateChanBuffer(ctx, 1)
}

// GetStateChanWithOptions returns a channel configured with functional options
func (fsm *Machine) GetStateChanWithOptions(
	ctx context.Context,
	opts ...SubscriberOption,
) <-chan string {
	config := &subscriberConfig{
		sendInitial: true,
		syncTimeout: defaultSyncTimeout,
	}

	for _, opt := range opts {
		opt(config)
	}

	if ctx == nil {
		fsm.logger.Error("context is nil; cannot create state channel")
		return nil
	}

	var ch chan string
	if config.customChannel != nil {
		ch = config.customChannel
	} else {
		ch = make(chan string, 1)
	}

	unsubCallback := fsm.addSubscriberWithConfig(ch, config)

	go func() {
		<-ctx.Done()
		unsubCallback()
		close(ch)
	}()

	return ch
}

// GetStateChanBuffer returns a channel with a configurable buffer size. The current state
// of the FSM will be sent immediately.
func (fsm *Machine) GetStateChanBuffer(ctx context.Context, chanBufferSize int) <-chan string {
	if ctx == nil {
		fsm.logger.Error("context is nil; cannot create state channel")
		return nil
	}

	ch := make(chan string, chanBufferSize)
	unsubCallback := fsm.AddSubscriber(ch)

	go func() {
		// block here (in the background) until the context is done
		<-ctx.Done()

		// unsubscribe and close the channel
		unsubCallback()
		close(ch)
	}()

	return ch
}

// AddSubscriber adds your channel to the internal list of broadcast targets. It will receive
// the current state if the channel (if possible), and will also receive future state changes
// when the FSM state is updated. A callback function is returned that should be called to
// remove the channel from the list of subscribers when this is no longer needed.
//
// Deprecated: Use GetStateChanWithOptions with WithCustomChannel instead.
func (fsm *Machine) AddSubscriber(ch chan string) func() {
	config := &subscriberConfig{
		sendInitial: true,
		syncTimeout: defaultSyncTimeout,
	}
	return fsm.addSubscriberWithConfig(ch, config)
}

// addSubscriberWithConfig adds a subscriber with specific configuration
func (fsm *Machine) addSubscriberWithConfig(ch chan string, config *subscriberConfig) func() {
	fsm.subscriberMutex.Lock()
	defer fsm.subscriberMutex.Unlock()

	fsm.subscribers.Store(ch, config)

	if config.sendInitial {
		select {
		case ch <- fsm.GetState():
			fsm.logger.Debug("Sent initial state to channel")
		default:
			fsm.logger.Warn(
				"Unable to write initial state to channel; next state change will be sent instead",
			)
		}
	}

	return func() {
		fsm.unsubscribe(ch)
	}
}

// unsubscribe removes a channel from the internal list of broadcast targets.
func (fsm *Machine) unsubscribe(ch chan string) {
	fsm.subscriberMutex.Lock()
	fsm.subscribers.Delete(ch)
	fsm.subscriberMutex.Unlock()
}

// broadcast sends the new state to all subscriber channels.
// For async subscribers, if a channel is full, the state change is skipped for that channel, and a warning is logged.
// For sync subscribers, the broadcast blocks until the message is delivered or times out after 10 seconds.
// This, and the other subscriber-related methods, use a standard mutex instead of an RWMutex,
// because the broadcast sends should always be serial, and never concurrent, otherwise the order
// of state change notifications could be unpredictable.
func (fsm *Machine) broadcast(state string) {
	logger := fsm.logger.WithGroup("broadcast").With("state", state)

	// Lock during the entire broadcast to prevent race conditions
	// with subscribers being added or removed during iteration
	fsm.subscriberMutex.Lock()
	defer fsm.subscriberMutex.Unlock()

	var wg sync.WaitGroup

	fsm.subscribers.Range(func(key, value any) bool {
		ch := key.(chan string)

		// Handle type assertion safely
		config, ok := value.(*subscriberConfig)
		if !ok {
			logger.Error("Invalid subscriber config type; skipping subscriber")
			return true
		}

		if config.syncBroadcast {
			// Handle sync broadcast with timeout in parallel goroutines
			wg.Add(1)
			go func(ch chan string) {
				defer wg.Done()
				select {
				case ch <- state:
					logger.Debug("State delivered to synchronous subscriber")
				case <-time.After(config.syncTimeout):
					logger.Warn("Synchronous subscriber blocked; state delivery timed out",
						"timeout", config.syncTimeout,
						"channel_capacity", cap(ch), "channel_length", len(ch))
				}
			}(ch)
		} else {
			// Handle async broadcast (non-blocking)
			select {
			case ch <- state:
				logger.Debug("State delivered to asynchronous subscriber")
			default:
				logger.Debug("Asynchronous subscriber channel full; state delivery skipped",
					"channel_capacity", cap(ch), "channel_length", len(ch))
			}
		}
		return true // continue iteration
	})

	// Wait for all sync broadcasts to complete or timeout
	wg.Wait()
}
