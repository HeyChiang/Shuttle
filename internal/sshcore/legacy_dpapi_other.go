//go:build !windows

package sshcore

import "fmt"

// legacyDPAPIPrefix marks values written by older Windows builds that used
// DPAPI. Other platforms can never decrypt them.
const legacyDPAPIPrefix = "dpapi:v1:"

func decryptLegacyDPAPI(string) (string, error) {
	return "", fmt.Errorf("旧版 DPAPI 密文只能在原 Windows 账户下解密")
}
