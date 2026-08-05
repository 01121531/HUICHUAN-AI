package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/constant"
	"github.com/01121531/HUICHUAN-AI/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPlaygroundKeepsSelectedGroupUntilRoutingCompletes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/pg/chat/completions", strings.NewReader(`{
		"model":"gpt-5",
		"group":"deepseek",
		"metadata":{"group":"nested-value-must-remain"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	request, _, err := getModelRequest(c)
	require.NoError(t, err)
	require.Equal(t, "gpt-5", request.Model)
	require.Equal(t, "deepseek", request.Group)
	require.Equal(t, "deepseek", common.GetContextKeyString(c, constant.ContextKeyTokenGroup))

	playgroundRequest := &dto.PlayGroundRequest{}
	require.NoError(t, common.UnmarshalBodyReusable(c, playgroundRequest))
	require.Equal(t, "deepseek", playgroundRequest.Group)

	require.NoError(t, stripRoutingGroupAfterSelection(c))

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

func TestStripRoutingGroupAfterSelectionKeepsJSONBodyWithoutGroupReadable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	original := `{"model":"gemini-2.5-pro","contents":[]}`
	c.Request = httptest.NewRequest("POST", "/v1beta/models/gemini-2.5-pro:generateContent", strings.NewReader(original))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	require.NoError(t, stripRoutingGroupAfterSelection(c))
	out, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	require.JSONEq(t, original, string(out))
}
