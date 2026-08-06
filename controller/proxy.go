package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/gin-gonic/gin"
)

type proxyGroupRequest struct {
	Name                        string  `json:"name"`
	Enabled                     *bool   `json:"enabled"`
	MaxRequests                 int     `json:"max_requests"`
	MaxDurationSeconds          int     `json:"max_duration_seconds"`
	SwitchWaitSeconds           int     `json:"switch_wait_seconds"`
	MaxWaitingRequests          int     `json:"max_waiting_requests"`
	HealthCheckInterval         int     `json:"health_check_interval"`
	HealthFailureThreshold      int     `json:"health_failure_threshold"`
	ConsecutiveTimeoutThreshold int     `json:"consecutive_timeout_threshold"`
	WindowSize                  int     `json:"window_size"`
	WindowTimeoutRatio          float64 `json:"window_timeout_ratio"`
	BaseCooldownSeconds         int     `json:"base_cooldown_seconds"`
	MaxCooldownSeconds          int     `json:"max_cooldown_seconds"`
	RecoverySuccessCount        int     `json:"recovery_success_count"`
	AllowDirectFallback         *bool   `json:"allow_direct_fallback"`
}

type proxyRequest struct {
	GroupId        int     `json:"group_id"`
	Name           string  `json:"name"`
	Protocol       string  `json:"protocol"`
	Host           string  `json:"host"`
	Port           int     `json:"port"`
	Username       string  `json:"username"`
	Password       string  `json:"password"`
	Enabled        *bool   `json:"enabled"`
	Sort           int     `json:"sort"`
	ExpectedExitIp *string `json:"expected_exit_ip"`
}

type proxyBatchRequest struct {
	Proxies         string `json:"proxies"`
	DefaultProtocol string `json:"default_protocol"`
	NamePrefix      string `json:"name_prefix"`
	Enabled         *bool  `json:"enabled"`
}

type proxyBindingRequest struct {
	ProxyGroupId int   `json:"proxy_group_id"`
	Enabled      *bool `json:"enabled"`
}

type proxyHealthSettingsRequest struct {
	Enabled bool   `json:"enabled"`
	Time    string `json:"time"`
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func parsePositiveId(c *gin.Context, name string) (int, bool) {
	id, err := strconv.Atoi(c.Param(name))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的 ID")
		return 0, false
	}
	return id, true
}

func ListProxyGroups(c *gin.Context) {
	groups, err := model.ListProxyGroups()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": groups})
}

func ListProxyPoolOptions(c *gin.Context) {
	options, err := model.ListProxyPoolOptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": options})
}

func GetProxyHealthSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": model.GetProxyDailyHealthCheckSettings()})
}

func UpdateProxyHealthSettings(c *gin.Context) {
	var req proxyHealthSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	if err := model.UpdateProxyDailyHealthCheckSettings(req.Enabled, req.Time); err != nil {
		common.ApiErrorMsg(c, "保存每日检测设置失败: "+err.Error())
		return
	}
	settings := model.GetProxyDailyHealthCheckSettings()
	recordManageAudit(c, "proxy.health_settings.update", map[string]interface{}{
		"enabled": settings.Enabled, "time": settings.Time, "timezone": settings.Timezone,
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": settings})
}

func CreateProxyGroup(c *gin.Context) {
	var req proxyGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	group := &model.ProxyGroup{
		Name:                        strings.TrimSpace(req.Name),
		Enabled:                     boolValue(req.Enabled, true),
		Status:                      model.ProxyGroupStatusAvailable,
		MaxRequests:                 req.MaxRequests,
		MaxDurationSeconds:          req.MaxDurationSeconds,
		SwitchWaitSeconds:           req.SwitchWaitSeconds,
		MaxWaitingRequests:          req.MaxWaitingRequests,
		HealthCheckInterval:         req.HealthCheckInterval,
		HealthFailureThreshold:      req.HealthFailureThreshold,
		ConsecutiveTimeoutThreshold: req.ConsecutiveTimeoutThreshold,
		WindowSize:                  req.WindowSize,
		WindowTimeoutRatio:          req.WindowTimeoutRatio,
		BaseCooldownSeconds:         req.BaseCooldownSeconds,
		MaxCooldownSeconds:          req.MaxCooldownSeconds,
		RecoverySuccessCount:        req.RecoverySuccessCount,
		AllowDirectFallback:         boolValue(req.AllowDirectFallback, false),
	}
	if err := model.CreateProxyGroup(group); err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidateChannelProxyConfig(0)
	recordManageAudit(c, "proxy.group.create", map[string]interface{}{"id": group.Id, "name": group.Name})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": group})
}

func UpdateProxyGroup(c *gin.Context) {
	id, ok := parsePositiveId(c, "id")
	if !ok {
		return
	}
	var req proxyGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	group, err := model.GetProxyGroupById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	group.Name = strings.TrimSpace(req.Name)
	group.Enabled = boolValue(req.Enabled, group.Enabled)
	group.MaxRequests = req.MaxRequests
	group.MaxDurationSeconds = req.MaxDurationSeconds
	group.SwitchWaitSeconds = req.SwitchWaitSeconds
	group.MaxWaitingRequests = req.MaxWaitingRequests
	group.HealthCheckInterval = req.HealthCheckInterval
	group.HealthFailureThreshold = req.HealthFailureThreshold
	group.ConsecutiveTimeoutThreshold = req.ConsecutiveTimeoutThreshold
	group.WindowSize = req.WindowSize
	group.WindowTimeoutRatio = req.WindowTimeoutRatio
	group.BaseCooldownSeconds = req.BaseCooldownSeconds
	group.MaxCooldownSeconds = req.MaxCooldownSeconds
	group.RecoverySuccessCount = req.RecoverySuccessCount
	group.AllowDirectFallback = boolValue(req.AllowDirectFallback, group.AllowDirectFallback)
	if err := model.UpdateProxyGroup(group); err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidateChannelProxyConfig(0)
	recordManageAudit(c, "proxy.group.update", map[string]interface{}{"id": group.Id, "name": group.Name})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": group})
}

func DeleteProxyGroup(c *gin.Context) {
	id, ok := parsePositiveId(c, "id")
	if !ok {
		return
	}
	unboundChannelCount, err := model.DeleteProxyGroup(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidateChannelProxyConfig(0)
	recordManageAudit(c, "proxy.group.delete", map[string]interface{}{
		"id": id, "unbound_channel_count": unboundChannelCount,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    gin.H{"unbound_channel_count": unboundChannelCount},
	})
}

func ListGroupProxies(c *gin.Context) {
	groupId, ok := parsePositiveId(c, "id")
	if !ok {
		return
	}
	proxies, err := model.ListProxiesByGroup(groupId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": proxies})
}

func CreateProxy(c *gin.Context) {
	var req proxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	proxy := &model.Proxy{
		GroupId: req.GroupId, Name: strings.TrimSpace(req.Name), Protocol: req.Protocol,
		Host: req.Host, Port: req.Port, Username: req.Username, Password: req.Password,
		Enabled: boolValue(req.Enabled, true), Status: model.ProxyStatusAvailable, Sort: req.Sort,
	}
	if req.ExpectedExitIp != nil {
		proxy.ExpectedExitIp = strings.TrimSpace(*req.ExpectedExitIp)
	}
	if err := model.CreateProxy(proxy); err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidateChannelProxyConfig(0)
	service.ResetProxyClientCache()
	recordManageAudit(c, "proxy.create", map[string]interface{}{"id": proxy.Id, "group_id": proxy.GroupId, "name": proxy.Name})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": proxy})
}

func BatchCreateProxies(c *gin.Context) {
	groupId, ok := parsePositiveId(c, "id")
	if !ok {
		return
	}
	var req proxyBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	defaultProtocol := strings.TrimSpace(req.DefaultProtocol)
	if defaultProtocol == "" {
		defaultProtocol = "socks5"
	}
	parsed, err := model.ParseProxyBatchList(req.Proxies, defaultProtocol)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	created, skipped, err := model.CreateProxiesBatch(
		groupId, parsed, boolValue(req.Enabled, true), req.NamePrefix,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidateChannelProxyConfig(0)
	service.ResetProxyClientCache()
	recordManageAudit(c, "proxy.batch_create", map[string]interface{}{
		"group_id": groupId, "created_count": len(created), "skipped_count": skipped,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"created_count": len(created),
			"skipped_count": skipped,
			"proxies":       created,
		},
	})
}

func UpdateProxy(c *gin.Context) {
	id, ok := parsePositiveId(c, "id")
	if !ok {
		return
	}
	var req proxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	proxy, err := model.GetProxyById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if req.GroupId > 0 {
		proxy.GroupId = req.GroupId
	}
	proxy.Name = strings.TrimSpace(req.Name)
	proxy.Protocol = req.Protocol
	proxy.Host = req.Host
	proxy.Port = req.Port
	proxy.Username = req.Username
	if req.ExpectedExitIp != nil {
		proxy.ExpectedExitIp = strings.TrimSpace(*req.ExpectedExitIp)
	}
	if req.Password != "" {
		proxy.Password = req.Password
	}
	proxy.Enabled = boolValue(req.Enabled, proxy.Enabled)
	proxy.Sort = req.Sort
	if err := model.UpdateProxy(proxy); err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidateChannelProxyConfig(0)
	service.ResetProxyClientCache()
	recordManageAudit(c, "proxy.update", map[string]interface{}{"id": proxy.Id, "group_id": proxy.GroupId, "name": proxy.Name})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": proxy})
}

func DeleteProxy(c *gin.Context) {
	id, ok := parsePositiveId(c, "id")
	if !ok {
		return
	}
	if err := model.DeleteProxy(id); err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidateChannelProxyConfig(0)
	service.ResetProxyClientCache()
	recordManageAudit(c, "proxy.delete", map[string]interface{}{"id": id})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func ListProxyBindings(c *gin.Context) {
	bindings, err := model.ListChannelProxyBindings()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": bindings})
}

func UpsertProxyBinding(c *gin.Context) {
	channelId, ok := parsePositiveId(c, "channel_id")
	if !ok {
		return
	}
	var req proxyBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	binding := &model.ChannelProxyBinding{
		ChannelId: channelId, ProxyGroupId: req.ProxyGroupId, Enabled: boolValue(req.Enabled, true),
	}
	if err := model.UpsertChannelProxyBinding(binding); err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidateChannelProxyConfig(channelId)
	recordManageAudit(c, "proxy.binding.upsert", map[string]interface{}{"channel_id": channelId, "proxy_group_id": binding.ProxyGroupId})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": binding})
}

func DeleteProxyBinding(c *gin.Context) {
	channelId, ok := parsePositiveId(c, "channel_id")
	if !ok {
		return
	}
	if err := model.DeleteChannelProxyBinding(channelId); err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidateChannelProxyConfig(channelId)
	recordManageAudit(c, "proxy.binding.delete", map[string]interface{}{"channel_id": channelId})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func ListProxyLogAnalyses(c *gin.Context) {
	proxyId, _ := strconv.Atoi(c.Query("proxy_id"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	analyses, err := model.ListProxyLogAnalyses(proxyId, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": analyses})
}

func GetProxyTrend(c *gin.Context) {
	groupId, err := strconv.Atoi(c.Query("group_id"))
	if err != nil || groupId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "group_id must be a positive integer"})
		return
	}
	proxyId := 0
	if rawProxyId := c.Query("proxy_id"); rawProxyId != "" {
		proxyId, err = strconv.Atoi(rawProxyId)
		if err != nil || proxyId <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "proxy_id must be a positive integer"})
			return
		}
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "limit must be a positive integer"})
		return
	}
	trend, err := service.GetProxyTrend(groupId, proxyId, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": trend})
}

func ListProxyStateEvents(c *gin.Context) {
	proxyId, _ := strconv.Atoi(c.Query("proxy_id"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	events, err := model.ListProxyStateEvents(proxyId, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": events})
}

func ListProxyUpstreamAttempts(c *gin.Context) {
	proxyId, _ := strconv.Atoi(c.Query("proxy_id"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	attempts, err := model.ListProxyUpstreamAttempts(proxyId, c.Query("request_id"), limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": attempts})
}

func CheckProxyNow(c *gin.Context) {
	id, ok := parsePositiveId(c, "id")
	if !ok {
		return
	}
	result, err := service.CheckManagedProxyNow(c.Request.Context(), id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "proxy.check", map[string]interface{}{"id": id, "success": result.Success, "failure_reason": result.FailureReason})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

func CheckAllProxiesNow(c *gin.Context) {
	enqueueProxyFullHealthCheck(c, 0)
}

func CheckProxyGroupNow(c *gin.Context) {
	groupId, ok := parsePositiveId(c, "id")
	if !ok {
		return
	}
	enqueueProxyFullHealthCheck(c, groupId)
}

func enqueueProxyFullHealthCheck(c *gin.Context, groupId int) {
	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeProxyManualCheck, map[string]int{"group_id": groupId})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "proxy.check_all", map[string]interface{}{"group_id": groupId, "task_id": task.TaskID, "created": created})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{
		"task_id": task.TaskID, "status": task.Status, "created": created,
	}})
}

func PauseProxy(c *gin.Context) {
	setProxyPaused(c, true)
}

func ResumeProxy(c *gin.Context) {
	setProxyPaused(c, false)
}

func ResetProxyObservation(c *gin.Context) {
	id, ok := parsePositiveId(c, "id")
	if !ok {
		return
	}
	proxy, err := model.ResetProxyObservationCounters(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "proxy.observation.reset", map[string]interface{}{"id": id})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": proxy})
}

func setProxyPaused(c *gin.Context, paused bool) {
	id, ok := parsePositiveId(c, "id")
	if !ok {
		return
	}
	if err := service.SetManagedProxyPaused(c.Request.Context(), id, paused); err != nil {
		common.ApiError(c, err)
		return
	}
	action := "proxy.resume"
	if paused {
		action = "proxy.pause"
	}
	recordManageAudit(c, action, map[string]interface{}{"id": id})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func SwitchProxyGroupNow(c *gin.Context) {
	id, ok := parsePositiveId(c, "id")
	if !ok {
		return
	}
	nextProxyId, err := service.SwitchManagedProxyGroupNow(c.Request.Context(), id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "proxy.group.switch", map[string]interface{}{"id": id, "next_proxy_id": nextProxyId})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"current_proxy_id": nextProxyId}})
}
