package dataset_capture_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizePolicy(t *testing.T) {
	policy, err := Normalize(Policy{
		Enabled:   true,
		ModelMode: ModelModeSelected,
		Models:    []string{" gpt-5.2 ", "claude-sonnet-4", "gpt-5.2", ""},
	})
	require.NoError(t, err)
	assert.Equal(t, Policy{
		Enabled:   true,
		ModelMode: ModelModeSelected,
		Models:    []string{"claude-sonnet-4", "gpt-5.2"},
	}, policy)
}

func TestSelectedPolicyRequiresModel(t *testing.T) {
	_, err := Normalize(Policy{ModelMode: ModelModeSelected})
	require.EqualError(t, err, "selected dataset capture model mode requires at least one model")
}

func TestAllowsModelUsesExactSiteModel(t *testing.T) {
	original := Get()
	t.Cleanup(func() { require.NoError(t, Apply(original)) })
	require.NoError(t, Apply(Policy{
		Enabled:   true,
		ModelMode: ModelModeSelected,
		Models:    []string{"gpt-5.2"},
	}))
	assert.True(t, AllowsModel("gpt-5.2"))
	assert.False(t, AllowsModel("GPT-5.2"))
	assert.False(t, AllowsModel("gpt-5.2-2026-06-01"))

	copyOfPolicy := Get()
	copyOfPolicy.Models[0] = "mutated"
	assert.True(t, AllowsModel("gpt-5.2"), "callers must not mutate the runtime snapshot")
}

func TestParsePolicy(t *testing.T) {
	policy, err := Parse("true", "selected", `["gpt-5.2"]`)
	require.NoError(t, err)
	assert.Equal(t, Policy{Enabled: true, ModelMode: ModelModeSelected, Models: []string{"gpt-5.2"}}, policy)
}
