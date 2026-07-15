package model

import (
	"strconv"
	"strings"

	"gorm.io/gorm"
)

type DatasetCaptureFilter struct {
	Node       string
	StartTime  int64
	EndTime    int64
	Models     []string
	TokenIDs   []int
	Groups     []string
	ChannelIDs []int
	Username   string
	CaptureIDs []string
	NoMatches  bool
}

type DatasetCaptureSelection struct {
	UserIDs     []int
	CaptureIDs  []string
	AllFiltered bool
}

type DatasetCaptureUserGroup struct {
	UserID      int    `json:"user_id"`
	Username    string `json:"username"`
	RecordCount int64  `json:"record_count"`
	TokenCount  int64  `json:"token_count"`
	LastCapture int64  `json:"last_capture"`
	TotalSize   int64  `json:"total_size"`
}

type DatasetCaptureTotals struct {
	UserCount   int64 `json:"user_count"`
	RecordCount int64 `json:"record_count"`
	TotalSize   int64 `json:"total_size"`
}

type DatasetCaptureRecordSummary struct {
	CaptureID      string `json:"capture_id"`
	UserID         int    `json:"user_id"`
	Username       string `json:"username"`
	TokenID        int    `json:"token_id"`
	TokenName      string `json:"token_name"`
	TokenScope     string `json:"token_scope"`
	UserGroup      string `json:"user_group"`
	RequestedModel string `json:"requested_model"`
	EffectiveModel string `json:"effective_model"`
	ChannelID      int    `json:"channel_id"`
	SessionID      string `json:"session_id"`
	CapturedAt     int64  `json:"captured_at"`
	RecordSize     int64  `json:"record_size"`
}

type DatasetCaptureFacetToken struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

type DatasetCaptureFacets struct {
	Models     []string                   `json:"models"`
	Tokens     []DatasetCaptureFacetToken `json:"tokens"`
	Groups     []string                   `json:"groups"`
	ChannelIDs []int                      `json:"channel_ids"`
}

func ListDatasetCaptureUsers(filter DatasetCaptureFilter, page, pageSize int) ([]DatasetCaptureUserGroup, int64, error) {
	query := applyDatasetCaptureFilter(DB.Model(&DatasetCaptureIndex{}), filter)
	var total int64
	if err := query.Distinct("user_id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	groups := make([]DatasetCaptureUserGroup, 0, pageSize)
	err := applyDatasetCaptureFilter(DB.Model(&DatasetCaptureIndex{}), filter).
		Select("user_id, COUNT(*) AS record_count, COUNT(DISTINCT token_scope) AS token_count, MAX(captured_at) AS last_capture, SUM(record_size) AS total_size").
		Group("user_id").
		Order("last_capture DESC, user_id ASC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&groups).Error
	if err != nil {
		return nil, 0, err
	}
	userNames, err := datasetCaptureUserNames(groupUserIDs(groups))
	if err != nil {
		return nil, 0, err
	}
	for index := range groups {
		groups[index].Username = captureUserLabel(groups[index].UserID, userNames)
	}
	return groups, total, nil
}

func GetDatasetCaptureTotals(filter DatasetCaptureFilter) (DatasetCaptureTotals, error) {
	var totals DatasetCaptureTotals
	err := applyDatasetCaptureFilter(DB.Model(&DatasetCaptureIndex{}), filter).
		Select("COUNT(DISTINCT user_id) AS user_count, COUNT(*) AS record_count, COALESCE(SUM(record_size), 0) AS total_size").
		Scan(&totals).Error
	return totals, err
}

func ListDatasetCaptureRecords(filter DatasetCaptureFilter, userID, page, pageSize int) ([]DatasetCaptureRecordSummary, int64, error) {
	filterQuery := applyDatasetCaptureFilter(DB.Model(&DatasetCaptureIndex{}), filter).Where("user_id = ?", userID)
	var total int64
	if err := filterQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	indices := make([]DatasetCaptureIndex, 0, pageSize)
	err := applyDatasetCaptureFilter(DB.Model(&DatasetCaptureIndex{}), filter).
		Where("user_id = ?", userID).
		Order("captured_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&indices).Error
	if err != nil {
		return nil, 0, err
	}
	summaries, _, err := datasetCaptureSummaries(indices)
	return summaries, total, err
}

func ListDatasetCaptureCandidates(filter DatasetCaptureFilter, limit int) ([]DatasetCaptureIndex, error) {
	indices := make([]DatasetCaptureIndex, 0)
	err := applyDatasetCaptureFilter(DB.Model(&DatasetCaptureIndex{}), filter).
		Order("file_id ASC, row ASC").
		Limit(limit).
		Find(&indices).Error
	return indices, err
}

func ListDatasetCaptureExportIndices(filter DatasetCaptureFilter, selection DatasetCaptureSelection) ([]DatasetCaptureIndex, error) {
	query := applyDatasetCaptureFilter(DB.Model(&DatasetCaptureIndex{}), filter)
	if !selection.AllFiltered {
		switch {
		case len(selection.UserIDs) > 0 && len(selection.CaptureIDs) > 0:
			query = query.Where("user_id IN ? OR capture_id IN ?", selection.UserIDs, selection.CaptureIDs)
		case len(selection.UserIDs) > 0:
			query = query.Where("user_id IN ?", selection.UserIDs)
		case len(selection.CaptureIDs) > 0:
			query = query.Where("capture_id IN ?", selection.CaptureIDs)
		default:
			query = query.Where("1 = 0")
		}
	}
	indices := make([]DatasetCaptureIndex, 0)
	err := query.
		Order("user_id ASC, token_id ASC, token_scope ASC, session_id ASC, captured_at ASC, row ASC, id ASC").
		Find(&indices).Error
	return indices, err
}

func GetDatasetCaptureIndex(node, captureID string) (DatasetCaptureIndex, error) {
	var index DatasetCaptureIndex
	err := DB.Where("node = ? AND capture_id = ?", node, captureID).Take(&index).Error
	return index, err
}

func ListDatasetCaptureIndicesByCaptureIDs(node string, captureIDs []string) ([]DatasetCaptureIndex, error) {
	indices := make([]DatasetCaptureIndex, 0, len(captureIDs))
	if len(captureIDs) == 0 {
		return indices, nil
	}
	err := DB.Where("node = ? AND capture_id IN ?", node, captureIDs).
		Order("file_id ASC, row ASC").
		Find(&indices).Error
	return indices, err
}

func GetDatasetCaptureSummary(index DatasetCaptureIndex) (DatasetCaptureRecordSummary, error) {
	summaries, _, err := datasetCaptureSummaries([]DatasetCaptureIndex{index})
	if err != nil {
		return DatasetCaptureRecordSummary{}, err
	}
	return summaries[0], nil
}

func GetDatasetCaptureFacets(node string) (DatasetCaptureFacets, error) {
	facets := DatasetCaptureFacets{
		Models:     []string{},
		Tokens:     []DatasetCaptureFacetToken{},
		Groups:     []string{},
		ChannelIDs: []int{},
	}
	if err := DB.Model(&DatasetCaptureIndex{}).Where("node = ?", node).
		Distinct("effective_model").Order("effective_model ASC").Pluck("effective_model", &facets.Models).Error; err != nil {
		return facets, err
	}
	tokenRows := make([]struct {
		TokenID    int
		TokenScope string
	}, 0)
	if err := DB.Model(&DatasetCaptureIndex{}).Where("node = ?", node).
		Select("token_id, token_scope").Group("token_id, token_scope").Order("token_scope ASC").Scan(&tokenRows).Error; err != nil {
		return facets, err
	}
	tokenIDs := make([]int, 0, len(tokenRows))
	for _, token := range tokenRows {
		if token.TokenID > 0 {
			tokenIDs = append(tokenIDs, token.TokenID)
		}
	}
	tokenNames, err := datasetCaptureTokenNames(tokenIDs)
	if err != nil {
		return facets, err
	}
	for _, token := range tokenRows {
		facets.Tokens = append(facets.Tokens, DatasetCaptureFacetToken{
			ID: token.TokenID, Name: captureTokenLabel(token.TokenID, token.TokenScope, tokenNames), Scope: token.TokenScope,
		})
	}
	if err := DB.Model(&DatasetCaptureIndex{}).Where("node = ? AND user_group <> ''", node).
		Distinct("user_group").Order("user_group ASC").Pluck("user_group", &facets.Groups).Error; err != nil {
		return facets, err
	}
	if err := DB.Model(&DatasetCaptureIndex{}).Where("node = ? AND channel_id > 0", node).
		Distinct("channel_id").Order("channel_id ASC").Pluck("channel_id", &facets.ChannelIDs).Error; err != nil {
		return facets, err
	}
	return facets, nil
}

func applyDatasetCaptureFilter(query *gorm.DB, filter DatasetCaptureFilter) *gorm.DB {
	query = query.Where("node = ?", filter.Node)
	if filter.NoMatches {
		return query.Where("1 = 0")
	}
	if filter.StartTime > 0 {
		query = query.Where("captured_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("captured_at <= ?", filter.EndTime)
	}
	if len(filter.Models) > 0 {
		query = query.Where("effective_model IN ?", filter.Models)
	}
	if len(filter.TokenIDs) > 0 {
		query = query.Where("token_id IN ?", filter.TokenIDs)
	}
	if len(filter.Groups) > 0 {
		query = query.Where("user_group IN ?", filter.Groups)
	}
	if len(filter.ChannelIDs) > 0 {
		query = query.Where("channel_id IN ?", filter.ChannelIDs)
	}
	if len(filter.CaptureIDs) > 0 {
		query = query.Where("capture_id IN ?", filter.CaptureIDs)
	}
	keyword := strings.TrimSpace(filter.Username)
	if keyword != "" {
		userQuery := DB.Unscoped().Model(&User{}).Select("id").Where("username LIKE ?", "%"+keyword+"%")
		if userID, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("user_id = ? OR user_id IN (?)", userID, userQuery)
		} else {
			query = query.Where("user_id IN (?)", userQuery)
		}
	}
	return query
}

func datasetCaptureSummaries(indices []DatasetCaptureIndex) ([]DatasetCaptureRecordSummary, int64, error) {
	userIDs := make([]int, 0, len(indices))
	tokenIDs := make([]int, 0, len(indices))
	for _, index := range indices {
		if index.UserID > 0 {
			userIDs = append(userIDs, index.UserID)
		}
		if index.TokenID > 0 {
			tokenIDs = append(tokenIDs, index.TokenID)
		}
	}
	userNames, err := datasetCaptureUserNames(userIDs)
	if err != nil {
		return nil, 0, err
	}
	tokenNames, err := datasetCaptureTokenNames(tokenIDs)
	if err != nil {
		return nil, 0, err
	}
	result := make([]DatasetCaptureRecordSummary, 0, len(indices))
	for _, index := range indices {
		result = append(result, DatasetCaptureRecordSummary{
			CaptureID: index.CaptureID, UserID: index.UserID,
			Username: captureUserLabel(index.UserID, userNames),
			TokenID:  index.TokenID, TokenName: captureTokenLabel(index.TokenID, index.TokenScope, tokenNames),
			TokenScope: index.TokenScope, UserGroup: index.UserGroup,
			RequestedModel: index.RequestedModel, EffectiveModel: index.EffectiveModel,
			ChannelID: index.ChannelID, SessionID: index.SessionID,
			CapturedAt: index.CapturedAt, RecordSize: index.RecordSize,
		})
	}
	return result, int64(len(result)), nil
}

func datasetCaptureUserNames(ids []int) (map[int]string, error) {
	result := make(map[int]string)
	if len(ids) == 0 {
		return result, nil
	}
	rows := make([]struct {
		ID       int
		Username string
	}, 0)
	if err := DB.Unscoped().Model(&User{}).Select("id", "username").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ID] = row.Username
	}
	return result, nil
}

func datasetCaptureTokenNames(ids []int) (map[int]string, error) {
	result := make(map[int]string)
	if len(ids) == 0 {
		return result, nil
	}
	rows := make([]struct {
		ID   int
		Name string
	}, 0)
	if err := DB.Unscoped().Model(&Token{}).Select("id", "name").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ID] = row.Name
	}
	return result, nil
}

func captureUserLabel(userID int, names map[int]string) string {
	if userID == 0 {
		return "Anonymous"
	}
	if name := names[userID]; name != "" {
		return name
	}
	return "Deleted user #" + strconv.Itoa(userID)
}

func captureTokenLabel(tokenID int, scope string, names map[int]string) string {
	if name := names[tokenID]; name != "" {
		return name
	}
	switch scope {
	case "playground":
		return "Playground"
	case "anonymous", "":
		return "Anonymous"
	default:
		return "Deleted token #" + strconv.Itoa(tokenID)
	}
}

func groupUserIDs(groups []DatasetCaptureUserGroup) []int {
	ids := make([]int, 0, len(groups))
	for _, group := range groups {
		if group.UserID > 0 {
			ids = append(ids, group.UserID)
		}
	}
	return ids
}
