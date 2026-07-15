package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDatasetCapturePermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = wasMaster })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(db))
	require.NoError(t, authz.SetUserPermissions(20, authz.PermissionsMap{
		authz.ResourceDatasetCapture: {
			authz.ActionView: true,
		},
	}))
	require.NoError(t, authz.SetUserPermissions(21, authz.PermissionsMap{
		authz.ResourceDatasetCapture: {
			authz.ActionView:     true,
			authz.ActionDownload: true,
		},
	}))

	tests := []struct {
		name       string
		userID     int
		role       int
		download   bool
		wantStatus int
	}{
		{name: "root can download implicitly", userID: 1, role: common.RoleRootUser, download: true, wantStatus: http.StatusNoContent},
		{name: "admin without grant cannot view", userID: 19, role: common.RoleAdminUser, wantStatus: http.StatusForbidden},
		{name: "admin with view grant can view", userID: 20, role: common.RoleAdminUser, wantStatus: http.StatusNoContent},
		{name: "view grant alone cannot download", userID: 20, role: common.RoleAdminUser, download: true, wantStatus: http.StatusForbidden},
		{name: "admin with both grants can download", userID: 21, role: common.RoleAdminUser, download: true, wantStatus: http.StatusNoContent},
		{name: "common user cannot use explicit admin grants", userID: 21, role: common.RoleCommonUser, wantStatus: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			handlers := []gin.HandlerFunc{
				func(c *gin.Context) {
					c.Set("id", test.userID)
					c.Set("role", test.role)
					c.Next()
				},
				RequirePermission(authz.DatasetCaptureView),
			}
			if test.download {
				handlers = append(handlers, RequirePermission(authz.DatasetCaptureDownload))
			}
			handlers = append(handlers, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			router.GET("/", handlers...)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			router.ServeHTTP(response, request)
			require.Equal(t, test.wantStatus, response.Code)
		})
	}
}
