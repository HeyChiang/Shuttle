//go:build !windows

package sshcore

import "os"

// isHiddenFile 在非 Windows 平台不做隐藏属性过滤。
func isHiddenFile(info os.FileInfo) bool {
	return false
}
