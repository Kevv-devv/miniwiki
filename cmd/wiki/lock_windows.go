//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var winLockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
var winUnlockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")

func acquireWikiLock(db string) (func(), error) {
	path := db + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	var ov syscall.Overlapped
	ret, _, callErr := winLockFileEx.Call(f.Fd(), uintptr(0x00000002|0x00000001), 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if ret == 0 {
		_ = f.Close()
		return nil, fmt.Errorf("wiki already in use: %v", callErr)
	}
	return func() { winUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ov))); _ = f.Close() }, nil
}
