//go:build !windows && !linux && !darwin

package systemupdate

func RunHelperIfRequested() bool {
	return false
}
