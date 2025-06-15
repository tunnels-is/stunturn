//go:build linux || darwin

// file: reuse_addr_unix.go
package main

import "syscall"

// setReuseAddr sets the SO_REUSEADDR socket option for a given file descriptor.
// This is the implementation for Linux and macOS.
func setReuseAddr(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
}
