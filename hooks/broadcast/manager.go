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

package broadcast

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Manager handles state change notifications to subscribers.
type Manager struct {
	mu          sync.Mutex
	subscribers sync.Map
	logger      *slog.Logger
}

// NewManager creates a new broadcast manager.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		logger: logger,
	}
}

// GetStateChan returns a channel that will receive state change notifications.
func (m *Manager) GetStateChan(ctx context.Context) <-chan string {
	return m.GetStateChanBuffer(ctx, 1)
}

// GetStateChanWithOptions returns a channel configured with functional options.
func (m *Manager) GetStateChanWithOptions(ctx context.Context, opts ...Option) <-chan string {
	config := &Config{
		syncTimeout: defaultAsyncTimeout,
	}

	for _, opt := range opts {
		opt(config)
	}

	if ctx == nil {
		m.logger.Error("context is nil; cannot create state channel")
		return nil
	}

	var ch chan string
	if config.customChannel != nil {
		ch = config.customChannel
	} else {
		ch = make(chan string, 1)
	}

	unsubCallback := m.addSubscriberWithConfig(ch, config)

	go func() {
		<-ctx.Done()
		unsubCallback()
		close(ch)
	}()

	return ch
}

// GetStateChanBuffer returns a channel with a configurable buffer size.
func (m *Manager) GetStateChanBuffer(ctx context.Context, chanBufferSize int) <-chan string {
	if ctx == nil {
		m.logger.Error("context is nil; cannot create state channel")
		return nil
	}

	ch := make(chan string, chanBufferSize)
	unsubCallback := m.AddSubscriber(ch)

	go func() {
		<-ctx.Done()
		unsubCallback()
		close(ch)
	}()

	return ch
}

// AddSubscriber adds a channel to receive state change broadcasts.
// Returns a callback function to unsubscribe and remove the channel.
//
// Deprecated: Use GetStateChanWithOptions with WithCustomChannel instead.
func (m *Manager) AddSubscriber(ch chan string) func() {
	config := &Config{
		syncTimeout: defaultAsyncTimeout,
	}
	return m.addSubscriberWithConfig(ch, config)
}

// addSubscriberWithConfig adds a subscriber with specific configuration.
func (m *Manager) addSubscriberWithConfig(ch chan string, config *Config) func() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.subscribers.Store(ch, config)

	return func() {
		m.unsubscribe(ch)
	}
}

// unsubscribe removes a channel from receiving broadcasts.
func (m *Manager) unsubscribe(ch chan string) {
	m.mu.Lock()
	m.subscribers.Delete(ch)
	m.mu.Unlock()
}

// Broadcast sends the state to all subscriber channels.
// Subscribers with negative timeout block indefinitely until delivered.
// Subscribers with timeout=0 behave asynchronously (drop messages if channel is full).
// Subscribers with positive timeout block until delivered or timeout.
// The mutex ensures broadcasts are always serial to maintain consistent ordering.
func (m *Manager) Broadcast(state string) {
	logger := m.logger.WithGroup("broadcast").With("state", state)

	// Lock during the entire broadcast to prevent race conditions
	m.mu.Lock()
	defer m.mu.Unlock()

	var wg sync.WaitGroup

	m.subscribers.Range(func(key, value any) bool {
		ch := key.(chan string)

		config, ok := value.(*Config)
		if !ok {
			logger.Error("Invalid subscriber config type; skipping subscriber")
			return true
		}

		if config.syncTimeout < 0 {
			// Handle infinite blocking broadcast
			wg.Add(1)
			go func(ch chan string) {
				defer wg.Done()
				ch <- state
				logger.Debug("State delivered to blocking subscriber")
			}(ch)
		} else if config.syncTimeout == 0 {
			// Handle async broadcast (non-blocking)
			select {
			case ch <- state:
				logger.Debug("State delivered to asynchronous subscriber")
			default:
				logger.Debug("Asynchronous subscriber channel full; state delivery skipped",
					"channel_capacity", cap(ch), "channel_length", len(ch))
			}
		} else {
			// Handle sync broadcast with positive timeout
			wg.Add(1)
			go func(ch chan string, timeout time.Duration) {
				defer wg.Done()
				select {
				case ch <- state:
					logger.Debug("State delivered to synchronous subscriber")
				case <-time.After(timeout):
					logger.Warn("Synchronous subscriber blocked; state delivery timed out",
						"timeout", timeout,
						"channel_capacity", cap(ch), "channel_length", len(ch))
				}
			}(ch, config.syncTimeout)
		}
		return true
	})

	wg.Wait()
}
