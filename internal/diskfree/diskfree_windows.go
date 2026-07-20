//go:build windows

// Package diskfree reports available bytes at a path (§8a space-aware fail-down).
package diskfree

import "golang.org/x/sys/windows"

// Available returns bytes free to the caller at path's volume.
func Available(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &totalFree); err != nil {
		return 0, err
	}
	return freeToCaller, nil
}
