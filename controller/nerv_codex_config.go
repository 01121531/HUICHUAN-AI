package controller

import (
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/service"
	"github.com/gin-gonic/gin"
)

type nervCodexConfigRequest struct {
	Home            string `json:"home"`
	Backend         string `json:"backend"`
	WSLDistro       string `json:"wsl_distro"`
	DockerContainer string `json:"docker_container"`
	SSHHost         string `json:"ssh_host"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
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

func ApplyNERVMCPConfig(c *gin.Context) {
	var request nervCodexConfigRequest
	_ = c.ShouldBindJSON(&request)
	basePath, exists, _ := findNERVAssetBasePath()
	if !exists {
		common.ApiErrorMsg(c, "NERV 内置资产目录未找到")
		return
	}
	result, err := service.ApplyNERVMCPConfig(strings.TrimSpace(request.Home), basePath, service.NERVMCPConfigOptions{
		Backend:         strings.TrimSpace(request.Backend),
		WSLDistro:       strings.TrimSpace(request.WSLDistro),
		DockerContainer: strings.TrimSpace(request.DockerContainer),
		SSHHost:         strings.TrimSpace(request.SSHHost),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func RemoveNERVMCPConfig(c *gin.Context) {
	var request nervCodexConfigRequest
	_ = c.ShouldBindJSON(&request)
	basePath, _, _ := findNERVAssetBasePath()
	result, err := service.RemoveNERVMCPConfig(strings.TrimSpace(request.Home), basePath)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func RunNERVCodexVerify(c *gin.Context) {
	var request nervCodexConfigRequest
	_ = c.ShouldBindJSON(&request)
	basePath, exists, _ := findNERVAssetBasePath()
	if !exists {
		common.ApiErrorMsg(c, "NERV 内置资产目录未找到")
		return
	}
	result, err := service.RunNERVCodexVerify(strings.TrimSpace(request.Home), basePath, request.TimeoutSeconds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
