//go:build !windows

package systemupdate

import "os"

func replaceFile(source string, destination string) error {
	return os.Rename(source, destination)
}
