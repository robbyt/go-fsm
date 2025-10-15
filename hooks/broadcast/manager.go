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
	"fmt"
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

// GetStateChan returns a channel that receives state change notifications.
// The channel is automatically closed when ctx is cancelled.
// Use functional options to customize buffer size, timeout behavior, or provide a custom channel.
// Returns an error if ctx is nil.
func (m *Manager) GetStateChan(ctx context.Context, opts ...Option) (<-chan string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context cannot be nil")
	}

	config := &Config{
		syncTimeout: defaultAsyncTimeout,
	}

	for _, opt := range opts {
		opt(config)
	}

	var ch chan string
	if config.customChannel != nil {
		ch = config.customChannel
	} else {
		ch = make(chan string, 1)
	}

	// Register subscriber
	m.mu.Lock()
	m.subscribers.Store(ch, config)
	m.mu.Unlock()

	// Handle cleanup when context is cancelled
	go func() {
		<-ctx.Done()
		m.unsubscribe(ch)
		close(ch)
	}()

	return ch, nil
}

// BroadcastHook returns a function compatible with hooks.ActionFunc signature.
// This can be passed directly to RegisterPostTransitionHook without manual wrapping.
func (m *Manager) BroadcastHook() func(ctx context.Context, from, to string) {
	return func(ctx context.Context, from, to string) {
		m.Broadcast(to)
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
