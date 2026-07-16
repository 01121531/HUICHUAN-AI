package controller

import (
	"net/http"
	"sort"

	"github.com/01121531/HUICHUAN-AI/common"
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
	recordManageAudit(c, "dataset_capture.policy_update", map[string]interface{}{
		"enabled_before": previous.Enabled,
		"enabled_after":  policy.Enabled,
		"mode_before":    previous.ModelMode,
		"mode_after":     policy.ModelMode,
		"models_before":  len(previous.Models),
		"models_after":   len(policy.Models),
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
