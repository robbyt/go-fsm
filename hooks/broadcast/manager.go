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
	"iter"
	"log/slog"
	"sync"
	"time"
)

// Manager handles state change notifications to subscribers.
type Manager struct {
	mu          sync.Mutex
	subscribers sync.Map
	logger      slog.Handler
}

// NewManager creates a new broadcast manager.
func NewManager(handler slog.Handler) *Manager {
	if handler == nil {
		handler = slog.Default().Handler()
	}
	return &Manager{
		logger: handler,
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

	config := &Config{}

	for _, opt := range opts {
		opt(config)
	}

	var ch chan string
	if config.channel != nil {
		ch = config.channel
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
		if !config.externalChannel {
			close(ch)
		}
	}()

	return ch, nil
}

// Broadcast sends the state to all subscriber channels.
// Delivery behavior depends on subscriber timeout configuration:
// - timeout = 0 (default): best-effort, drops message if channel is full
// - timeout > 0: blocks up to timeout duration, then drops
// - timeout < 0: blocks indefinitely until delivered (guaranteed delivery)
// The mutex ensures broadcasts are always serial to maintain consistent ordering.
func (m *Manager) Broadcast(state string) {
	logger := slog.New(m.logger.WithGroup("broadcast")).With("state", state)

	// Lock during the entire broadcast to prevent race conditions
	m.mu.Lock()
	defer m.mu.Unlock()

	var wg sync.WaitGroup

	for ch, config := range m.iterSubscribers() {
		if config.timeout < 0 {
			// Negative timeout: block indefinitely (guaranteed delivery)
			wg.Go(func() {
				ch <- state
				logger.Debug("State delivered to guaranteed delivery subscriber")
			})
		} else if config.timeout > 0 {
			// Positive timeout: block up to timeout duration
			timeout := config.timeout
			wg.Go(func() {
				select {
				case ch <- state:
					logger.Debug("State delivered to timeout subscriber")
				case <-time.After(timeout):
					logger.Warn("Timeout subscriber blocked; state delivery timed out",
						"timeout", timeout,
						"channel_capacity", cap(ch), "channel_length", len(ch))
				}
			})
		} else {
			// Zero timeout: best-effort delivery (non-blocking)
			select {
			case ch <- state:
				logger.Debug("State delivered to best-effort subscriber")
			default:
				logger.Debug("Best-effort subscriber channel full; state delivery skipped",
					"channel_capacity", cap(ch), "channel_length", len(ch))
			}
		}
	}

	wg.Wait()
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

// iterSubscribers returns a sequence of all subscriber channels and their configs.
func (m *Manager) iterSubscribers() iter.Seq2[chan string, *Config] {
	return func(yield func(chan string, *Config) bool) {
		m.subscribers.Range(func(key, value any) bool {
			ch, ok := key.(chan string)
			if !ok {
				return true
			}

			config, ok := value.(*Config)
			if !ok {
				slog.New(m.logger).Error("Invalid subscriber config type; skipping subscriber")
				return true
			}

			return yield(ch, config)
		})
	}
}
