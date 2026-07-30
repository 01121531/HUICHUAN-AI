package controller

import (
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/gin-gonic/gin"
)

type nervProxyProcessRequest struct {
	Home          string `json:"home"`
	RestoreConfig *bool  `json:"restore_config"`
}

func GetNERVProxyProcessStatus(c *gin.Context) {
	basePath, _, _ := findNERVAssetBasePath()
	common.ApiSuccess(c, service.NERVProxyProcessStatusFor(basePath))
}

func StartNERVProxyProcess(c *gin.Context) {
	var request nervProxyProcessRequest
	_ = c.ShouldBindJSON(&request)
	basePath, exists, _ := findNERVAssetBasePath()
	if !exists {
		common.ApiErrorMsg(c, "NERV 内置资产目录未找到")
		return
	}
	result, err := service.StartNERVProxyProcess(basePath, strings.TrimSpace(request.Home))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func StopNERVProxyProcess(c *gin.Context) {
	var request nervProxyProcessRequest
	_ = c.ShouldBindJSON(&request)
	restoreConfig := true
	if request.RestoreConfig != nil {
		restoreConfig = *request.RestoreConfig
	}
	basePath, _, _ := findNERVAssetBasePath()
	result, err := service.StopNERVProxyProcess(basePath, restoreConfig)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
