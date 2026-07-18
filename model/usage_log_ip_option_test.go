package model

import (
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
)

func TestUpdateOptionMapNormalizesEmptyTrustedProxyCIDRs(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalMap
		common.OptionMapRWMutex.Unlock()
		common.SetUsageLogTrustedProxyCIDRs("")
	})

	if err := updateOptionMap("TrustedProxyCIDRs", ""); err != nil {
		t.Fatal(err)
	}

	common.OptionMapRWMutex.RLock()
	got := common.OptionMap["TrustedProxyCIDRs"]
	common.OptionMapRWMutex.RUnlock()
	if got != common.DefaultUsageLogTrustedProxyCIDRs {
		t.Fatalf("TrustedProxyCIDRs = %q, want %q", got, common.DefaultUsageLogTrustedProxyCIDRs)
	}
}
