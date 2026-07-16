package relay

import (
	relaycommon "github.com/01121531/HUICHUAN-AI/relay/common"
	"github.com/01121531/HUICHUAN-AI/types"
)

func huichuanErrorFromParamOverride(err error) *types.HUICHUANError {
	if fixedErr, ok := relaycommon.AsParamOverrideReturnError(err); ok {
		return relaycommon.HUICHUANErrorFromParamOverride(fixedErr)
	}
	return types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid, types.ErrOptionWithSkipRetry())
}
