package model

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"gorm.io/gorm"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id          int    `json:"id"`
	UserID      int    `json:"user_id" gorm:"index"`
	Username    string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
	ModelName   string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2"`
	UseGroup    string `json:"use_group" gorm:"index;size:64;default:''"`
	TokenID     int    `json:"token_id" gorm:"index;default:0"`
	ChannelID   int    `json:"channel_id" gorm:"index;default:0"`
	NodeName    string `json:"node_name" gorm:"index;size:64;default:''"`
	TokenUsed   int    `json:"token_used" gorm:"default:0"`
	Count       int    `json:"count" gorm:"default:0"`
	Quota       int    `json:"quota" gorm:"default:0"`
	Concurrency int    `json:"concurrency" gorm:"-"`
}

type concurrencyLogInterval struct {
	CreatedAt int64
	UseTime   int
}

type concurrencyEvent struct {
	timestamp int64
	delta     int
}

// calculatePeakConcurrencyByHour reconstructs request intervals from usage
// logs. Log timestamps are completion times, so each interval is
// [created_at-use_time, created_at).
func calculatePeakConcurrencyByHour(logs []concurrencyLogInterval, startTime, endTime int64) map[int64]int {
	result := make(map[int64]int)
	if endTime < startTime {
		return result
	}

	eventsByHour := make(map[int64][]concurrencyEvent)
	rangeEnd := endTime + 1
	for _, log := range logs {
		duration := int64(log.UseTime)
		if duration < 1 {
			duration = 1
		}
		intervalStart := log.CreatedAt - duration
		intervalEnd := log.CreatedAt
		if intervalStart < startTime {
			intervalStart = startTime
		}
		if intervalEnd > rangeEnd {
			intervalEnd = rangeEnd
		}
		if intervalEnd <= intervalStart {
			continue
		}

		firstHour := intervalStart - intervalStart%3600
		lastSecond := intervalEnd - 1
		lastHour := lastSecond - lastSecond%3600
		for hour := firstHour; hour <= lastHour; hour += 3600 {
			segmentStart := intervalStart
			if segmentStart < hour {
				segmentStart = hour
			}
			segmentEnd := intervalEnd
			if hourEnd := hour + 3600; segmentEnd > hourEnd {
				segmentEnd = hourEnd
			}
			eventsByHour[hour] = append(eventsByHour[hour],
				concurrencyEvent{timestamp: segmentStart, delta: 1},
				concurrencyEvent{timestamp: segmentEnd, delta: -1},
			)
		}
	}

	for hour, events := range eventsByHour {
		sort.Slice(events, func(i, j int) bool {
			if events[i].timestamp == events[j].timestamp {
				return events[i].delta < events[j].delta
			}
			return events[i].timestamp < events[j].timestamp
		})

		active, peak := 0, 0
		for _, event := range events {
			active += event.delta
			if active > peak {
				peak = active
			}
		}
		result[hour] = peak
	}
	return result
}

func GetPeakConcurrencyByHour(startTime, endTime int64, userID int, username string) (map[int64]int, error) {
	var logs []concurrencyLogInterval
	tx := LOG_DB.Model(&Log{}).
		Select("created_at, use_time").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? AND created_at <= ?", startTime, endTime)
	if userID > 0 {
		tx = tx.Where("user_id = ?", userID)
	} else if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if err := tx.Find(&logs).Error; err != nil {
		return nil, err
	}
	return calculatePeakConcurrencyByHour(logs, startTime, endTime), nil
}

func AttachConcurrencyToQuotaData(data []*QuotaData, concurrency map[int64]int) {
	assigned := make(map[int64]bool)
	for _, item := range data {
		if item == nil {
			continue
		}
		hour := item.CreatedAt - item.CreatedAt%3600
		if assigned[hour] {
			continue
		}
		item.Concurrency = concurrency[hour]
		assigned[hour] = true
	}
}

type QuotaDataLogParams struct {
	UserID    int
	Username  string
	ModelName string
	Quota     int
	CreatedAt int64
	TokenUsed int
	UseGroup  string
	TokenID   int
	ChannelID int
	NodeName  string
}

func UpdateQuotaData() {
	for {
		if common.DataExportEnabled {
			common.SysLog("正在更新数据看板数据...")
			SaveQuotaDataCache()
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}

func logQuotaDataCache(quotaData *QuotaData) {
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%d\x00%s\x00%d\x00%d\x00%s",
		quotaData.UserID,
		quotaData.Username,
		quotaData.ModelName,
		quotaData.CreatedAt,
		quotaData.UseGroup,
		quotaData.TokenID,
		quotaData.ChannelID,
		quotaData.NodeName,
	)
	count := quotaData.Count
	quota := quotaData.Quota
	tokenUsed := quotaData.TokenUsed
	cachedQuotaData, ok := CacheQuotaData[key]
	if ok {
		cachedQuotaData.Count += count
		cachedQuotaData.Quota += quota
		cachedQuotaData.TokenUsed += tokenUsed
		quotaData = cachedQuotaData
	}
	CacheQuotaData[key] = quotaData
}

func LogQuotaData(params QuotaDataLogParams) {
	// 只精确到小时
	createdAt := params.CreatedAt - (params.CreatedAt % 3600)
	quotaData := &QuotaData{
		UserID:    params.UserID,
		Username:  params.Username,
		ModelName: params.ModelName,
		CreatedAt: createdAt,
		UseGroup:  params.UseGroup,
		TokenID:   params.TokenID,
		ChannelID: params.ChannelID,
		NodeName:  params.NodeName,
		Count:     1,
		Quota:     params.Quota,
		TokenUsed: params.TokenUsed,
	}

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(quotaData)
}

func SaveQuotaDataCache() {
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	size := len(CacheQuotaData)
	// 如果缓存中有数据，就保存到数据库中
	// 1. 先查询数据库中是否有数据
	// 2. 如果有数据，就更新数据
	// 3. 如果没有数据，就插入数据
	for _, quotaData := range CacheQuotaData {
		quotaDataDB := &QuotaData{}
		DB.Table("quota_data").
			Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
				quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName).
			First(quotaDataDB)
		if quotaDataDB.Id > 0 {
			//quotaDataDB.Count += quotaData.Count
			//quotaDataDB.Quota += quotaData.Quota
			//DB.Table("quota_data").Save(quotaDataDB)
			increaseQuotaData(quotaData)
		} else {
			DB.Table("quota_data").Create(quotaData)
		}
	}
	CacheQuotaData = make(map[string]*QuotaData)
	common.SysLog(fmt.Sprintf("保存数据看板数据成功，共保存%d条数据", size))
}

func increaseQuotaData(quotaData *QuotaData) {
	err := DB.Table("quota_data").
		Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
			quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName).
		Updates(map[string]interface{}{
			"count":      gorm.Expr("count + ?", quotaData.Count),
			"quota":      gorm.Expr("quota + ?", quotaData.Quota),
			"token_used": gorm.Expr("token_used + ?", quotaData.TokenUsed),
		}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("increaseQuotaData error: %s", err))
	}
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	err = DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetAllQuotaDates(startTime int64, endTime int64, username string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsername(username, startTime, endTime)
	}
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	// only select model_name, sum(count) as count, sum(quota) as quota, model_name, created_at from quota_data group by model_name, created_at;
	//err = DB.Table("quota_data").Where("created_at >= ? and created_at <= ?", startTime, endTime).Find(&quotaDatas).Error
	err = DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime).Group("model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}
