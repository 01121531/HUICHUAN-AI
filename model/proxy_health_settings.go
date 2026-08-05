package model

import (
	"errors"
	"strings"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
)

const (
	ProxyDailyHealthCheckEnabledOption = "ProxyDailyHealthCheckEnabled"
	ProxyDailyHealthCheckTimeOption    = "ProxyDailyHealthCheckTime"
	DefaultProxyDailyHealthCheckTime   = "08:00"
)

var proxyHealthCheckLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type ProxyDailyHealthCheckSettings struct {
	Enabled  bool   `json:"enabled"`
	Time     string `json:"time"`
	Timezone string `json:"timezone"`
}

func GetProxyDailyHealthCheckSettings() ProxyDailyHealthCheckSettings {
	settings := ProxyDailyHealthCheckSettings{
		Enabled:  true,
		Time:     DefaultProxyDailyHealthCheckTime,
		Timezone: "Asia/Shanghai",
	}
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	if value, ok := common.OptionMap[ProxyDailyHealthCheckEnabledOption]; ok {
		settings.Enabled = value == "true"
	}
	if value := strings.TrimSpace(common.OptionMap[ProxyDailyHealthCheckTimeOption]); validProxyDailyHealthCheckTime(value) {
		settings.Time = value
	}
	return settings
}

func UpdateProxyDailyHealthCheckSettings(enabled bool, checkTime string) error {
	checkTime = strings.TrimSpace(checkTime)
	if !validProxyDailyHealthCheckTime(checkTime) {
		return errors.New("daily proxy check time must use HH:mm format")
	}
	enabledValue := "false"
	if enabled {
		enabledValue = "true"
	}
	return UpdateOptionsBulk(map[string]string{
		ProxyDailyHealthCheckEnabledOption: enabledValue,
		ProxyDailyHealthCheckTimeOption:    checkTime,
	})
}

func IsProxyDailyHealthCheckDue(now time.Time, latestUnix int64) bool {
	settings := GetProxyDailyHealthCheckSettings()
	if !settings.Enabled {
		return false
	}
	parsed, err := time.ParseInLocation("15:04", settings.Time, proxyHealthCheckLocation)
	if err != nil {
		return false
	}
	localNow := now.In(proxyHealthCheckLocation)
	scheduled := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), parsed.Hour(), parsed.Minute(), 0, 0, proxyHealthCheckLocation)
	if localNow.Before(scheduled) {
		return false
	}
	if latestUnix <= 0 {
		return true
	}
	return time.Unix(latestUnix, 0).In(proxyHealthCheckLocation).Before(scheduled)
}

func validProxyDailyHealthCheckTime(value string) bool {
	if len(value) != 5 {
		return false
	}
	_, err := time.Parse("15:04", value)
	return err == nil
}
