//go:build windows

package storage

// Windows 不提供 Unix 目录 fsync，目录初始化和尾行备份不额外同步目录。
func syncDirectory(string) error { return nil }
