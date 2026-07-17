package controller

import (
	"net/http"
	"sort"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/middleware"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/setting/dataset_capture_setting"
	"github.com/gin-gonic/gin"
)

type datasetCaptureModelOption struct {
	ID        string `json:"id"`
	Available bool   `json:"available"`
}

func GetDatasetCapturePolicy(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": dataset_capture_setting.Get()})
}

func UpdateDatasetCapturePolicy(c *gin.Context) {
	var request dataset_capture_setting.Policy
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid dataset capture policy"})
		return
	}
	policy, err := dataset_capture_setting.Normalize(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	previous := dataset_capture_setting.Get()
	if err := model.UpdateDatasetCapturePolicy(policy); err != nil {
		common.ApiError(c, err)
		return
	}
	middleware.ReloadDatasetCapture()
	recordManageAudit(c, "dataset_capture.policy_update", map[string]interface{}{
		"enabled_before":              previous.Enabled,
		"enabled_after":               policy.Enabled,
		"mode_before":                 previous.ModelMode,
		"mode_after":                  policy.ModelMode,
		"models_before":               len(previous.Models),
		"models_after":                len(policy.Models),
		"user_mode_before":            previous.UserMode,
		"user_mode_after":             policy.UserMode,
		"users_before":                len(previous.UserIDs),
		"users_after":                 len(policy.UserIDs),
		"token_mode_before":           previous.TokenMode,
		"token_mode_after":            policy.TokenMode,
		"tokens_before":               len(previous.TokenIDs),
		"tokens_after":                len(policy.TokenIDs),
		"alerts_before":               previous.Alerts.Enabled,
		"alerts_after":                policy.Alerts.Enabled,
		"access_alerts_before":        previous.Alerts.Access.Enabled,
		"access_alerts_after":         policy.Alerts.Access.Enabled,
		"access_actions_before":       len(previous.Alerts.Access.Actions),
		"access_actions_after":        len(policy.Alerts.Access.Actions),
		"access_operator_mode_before": previous.Alerts.Access.OperatorMode,
		"access_operator_mode_after":  policy.Alerts.Access.OperatorMode,
		"access_operators_before":     len(previous.Alerts.Access.OperatorUserIDs),
		"access_operators_after":      len(policy.Alerts.Access.OperatorUserIDs),
		"access_owner_mode_before":    previous.Alerts.Access.OwnerMode,
		"access_owner_mode_after":     policy.Alerts.Access.OwnerMode,
		"access_owners_before":        len(previous.Alerts.Access.OwnerUserIDs),
		"access_owners_after":         len(policy.Alerts.Access.OwnerUserIDs),
		"performance_changed":         previous.Performance != policy.Performance,
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": policy})
}

func ListDatasetCapturePolicyModels(c *gin.Context) {
	availableModels := model.GetEnabledModels()
	sort.Strings(availableModels)
	options := make([]datasetCaptureModelOption, 0, len(availableModels))
	seen := make(map[string]struct{}, len(availableModels))
	for _, modelID := range availableModels {
		if modelID == "" {
			continue
		}
		seen[modelID] = struct{}{}
		options = append(options, datasetCaptureModelOption{ID: modelID, Available: true})
	}
	for _, modelID := range dataset_capture_setting.Get().Models {
		if _, exists := seen[modelID]; exists {
			continue
		}
		options = append(options, datasetCaptureModelOption{ID: modelID, Available: false})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"models": options}})
}

func ListDatasetCapturePolicySubjects(c *gin.Context) {
	policy := dataset_capture_setting.Get()
	selectedUsers := append([]int{}, policy.UserIDs...)
	selectedUsers = append(selectedUsers, policy.Alerts.Access.OperatorUserIDs...)
	selectedUsers = append(selectedUsers, policy.Alerts.Access.OwnerUserIDs...)
	users, tokens, err := model.ListDatasetCapturePolicySubjects(selectedUsers, policy.TokenIDs, 500)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	operators := make([]model.DatasetCapturePolicyUser, 0)
	for _, user := range users {
		if user.Role == common.RoleAdminUser || user.Role == common.RoleRootUser {
			operators = append(operators, user)
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"users": users, "operators": operators, "tokens": tokens,
	}})
}
