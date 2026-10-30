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

// encodedPrefix marks a value encrypted with Windows DPAPI. Values without the
// prefix are treated as legacy plaintext and passed through unchanged.
const encodedPrefix = "dpapi:v1:"

const cryptProtectUIFobidden = 0x1

var (
	procCryptProtectData   = syscall.NewLazyDLL("crypt32.dll").NewProc("CryptProtectData")
	procCryptUnprotectData = syscall.NewLazyDLL("crypt32.dll").NewProc("CryptUnprotectData")
	procLocalFree          = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
)

// dataBlob mirrors the Win32 DATA_BLOB structure.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

// encryptSecret encrypts a secret with DPAPI, bound to the current Windows
// user account, and returns it as "dpapi:v1:<base64>".
func encryptSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	data := []byte(plain)
	in := dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	var out dataBlob
	ok, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // szDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		cryptProtectUIFobidden,
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(data)
	if ok == 0 {
		return "", fmt.Errorf("DPAPI 加密失败: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	sealed := base64.StdEncoding.EncodeToString(unsafe.Slice(out.pbData, out.cbData))
	return encodedPrefix + sealed, nil
}

// decryptSecret decrypts a value produced by encryptSecret.
func decryptSecret(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	payload, ok := strings.CutPrefix(stored, encodedPrefix)
	if !ok {
		return stored, nil // 旧版明文，保持兼容
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
	ret, _, err := procCryptUnprotectData.Call(
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
		return "", fmt.Errorf("DPAPI 解密失败: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return string(unsafe.Slice(out.pbData, out.cbData)), nil
}
