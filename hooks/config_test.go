package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHookTypeString(t *testing.T) {
	assert.Equal(t, "pre", HookTypePre.String(), "HookTypePre should return 'pre'")
	assert.Equal(t, "post", HookTypePost.String(), "HookTypePost should return 'post'")

	// unknown value (not one of the defined constants)
	var unk HookType = 999
	assert.Equal(t, "unknown", unk.String(), "Unknown HookType should return 'unknown'")
}
