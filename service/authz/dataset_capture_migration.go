package authz

import (
	"strconv"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	legacyDatasetCaptureAdminVisibleOption = "DatasetCaptureAdminVisible"
	datasetCapturePermissionMigratedOption = "DatasetCapturePermissionMigrated"
)

// MigrateLegacyDatasetCapturePermissions preserves the old global visibility
// behavior once, then makes per-administrator grants authoritative.
func MigrateLegacyDatasetCapturePermissions(db *gorm.DB) error {
	if !common.IsMasterNode {
		return nil
	}

	var marker model.Option
	result := db.Where("key = ?", datasetCapturePermissionMigratedOption).Limit(1).Find(&marker)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		migrated, parseErr := strconv.ParseBool(marker.Value)
		if parseErr == nil && migrated {
			return nil
		}
	}

	legacyVisible := false
	var legacy model.Option
	result = db.Where("key = ?", legacyDatasetCaptureAdminVisibleOption).Limit(1).Find(&legacy)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		legacyVisible, _ = strconv.ParseBool(legacy.Value)
	}

	adminIDs := make([]int, 0)
	if legacyVisible {
		if err := db.Model(&model.User{}).
			Where("role = ?", common.RoleAdminUser).
			Pluck("id", &adminIDs).Error; err != nil {
			return err
		}
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		for _, userID := range adminIDs {
			if err := SetUserPermissionsInTx(tx, userID, PermissionsMap{
				ResourceDatasetCapture: {
					ActionView:     true,
					ActionDownload: true,
				},
			}); err != nil {
				return err
			}
		}
		marker := model.Option{Key: datasetCapturePermissionMigratedOption, Value: "true"}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value"}),
		}).Create(&marker).Error
	})
	if err != nil {
		return err
	}
	return ReloadPolicy()
}
