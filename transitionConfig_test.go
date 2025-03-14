package fsm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTransitionWithIndex(t *testing.T) {
	t.Parallel()

	transitions := TransitionsConfig{
		"StateA": {"StateB", "StateC"},
		"StateB": {"StateC", "StateD"},
	}

	expected := transitionIndex{
		"StateA": {"StateB": {}, "StateC": {}},
		"StateB": {"StateC": {}, "StateD": {}},
	}

	result := makeIndex(transitions)
	assert.Equal(t, expected, result)
}
