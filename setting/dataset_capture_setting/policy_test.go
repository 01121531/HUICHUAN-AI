package dataset_capture_setting

import (
	"encoding/json"
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
	want := DefaultPolicy()
	want.Enabled = true
	want.ModelMode = ModelModeSelected
	want.Models = []string{"claude-sonnet-4", "gpt-5.2"}
	assert.Equal(t, want, policy)
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
	want := DefaultPolicy()
	want.Enabled = true
	want.ModelMode = ModelModeSelected
	want.Models = []string{"gpt-5.2"}
	assert.Equal(t, want, policy)
}

func TestGetSerializesEmptyModelsAsArray(t *testing.T) {
	original := Get()
	t.Cleanup(func() { require.NoError(t, Apply(original)) })
	require.NoError(t, Apply(Policy{ModelMode: ModelModeAll, Models: nil}))

	policy := Get()
	assert.NotNil(t, policy.Models)
	encoded, err := json.Marshal(policy)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, float64(CurrentVersion), decoded["version"])
	assert.Equal(t, []any{}, decoded["models"])
	assert.Equal(t, []any{}, decoded["user_ids"])
	assert.Equal(t, []any{}, decoded["token_ids"])
}

func TestAllowsRequestUsesUserTokenAndStreamScope(t *testing.T) {
	original := Get()
	t.Cleanup(func() { require.NoError(t, Apply(original)) })
	policy := DefaultPolicy()
	policy.Enabled = true
	policy.UserMode = ScopeModeSelected
	policy.UserIDs = []int{7}
	policy.TokenMode = ScopeModeSelected
	policy.TokenIDs = []int{9}
	policy.CaptureStream = false
	policy.PreserveMultimodalBase64 = false
	require.NoError(t, Apply(policy))
	assert.True(t, AllowsRequest("any-model", 7, 9, false))
	allowed, preserveBase64 := RequestCaptureOptions("any-model", 7, 9, false)
	assert.True(t, allowed)
	assert.False(t, preserveBase64)
	assert.False(t, AllowsRequest("any-model", 8, 9, false))
	assert.False(t, AllowsRequest("any-model", 7, 10, false))
	assert.False(t, AllowsRequest("any-model", 7, 9, true))
}

func TestNormalizeRejectsInvalidPerformanceAndAlertRecipient(t *testing.T) {
	policy := DefaultPolicy()
	policy.Performance.QueueSize = 1
	_, err := Normalize(policy)
	require.EqualError(t, err, "dataset capture queue_size must be between 16 and 65536")

	policy = DefaultPolicy()
	policy.Alerts.Enabled = true
	policy.Alerts.Recipients = []string{"not-an-email"}
	_, err = Normalize(policy)
	require.ErrorContains(t, err, "invalid dataset capture alert recipient")
}

func TestNormalizeAcceptsSpoolWriteAlert(t *testing.T) {
	policy := DefaultPolicy()
	policy.Alerts.Types = []string{"spool_write_failed"}
	normalized, err := Normalize(policy)
	require.NoError(t, err)
	assert.Equal(t, []string{"spool_write_failed"}, normalized.Alerts.Types)
}
