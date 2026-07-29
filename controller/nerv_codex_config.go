package controller

import (
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/gin-gonic/gin"
)

type nervCodexConfigRequest struct {
	Home string `json:"home"`
}

func GetNERVCodexConfigStatus(c *gin.Context) {
	basePath, _, _ := findNERVAssetBasePath()
	home := strings.TrimSpace(c.Query("home"))
	common.ApiSuccess(c, service.NERVCodexConfigStatusFor(home, basePath))
}

func ApplyNERVCodexConfig(c *gin.Context) {
	var request nervCodexConfigRequest
	_ = c.ShouldBindJSON(&request)
	basePath, exists, _ := findNERVAssetBasePath()
	if !exists {
		common.ApiErrorMsg(c, "NERV 内置资产目录未找到")
		return
	}
	result, err := service.ApplyNERVCodexConfig(strings.TrimSpace(request.Home), basePath)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func RemoveNERVCodexConfig(c *gin.Context) {
	var request nervCodexConfigRequest
	_ = c.ShouldBindJSON(&request)
	basePath, _, _ := findNERVAssetBasePath()
	result, err := service.RemoveNERVCodexConfig(strings.TrimSpace(request.Home), basePath)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
