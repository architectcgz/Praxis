//go:build windows

package jsonl

import (
	"golang.org/x/sys/windows"
)

// Windows 不支持 Unix 目录 fsync；新文件发布使用 WRITE_THROUGH 的移动操作。
func syncDirectory(string) error { return nil }
func publishFile(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, target, windows.MOVEFILE_WRITE_THROUGH)
}
