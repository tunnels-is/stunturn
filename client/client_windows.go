//go:build windows

package client

import "golang.org/x/sys/windows"

// setReuseAddr sets the SO_REUSEADDR socket option for a given file descriptor.
// This is the implementation for Windows.
func setReuseAddr(fd uintptr) error {
	return windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
}
