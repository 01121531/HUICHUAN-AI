package service

import (
	"strings"

	"github.com/01121531/HUICHUAN-AI/constant"
)

func CoverTaskActionToModelName(platform constant.TaskPlatform, action string) string {
	return strings.ToLower(string(platform)) + "_" + strings.ToLower(action)
}
