package common

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNewOutboundJSONBodyStripsGatewayOnlyFieldsAtFinalBoundary(t *testing.T) {
	body, size, closer, err := NewOutboundJSONBody([]byte(`{
		"model":"gpt-5",
		"group":"internal-routing-group",
		"conversation_id":"gateway-conversation",
		"metadata":{"group":"nested-value-must-remain","conversation_id":"nested-conversation-must-remain"}
	}`))
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	out, err := io.ReadAll(body)
	require.NoError(t, err)
	require.EqualValues(t, len(out), size)
	require.False(t, gjson.GetBytes(out, "group").Exists())
	require.False(t, gjson.GetBytes(out, "conversation_id").Exists())
	require.Equal(t, "nested-value-must-remain", gjson.GetBytes(out, "metadata.group").String())
	require.Equal(t, "nested-conversation-must-remain", gjson.GetBytes(out, "metadata.conversation_id").String())
}
