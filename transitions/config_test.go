package transitions

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("creates transitions with valid config", func(t *testing.T) {
		config := map[string][]string{
			"StateA": {"StateB", "StateC"},
			"StateB": {"StateC", "StateD"},
			"StateC": {},
			"StateD": {},
		}

		trans, err := New(config)
		require.NoError(t, err)
		require.NotNil(t, trans)
	})

	t.Run("returns error for empty config", func(t *testing.T) {
		trans, err := New(map[string][]string{})
		require.ErrorIs(t, err, ErrEmptyConfig)
		assert.Nil(t, trans)
	})

	t.Run("returns error for nil config", func(t *testing.T) {
		trans, err := New(nil)
		require.ErrorIs(t, err, ErrEmptyConfig)
		assert.Nil(t, trans)
	})

	t.Run("returns error for undefined destination states", func(t *testing.T) {
		config := map[string][]string{
			"StateA": {"StateB", "StateC"},
		}
		trans, err := New(config)
		require.ErrorIs(t, err, ErrUndefinedDestinations)
		assert.Nil(t, trans)
		// Still verify the states are listed
		assert.Contains(t, err.Error(), "StateB")
		assert.Contains(t, err.Error(), "StateC")
	})

	t.Run("accepts explicit terminal states with empty transitions", func(t *testing.T) {
		config := map[string][]string{
			"StateA": {"StateB", "StateC"},
			"StateB": {"StateC"},
			"StateC": {}, // Explicit terminal state
		}
		trans, err := New(config)
		require.NoError(t, err)
		require.NotNil(t, trans)
		assert.True(t, trans.HasState("StateA"))
		assert.True(t, trans.HasState("StateB"))
		assert.True(t, trans.HasState("StateC"))
	})

	t.Run("returns error for partially defined states", func(t *testing.T) {
		config := map[string][]string{
			"StateA": {"StateB"},
			"StateB": {"StateC"}, // StateC not defined
		}
		trans, err := New(config)
		require.ErrorIs(t, err, ErrUndefinedDestinations)
		assert.Nil(t, trans)
		assert.Contains(t, err.Error(), "StateC")
	})

	t.Run("returns error for empty string source state", func(t *testing.T) {
		config := map[string][]string{
			"": {"StateA"},
		}
		trans, err := New(config)
		require.ErrorIs(t, err, ErrEmptyStateName)
		assert.Nil(t, trans)
	})

	t.Run("returns error for empty string destination state", func(t *testing.T) {
		config := map[string][]string{
			"StateA": {""},
		}
		trans, err := New(config)
		require.ErrorIs(t, err, ErrEmptyStateName)
		assert.Nil(t, trans)
	})

	t.Run("returns error for whitespace-only source state", func(t *testing.T) {
		config := map[string][]string{
			"   ": {"StateA"},
		}
		trans, err := New(config)
		require.ErrorIs(t, err, ErrWhitespaceStateName)
		assert.Nil(t, trans)
	})

	t.Run("returns error for whitespace-only destination state", func(t *testing.T) {
		config := map[string][]string{
			"StateA": {"  \t  "},
		}
		trans, err := New(config)
		require.ErrorIs(t, err, ErrWhitespaceStateName)
		assert.Nil(t, trans)
	})

	t.Run("returns error for source state with leading whitespace", func(t *testing.T) {
		config := map[string][]string{
			" StateA": {"StateB"},
			"StateB":  {},
		}
		trans, err := New(config)
		require.ErrorIs(t, err, ErrInvalidWhitespace)
		assert.Nil(t, trans)
	})

	t.Run("returns error for source state with trailing whitespace", func(t *testing.T) {
		config := map[string][]string{
			"StateA ": {"StateB"},
			"StateB":  {},
		}
		trans, err := New(config)
		require.ErrorIs(t, err, ErrInvalidWhitespace)
		assert.Nil(t, trans)
	})

	t.Run("returns error for destination state with whitespace", func(t *testing.T) {
		config := map[string][]string{
			"StateA": {" StateB"},
		}
		trans, err := New(config)
		require.ErrorIs(t, err, ErrInvalidWhitespace)
		assert.Nil(t, trans)
	})

	t.Run("error message lists undefined states in sorted order", func(t *testing.T) {
		config := map[string][]string{
			"StateA": {"StateZ", "StateB", "StateM"},
		}
		trans, err := New(config)
		require.Error(t, err)
		assert.Nil(t, trans)
		// Error should list states in sorted order: [StateB StateM StateZ]
		assert.Contains(t, err.Error(), "[StateB StateM StateZ]")
	})

	t.Run("returns all errors at once for multiple validation failures", func(t *testing.T) {
		config := map[string][]string{
			"":        {"StateA"},            // Empty source state
			"StateA ": {""},                  // Trailing whitespace source, empty destination
			"StateB":  {" StateC", "StateD"}, // Whitespace destination, undefined destination
		}
		trans, err := New(config)
		require.Error(t, err)
		assert.Nil(t, trans)

		// Should contain all error types
		require.ErrorIs(t, err, ErrEmptyStateName)
		require.ErrorIs(t, err, ErrInvalidWhitespace)
		require.ErrorIs(t, err, ErrUndefinedDestinations)
	})
}

func TestMustNew(t *testing.T) {
	t.Parallel()

	t.Run("creates transitions with valid config", func(t *testing.T) {
		config := map[string][]string{
			"StateA": {"StateB", "StateC"},
			"StateB": {},
			"StateC": {},
		}

		assert.NotPanics(t, func() {
			trans := MustNew(config)
			assert.NotNil(t, trans)
		})
	})

	t.Run("panics with empty config", func(t *testing.T) {
		assert.Panics(t, func() {
			MustNew(map[string][]string{})
		})
	})

	t.Run("panics with undefined destination states", func(t *testing.T) {
		assert.Panics(t, func() {
			MustNew(map[string][]string{
				"StateA": {"StateB"},
			})
		})
	})

	t.Run("panics with empty state name", func(t *testing.T) {
		assert.Panics(t, func() {
			MustNew(map[string][]string{
				"": {"StateA"},
			})
		})
	})

	t.Run("panics with whitespace state name", func(t *testing.T) {
		assert.Panics(t, func() {
			MustNew(map[string][]string{
				"StateA ": {"StateB"},
			})
		})
	})
}

func TestIsTransitionAllowed(t *testing.T) {
	t.Parallel()

	trans := MustNew(map[string][]string{
		"StateA": {"StateB", "StateC"},
		"StateB": {"StateC", "StateD"},
		"StateC": {},
		"StateD": {},
	})

	t.Run("returns true for allowed transition", func(t *testing.T) {
		assert.True(t, trans.IsTransitionAllowed("StateA", "StateB"))
		assert.True(t, trans.IsTransitionAllowed("StateA", "StateC"))
		assert.True(t, trans.IsTransitionAllowed("StateB", "StateC"))
		assert.True(t, trans.IsTransitionAllowed("StateB", "StateD"))
	})

	t.Run("returns false for disallowed transition", func(t *testing.T) {
		assert.False(t, trans.IsTransitionAllowed("StateA", "StateD"))
		assert.False(t, trans.IsTransitionAllowed("StateB", "StateA"))
	})

	t.Run("returns false for unknown source state", func(t *testing.T) {
		assert.False(t, trans.IsTransitionAllowed("StateZ", "StateA"))
	})
}

func TestGetAllowedTransitions(t *testing.T) {
	t.Parallel()

	trans := MustNew(map[string][]string{
		"StateA": {"StateB", "StateC"},
		"StateB": {"StateC", "StateD"},
		"StateC": {},
		"StateD": {},
	})

	t.Run("returns allowed transitions for valid state", func(t *testing.T) {
		targets, ok := trans.GetAllowedTransitions("StateA")
		assert.True(t, ok)
		assert.ElementsMatch(t, []string{"StateB", "StateC"}, targets)
	})

	t.Run("returns false for unknown state", func(t *testing.T) {
		targets, ok := trans.GetAllowedTransitions("StateZ")
		assert.False(t, ok)
		assert.Nil(t, targets)
	})
}

func TestHasState(t *testing.T) {
	t.Parallel()

	trans := MustNew(map[string][]string{
		"StateA": {"StateB", "StateC"},
		"StateB": {"StateC", "StateD"},
		"StateC": {},
		"StateD": {},
	})

	t.Run("returns true for source states", func(t *testing.T) {
		assert.True(t, trans.HasState("StateA"))
		assert.True(t, trans.HasState("StateB"))
		assert.True(t, trans.HasState("StateC"))
		assert.True(t, trans.HasState("StateD"))
	})

	t.Run("returns false for unknown state", func(t *testing.T) {
		assert.False(t, trans.HasState("StateZ"))
	})
}

func TestGetAllStates(t *testing.T) {
	t.Parallel()

	trans := MustNew(map[string][]string{
		"StateA": {"StateB", "StateC"},
		"StateB": {"StateC", "StateD"},
		"StateC": {},
		"StateD": {},
	})

	states := trans.GetAllStates()
	assert.ElementsMatch(t, []string{"StateA", "StateB", "StateC", "StateD"}, states)
}

func TestMarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("marshals transitions to JSON", func(t *testing.T) {
		trans := MustNew(map[string][]string{
			"StateA": {"StateB", "StateC"},
			"StateB": {"StateC"},
			"StateC": {},
		})

		jsonData, err := json.Marshal(trans)
		require.NoError(t, err)
		require.NotEmpty(t, jsonData)

		// Verify JSON structure
		var result map[string][]string
		err = json.Unmarshal(jsonData, &result)
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"StateB", "StateC"}, result["StateA"])
		assert.ElementsMatch(t, []string{"StateC"}, result["StateB"])
	})

	t.Run("marshals TypicalTransitions correctly", func(t *testing.T) {
		jsonData, err := json.Marshal(Typical)
		require.NoError(t, err)
		require.NotEmpty(t, jsonData)

		var result map[string][]string
		err = json.Unmarshal(jsonData, &result)
		require.NoError(t, err)
		require.NotEmpty(t, result)
	})
}

func TestUnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("unmarshals valid JSON into Transitions", func(t *testing.T) {
		jsonData := []byte(`{
			"StateA": ["StateB", "StateC"],
			"StateB": ["StateC", "StateD"],
			"StateC": [],
			"StateD": []
		}`)

		var trans Config
		err := json.Unmarshal(jsonData, &trans)
		require.NoError(t, err)

		// Verify config was loaded
		assert.True(t, trans.HasState("StateA"))
		assert.True(t, trans.HasState("StateB"))

		// Verify index was rebuilt correctly
		assert.True(t, trans.IsTransitionAllowed("StateA", "StateB"))
		assert.True(t, trans.IsTransitionAllowed("StateA", "StateC"))
		assert.True(t, trans.IsTransitionAllowed("StateB", "StateC"))
		assert.True(t, trans.IsTransitionAllowed("StateB", "StateD"))
		assert.False(t, trans.IsTransitionAllowed("StateA", "StateD"))

		// Verify GetAllowedTransitions works
		targets, ok := trans.GetAllowedTransitions("StateA")
		require.True(t, ok)
		assert.ElementsMatch(t, []string{"StateB", "StateC"}, targets)
	})

	t.Run("returns error for syntactically malformed JSON", func(t *testing.T) {
		invalidJSON := []byte(`{"StateA": [}`)

		var trans Config
		err := json.Unmarshal(invalidJSON, &trans)
		require.Error(t, err)
		// Syntactically invalid JSON fails before UnmarshalJSON is called
		assert.Contains(t, err.Error(), "invalid character")
	})

	t.Run("returns error for wrong value type in JSON", func(t *testing.T) {
		invalidJSON := []byte(`{"StateA": "not-an-array"}`)

		var trans Config
		err := json.Unmarshal(invalidJSON, &trans)
		require.Error(t, err)
		// Type mismatch is caught by json.Unmarshal before UnmarshalJSON
		assert.Contains(t, err.Error(), "cannot unmarshal")
	})

	t.Run("returns error for completely invalid JSON syntax", func(t *testing.T) {
		invalidJSON := []byte(`{this is not json}`)

		var trans Config
		err := json.Unmarshal(invalidJSON, &trans)
		require.Error(t, err)
		// Syntactically invalid JSON fails before UnmarshalJSON is called
		assert.Contains(t, err.Error(), "invalid character")
	})

	t.Run("returns error for wrong JSON structure type", func(t *testing.T) {
		invalidJSON := []byte(`["StateA", "StateB"]`)

		var trans Config
		err := json.Unmarshal(invalidJSON, &trans)
		require.Error(t, err)
		// Array instead of object is caught by json.Unmarshal
		assert.Contains(t, err.Error(), "cannot unmarshal")
	})

	t.Run("returns error for empty config", func(t *testing.T) {
		emptyJSON := []byte(`{}`)

		var trans Config
		err := json.Unmarshal(emptyJSON, &trans)
		require.ErrorIs(t, err, ErrEmptyConfig)
	})

	t.Run("returns error for undefined destination states", func(t *testing.T) {
		invalidJSON := []byte(`{
			"StateA": ["StateB", "StateC"]
		}`)

		var trans Config
		err := json.Unmarshal(invalidJSON, &trans)
		require.ErrorIs(t, err, ErrUndefinedDestinations)
	})

	t.Run("accepts explicit terminal states in JSON", func(t *testing.T) {
		validJSON := []byte(`{
			"StateA": ["StateB"],
			"StateB": []
		}`)

		var trans Config
		err := json.Unmarshal(validJSON, &trans)
		require.NoError(t, err)
		assert.True(t, trans.HasState("StateA"))
		assert.True(t, trans.HasState("StateB"))
		assert.True(t, trans.IsTransitionAllowed("StateA", "StateB"))
	})

	t.Run("returns error for empty state name in JSON", func(t *testing.T) {
		invalidJSON := []byte(`{
			"": ["StateA"]
		}`)

		var trans Config
		err := json.Unmarshal(invalidJSON, &trans)
		require.ErrorIs(t, err, ErrEmptyStateName)
	})

	t.Run("returns error for whitespace state name in JSON", func(t *testing.T) {
		invalidJSON := []byte(`{
			"StateA ": ["StateB"]
		}`)

		var trans Config
		err := json.Unmarshal(invalidJSON, &trans)
		require.ErrorIs(t, err, ErrInvalidWhitespace)
	})

	t.Run("returns error for destination with whitespace in JSON", func(t *testing.T) {
		invalidJSON := []byte(`{
			"StateA": [" StateB "]
		}`)

		var trans Config
		err := json.Unmarshal(invalidJSON, &trans)
		require.ErrorIs(t, err, ErrInvalidWhitespace)
	})
}

func TestAsMap(t *testing.T) {
	t.Parallel()

	t.Run("returns a copy of the config", func(t *testing.T) {
		original := MustNew(map[string][]string{
			"StateA": {"StateB", "StateC"},
			"StateB": {"StateD"},
			"StateC": {},
			"StateD": {},
		})

		result := original.AsMap()

		// Verify contents match
		assert.ElementsMatch(t, []string{"StateB", "StateC"}, result["StateA"])
		assert.ElementsMatch(t, []string{"StateD"}, result["StateB"])
		assert.ElementsMatch(t, []string{}, result["StateC"])
		assert.ElementsMatch(t, []string{}, result["StateD"])
	})

	t.Run("returned map is independent from original", func(t *testing.T) {
		original := MustNew(map[string][]string{
			"StateA": {"StateB"},
			"StateB": {},
		})

		result := original.AsMap()

		// Modify the returned map
		result["StateA"] = []string{"StateZ"}
		result["StateX"] = []string{"StateY"}

		// Original should be unchanged
		assert.True(t, original.IsTransitionAllowed("StateA", "StateB"))
		assert.False(t, original.IsTransitionAllowed("StateA", "StateZ"))
		assert.False(t, original.HasState("StateX"))
	})

	t.Run("returned slices are independent from original", func(t *testing.T) {
		original := MustNew(map[string][]string{
			"StateA": {"StateB", "StateC"},
			"StateB": {},
			"StateC": {},
		})

		result := original.AsMap()

		// Modify a slice in the returned map
		result["StateA"][0] = "StateZ"
		result["StateA"] = append(result["StateA"], "StateD")

		// Original should be unchanged
		assert.True(t, original.IsTransitionAllowed("StateA", "StateB"))
		assert.True(t, original.IsTransitionAllowed("StateA", "StateC"))
		assert.False(t, original.IsTransitionAllowed("StateA", "StateZ"))
		assert.False(t, original.IsTransitionAllowed("StateA", "StateD"))
	})

	t.Run("handles empty transitions correctly", func(t *testing.T) {
		original := MustNew(map[string][]string{
			"StateA": {},
		})

		result := original.AsMap()

		assert.Empty(t, result["StateA"])
	})
}

func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("round-trip preserves transitions", func(t *testing.T) {
		original := MustNew(map[string][]string{
			"StateA": {"StateB", "StateC"},
			"StateB": {"StateC", "StateD"},
			"StateC": {},
			"StateD": {},
		})

		// Marshal to JSON
		jsonData, err := json.Marshal(original)
		require.NoError(t, err)

		// Unmarshal from JSON
		var restored Config
		err = json.Unmarshal(jsonData, &restored)
		require.NoError(t, err)

		// Verify both work identically
		assert.Equal(t, original.HasState("StateA"), restored.HasState("StateA"))
		assert.Equal(t, original.HasState("StateB"), restored.HasState("StateB"))
		assert.Equal(t, original.HasState("StateC"), restored.HasState("StateC"))

		assert.Equal(t,
			original.IsTransitionAllowed("StateA", "StateB"),
			restored.IsTransitionAllowed("StateA", "StateB"))
		assert.Equal(t,
			original.IsTransitionAllowed("StateA", "StateC"),
			restored.IsTransitionAllowed("StateA", "StateC"))
		assert.Equal(t,
			original.IsTransitionAllowed("StateB", "StateD"),
			restored.IsTransitionAllowed("StateB", "StateD"))
		assert.Equal(t,
			original.IsTransitionAllowed("StateA", "StateD"),
			restored.IsTransitionAllowed("StateA", "StateD"))

		// Verify GetAllStates returns same states
		assert.ElementsMatch(t, original.GetAllStates(), restored.GetAllStates())
	})

	t.Run("round-trip with TypicalTransitions", func(t *testing.T) {
		// Marshal TypicalTransitions
		jsonData, err := json.Marshal(Typical)
		require.NoError(t, err)

		// Unmarshal into new instance
		var restored Config
		err = json.Unmarshal(jsonData, &restored)
		require.NoError(t, err)

		// Verify key transitions work
		assert.True(t, restored.IsTransitionAllowed(StatusNew, StatusBooting))
		assert.True(t, restored.IsTransitionAllowed(StatusBooting, StatusRunning))
		assert.False(t, restored.IsTransitionAllowed(StatusNew, StatusRunning))

		// Verify all states are present
		assert.ElementsMatch(t, Typical.GetAllStates(), restored.GetAllStates())
	})
}
