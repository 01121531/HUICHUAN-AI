//go:build !windows

package systemupdate

func RunHelperIfRequested() bool {
	return false
}
