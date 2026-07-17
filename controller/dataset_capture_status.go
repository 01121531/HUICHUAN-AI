package controller

import (
	"net/http"

	"github.com/01121531/HUICHUAN-AI/middleware"
	"github.com/gin-gonic/gin"
)

func GetDatasetCaptureRuntimeStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": middleware.GetDatasetCaptureRuntimeStatus()})
}

func SendDatasetCaptureTestAlert(c *gin.Context) {
	if !middleware.SendDatasetCaptureTestAlert() {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "dataset capture alerts are disabled or recipients are empty"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "message": "dataset capture test alert queued"})
}
