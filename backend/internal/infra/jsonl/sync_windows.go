//go:build windows

package jsonl

// Windows 不提供 Unix 目录 fsync，目录初始化不额外同步。
func syncDirectory(string) error { return nil }
