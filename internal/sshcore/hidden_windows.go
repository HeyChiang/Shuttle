//go:build windows

package sshcore

import (
	"os"
	"syscall"
)

// isHiddenFile 判断是否为 Windows 默认隐藏的文件，即带隐藏或系统属性的条目
// （pagefile.sys、hiberfil.sys、System Volume Information 等）。
func isHiddenFile(info os.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	attrs := data.FileAttributes
	return attrs&syscall.FILE_ATTRIBUTE_HIDDEN != 0 || attrs&syscall.FILE_ATTRIBUTE_SYSTEM != 0
}
