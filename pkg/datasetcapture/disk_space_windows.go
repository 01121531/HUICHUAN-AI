//go:build windows

package datasetcapture

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func availableDiskBytes(path string) (int64, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	root := filepath.VolumeName(absolute) + `\`
	rootPointer, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(rootPointer, &free, nil, nil); err != nil {
		return 0, err
	}
	if free > uint64(^uint64(0)>>1) {
		return int64(^uint64(0) >> 1), nil
	}
	return int64(free), nil
}
