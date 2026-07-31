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

	// ctxDone is the subscriber's context cancellation signal. Broadcast selects
	// on it so a blocking send to a subscriber whose context has been cancelled
	// is abandoned instead of holding the manager mutex forever. It is nil for a
	// non-cancellable context, and a nil channel is never ready.
	ctxDone <-chan struct{}

	// departed is closed exactly once when this subscription ends, by either
	// context cancellation or an explicit unsubscribe. Broadcast selects on it
	// alongside ctxDone so an explicit unsubscribe can release a parked send.
	departed   chan struct{}
	signalOnce sync.Once

	// stopAfterFunc releases this subscription's context.AfterFunc association.
	// It is assigned under Manager.mu before the subscription is published, and
	// is only ever read by UnsubscribeAll, which reaches the subscription through
	// the map and so observes the write. depart must never call it: on the
	// context path depart IS the callback.
	stopAfterFunc func() bool
}

// key returns the map key for this subscription. Subscribers are keyed by the
// receive side of the channel, which is the form GetStateChan returns and the
// form a duplicate-registration check needs to compare against.
func (s *subscription) key() <-chan string {
	return s.ch
}

// signalDepart marks this subscription as ended, releasing any broadcast
// currently parked on a blocking send to it. Safe to call repeatedly and from
// any goroutine; it deliberately takes no lock, because callers must be able to
// signal before contending for the manager mutex.
func (s *subscription) signalDepart() {
	s.signalOnce.Do(func() { close(s.departed) })
}

// live reports whether this subscription still receives broadcasts. Departure
// counts as dead as well as context cancellation: teardown signals departure
// before it can acquire the manager mutex, and a GetStateChan that wins the
// mutex in that window must see the slot as free rather than rejecting the new
// registration as a duplicate.
func (s *subscription) live() bool {
	select {
	case <-s.departed:
		return false
	case <-s.ctxDone: // nil for a non-cancellable context, and never ready
		return false
	default:
		return true
	}
}

// Manager handles state change notifications to subscribers.
type Manager struct {
	mu sync.Mutex

	// subscribers maps <-chan string to *subscription.
	//
	// The locking discipline here is deliberately path-dependent, and a future
	// change to a plain map must preserve both halves:
	//
	//   - GetStateChan does its duplicate Load and its Store under mu, because
	//     the pair has to be atomic or two concurrent registrations of the same
	//     channel both succeed.
	//   - Unsubscribe and UnsubscribeAll's first pass read WITHOUT mu, because
	//     they must signal departure before contending for a mutex that a
	//     broadcast parked on a blocking send is holding.
	//
	// A plain map would therefore need its own second mutex for the unlocked
	// reads; reusing mu for them reintroduces the deadlock.
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

// GetStateChan returns a channel that receives state change notifications.
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
// lifetime of the manager. Prefer a cancellable context. An already-cancelled
// context is rejected.
//
// A channel may hold only one live subscription per manager: subscribing the
// same channel twice returns ErrChannelAlreadySubscribed. Re-subscribing a
// channel whose previous subscription has ended is allowed, as is subscribing
// one channel to several managers.
func (m *Manager) GetStateChan(ctx context.Context, opts ...Option) (<-chan string, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	// Reject an already-cancelled context rather than registering a subscription
	// that is dead on arrival and would be torn down again immediately.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cannot subscribe with a cancelled context: %w", err)
	}

	config := &Config{}

	for _, opt := range opts {
		opt(config)
	}

	sub := &subscription{
		timeout:         config.timeout,
		externalChannel: config.externalChannel,
		ctxDone:         ctx.Done(),
		departed:        make(chan struct{}),
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

	// Register the subscriber and arm context cleanup as one critical section.
	//
	// context.AfterFunc runs its callback immediately, on a fresh goroutine, when
	// the context is already cancelled -- so arming it outside the lock would let
	// that callback finish departing before the Store below published the
	// subscription, leaving a departed (and possibly closed) entry in the map.
	// Holding m.mu across arm-and-store closes that window: depart signals
	// without a lock but cannot reach its CompareAndDelete until we release, at
	// which point it correctly removes what we just stored.
	//
	// AfterFunc also replaces a `go func() { <-ctx.Done(); ... }()` cleanup
	// goroutine. For a non-cancellable context (context.Background()) it
	// registers nothing and starts nothing, so such a subscriber no longer leaks
	// a parked goroutine; it lives until Unsubscribe or UnsubscribeAll.
	//
	// The duplicate check shares this critical section: splitting it from the
	// Store would let two concurrent registrations of the same channel both pass.
	m.mu.Lock()
	if value, loaded := m.subscribers.Load(sub.key()); loaded {
		if existing, ok := value.(*subscription); ok && existing.live() {
			m.mu.Unlock()
			return nil, ErrChannelAlreadySubscribed
		}
		// The entry is dead: its context was cancelled, or an unsubscribe
		// signalled departure and is still waiting for this mutex. Take the slot
		// over -- depart uses CompareAndDelete, so the older teardown cannot
		// evict us.
	}
	sub.stopAfterFunc = context.AfterFunc(ctx, func() { m.depart(sub) })
	m.subscribers.Store(sub.key(), sub)
	m.mu.Unlock()

	return sub.ch, nil
}

// UnsubscribeAll ends every current subscription. It is used to release all
// subscribers at once when tearing down whatever owns this manager.
//
// Unlike a context cancellation, UnsubscribeAll cannot be delayed by the broadcast it is
// trying to release: it signals every subscription it can see before acquiring
// the manager mutex, so any parked send has already been given its exit.
//
// A subscription created concurrently with UnsubscribeAll is included if and
// only if it completed registration before the second pass acquired the mutex.
func (m *Manager) UnsubscribeAll() {
	// Phase 1: signal without the mutex, freeing any parked broadcast so that
	// phase 2 can acquire m.mu.
	seen := make(map[*subscription]struct{})
	for sub := range m.iterSubscribers() {
		sub.signalDepart()
		seen[sub] = struct{}{}
	}

	// Phase 2: under the mutex, remove everything still registered. Ranging
	// again picks up any subscription that won the mutex ahead of us.
	m.mu.Lock()
	for sub := range m.iterSubscribers() {
		sub.signalDepart()
		seen[sub] = struct{}{}
		if m.subscribers.CompareAndDelete(sub.key(), sub) && !sub.externalChannel {
			close(sub.ch)
		}
	}
	m.mu.Unlock()

	// Phase 3: release the context associations for everything either pass saw.
	// A take-over can make phase 1 observe an old record and phase 2 its
	// replacement, so both need stopping.
	for sub := range seen {
		sub.stopAfterFunc()
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
		// departed and ctxDone are the subscriber's departure signals. Selecting
		// on them in the blocking branches ensures a subscriber that has gone
		// away -- its context was cancelled, or it was explicitly unsubscribed --
		// cannot block the broadcast indefinitely while the manager mutex is
		// held, which would otherwise deadlock departure and every future
		// Broadcast.
		done := sub.ctxDone
		departed := sub.departed
		if sub.timeout < 0 {
			// Negative timeout: block until delivered or the subscriber departs
			// (guaranteed delivery for live subscribers).
			wg.Go(func() {
				select {
				case ch <- state:
					logger.Debug("State delivered to guaranteed delivery subscriber")
				case <-done:
					logger.Debug("Guaranteed-delivery subscriber departed before delivery; aborting send")
				case <-departed:
					logger.Debug("Guaranteed-delivery subscriber unsubscribed before delivery; aborting send")
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

// depart ends a subscription: it releases any in-flight blocking send to it,
// removes it from the subscriber set, and closes the channel when the manager
// owns it. Idempotent and safe to call from any goroutine.
//
// The ordering is load-bearing. departed is closed BEFORE m.mu is acquired:
// Broadcast holds m.mu for the whole broadcast and may be parked in a blocking
// send to this subscriber, so signalling first lets that send abort and the
// mutex drop. Acquiring m.mu afterwards is what makes removal synchronous --
// once depart returns, no broadcast is sending on this channel.
func (m *Manager) depart(sub *subscription) {
	sub.signalDepart()

	m.mu.Lock()
	defer m.mu.Unlock()

	// CompareAndDelete rather than Delete: the same channel may already have been
	// re-subscribed under a new subscription, and a late departure must neither
	// evict that newer entry nor close the channel it is now using.
	if m.subscribers.CompareAndDelete(sub.key(), sub) && !sub.externalChannel {
		// Closing under m.mu is the send/close barrier: broadcasts hold the same
		// mutex and wait for their send goroutines, so none is in flight here.
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
