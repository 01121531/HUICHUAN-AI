package service

import (
	"github.com/01121531/HUICHUAN-AI/setting/operation_setting"
	"github.com/01121531/HUICHUAN-AI/setting/system_setting"
)

func GetCallbackAddress() string {
	if operation_setting.CustomCallbackAddress == "" {
		return system_setting.ServerAddress
	}
	return operation_setting.CustomCallbackAddress
}
