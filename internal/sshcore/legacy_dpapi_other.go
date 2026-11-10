//go:build !windows

package sshcore

import "fmt"

// legacyDPAPIPrefix marks values written by older Windows builds that used
// DPAPI. Other platforms can never decrypt them.
const legacyDPAPIPrefix = "dpapi:v1:"

func decryptLegacyDPAPI(string) (string, error) {
	return "", fmt.Errorf("legacy DPAPI ciphertext can only be decrypted by the original Windows account")
}
