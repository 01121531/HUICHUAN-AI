package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupProxyControllerTestDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Channel{}, &model.Log{}, &model.ProxyGroup{}, &model.Proxy{}, &model.ChannelProxyBinding{},
		&model.ProxyLogAnalysis{}, &model.ProxyLogAnalysisCursor{}, &model.ProxyStateEvent{},
	))
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

func proxyJSONContext(t *testing.T, method string, body any, params ...gin.Param) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/", bytes.NewReader(data))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params
	return c, recorder
}

func TestProxyManagementHandlersCreateAndBindWithoutLeakingPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupProxyControllerTestDB(t)

	groupContext, groupRecorder := proxyJSONContext(t, http.MethodPost, map[string]any{
		"name": "测试代理组", "enabled": true, "max_requests": 25,
	})
	CreateProxyGroup(groupContext)
	require.Equal(t, http.StatusOK, groupRecorder.Code)
	var groupResponse struct {
		Success bool             `json:"success"`
		Data    model.ProxyGroup `json:"data"`
	}
	require.NoError(t, json.Unmarshal(groupRecorder.Body.Bytes(), &groupResponse))
	require.True(t, groupResponse.Success)
	require.NotZero(t, groupResponse.Data.Id)

	proxyContext, proxyRecorder := proxyJSONContext(t, http.MethodPost, map[string]any{
		"group_id": groupResponse.Data.Id,
		"name":     "出口一",
		"protocol": "socks5",
		"host":     "127.0.0.1",
		"port":     1080,
		"username": "proxy-user",
		"password": "never-return-this-secret",
		"enabled":  true,
	})
	CreateProxy(proxyContext)
	require.Equal(t, http.StatusOK, proxyRecorder.Code)
	require.NotContains(t, proxyRecorder.Body.String(), "never-return-this-secret")

	var storedProxy model.Proxy
	require.NoError(t, model.DB.Where("group_id = ?", groupResponse.Data.Id).First(&storedProxy).Error)
	require.Equal(t, "never-return-this-secret", storedProxy.Password)

	channel := &model.Channel{Id: 99201, Name: "binding-test", Key: "test-key"}
	require.NoError(t, model.DB.Create(channel).Error)
	bindingContext, bindingRecorder := proxyJSONContext(t, http.MethodPut, map[string]any{
		"proxy_group_id": groupResponse.Data.Id,
		"enabled":        true,
	}, gin.Param{Key: "channel_id", Value: "99201"})
	UpsertProxyBinding(bindingContext)
	require.Equal(t, http.StatusOK, bindingRecorder.Code)

	config, err := model.GetChannelProxyRuntimeConfig(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Equal(t, groupResponse.Data.Id, config.Group.Id)

	require.NoError(t, model.DB.Create(&model.ProxyLogAnalysis{
		AnalysisKey: "controller-proxy-analysis", RequestId: "request-1",
		LogType: model.LogTypeConsume, LogCreatedAt: 100, ProxyId: storedProxy.Id,
		ProxyGroupId: groupResponse.Data.Id, Counted: true, IsTimeout: true, Reason: "duration",
	}).Error)
	require.NoError(t, model.DB.Create(&model.ProxyStateEvent{
		ProxyId: storedProxy.Id, ProxyGroupId: groupResponse.Data.Id,
		EventType: "auto_paused", FromStatus: model.ProxyStatusWatching, ToStatus: model.ProxyStatusCooling,
	}).Error)
	analysisContext, analysisRecorder := proxyJSONContext(t, http.MethodGet, nil)
	analysisContext.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/?proxy_id=%d", storedProxy.Id), nil)
	ListProxyLogAnalyses(analysisContext)
	require.Equal(t, http.StatusOK, analysisRecorder.Code)
	require.Contains(t, analysisRecorder.Body.String(), "duration")
	require.NotContains(t, analysisRecorder.Body.String(), "never-return-this-secret")

	eventContext, eventRecorder := proxyJSONContext(t, http.MethodGet, nil)
	eventContext.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/?proxy_id=%d", storedProxy.Id), nil)
	ListProxyStateEvents(eventContext)
	require.Equal(t, http.StatusOK, eventRecorder.Code)
	require.Contains(t, eventRecorder.Body.String(), "auto_paused")
}
