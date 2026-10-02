package fsm

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/transitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransitionDB implements the transitionDB interface for unit testing
type mockTransitionDB struct {
	allowedTransitions map[string][]string
	states             []string
}

func newMockTransitionDB(transitions map[string][]string) *mockTransitionDB {
	states := make([]string, 0, len(transitions))
	for state := range transitions {
		states = append(states, state)
	}
	return &mockTransitionDB{
		allowedTransitions: transitions,
		states:             states,
	}
}

func (m *mockTransitionDB) IsTransitionAllowed(from, to string) bool {
	allowed, ok := m.allowedTransitions[from]
	if !ok {
		return false
	}
	return slices.Contains(allowed, to)
}

func (m *mockTransitionDB) HasState(state string) bool {
	_, ok := m.allowedTransitions[state]
	return ok
}

func (m *mockTransitionDB) GetAllStates() []string {
	return append([]string(nil), m.states...)
}

// mockCallbackExecutor implements the CallbackExecutor interface for unit testing
type mockCallbackExecutor struct {
	mu                sync.Mutex
	preTransitionErr  error
	preTransitions    []transition
	postTransitions   []transition
	receivedContexts  []context.Context
	shouldPanicOnPre  bool
	shouldPanicOnPost bool
}

type transition struct {
	from string
	to   string
}

func newMockCallbackExecutor() *mockCallbackExecutor {
	return &mockCallbackExecutor{
		preTransitions:   []transition{},
		postTransitions:  []transition{},
		receivedContexts: []context.Context{},
	}
}

func (m *mockCallbackExecutor) ExecutePreTransitionHooks(ctx context.Context, from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shouldPanicOnPre {
		panic("pre-transition panic")
	}
	m.receivedContexts = append(m.receivedContexts, ctx)
	m.preTransitions = append(m.preTransitions, transition{from, to})
	return m.preTransitionErr
}

func (m *mockCallbackExecutor) ExecutePostTransitionHooks(ctx context.Context, from, to string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shouldPanicOnPost {
		panic("post-transition panic")
	}
	m.receivedContexts = append(m.receivedContexts, ctx)
	m.postTransitions = append(m.postTransitions, transition{from, to})
}

func (m *mockCallbackExecutor) getPreTransitions() []transition {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]transition(nil), m.preTransitions...)
}

func (m *mockCallbackExecutor) getPostTransitions() []transition {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]transition(nil), m.postTransitions...)
}

func (m *mockCallbackExecutor) getReceivedContexts() []context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]context.Context(nil), m.receivedContexts...)
}

// mustSubscribe registers ch on m and fails the test immediately if
// registration errors. The subscription ends when ctx is cancelled.
func mustSubscribe(t *testing.T, m *Machine, ctx context.Context, ch chan string) {
	t.Helper()

	require.NoError(t, m.Subscribe(ctx, ch))
}

// Unit tests with mocked dependencies
func TestFSM_New_Validation(t *testing.T) {
	t.Parallel()

	t.Run("Nil transitions returns error", func(t *testing.T) {
		fsm, err := New("initial", nil)
		assert.Nil(t, fsm)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidConfiguration)
	})

	t.Run("Initial state not in transitions returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		fsm, err := New("invalid", mockDB)
		assert.Nil(t, fsm)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidConfiguration)
	})

	t.Run("Valid initialization succeeds", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		fsm, err := New("state1", mockDB)
		require.NoError(t, err)
		require.NotNil(t, fsm)
		assert.Equal(t, "state1", fsm.GetState())
	})
}

func TestFSM_GetState(t *testing.T) {
	t.Parallel()

	mockDB := newMockTransitionDB(map[string][]string{
		"initial": {"next"},
		"next":    {},
	})

	fsm, err := New("initial", mockDB)
	require.NoError(t, err)

	assert.Equal(t, "initial", fsm.GetState())
}

// TestFSM_GetState_ZeroValue verifies the comma-ok type assertion in
// GetState handles a zero-value Machine (atomic.Value.Load returns nil)
// without panicking. Constructing a Machine outside New() is not part of
// the public API, but this defends against accidental misuse.
func TestFSM_GetState_ZeroValue(t *testing.T) {
	t.Parallel()

	var fsm Machine
	assert.NotPanics(t, func() {
		assert.Empty(t, fsm.GetState())
	})
}

func TestFSM_SetState(t *testing.T) {
	t.Parallel()

	t.Run("SetState to valid state succeeds", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {"state3"},
			"state3": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.SetState("state2")
		require.NoError(t, err)
		assert.Equal(t, "state2", fsm.GetState())
	})

	t.Run("SetState to invalid state returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.SetState("invalid")
		require.ErrorIs(t, err, ErrInvalidConfiguration)
		assert.Equal(t, "state1", fsm.GetState()) // State should not change
	})

	t.Run("SetState calls post-transition hooks", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		err = fsm.SetState("state2")
		require.NoError(t, err)

		postTransitions := mockCallbacks.getPostTransitions()
		require.Len(t, postTransitions, 1)
		assert.Equal(t, "state1", postTransitions[0].from)
		assert.Equal(t, "state2", postTransitions[0].to)
	})
}

func TestFSM_Transition(t *testing.T) {
	t.Parallel()

	t.Run("Valid transition succeeds", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {"state3"},
			"state3": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.Transition("state2")
		require.NoError(t, err)
		assert.Equal(t, "state2", fsm.GetState())
	})

	t.Run("Invalid transition returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		// state1 -> state3 is not allowed
		err = fsm.Transition("state3")
		require.ErrorIs(t, err, ErrInvalidStateTransition)
		assert.Equal(t, "state1", fsm.GetState())
	})

	t.Run("Transition to non-existent state returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.Transition("nonexistent")
		require.ErrorIs(t, err, ErrInvalidStateTransition)
		assert.Equal(t, "state1", fsm.GetState())
	})
}

func TestFSM_Transition_WithCallbacks(t *testing.T) {
	t.Parallel()

	t.Run("Pre and post-transition hooks execute in order", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		err = fsm.Transition("state2")
		require.NoError(t, err)

		preTransitions := mockCallbacks.getPreTransitions()
		postTransitions := mockCallbacks.getPostTransitions()

		require.Len(t, preTransitions, 1)
		require.Len(t, postTransitions, 1)

		assert.Equal(t, "state1", preTransitions[0].from)
		assert.Equal(t, "state2", preTransitions[0].to)
		assert.Equal(t, "state1", postTransitions[0].from)
		assert.Equal(t, "state2", postTransitions[0].to)
	})

	t.Run("Pre-transition hook error prevents transition", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()
		mockCallbacks.preTransitionErr = assert.AnError

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		err = fsm.Transition("state2")
		require.Error(t, err)
		assert.Equal(t, "state1", fsm.GetState()) // State should not change

		preTransitions := mockCallbacks.getPreTransitions()
		postTransitions := mockCallbacks.getPostTransitions()

		require.Len(t, preTransitions, 1) // Pre-hook was called
		require.Empty(t, postTransitions) // Post-hook was NOT called
	})
}

func TestFSM_TransitionBool(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		currentState   string
		targetState    string
		allowed        bool
		expectedResult bool
		expectedState  string
	}{
		{
			name:           "Valid transition returns true",
			currentState:   "state1",
			targetState:    "state2",
			allowed:        true,
			expectedResult: true,
			expectedState:  "state2",
		},
		{
			name:           "Invalid transition returns false",
			currentState:   "state1",
			targetState:    "state3",
			allowed:        false,
			expectedResult: false,
			expectedState:  "state1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			transitions := map[string][]string{
				"state1": {},
				"state2": {},
				"state3": {},
			}
			if tc.allowed {
				transitions[tc.currentState] = []string{tc.targetState}
			}

			mockDB := newMockTransitionDB(transitions)
			fsm, err := New(tc.currentState, mockDB)
			require.NoError(t, err)

			result := fsm.TransitionBool(tc.targetState)
			assert.Equal(t, tc.expectedResult, result)
			assert.Equal(t, tc.expectedState, fsm.GetState())
		})
	}
}

func TestFSM_TransitionIfCurrentState(t *testing.T) {
	t.Parallel()

	t.Run("Transition succeeds when current state matches", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.TransitionIfCurrentState("state1", "state2")
		require.NoError(t, err)
		assert.Equal(t, "state2", fsm.GetState())
	})

	t.Run("Transition fails when current state does not match", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {"state3"},
			"state3": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.TransitionIfCurrentState("state2", "state3")
		require.ErrorIs(t, err, ErrCurrentStateIncorrect)
		assert.Equal(t, "state1", fsm.GetState()) // State should not change
	})

	t.Run("Transition fails when transition is invalid", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		// state1 is correct, but transition to state3 is not allowed
		err = fsm.TransitionIfCurrentState("state1", "state3")
		require.ErrorIs(t, err, ErrInvalidStateTransition)
		assert.Equal(t, "state1", fsm.GetState())
	})
}

func TestFSM_Concurrency(t *testing.T) {
	t.Parallel()

	t.Run("Concurrent reads are safe", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				_ = fsm.GetState()
			})
		}
		wg.Wait()
	})

	t.Run("Concurrent writes are safe", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {"state1"},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		var wg sync.WaitGroup
		for range 50 {
			wg.Go(func() {
				// Error intentionally ignored - testing concurrent safety not success
				//nolint:errcheck
				_ = fsm.Transition("state2")
			})
			wg.Go(func() {
				// Error intentionally ignored - testing concurrent safety not success
				//nolint:errcheck
				_ = fsm.Transition("state1")
			})
		}
		wg.Wait()

		// Final state should be either state1 or state2
		finalState := fsm.GetState()
		assert.Contains(t, []string{"state1", "state2"}, finalState)
	})

	t.Run("GetAllStates races with Transition", func(t *testing.T) {
		// Regression: GetAllStates used to take the FSM's RLock. The lock was
		// removed because the bundled transitions.Config is immutable after
		// construction. This test asserts that running GetAllStates
		// concurrently with Transition is race-detector clean and never
		// returns a malformed slice.
		fsm, err := NewSimple("state1", map[string][]string{
			"state1": {"state2"},
			"state2": {"state1"},
		})
		require.NoError(t, err)

		expected := []string{"state1", "state2"}

		var wg sync.WaitGroup
		for range 50 {
			wg.Go(func() {
				for range 20 {
					states := fsm.GetAllStates()
					assert.ElementsMatch(t, expected, states)
				}
			})
			wg.Go(func() {
				for range 20 {
					//nolint:errcheck // racing for safety, not correctness
					_ = fsm.Transition("state2")
					//nolint:errcheck
					_ = fsm.Transition("state1")
				}
			})
		}
		wg.Wait()
	})
}

func TestFSM_Options(t *testing.T) {
	t.Parallel()

	t.Run("WithLogger option sets custom logger", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {},
		})

		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		fsm, err := New("state1", mockDB, WithLogger(logger))
		require.NoError(t, err)
		require.NotNil(t, fsm)
		assert.NotNil(t, fsm.logger)
	})

	t.Run("WithLogHandler option creates logger from handler", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {},
		})

		handler := slog.NewTextHandler(os.Stdout, nil)
		fsm, err := New("state1", mockDB, WithLogHandler(handler))
		require.NoError(t, err)
		require.NotNil(t, fsm)
		assert.NotNil(t, fsm.logger)
	})

	t.Run("WithCallbackRegistry option sets callback executor", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)
		require.NotNil(t, fsm)

		// Verify callbacks are executed
		err = fsm.Transition("state2")
		require.NoError(t, err)

		postTransitions := mockCallbacks.getPostTransitions()
		require.Len(t, postTransitions, 1)
	})

	t.Run("Nil logger in WithLogger returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {},
		})

		fsm, err := New("state1", mockDB, WithLogger(nil))
		require.Error(t, err)
		assert.Nil(t, fsm)
	})

	t.Run("Nil handler in WithLogHandler returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {},
		})

		fsm, err := New("state1", mockDB, WithLogHandler(nil))
		require.Error(t, err)
		assert.Nil(t, fsm)
	})
}

// TestFSM_TransitionWithContext verifies context flows through to hooks
func TestFSM_TransitionWithContext(t *testing.T) {
	t.Parallel()

	type contextKey string
	const requestIDKey contextKey = "request_id"

	t.Run("Context with values flows to hooks", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		// Create context with a value
		ctx := context.WithValue(context.Background(), requestIDKey, "req-123")

		err = fsm.TransitionWithContext(ctx, "state2")
		require.NoError(t, err)

		// Verify hooks received the context
		receivedContexts := mockCallbacks.getReceivedContexts()
		require.Len(t, receivedContexts, 2) // pre + post hooks

		// Check pre-transition hook got the context
		preCtx := receivedContexts[0]
		assert.Equal(t, "req-123", preCtx.Value(requestIDKey))

		// Check post-transition hook got the context
		postCtx := receivedContexts[1]
		assert.Equal(t, "req-123", postCtx.Value(requestIDKey))
	})

	t.Run("Regular Transition uses Background context", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		err = fsm.Transition("state2")
		require.NoError(t, err)

		// Verify hooks received context (should be Background)
		receivedContexts := mockCallbacks.getReceivedContexts()
		require.Len(t, receivedContexts, 2)

		// Background context has no values
		preCtx := receivedContexts[0]
		assert.Nil(t, preCtx.Value(requestIDKey))
	})

	t.Run("TransitionIfCurrentStateWithContext flows context", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		ctx := context.WithValue(context.Background(), requestIDKey, "req-456")

		err = fsm.TransitionIfCurrentStateWithContext(ctx, "state1", "state2")
		require.NoError(t, err)

		// Verify context flowed through
		receivedContexts := mockCallbacks.getReceivedContexts()
		require.Len(t, receivedContexts, 2)
		assert.Equal(t, "req-456", receivedContexts[0].Value(requestIDKey))
		assert.Equal(t, "req-456", receivedContexts[1].Value(requestIDKey))
	})
}

func TestFSM_Subscribe(t *testing.T) {
	t.Parallel()

	t.Run("Error: nil context", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		c := make(chan string, 1)
		//nolint:staticcheck // Intentionally testing nil context error
		err = fsm.Subscribe(nil, c)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidConfiguration)
		assert.Contains(t, err.Error(), "context cannot be nil")
	})

	// Subscribe broadcasts from the Machine itself, so it works with no
	// callback registry, with a CallbackExecutor that cannot register hooks,
	// and with a registry that has no transition table for wildcard hooks.
	// Each of these used to be an error.
	t.Run("Success: no registry requirement", func(t *testing.T) {
		registryWithoutTransitions, err := hooks.NewRegistry()
		require.NoError(t, err)

		tests := []struct {
			name string
			opts []Option
		}{
			{name: "nil callbacks"},
			{name: "callbacks not HookRegistrar", opts: []Option{WithCallbackRegistry(newMockCallbackExecutor())}},
			{name: "registry without transitions", opts: []Option{WithCallbackRegistry(registryWithoutTransitions)}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockDB := newMockTransitionDB(map[string][]string{
					"state1": {"state2"},
					"state2": {},
				})
				fsm, err := New("state1", mockDB, tt.opts...)
				require.NoError(t, err)

				c := make(chan string, 2)
				require.NoError(t, fsm.Subscribe(t.Context(), c))
				assert.Equal(t, "state1", <-c)

				require.NoError(t, fsm.Transition("state2"))
				assert.Equal(t, "state2", <-c)
			})
		}

		assert.Empty(t, registryWithoutTransitions.GetHooks(),
			"Subscribe must not register anything on the callback registry")
	})

	t.Run("Success: SetState is broadcast", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		c := make(chan string, 2)
		require.NoError(t, fsm.Subscribe(t.Context(), c))
		assert.Equal(t, "state1", <-c)

		require.NoError(t, fsm.SetState("state2"))
		assert.Equal(t, "state2", <-c)
	})

	t.Run("Success: basic usage with buffered channel", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2", "state3"},
			"state2": {"state3"},
			"state3": {},
		})

		registry, err := hooks.NewRegistry(
			hooks.WithTransitions(transitions.MustNew(map[string][]string{
				"state1": {"state2", "state3"},
				"state2": {"state3"},
				"state3": {},
			})),
		)
		require.NoError(t, err)

		fsm, err := New("state1", mockDB, WithCallbackRegistry(registry))
		require.NoError(t, err)

		ctx := t.Context()

		c := make(chan string, 10)
		err = fsm.Subscribe(ctx, c)
		require.NoError(t, err)

		// Should receive initial state immediately
		initialState := <-c
		assert.Equal(t, "state1", initialState)

		// Transition and verify broadcast
		err = fsm.Transition("state2")
		require.NoError(t, err)

		newState := <-c
		assert.Equal(t, "state2", newState)

		// Another transition
		err = fsm.Transition("state3")
		require.NoError(t, err)

		finalState := <-c
		assert.Equal(t, "state3", finalState)
	})

	t.Run("Success: multiple channels receive broadcasts", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		registry, err := hooks.NewRegistry(
			hooks.WithTransitions(transitions.MustNew(map[string][]string{
				"state1": {"state2"},
				"state2": {},
			})),
		)
		require.NoError(t, err)

		fsm, err := New("state1", mockDB, WithCallbackRegistry(registry))
		require.NoError(t, err)

		ctx := t.Context()

		// Register first channel
		c1 := make(chan string, 10)
		err = fsm.Subscribe(ctx, c1)
		require.NoError(t, err)

		// Register second channel
		c2 := make(chan string, 10)
		err = fsm.Subscribe(ctx, c2)
		require.NoError(t, err)

		// Both should receive initial state
		assert.Equal(t, "state1", <-c1)
		assert.Equal(t, "state1", <-c2)

		// Transition
		err = fsm.Transition("state2")
		require.NoError(t, err)

		// Both should receive new state
		assert.Equal(t, "state2", <-c1)
		assert.Equal(t, "state2", <-c2)
	})

	t.Run("Success: custom broadcast timeout", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		registry, err := hooks.NewRegistry(
			hooks.WithTransitions(transitions.MustNew(map[string][]string{
				"state1": {"state2"},
				"state2": {},
			})),
		)
		require.NoError(t, err)

		// Test with custom timeout
		fsm, err := New("state1", mockDB,
			WithCallbackRegistry(registry),
			WithBroadcastTimeout(500*time.Millisecond),
		)
		require.NoError(t, err)

		ctx := t.Context()

		c := make(chan string, 10)
		err = fsm.Subscribe(ctx, c)
		require.NoError(t, err)

		// Should work normally with custom timeout
		assert.Equal(t, "state1", <-c)

		err = fsm.Transition("state2")
		require.NoError(t, err)

		assert.Equal(t, "state2", <-c)
	})

	t.Run("Success: no goroutine leak with synctest", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			mockDB := newMockTransitionDB(map[string][]string{
				"state1": {"state2"},
				"state2": {},
			})

			registry, err := hooks.NewRegistry(
				hooks.WithTransitions(transitions.MustNew(map[string][]string{
					"state1": {"state2"},
					"state2": {},
				})),
			)
			require.NoError(t, err)

			fsm, err := New("state1", mockDB, WithCallbackRegistry(registry))
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			c := make(chan string, 10)
			err = fsm.Subscribe(ctx, c)
			require.NoError(t, err)

			// Receive states using select to avoid goroutine leak
			select {
			case state := <-c:
				assert.Equal(t, "state1", state)
			case <-time.After(1 * time.Second):
				t.Fatal("timeout waiting for initial state")
			}

			err = fsm.Transition("state2")
			require.NoError(t, err)

			select {
			case state := <-c:
				assert.Equal(t, "state2", state)
			case <-time.After(1 * time.Second):
				t.Fatal("timeout waiting for state2")
			}

			// synctest will detect goroutine leaks if we don't handle cleanup correctly
		})
	})
}

// TestSubscribe_FromCustomOption verifies that a custom Option may subscribe
// and transition while New is still applying options. Option is an exported
// func(*Machine) error, so this is a supported extension point, and the
// broadcast path must not depend on New having finished.
func TestSubscribe_FromCustomOption(t *testing.T) {
	t.Parallel()

	ch := make(chan string, 2)
	subscribeAndBoot := func(m *Machine) error {
		if err := m.Subscribe(t.Context(), ch); err != nil {
			return err
		}
		return m.Transition(transitions.StatusBooting)
	}

	machine, err := New(transitions.StatusNew, transitions.Typical, subscribeAndBoot)
	require.NoError(t, err)
	assert.Equal(t, transitions.StatusNew, <-ch)
	assert.Equal(t, transitions.StatusBooting, <-ch)

	// The subscription made during construction keeps receiving afterwards.
	require.NoError(t, machine.Transition(transitions.StatusRunning))
	assert.Equal(t, transitions.StatusRunning, <-ch)
}

// TestSubscribe_SharedRegistryIsolatesMachines verifies that machines sharing
// one hooks.Registry deliver only their own transitions to their subscribers.
// When broadcasting was a wildcard post-transition hook registered on the
// registry, every machine's hook fired on every machine's transitions, so a
// subscriber to m2 received m1's states.
func TestSubscribe_SharedRegistryIsolatesMachines(t *testing.T) {
	t.Parallel()

	reg, err := hooks.NewRegistry(hooks.WithTransitions(transitions.Typical))
	require.NoError(t, err)

	m1, err := New(transitions.StatusNew, transitions.Typical, WithCallbackRegistry(reg))
	require.NoError(t, err)
	m2, err := New(transitions.StatusNew, transitions.Typical, WithCallbackRegistry(reg))
	require.NoError(t, err)

	ch1 := make(chan string, 10)
	ch2 := make(chan string, 10)
	mustSubscribe(t, m1, t.Context(), ch1)
	mustSubscribe(t, m2, t.Context(), ch2)
	assert.Equal(t, transitions.StatusNew, <-ch1)
	assert.Equal(t, transitions.StatusNew, <-ch2)

	// Broadcasts are delivered synchronously before Transition returns, so the
	// channels can be inspected immediately.
	require.NoError(t, m1.Transition(transitions.StatusBooting))
	assert.Equal(t, transitions.StatusBooting, <-ch1)
	assert.Empty(t, ch2, "m2's subscriber must not receive m1's transition")

	require.NoError(t, m2.Transition(transitions.StatusBooting))
	assert.Equal(t, transitions.StatusBooting, <-ch2)
	assert.Empty(t, ch1, "m1's subscriber must not receive m2's transition")
}

// TestSubscribe_InitialSendIsAtomic verifies that registering a subscriber
// and sending its initial state happen atomically with respect to transitions.
// Before Subscribe held the read lock across registration and the initial
// send, a concurrent transition could broadcast between the two steps, so the
// subscriber observed a duplicated or stale first value. With strictly
// alternating Running<->Reloading transitions, the initial value (current
// state) and the first broadcast that follows must always differ; an equal
// pair signals the race.
func TestSubscribe_InitialSendIsAtomic(t *testing.T) {
	reg, err := hooks.NewRegistry(hooks.WithTransitions(transitions.Typical))
	require.NoError(t, err)
	machine, err := New(transitions.StatusRunning, transitions.Typical,
		WithCallbackRegistry(reg),
	)
	require.NoError(t, err)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				machine.TransitionBool(transitions.StatusReloading)
				machine.TransitionBool(transitions.StatusRunning)
				// Yield rather than spin flat-out; keeps transitions frequent
				// enough to exercise the race without pegging a CPU under -race.
				runtime.Gosched()
			}
		}
	})
	t.Cleanup(func() {
		close(stop)
		wg.Wait()
	})

	for i := range 500 {
		ctx, cancel := context.WithCancel(context.Background())
		ch := make(chan string, 256)
		require.NoError(t, machine.Subscribe(ctx, ch))

		// Collect the initial value plus the first broadcast.
		var got []string
		require.Eventuallyf(t, func() bool {
			select {
			case s := <-ch:
				got = append(got, s)
			default:
			}
			return len(got) >= 2
		}, 2*time.Second, time.Millisecond,
			"iteration %d: timed out collecting states, got %v", i, got)
		cancel()

		require.NotEqualf(t, got[0], got[1],
			"iteration %d: initial value %q duplicated by the first broadcast %v; subscribe and initial send were not atomic",
			i, got[0], got)
	}
}

// TestSubscribe_AlreadyCancelledContextRejected verifies that subscribing with
// an already-cancelled context is rejected, deterministically, before anything
// is registered.
//
// This test previously claimed to cover the initial-state send aborting on
// cancellation, but once Subscribe began rejecting already-cancelled contexts up
// front it never reached that send -- it passed for the wrong reason, because
// the rejection error also wraps context.Canceled. The genuine mid-flight case
// is covered by TestSubscribe_InitialSendCancelledMidFlight; this one now
// asserts the behaviour it actually exercises.
func TestSubscribe_AlreadyCancelledContextRejected(t *testing.T) {
	t.Parallel()

	reg, err := hooks.NewRegistry(hooks.WithTransitions(transitions.Typical))
	require.NoError(t, err)
	machine, err := New(transitions.StatusNew, transitions.Typical, WithCallbackRegistry(reg))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := make(chan string, 1)
	err = machine.Subscribe(ctx, ch)
	require.ErrorIs(t, err, context.Canceled)

	// Rejected before registration: the channel is free to subscribe under a
	// live context, which a lingering entry would refuse as a duplicate.
	require.NoError(t, machine.Subscribe(t.Context(), ch))
}

// TestGetStateChan_DeprecatedAliasDelegatesToSubscribe verifies that the
// deprecated wrapper still works and behaves identically to Subscribe, so
// callers on the old name keep working.
func TestGetStateChan_DeprecatedAliasDelegatesToSubscribe(t *testing.T) {
	t.Parallel()

	reg, err := hooks.NewRegistry(hooks.WithTransitions(transitions.Typical))
	require.NoError(t, err)
	machine, err := New(transitions.StatusNew, transitions.Typical, WithCallbackRegistry(reg))
	require.NoError(t, err)

	ch := make(chan string, 10)
	require.NoError(t, machine.GetStateChan(t.Context(), ch))
	assert.Equal(t, transitions.StatusNew, <-ch)

	require.NoError(t, machine.Transition(transitions.StatusBooting))
	require.Eventually(t, func() bool {
		select {
		case state := <-ch:
			return assert.Equal(t, transitions.StatusBooting, state)
		default:
			return false
		}
	}, 100*time.Millisecond, 10*time.Millisecond, "the deprecated alias should deliver transitions")
}

func TestFSM_IsTransitionAllowed(t *testing.T) {
	t.Parallel()

	machine, err := New(transitions.StatusRunning, transitions.Typical)
	require.NoError(t, err)

	assert.True(t, machine.IsTransitionAllowed(transitions.StatusReloading), "Running->Reloading is allowed")
	assert.True(t, machine.IsTransitionAllowed(transitions.StatusError), "Running->Error is allowed")
	assert.False(t, machine.IsTransitionAllowed(transitions.StatusNew), "Running->New is not allowed")
	assert.False(t, machine.IsTransitionAllowed("nonexistent"), "unknown target is not allowed")
}

func TestFSM_AvailableTransitions(t *testing.T) {
	t.Parallel()

	t.Run("returns sorted allowed targets from current state", func(t *testing.T) {
		machine, err := New(transitions.StatusRunning, transitions.Typical)
		require.NoError(t, err)

		assert.Equal(t,
			[]string{transitions.StatusError, transitions.StatusReloading, transitions.StatusStopping},
			machine.AvailableTransitions(),
		)
	})

	t.Run("includes a self-loop target", func(t *testing.T) {
		machine, err := New(transitions.StatusUnknown, transitions.Typical)
		require.NoError(t, err)

		assert.Equal(t, []string{transitions.StatusUnknown}, machine.AvailableTransitions())
	})

	t.Run("returns empty slice for a terminal state", func(t *testing.T) {
		machine, err := NewSimple("a", map[string][]string{
			"a": {"b"},
			"b": {},
		})
		require.NoError(t, err)
		require.NoError(t, machine.Transition("b"))

		assert.Empty(t, machine.AvailableTransitions())
	})
}

func TestFSM_IsTerminal(t *testing.T) {
	t.Parallel()

	t.Run("true when current state has no outgoing transitions", func(t *testing.T) {
		machine, err := NewSimple("a", map[string][]string{
			"a": {"b"},
			"b": {},
		})
		require.NoError(t, err)

		assert.False(t, machine.IsTerminal(), "a has an outgoing transition")
		require.NoError(t, machine.Transition("b"))
		assert.True(t, machine.IsTerminal(), "b is terminal")
	})

	t.Run("false for a self-looping state", func(t *testing.T) {
		machine, err := New(transitions.StatusUnknown, transitions.Typical)
		require.NoError(t, err)

		assert.False(t, machine.IsTerminal(), "Unknown self-loops, so it is not terminal")
	})
}

// TestSubscribe_NilChannelRejected verifies that a nil channel is rejected
// before any state is touched. Previously the nil channel fell through to the
// initial-state send, which blocks forever on a nil channel while holding the
// FSM read lock; with a non-cancellable context that permanently wedged every
// Transition, SetState, and MarshalJSON call on the machine.
func TestSubscribe_NilChannelRejected(t *testing.T) {
	t.Parallel()

	t.Run("Returns ErrInvalidConfiguration and leaves the machine usable", func(t *testing.T) {
		reg, err := hooks.NewRegistry(hooks.WithTransitions(transitions.Typical))
		require.NoError(t, err)
		machine, err := New(transitions.StatusNew, transitions.Typical, WithCallbackRegistry(reg))
		require.NoError(t, err)

		err = machine.Subscribe(context.Background(), nil)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidConfiguration)
		require.ErrorContains(t, err, "state channel cannot be nil")

		// Nothing was registered on the callback registry.
		assert.Empty(t, reg.GetHooks())

		// The read lock was never taken: transitions still work.
		require.NoError(t, machine.Transition(transitions.StatusBooting))
		assert.Equal(t, transitions.StatusBooting, machine.GetState())

		// A later valid subscription still succeeds.
		ch := make(chan string, 1)
		mustSubscribe(t, machine, t.Context(), ch)
		assert.Equal(t, transitions.StatusBooting, <-ch)
	})

	t.Run("Spawns no goroutine and registers no subscriber", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			reg, err := hooks.NewRegistry(hooks.WithTransitions(transitions.Typical))
			require.NoError(t, err)
			machine, err := New(transitions.StatusNew, transitions.Typical, WithCallbackRegistry(reg))
			require.NoError(t, err)

			// context.Background() is deliberate: on the old code both this call
			// and the manager's cleanup goroutine block on it forever, which the
			// bubble reports as a deadlock.
			require.ErrorIs(t, machine.Subscribe(context.Background(), nil), ErrInvalidConfiguration)
			synctest.Wait()

			require.NoError(t, machine.Transition(transitions.StatusBooting))
		})
	})
}

// TestSubscribe_InitialSendCancelledMidFlight covers the initial-send
// cancellation path. Its sibling, TestSubscribe_AlreadyCancelledContextRejected,
// pre-cancels the context and so is now rejected by the broadcast manager before
// the send is ever reached; this exercises a cancellation that lands while the
// send is genuinely blocked on a full channel.
func TestSubscribe_InitialSendCancelledMidFlight(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		reg, err := hooks.NewRegistry(hooks.WithTransitions(transitions.Typical))
		require.NoError(t, err)
		machine, err := New(transitions.StatusNew, transitions.Typical, WithCallbackRegistry(reg))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		ch := make(chan string) // unbuffered, no reader: the initial send blocks

		var subErr error
		done := make(chan struct{})
		go func() {
			subErr = machine.Subscribe(ctx, ch)
			close(done)
		}()

		// Let the subscription reach the blocked send before cancelling.
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("Subscribe returned before the initial send could block")
		default:
		}

		cancel()
		synctest.Wait()

		<-done
		require.ErrorIs(t, subErr, context.Canceled)
	})
}
