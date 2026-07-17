package model

type DatasetCapturePolicyUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

type DatasetCapturePolicyToken struct {
	ID       int    `json:"id"`
	UserID   int    `json:"user_id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

func ListDatasetCapturePolicySubjects(selectedUserIDs, selectedTokenIDs []int, limit int) ([]DatasetCapturePolicyUser, []DatasetCapturePolicyToken, error) {
	if limit < 1 || limit > 1000 {
		limit = 500
	}
	users := make([]DatasetCapturePolicyUser, 0, limit)
	if err := DB.Unscoped().Model(&User{}).
		Select("id, username").Order("id DESC").Limit(limit).Scan(&users).Error; err != nil {
		return nil, nil, err
	}
	if err := appendSelectedPolicyUsers(&users, selectedUserIDs); err != nil {
		return nil, nil, err
	}

	tokens := make([]DatasetCapturePolicyToken, 0, limit)
	if err := DB.Unscoped().Table("tokens").
		Select("tokens.id, tokens.user_id, tokens.name, users.username").
		Joins("LEFT JOIN users ON users.id = tokens.user_id").
		Order("tokens.id DESC").Limit(limit).Scan(&tokens).Error; err != nil {
		return nil, nil, err
	}
	if err := appendSelectedPolicyTokens(&tokens, selectedTokenIDs); err != nil {
		return nil, nil, err
	}
	return users, tokens, nil
}

func appendSelectedPolicyUsers(users *[]DatasetCapturePolicyUser, selected []int) error {
	missing := missingPolicyIDs(selected, func() map[int]struct{} {
		seen := make(map[int]struct{}, len(*users))
		for _, user := range *users {
			seen[user.ID] = struct{}{}
		}
		return seen
	}())
	if len(missing) == 0 {
		return nil
	}
	var extra []DatasetCapturePolicyUser
	if err := DB.Unscoped().Model(&User{}).Select("id, username").Where("id IN ?", missing).Scan(&extra).Error; err != nil {
		return err
	}
	*users = append(*users, extra...)
	return nil
}

func appendSelectedPolicyTokens(tokens *[]DatasetCapturePolicyToken, selected []int) error {
	seen := make(map[int]struct{}, len(*tokens))
	for _, token := range *tokens {
		seen[token.ID] = struct{}{}
	}
	missing := missingPolicyIDs(selected, seen)
	if len(missing) == 0 {
		return nil
	}
	var extra []DatasetCapturePolicyToken
	if err := DB.Unscoped().Table("tokens").
		Select("tokens.id, tokens.user_id, tokens.name, users.username").
		Joins("LEFT JOIN users ON users.id = tokens.user_id").
		Where("tokens.id IN ?", missing).Scan(&extra).Error; err != nil {
		return err
	}
	*tokens = append(*tokens, extra...)
	return nil
}

func missingPolicyIDs(selected []int, seen map[int]struct{}) []int {
	missing := make([]int, 0)
	for _, id := range selected {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		missing = append(missing, id)
	}
	return missing
}
