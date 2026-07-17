package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListDatasetCapturePolicySubjectsDoesNotReturnTokenKey(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&User{}, &Token{}))
	user := User{Username: "capture-policy-user", Role: 10}
	require.NoError(t, DB.Create(&user).Error)
	token := Token{UserId: user.Id, Name: "visible-token-name", Key: "secret-token-key"}
	require.NoError(t, DB.Create(&token).Error)
	t.Cleanup(func() {
		_ = DB.Unscoped().Delete(&token).Error
		_ = DB.Unscoped().Delete(&user).Error
	})

	users, tokens, err := ListDatasetCapturePolicySubjects([]int{user.Id}, []int{token.Id}, 1)
	require.NoError(t, err)
	assert.Contains(t, users, DatasetCapturePolicyUser{ID: user.Id, Username: user.Username, Role: user.Role})
	assert.Contains(t, tokens, DatasetCapturePolicyToken{ID: token.Id, UserID: user.Id, Name: token.Name, Username: user.Username})
}
