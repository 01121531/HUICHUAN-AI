package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatasetCaptureQueriesGroupAndFilterMetadata(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&DatasetCaptureIndex{}))
	userIDs := []int{801, 802}
	tokenIDs := []int{901, 902, 903}
	require.NoError(t, DB.Where("user_id IN ?", userIDs).Delete(&DatasetCaptureIndex{}).Error)
	require.NoError(t, DB.Unscoped().Where("id IN ?", tokenIDs).Delete(&Token{}).Error)
	require.NoError(t, DB.Unscoped().Where("id IN ?", userIDs).Delete(&User{}).Error)
	t.Cleanup(func() {
		_ = DB.Where("user_id IN ?", userIDs).Delete(&DatasetCaptureIndex{}).Error
		_ = DB.Unscoped().Where("id IN ?", tokenIDs).Delete(&Token{}).Error
		_ = DB.Unscoped().Where("id IN ?", userIDs).Delete(&User{}).Error
	})
	require.NoError(t, DB.Create(&User{Id: 801, Username: "capture-alice", Password: "password", AffCode: "capture-alice"}).Error)
	require.NoError(t, DB.Create(&User{Id: 802, Username: "capture-bob", Password: "password", AffCode: "capture-bob"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 901, UserId: 801, Key: "capture-token-901", Name: "alice-codex"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 902, UserId: 801, Key: "capture-token-902", Name: "alice-default"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 903, UserId: 802, Key: "capture-token-903", Name: "bob-codex"}).Error)

	indices := []DatasetCaptureIndex{
		{CaptureID: "capture-query-000000001", Node: "node-query", FileID: "file-query-00000000001", Row: 1, UserID: 801, TokenID: 901, TokenScope: "901", UserGroup: "vip", EffectiveModel: "model-a", ChannelID: 7, SessionID: "0000000000000001", CapturedAt: 100, RecordSize: 10},
		{CaptureID: "capture-query-000000002", Node: "node-query", FileID: "file-query-00000000002", Row: 1, UserID: 801, TokenID: 902, TokenScope: "902", UserGroup: "default", EffectiveModel: "model-b", ChannelID: 8, SessionID: "0000000000000002", CapturedAt: 200, RecordSize: 20},
		{CaptureID: "capture-query-000000003", Node: "node-query", FileID: "file-query-00000000003", Row: 1, UserID: 802, TokenID: 903, TokenScope: "903", UserGroup: "vip", EffectiveModel: "model-a", ChannelID: 7, SessionID: "0000000000000003", CapturedAt: 150, RecordSize: 30},
		{CaptureID: "capture-query-000000004", Node: "other-node", FileID: "file-query-00000000004", Row: 1, UserID: 802, TokenID: 903, TokenScope: "903", UserGroup: "vip", EffectiveModel: "model-a", ChannelID: 7, SessionID: "0000000000000004", CapturedAt: 300, RecordSize: 40},
	}
	require.NoError(t, DB.Create(&indices).Error)

	filter := DatasetCaptureFilter{Node: "node-query", Models: []string{"model-a"}, Groups: []string{"vip"}, ChannelIDs: []int{7}}
	users, total, err := ListDatasetCaptureUsers(filter, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, users, 2)
	assert.Equal(t, "capture-bob", users[0].Username)
	assert.Equal(t, int64(30), users[0].TotalSize)
	assert.Equal(t, "capture-alice", users[1].Username)
	totals, err := GetDatasetCaptureTotals(filter)
	require.NoError(t, err)
	assert.Equal(t, int64(2), totals.UserCount)
	assert.Equal(t, int64(2), totals.RecordCount)
	assert.Equal(t, int64(40), totals.TotalSize)

	records, recordTotal, err := ListDatasetCaptureRecords(DatasetCaptureFilter{
		Node: "node-query", TokenIDs: []int{902}, Username: "alice",
	}, 801, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), recordTotal)
	require.Len(t, records, 1)
	assert.Equal(t, "alice-default", records[0].TokenName)
	assert.Equal(t, "model-b", records[0].EffectiveModel)

	exportIndices, err := ListDatasetCaptureExportIndices(
		DatasetCaptureFilter{Node: "node-query", Models: []string{"model-a"}},
		DatasetCaptureSelection{
			UserIDs: []int{801}, CaptureIDs: []string{"capture-query-000000001", "capture-query-000000003"},
		},
	)
	require.NoError(t, err)
	require.Len(t, exportIndices, 2, "overlapping user and record selections must be deduplicated")
	assert.Equal(t, "capture-query-000000001", exportIndices[0].CaptureID)
	assert.Equal(t, "capture-query-000000003", exportIndices[1].CaptureID)

	allFiltered, err := ListDatasetCaptureExportIndices(
		DatasetCaptureFilter{Node: "node-query", Groups: []string{"vip"}},
		DatasetCaptureSelection{AllFiltered: true},
	)
	require.NoError(t, err)
	require.Len(t, allFiltered, 2)
	assert.Equal(t, 801, allFiltered[0].UserID)
	assert.Equal(t, 802, allFiltered[1].UserID)

	facets, err := GetDatasetCaptureFacets("node-query")
	require.NoError(t, err)
	assert.Equal(t, []string{"model-a", "model-b"}, facets.Models)
	assert.Equal(t, []string{"default", "vip"}, facets.Groups)
	assert.Equal(t, []int{7, 8}, facets.ChannelIDs)
	assert.Len(t, facets.Tokens, 3)
}

func TestDatasetCaptureFilterNoMatches(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&DatasetCaptureIndex{}))
	users, total, err := ListDatasetCaptureUsers(DatasetCaptureFilter{Node: "node-query", NoMatches: true}, 1, 20)
	require.NoError(t, err)
	assert.Empty(t, users)
	assert.Zero(t, total)
}
