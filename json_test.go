package fsm

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/transitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSM_JSONPersistence(t *testing.T) {
	t.Parallel()

	testHandler := slog.NewTextHandler(
		os.Stdout,
		&slog.HandlerOptions{Level: slog.LevelError},
	)

	transConfig := transitions.MustNew(map[string][]string{
		"running":   {"stopped"},
		"stopped":   {},
		"reloading": {"running"},
	})

	fsm, err := New("running", transConfig, WithLogHandler(testHandler))
	require.NoError(t, err)

	jsonData, err := json.Marshal(fsm)
	require.NoError(t, err)

	expectedJSON := `{"state":"running","transitions":{"running":["stopped"],"stopped":[],"reloading":["running"]}}`
	assert.JSONEq(t, expectedJSON, string(jsonData))
}

func TestFSM_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	// Create an FSM with transitions
	transConfig := transitions.MustNew(map[string][]string{
		"new":     {"booting"},
		"booting": {"running", "error"},
		"running": {"stopped", "error"},
		"stopped": {},
		"error":   {},
	})

	originalFSM, err := New("booting", transConfig)
	require.NoError(t, err)

	// Marshal to JSON
	jsonData, err := json.Marshal(originalFSM)
	require.NoError(t, err)

	// Restore FSM from JSON
	restoredFSM, err := NewFromJSON(jsonData)
	require.NoError(t, err)

	// Verify state was restored
	assert.Equal(t, "booting", restoredFSM.GetState())

	// Verify transitions work
	err = restoredFSM.Transition("running")
	require.NoError(t, err)
	assert.Equal(t, "running", restoredFSM.GetState())

	// Verify invalid transition is rejected
	err = restoredFSM.Transition("booting")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidStateTransition)
}

func TestFSM_JSONWithHooks(t *testing.T) {
	t.Parallel()

	// Create FSM with hooks
	transConfig := transitions.MustNew(map[string][]string{
		"offline": {"online"},
		"online":  {"offline"},
	})

	// Need to import hooks package
	registry, err := hooks.NewRegistry(
		hooks.WithTransitions(transConfig),
	)
	require.NoError(t, err)

	err = registry.RegisterPreTransitionHook(hooks.PreTransitionHookConfig{
		Name: "validate-connection",
		From: []string{"offline"},
		To:   []string{"online"},
		Guard: func(ctx context.Context, from, to string) error {
			return nil
		},
	})
	require.NoError(t, err)

	fsm, err := New("offline", transConfig, WithCallbackRegistry(registry))
	require.NoError(t, err)

	// Marshal to JSON
	jsonData, err := json.Marshal(fsm)
	require.NoError(t, err)

	// Verify hooks are in JSON
	var pState map[string]interface{}
	err = json.Unmarshal(jsonData, &pState)
	require.NoError(t, err)
	assert.Contains(t, pState, "hooks")
	hooksList := pState["hooks"].([]interface{})
	assert.Len(t, hooksList, 1)

	// NewFromJSON should warn and drop hooks
	restoredFSM, err := NewFromJSON(jsonData)
	require.NoError(t, err)

	// Verify state and transitions restored
	assert.Equal(t, "offline", restoredFSM.GetState())

	// Hooks should NOT be restored (callbacks should be nil)
	err = restoredFSM.Transition("online")
	require.NoError(t, err)
}

func TestFSM_NewFromJSON_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		jsonData    string
		expectError error
	}{
		{
			name:        "empty state",
			jsonData:    `{"state":"","transitions":{"a":["b"],"b":[]}}`,
			expectError: ErrEmptyState,
		},
		{
			name:        "empty transitions",
			jsonData:    `{"state":"running","transitions":{}}`,
			expectError: ErrEmptyTransitions,
		},
		{
			name:        "invalid transitions",
			jsonData:    `{"state":"running","transitions":{"running":["undefined"]}}`,
			expectError: ErrInvalidJSONTransitions,
		},
		{
			name:        "state not in transitions",
			jsonData:    `{"state":"invalid","transitions":{"running":["stopped"],"stopped":[]}}`,
			expectError: ErrInvalidConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewFromJSON([]byte(tt.jsonData))
			require.Error(t, err)
			require.ErrorIs(t, err, tt.expectError)
		})
	}
}

func TestFSM_JSONConcurrency(t *testing.T) {
	t.Parallel()

	transConfig := transitions.MustNew(map[string][]string{
		"state1": {"state2"},
		"state2": {"state1"},
	})

	t.Run("Concurrent MarshalJSON calls are safe", func(t *testing.T) {
		fsm, err := New("state1", transConfig)
		require.NoError(t, err)

		var wg sync.WaitGroup
		for range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := json.Marshal(fsm)
				assert.NoError(t, err)
			}()
		}
		wg.Wait()
	})

	t.Run("MarshalJSON concurrent with transitions is safe", func(t *testing.T) {
		fsm, err := New("state1", transConfig)
		require.NoError(t, err)

		var wg sync.WaitGroup

		// Start concurrent transitions (some may fail due to race conditions)
		for i := range 5 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				if idx%2 == 0 {
					_ = fsm.Transition("state2") //nolint:errcheck
				} else {
					_ = fsm.Transition("state1") //nolint:errcheck
				}
			}(i)
		}

		// Start concurrent marshal operations
		for range 5 {
			wg.Go(func() {
				_, err := json.Marshal(fsm)
				assert.NoError(t, err)
			})
		}

		wg.Wait()
	})

	t.Run("NewFromJSON followed by concurrent operations is safe", func(t *testing.T) {
		originalFSM, err := New("state1", transConfig)
		require.NoError(t, err)

		jsonData, err := json.Marshal(originalFSM)
		require.NoError(t, err)

		restoredFSM, err := NewFromJSON(jsonData)
		require.NoError(t, err)

		var wg sync.WaitGroup

		// Concurrent reads
		for range 5 {
			wg.Go(func() {
				_ = restoredFSM.GetState()
			})
		}

		// Concurrent transitions (some may fail if FSM already in state2)
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = restoredFSM.Transition("state2") //nolint:errcheck
			}()
		}

		wg.Wait()
	})

	t.Run("GetAllStates concurrent with NewFromJSON is safe", func(t *testing.T) {
		originalFSM, err := New("state1", transConfig)
		require.NoError(t, err)

		jsonData, err := json.Marshal(originalFSM)
		require.NoError(t, err)

		var wg sync.WaitGroup

		// Call GetAllStates on original FSM concurrently
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = originalFSM.GetAllStates()
			}()
		}

		// Create new FSMs from JSON concurrently
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				fsm, err := NewFromJSON(jsonData)
				assert.NoError(t, err)
				if fsm != nil {
					_ = fsm.GetAllStates()
				}
			}()
		}

		wg.Wait()
	})
}
