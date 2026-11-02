//go:build windows

package sshcore

import (
	"encoding/base64"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// legacyDPAPIPrefix marks values written by older builds that used Windows
// DPAPI. They can only be read by the original Windows user on the original
// machine, so this code exists purely for the one-time upgrade path.
const legacyDPAPIPrefix = "dpapi:v1:"

const cryptProtectUIFobidden = 0x1

var (
	procCryptUnprotectData = syscall.NewLazyDLL("crypt32.dll").NewProc("CryptUnprotectData")
	procLocalFree          = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
)

// dataBlob mirrors the Win32 DATA_BLOB structure.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

// decryptLegacyDPAPI decrypts a value produced by the removed DPAPI path.
func decryptLegacyDPAPI(stored string) (string, error) {
	payload, ok := strings.CutPrefix(stored, legacyDPAPIPrefix)
	if !ok {
		return "", fmt.Errorf("不是 DPAPI 密文")
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("密文格式无效: %w", err)
	}
	if len(raw) == 0 {
		return "", nil
	}
	in := dataBlob{cbData: uint32(len(raw)), pbData: &raw[0]}
	var out dataBlob
	ret, _, callErr := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		cryptProtectUIFobidden,
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(raw)
	if ret == 0 {
		return "", fmt.Errorf("DPAPI 解密失败（可能不是原 Windows 账户）: %w", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return string(unsafe.Slice(out.pbData, out.cbData)), nil
}
