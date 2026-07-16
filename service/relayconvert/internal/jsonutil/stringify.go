package jsonutil

import (
	"fmt"

	"github.com/01121531/HUICHUAN-AI/common"
)

func ToJSONString(v interface{}) string {
	bytes, err := common.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(bytes)
}
