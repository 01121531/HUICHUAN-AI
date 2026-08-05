package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGetModelFromJSONBodyConsumesRoutingGroupBeforePassThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-5",
		"group":"internal-routing-group",
		"metadata":{"group":"nested-value-must-remain"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	request, err := getModelFromJSONBody(c)
	require.NoError(t, err)
	require.Equal(t, "gpt-5", request.Model)
	require.Equal(t, "internal-routing-group", request.Group)

	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	_, err = storage.Seek(0, io.SeekStart)
	require.NoError(t, err)
	out, err := io.ReadAll(storage)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(out, "group").Exists())
	require.Equal(t, "nested-value-must-remain", gjson.GetBytes(out, "metadata.group").String())
	require.EqualValues(t, len(out), c.Request.ContentLength)
}
