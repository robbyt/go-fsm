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

// Package fsm provides a thread-safe finite state machine implementation.
// It allows defining custom states and transitions, managing state changes,
// subscribing to state updates via channels, and persisting/restoring state via JSON.
//
// Example usage:
//
//	machine, err := fsm.NewSimple("new", map[string][]string{
//	    "new":     {"running"},
//	    "running": {"stopped"},
//	    "stopped": {},
//	})
//	if err != nil {
//	    return err
//	}
//
//	err = machine.Transition("running")
//	if err != nil {
//	    return err
//	}
package fsm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/hooks/broadcast"
	"github.com/robbyt/go-fsm/v2/transitions"
)

// transitionDB is the contract a transition table must satisfy.
//
// Implementations MUST be safe for concurrent reads from multiple goroutines.
// The Machine does not guard transitionDB calls with its mutex
// (GetAllStates, HasState, IsTransitionAllowed all run lock-free), so any
// locking or copying needed for read safety is the implementation's
// responsibility. The bundled transitions.Config satisfies this contract:
// its index is built once at construction and never mutated. The one exception
// is transitions.Config.UnmarshalJSON, which rebuilds the config in place; see
// its doc comment. Never unmarshal into a Config that is already attached to a
// Machine.
type transitionDB interface {
	HasState(state string) bool
	GetAllStates() []string
	IsTransitionAllowed(from, to string) bool
}

// Machine represents a finite state machine that tracks its current state
// and manages state transitions.
type Machine struct {
	mutex sync.RWMutex
	state atomic.Value

	transitions transitionDB
	callbacks   CallbackExecutor
	logger      *slog.Logger

	broadcastTimeout time.Duration

	// id is a process-unique identifier assigned by New, used to build a
	// collision-free name for this Machine's broadcast hook. A Machine built
	// without New (the zero value) gets id 0, but such a Machine already fails
	// on any transition, so it is not guarded here.
	id uint64

	// subMu gates the lazily-created broadcast plumbing below. It is separate
	// from mutex on purpose: a broadcast runs from a post-transition hook while
	// mutex is held for writing, so Close must never take mutex -- it has to be
	// able to reach the broadcast manager to release exactly that broadcast.
	//
	// Nested lock order: mutex -> subMu. Never acquire mutex while holding subMu.
	subMu            sync.RWMutex
	broadcastManager *broadcast.Manager
	hookName         string
	closed           bool
	closeErr         error
}

// machineIDCounter stamps each Machine with a process-unique id. A pointer
// (%p) is not unique over time: a garbage-collected Machine's address can be
// reused by a later allocation, and on a shared registry that produced a
// spurious ErrHookNameAlreadyExists for the new Machine.
var machineIDCounter atomic.Uint64

// New creates a finite state machine with the specified initial state and transitions.
// For simpler usage with inline maps, see NewSimple().
//
// Example usage:
//
//	trans := transitions.MustNew(map[string][]string{
//	    transitions.StatusNew:     {transitions.StatusBooting, transitions.StatusError},
//	    transitions.StatusBooting: {transitions.StatusRunning, transitions.StatusError},
//	    transitions.StatusRunning: {transitions.StatusReloading, transitions.StatusStopping, transitions.StatusError},
//	})
//	machine, err := fsm.New(transitions.StatusNew, trans)
//
// Or use the provided transitions.Typical:
//
//	machine, err := fsm.New(transitions.StatusNew, transitions.Typical)
func New(
	initialState string,
	trans transitionDB,
	opts ...Option,
) (*Machine, error) {
	handler := slog.Default().
		Handler().
		WithGroup("fsm")

	if trans == nil {
		return nil, fmt.Errorf("%w: transitions is nil", ErrInvalidConfiguration)
	}

	if !trans.HasState(initialState) {
		return nil, fmt.Errorf(
			"%w: initial state '%s' is not defined in transitions",
			ErrInvalidConfiguration,
			initialState,
		)
	}

	m := &Machine{
		transitions:      trans,
		logger:           slog.New(handler),
		broadcastTimeout: 100 * time.Millisecond,
		id:               machineIDCounter.Add(1),
	}
	m.state.Store(initialState)

	for _, opt := range opts {
		if err := opt(m); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	return m, nil
}

// NewSimple creates a FSM with a map-based transition configuration.
//
// Example usage:
//
//	machine, err := fsm.NewSimple("online", map[string][]string{
//	    "online":  {"offline", "error"},
//	    "offline": {"online", "error"},
//	    "error":   {},
//	})
func NewSimple(
	initialState string,
	allowedTransitions map[string][]string,
	opts ...Option,
) (*Machine, error) {
	trans, err := transitions.New(allowedTransitions)
	if err != nil {
		return nil, err
	}
	return New(initialState, trans, opts...)
}

// GetState returns the current state of the finite state machine.
//
// Safety: setState only stores strings, and atomic.Value would already
// panic at Store-time on a type mismatch, so the assertion below cannot
// fail in practice. The comma-ok form is used to defend against the only
// remaining edge case — a Load on a Machine constructed without going
// through New (zero-value, no initial Store), which returns nil from
// Load and would otherwise panic on the assertion. In that case the
// caller observes "" rather than a runtime panic.
func (fsm *Machine) GetState() string {
	v, _ := fsm.state.Load().(string)
	return v
}

// GetAllStates returns all allowed states that have been added to this FSM.
//
// The transition table is set at construction and not mutated, so this method
// does not take the FSM mutex. Callers passing a custom transitionDB
// implementation must ensure their type is safe for concurrent reads.
func (fsm *Machine) GetAllStates() []string {
	return fsm.transitions.GetAllStates()
}

// IsTransitionAllowed reports whether a transition from the current state to
// toState is permitted by the transition table. It mirrors
// transitions.Config.IsTransitionAllowed with the current state as the source.
// Like GetState, it takes no mutex and is a point-in-time snapshot, so the
// result may be stale if a concurrent transition is in flight.
func (fsm *Machine) IsTransitionAllowed(toState string) bool {
	return fsm.transitions.IsTransitionAllowed(fsm.GetState(), toState)
}

// AvailableTransitions returns the states the FSM may transition to from its
// current state, sorted alphabetically. It returns an empty slice when the
// current state has no outgoing transitions — that is, when it is terminal or
// not present in the transition table. Like GetState, this is a lock-free
// point-in-time snapshot.
func (fsm *Machine) AvailableTransitions() []string {
	current := fsm.GetState()
	allStates := fsm.transitions.GetAllStates()
	available := make([]string, 0, len(allStates))
	for _, state := range allStates {
		if fsm.transitions.IsTransitionAllowed(current, state) {
			available = append(available, state)
		}
	}
	// Sort explicitly: the transitionDB contract does not guarantee
	// GetAllStates ordering, but this method documents sorted output.
	slices.Sort(available)
	return available
}

// IsTerminal reports whether the current state has no outgoing transitions.
// Like GetState, this is a lock-free point-in-time snapshot.
func (fsm *Machine) IsTerminal() bool {
	current := fsm.GetState()
	for _, state := range fsm.transitions.GetAllStates() {
		if fsm.transitions.IsTransitionAllowed(current, state) {
			return false
		}
	}
	return true
}

// SetState updates the FSM's state, bypassing transition rules and pre-transition hooks.
// Returns an error if the state is not defined in the transition table.
func (fsm *Machine) SetState(state string) error {
	return fsm.SetStateWithContext(context.Background(), state)
}

// SetStateWithContext updates the FSM's state with a context, bypassing transition rules and pre-transition hooks.
// Returns an error if the state is not defined in the transition table.
func (fsm *Machine) SetStateWithContext(ctx context.Context, state string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("set state canceled: %w", err)
	}

	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()

	if !fsm.transitions.HasState(state) {
		return fmt.Errorf(
			"%w: state '%s' is not defined in transitions",
			ErrInvalidConfiguration,
			state,
		)
	}

	fromState := fsm.GetState()
	fsm.setState(state)
	fsm.logger.Debug("Set state", "from", fromState, "to", state)

	if fsm.callbacks != nil {
		fsm.callbacks.ExecutePostTransitionHooks(ctx, fromState, state)
	}

	return nil
}

// setState updates the FSM's state atomically.
// Assumes the caller holds the write lock.
func (fsm *Machine) setState(state string) {
	fsm.state.Store(state)
}

// Transition changes the FSM's state to toState if the transition is allowed.
// Returns an error if the transition is not permitted.
func (fsm *Machine) Transition(toState string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(context.Background(), toState)
}

// TransitionBool returns true if the transition to toState succeeds, false otherwise.
func (fsm *Machine) TransitionBool(toState string) bool {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(context.Background(), toState) == nil
}

// TransitionWithContext changes the FSM's state to toState with a context.
// The context is passed to all hooks for request-scoped values, tracing, or cancellation.
// Returns an error if the transition is not permitted.
func (fsm *Machine) TransitionWithContext(ctx context.Context, toState string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(ctx, toState)
}

// TransitionBoolWithContext returns true if the transition to toState succeeds with a context, false otherwise.
func (fsm *Machine) TransitionBoolWithContext(ctx context.Context, toState string) bool {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(ctx, toState) == nil
}

// TransitionIfCurrentState transitions to toState only if the current state matches fromState.
// Returns an error if the current state does not match or if the transition is not allowed.
func (fsm *Machine) TransitionIfCurrentState(fromState, toState string) error {
	return fsm.TransitionIfCurrentStateWithContext(context.Background(), fromState, toState)
}

// TransitionIfCurrentStateWithContext transitions to toState with a context only if the current state matches fromState.
// Returns an error if the current state does not match or if the transition is not allowed.
func (fsm *Machine) TransitionIfCurrentStateWithContext(ctx context.Context, fromState, toState string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()

	currentState := fsm.GetState()
	if currentState != fromState {
		return fmt.Errorf(
			"%w: current state is '%s', expected '%s' for transition to '%s'",
			ErrCurrentStateIncorrect,
			currentState,
			fromState,
			toState,
		)
	}

	return fsm.transition(ctx, toState)
}

// transition changes the FSM's state from the current state to toState.
// Assumes the caller holds the write lock.
// The context is passed to all hooks for request-scoped values, tracing, and cancellation handling.
// Hooks are responsible for checking context cancellation themselves if needed.
func (fsm *Machine) transition(ctx context.Context, toState string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("transition canceled: %w", err)
	}

	currentState := fsm.GetState()
	if !fsm.transitions.IsTransitionAllowed(currentState, toState) {
		return fmt.Errorf(
			"%w: transition from '%s' to '%s' is not allowed",
			ErrInvalidStateTransition,
			currentState, toState,
		)
	}

	if fsm.callbacks != nil {
		fsm.logger.Debug("Executing pre-transition hooks...", "from", currentState, "to", toState)
		if err := fsm.callbacks.ExecutePreTransitionHooks(ctx, currentState, toState); err != nil {
			fsm.logger.Debug("Pre-transition hooks failed, aborting the transition", "from", currentState, "to", toState, "error", err)
			return err
		}
		fsm.logger.Debug("Pre-transition hooks completed", "from", currentState, "to", toState)
	}

	fsm.setState(toState)
	fsm.logger.Debug("Transition successful", "from", currentState, "to", toState)

	if fsm.callbacks != nil {
		fsm.logger.Debug("Executing post-transition hooks...", "from", currentState, "to", toState)
		fsm.callbacks.ExecutePostTransitionHooks(ctx, currentState, toState)
		fsm.logger.Debug("Post-transition hooks completed", "from", currentState, "to", toState)
	}

	return nil
}

// GetStateChan registers a channel to receive state change notifications and sends
// the current state immediately, blocking on sending the initial state if the channel buffer is full.
// Use a buffered channel to avoid blocking.
//
// Subscriber registration and the initial-state send happen while holding the FSM read lock, so they
// are atomic with respect to transitions. Two consequences follow: GetStateChan must NOT be called
// from within a transition hook (the hook already holds the FSM write lock, so this would deadlock),
// and a full or unbuffered channel blocks transitions until the initial send drains. The initial
// send honors the provided context — if ctx is cancelled while it is blocked, GetStateChan returns
// the context's error.
//
// This method requires the FSM to be configured with a hooks.Registry (via WithCallbackRegistry).
// The registry must be created with WithTransitions() to support wildcard pattern matching.
// Returns an error if the callback executor does not support dynamic hook registration.
//
// The channel receives all future state transitions until the context is cancelled, or
// the Machine is closed with Close — whichever comes first. The channel is NOT closed;
// the caller maintains ownership and is responsible for its lifecycle, and must not
// close it while it is still subscribed.
//
// Passing a non-cancellable context (context.Background()) is supported, but then Close
// is the only way to end the subscription: a subscriber that is simply dropped stays
// registered for the life of the Machine. Prefer a cancellable context.
//
// GetStateChan can be called multiple times with different channels and contexts. All channels
// share the same broadcast manager, which is lazily initialized only once upon the first call.
// A channel may hold only one live subscription per Machine; subscribing the same channel twice
// returns broadcast.ErrChannelAlreadySubscribed. Re-subscribing a channel whose previous
// subscription has ended is allowed, as is subscribing one channel to several Machines.
// An already-cancelled context is rejected.
//
// Each Machine registers its broadcast hook under a name unique to that Machine, so a single
// hooks.Registry may be shared across Machines without GetStateChan failing. Note, however, that
// hooks registered on a shared registry fire for every Machine's transitions. When sharing a
// registry, call Close on a Machine you are discarding, or its broadcast hook keeps firing on
// every other Machine's transitions for the life of the registry.
//
// Broadcast delivery behavior is controlled by the timeout configured via WithBroadcastTimeout:
//   - timeout = 0: best-effort delivery (non-blocking, drops if channel is full)
//   - timeout > 0: blocks up to the timeout duration, then drops the message
//   - timeout < 0: guaranteed delivery (blocks indefinitely until delivered)
//
// Default timeout is 100ms if not configured.
//
// The channel must be bidirectional (chan string, not <-chan string) to allow the FSM
// to send state updates. The caller maintains ownership of the channel and controls
// its buffer size and lifecycle. It must be non-nil; a nil channel returns
// ErrNilStateChannel without registering a subscriber.
//
// Example:
//
//	registry, _ := hooks.NewRegistry(
//	    hooks.WithLogHandler(handler),
//	    hooks.WithTransitions(transitions.Typical),
//	)
//
//	machine, _ := fsm.New(
//	    transitions.StatusNew,
//	    transitions.Typical,
//	    fsm.WithCallbackRegistry(registry),
//	)
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	stateChan := make(chan string, 10)
//	if err := machine.GetStateChan(ctx, stateChan); err != nil {
//	    return err
//	}
//
//	go func() {
//	    for {
//	        select {
//	        case state := <-stateChan:
//	            log.Printf("State: %s", state)
//	        case <-ctx.Done():
//	            return
//	        }
//	    }
//	}()
func (fsm *Machine) GetStateChan(ctx context.Context, c chan string) error {
	if ctx == nil {
		return fmt.Errorf("%w: context cannot be nil", ErrInvalidConfiguration)
	}

	// Reject a nil channel before anything is registered. A nil channel makes the
	// initial-state send below block forever, and because that send happens under
	// the FSM read lock it would wedge every subsequent Transition, SetState, and
	// MarshalJSON on this machine, with no way to recover when the context is not
	// cancellable.
	if c == nil {
		return ErrNilStateChannel
	}

	if fsm.callbacks == nil {
		return ErrNoCallbackRegistry
	}

	registrar, ok := fsm.callbacks.(HookRegistrar)
	if !ok {
		return fmt.Errorf("%w that supports dynamic hook registration", ErrNoCallbackRegistry)
	}

	if _, err := fsm.ensureBroadcast(registrar); err != nil {
		return err
	}

	// Register the subscriber and send the current state while holding the read
	// lock. Broadcasts only happen under the write lock (the post-transition
	// hook runs inside transition()), so the read lock guarantees no transition
	// can broadcast between registering the channel and sending the initial
	// state. Without it, a concurrent transition could make the subscriber
	// observe a duplicated or out-of-order first value. Concurrent GetStateChan
	// callers still proceed in parallel under the shared read lock. (See the
	// method doc for the blocking and hook-reentrancy implications.)
	fsm.mutex.RLock()
	defer fsm.mutex.RUnlock()

	// subMu is held for reading across registration and the initial send so a
	// concurrent Close cannot detach the manager underneath us and leave this
	// subscriber permanently silent. Close takes it for writing, so it waits for
	// subscriptions already in flight and blocks new ones. Both are read locks
	// here, so concurrent GetStateChan callers still proceed in parallel.
	fsm.subMu.RLock()
	defer fsm.subMu.RUnlock()

	if fsm.closed {
		return ErrMachineClosed
	}

	_, err := fsm.broadcastManager.GetStateChan(
		ctx,
		broadcast.WithCustomChannel(c),
		broadcast.WithTimeout(fsm.broadcastTimeout),
	)
	if err != nil {
		return fmt.Errorf("failed to register channel: %w", err)
	}

	// Honor the context during the initial send so a full/unbuffered channel
	// cannot block here (and thus block transitions, since we hold the read
	// lock) past the caller's cancellation. The subscriber is unsubscribed by
	// the same ctx cancellation, via the broadcast manager.
	select {
	case c <- fsm.GetState():
	case <-ctx.Done():
		return fmt.Errorf("state channel registration canceled: %w", ctx.Err())
	}

	return nil
}

// ensureBroadcast lazily creates this Machine's broadcast manager and registers
// its post-transition broadcast hook, returning the manager.
//
// It replaces a sync.Once, which Close made untenable: a Once cannot be undone,
// reading the manager it writes from a goroutine that never called Do would be a
// data race, and a cached setup error permanently poisoned the Machine. A failed
// setup is no longer cached -- the next call retries.
//
// subMu is released before returning, so this never holds it while acquiring
// fsm.mutex and therefore does not participate in the mutex -> subMu order.
func (fsm *Machine) ensureBroadcast(registrar HookRegistrar) (*broadcast.Manager, error) {
	fsm.subMu.Lock()
	defer fsm.subMu.Unlock()

	if fsm.closed {
		return nil, ErrMachineClosed
	}

	if fsm.broadcastManager != nil {
		return fsm.broadcastManager, nil
	}

	manager := broadcast.NewManager(fsm.logger.Handler())

	// Name the hook after this Machine's process-unique id so several Machines
	// can share one registry without colliding.
	name := fmt.Sprintf("fsm.GetStateChan.%d", fsm.id)
	if err := registrar.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
		Name:   name,
		From:   []string{"*"},
		To:     []string{"*"},
		Action: manager.BroadcastHook,
	}); err != nil {
		return nil, err
	}

	fsm.broadcastManager = manager
	fsm.hookName = name

	return manager, nil
}

// Close releases only the state-subscription resources this Machine created:
// it ends every live GetStateChan subscription and removes the broadcast hook
// that GetStateChan registered on the callback registry. It does NOT stop the
// Machine -- GetState, Transition, SetState, MarshalJSON, and every hook the
// caller registered themselves keep working exactly as before.
//
// Call it when discarding a Machine whose hooks.Registry outlives it (a shared
// registry). Otherwise that hook, and the broadcast manager behind it, keep
// firing on every transition of every other Machine using that registry.
//
// Channels supplied to GetStateChan are owned by the caller and are not closed;
// their subscribers see silence, exactly as when their context is cancelled.
// After Close, GetStateChan returns ErrMachineClosed.
//
// Close is idempotent, but it does not forget a failure: if removing the hook
// fails, every subsequent call returns that same error, because a silently
// orphaned hook is the exact condition Close exists to prevent.
//
// Close blocks while an in-flight broadcast completes, and can block for as long
// as a concurrent GetStateChan holds the subscription gate -- which, with an
// unbuffered channel and a non-cancellable context, may be indefinitely. It is
// safe to call from a transition hook: Close never takes the FSM mutex, and hook
// executors release the registry lock before running hooks.
func (fsm *Machine) Close() error {
	fsm.subMu.Lock()
	defer fsm.subMu.Unlock()

	if fsm.closed {
		return fsm.closeErr
	}
	fsm.closed = true

	manager, hookName := fsm.broadcastManager, fsm.hookName
	if manager == nil {
		// GetStateChan was never called: nothing was ever registered.
		return nil
	}

	// Release subscribers first. UnsubscribeAll signals every subscription
	// before taking the manager mutex, so it cannot be blocked by the very
	// broadcast it is trying to free.
	manager.UnsubscribeAll()
	fsm.broadcastManager = nil
	fsm.hookName = ""

	remover, ok := fsm.callbacks.(HookRemover)
	if !ok {
		// Still closed and still unsubscribed: a partial failure must not leave
		// the Machine half-open. Name the hook so it can be removed by hand.
		fsm.closeErr = fmt.Errorf(
			"%w: callback registry does not support removing hooks; hook %q is orphaned",
			ErrInvalidConfiguration, hookName,
		)
		return fsm.closeErr
	}

	// A hook the caller already removed is not an error.
	if err := remover.RemoveHook(hookName); err != nil && !errors.Is(err, hooks.ErrHookNotFound) {
		fsm.closeErr = fmt.Errorf("failed to remove broadcast hook %q: %w", hookName, err)
		return fsm.closeErr
	}

	return nil
}
