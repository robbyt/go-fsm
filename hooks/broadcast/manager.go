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

// subscription is the manager's per-subscriber bookkeeping, built in
// GetStateChan once the options have been applied. Config remains purely the
// option target; everything the manager needs at runtime lives here.
type subscription struct {
	ch              chan string
	timeout         time.Duration
	externalChannel bool

	// done is closed when the subscription ends, whether by the subscriber's
	// context being cancelled or by UnsubscribeAll -- both go through cancel.
	// Broadcast selects on it so a blocking send to a subscriber that has gone
	// away is abandoned instead of holding the manager mutex forever.
	done   <-chan struct{}
	cancel context.CancelFunc
}

// key returns the map key for this subscription. Subscribers are keyed by the
// receive side of the channel, which is the form GetStateChan returns and the
// form a duplicate-registration check needs to compare against.
func (s *subscription) key() <-chan string {
	return s.ch
}

// Manager handles state change notifications to subscribers.
type Manager struct {
	mu sync.Mutex

	// subscribers maps <-chan string to *subscription. UnsubscribeAll ranges
	// over it WITHOUT mu on its first pass, because it must signal departure
	// before contending for a mutex that a parked broadcast may be holding; a
	// plain map would need its own second lock for that.
	subscribers sync.Map

	logger *slog.Logger
}

// NewManager creates a new broadcast manager.
func NewManager(handler slog.Handler) *Manager {
	if handler == nil {
		handler = slog.Default().Handler()
	}
	return &Manager{
		logger: slog.New(handler.WithGroup("broadcast")),
	}
}

// GetStateChan registers for state change notifications and returns the channel
// they will be delivered on.
// Use functional options to customize buffer size, timeout behavior, or provide a custom channel.
// Returns an error if ctx is nil.
//
// Cancelling ctx ends the subscription; UnsubscribeAll ends every subscription
// at once. A manager-owned channel is closed when the subscription ends; a
// channel supplied via WithCustomChannel belongs to its caller, who is
// responsible for closing it and must not do so while it is still subscribed.
//
// Passing a non-cancellable context (context.Background()) is supported and
// costs nothing, but leaves UnsubscribeAll as the only way to end the
// subscription, so a subscriber that is simply dropped stays registered for the
// lifetime of the manager. Prefer a cancellable context.
func (m *Manager) GetStateChan(ctx context.Context, opts ...Option) (<-chan string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context cannot be nil")
	}

	config := &Config{}

	for _, opt := range opts {
		opt(config)
	}

	// Each subscription gets its own cancellable context derived from the
	// caller's. Cancelling either one ends it; cancel is idempotent; and a
	// cancelled child detaches from its parent, so an ended subscription holds
	// no reference from a long-lived caller context.
	subCtx, cancel := context.WithCancel(ctx)
	sub := &subscription{
		timeout:         config.timeout,
		externalChannel: config.externalChannel,
		done:            subCtx.Done(),
		cancel:          cancel,
	}

	if config.channel != nil {
		sub.ch = config.channel
	} else {
		// Fall back to a manager-owned channel. Reset externalChannel in case a
		// caller passed WithCustomChannel(nil), which would otherwise leave it
		// true and prevent cleanup from closing this channel.
		sub.ch = make(chan string, 1)
		sub.externalChannel = false
	}

	m.mu.Lock()
	m.subscribers.Store(sub.key(), sub)
	m.mu.Unlock()

	// context.AfterFunc replaces a `go func() { <-ctx.Done(); ... }()` cleanup
	// goroutine: it starts nothing until the context is actually cancelled, so a
	// subscriber on a non-cancellable context no longer leaks a parked
	// goroutine. Armed after the Store so an already-cancelled context finds
	// the entry it has to remove.
	context.AfterFunc(subCtx, func() { m.remove(sub) })

	return sub.ch, nil
}

// UnsubscribeAll ends every current subscription. It is used to release all
// subscribers at once when tearing down whatever owns this manager.
//
// It signals every subscription before acquiring the manager mutex, so a
// broadcast parked on a blocking send is released rather than waited on. A
// subscription created concurrently is included if and only if it was stored
// before the second pass acquired the mutex.
func (m *Manager) UnsubscribeAll() {
	for sub := range m.iterSubscribers() {
		sub.cancel()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for sub := range m.iterSubscribers() {
		sub.cancel()
		m.removeLocked(sub)
	}
}

// Broadcast sends the state to all subscriber channels.
// Delivery behavior depends on subscriber timeout configuration:
// - timeout = 0 (default): best-effort, drops message if channel is full
// - timeout > 0: blocks up to timeout duration, then drops
// - timeout < 0: blocks indefinitely until delivered (guaranteed delivery)
// The mutex ensures broadcasts are always serial to maintain consistent ordering.
func (m *Manager) Broadcast(state string) {
	logger := m.logger.With("state", state)

	// Lock during the entire broadcast to ensure all subscribers receive broadcasts
	// in the same order, preventing race conditions where concurrent broadcasts
	// could arrive at different subscribers in different orders.
	m.mu.Lock()
	defer m.mu.Unlock()

	var wg sync.WaitGroup

	for sub := range m.iterSubscribers() {
		ch := sub.ch
		// done is the subscriber's departure signal. Selecting on it in the
		// blocking branches ensures a subscriber that has gone away cannot block
		// the broadcast indefinitely while the manager mutex is held.
		done := sub.done
		if sub.timeout < 0 {
			// Negative timeout: block until delivered or the subscriber departs
			// (guaranteed delivery for live subscribers).
			wg.Go(func() {
				select {
				case ch <- state:
					logger.Debug("State delivered to guaranteed delivery subscriber")
				case <-done:
					logger.Debug("Guaranteed-delivery subscriber departed before delivery; aborting send")
				}
			})
		} else if sub.timeout > 0 {
			// Positive timeout: block up to timeout duration
			timeout := sub.timeout
			wg.Go(func() {
				select {
				case ch <- state:
					logger.Debug("State delivered to timeout subscriber")
				case <-done:
					logger.Debug("Timeout subscriber departed before delivery; aborting send")
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
				// Logged at Warn for parity with the timeout-mode drop log
				// in the branch above: silently lost state updates often
				// indicate a slow consumer that needs investigation.
				logger.Warn("Best-effort subscriber channel full; state delivery skipped",
					"channel_capacity", cap(ch), "channel_length", len(ch))
			}
		}
	}

	wg.Wait()
}

// BroadcastHook returns a function compatible with hooks.ActionFunc signature.
// This can be passed directly to RegisterPostTransitionHook without manual wrapping.
func (m *Manager) BroadcastHook(_ context.Context, _, to string) {
	m.Broadcast(to)
}

// remove takes an ended subscription out of the subscriber set and closes its
// channel if the manager owns it. It runs after the subscription's context is
// cancelled, so any broadcast parked on this subscriber has already been
// released; acquiring m.mu is what makes the removal synchronous with respect
// to broadcasts.
func (m *Manager) remove(sub *subscription) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeLocked(sub)
}

// removeLocked is remove with m.mu already held. LoadAndDelete so that only
// the caller that actually removed the entry closes a manager-owned channel:
// the context AfterFunc and UnsubscribeAll can both target the same
// subscription, and the mutex serialises them.
func (m *Manager) removeLocked(sub *subscription) {
	if _, loaded := m.subscribers.LoadAndDelete(sub.key()); loaded && !sub.externalChannel {
		close(sub.ch)
	}
}

// iterSubscribers returns a sequence of all current subscriptions.
func (m *Manager) iterSubscribers() iter.Seq[*subscription] {
	return func(yield func(*subscription) bool) {
		m.subscribers.Range(func(_, value any) bool {
			sub, ok := value.(*subscription)
			if !ok {
				m.logger.Error("Invalid subscriber type; skipping subscriber")
				return true
			}

			return yield(sub)
		})
	}
}
