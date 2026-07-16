package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatasetCaptureAccessAuditPreservesExactRecords(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&DatasetCaptureAccessAudit{}, &DatasetCaptureAccessAuditItem{}))
	eventID, err := BeginDatasetCaptureAccessAudit(DatasetCaptureAccessAuditInput{
		Action:         DatasetCaptureAuditActionDownload,
		OperatorUserID: 99101, OperatorUsername: "capture-auditor",
		OperatorRole: 100, AuthMethod: "session", IP: "127.0.0.1", Node: "audit-node",
		SelectionMode: "records", Bytes: 321,
		Records: []DatasetCaptureRecordSummary{
			{CaptureID: "111111111111111111111111", UserID: 99102, Username: "alice", TokenID: 99103, TokenName: "token-a", UserGroup: "vip", EffectiveModel: "model-a", ChannelID: 7, SessionID: "0000000000000001", CapturedAt: 100},
			{CaptureID: "222222222222222222222222", UserID: 99104, Username: "bob", TokenID: 99105, TokenName: "token-b", UserGroup: "default", EffectiveModel: "model-b", ChannelID: 8, SessionID: "0000000000000002", CapturedAt: 200},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		var audit DatasetCaptureAccessAudit
		if DB.Where("event_id = ?", eventID).Take(&audit).Error == nil {
			_ = DB.Where("audit_id = ?", audit.ID).Delete(&DatasetCaptureAccessAuditItem{}).Error
			_ = DB.Delete(&audit).Error
		}
	})
	require.NoError(t, CompleteDatasetCaptureAccessAudit(eventID, DatasetCaptureAuditOutcomeDelivered))

	entries, total, err := ListDatasetCaptureAccessAudits(DatasetCaptureAccessAuditFilter{
		Action: DatasetCaptureAuditActionDownload, Admin: "capture-auditor",
	}, 1, 20)
	require.NoError(t, err)
	require.GreaterOrEqual(t, total, int64(2))
	matched := make([]DatasetCaptureAccessAuditEntry, 0, 2)
	for _, entry := range entries {
		if entry.EventID == eventID {
			matched = append(matched, entry)
		}
	}
	require.Len(t, matched, 2)
	assert.Equal(t, DatasetCaptureAuditOutcomeDelivered, matched[0].Outcome)
	assert.Equal(t, 2, matched[0].RecordCount)
	assert.Equal(t, int64(321), matched[0].Bytes)
	assert.Equal(t, "111111111111111111111111", matched[0].CaptureID)
	assert.Equal(t, "alice", matched[0].Username)
	assert.Equal(t, "222222222222222222222222", matched[1].CaptureID)
	assert.Equal(t, "token-b", matched[1].TokenName)
}

func TestDatasetCaptureAccessAuditRejectsIncompleteEvent(t *testing.T) {
	_, err := BeginDatasetCaptureAccessAudit(DatasetCaptureAccessAuditInput{
		Action: DatasetCaptureAuditActionView, OperatorUserID: 1, OperatorUsername: "root",
	})
	require.Error(t, err)
}
