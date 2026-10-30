//go:build !windows

package sshcore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// encodedPrefix marks a value encrypted with the local AES-GCM key. Values
// without the prefix are treated as legacy plaintext and passed through.
const encodedPrefix = "aes:v1:"

// keyFileName is the per-installation AES-256 key stored next to hosts.json.
const keyFileName = "hosts.key"

// keyOnce loads (or creates on first use) the local encryption key.
var keyOnce = sync.OnceValues(loadOrCreateKey)

func loadOrCreateKey() ([]byte, error) {
	dir, err := appDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, keyFileName)
	key, err := os.ReadFile(path)
	if err == nil && len(key) == 32 {
		return key, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("无法读取密钥文件: %w", err)
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("无法生成密钥: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("无法写入密钥文件: %w", err)
	}
	return key, nil
}

// encryptSecret encrypts a secret with AES-256-GCM and returns it as
// "aes:v1:<base64(nonce|ciphertext)>".
func encryptSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	key, err := keyOnce()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("无法生成随机数: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return encodedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
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
	key, err := keyOnce()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("密文长度无效")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}
	return string(plain), nil
}
