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

// encodedPrefix marks a value encrypted with the local master key. Values
// without a known prefix are treated as legacy plaintext and passed through.
const encodedPrefix = "aes:v1:"

// masterKeyFileName is the per-installation AES-256 key stored next to the
// database. The key is generated on first use and never embedded in the binary,
// so a leaked build does not expose every installation's data.
const masterKeyFileName = "master.key"

// keyOnce loads (or creates on first use) the local master key.
var keyOnce = sync.OnceValues(loadOrCreateMasterKey)

// loadOrCreateMasterKey returns the 32-byte master key, generating it once.
// A malformed existing key is reported instead of overwritten: replacing it
// would silently make every stored password undecryptable.
func loadOrCreateMasterKey() ([]byte, error) {
	dir, err := appDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, masterKeyFileName)
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, fmt.Errorf("主密钥文件 %s 已损坏（长度 %d），请备份后删除该文件再重新输入密码", path, len(key))
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("无法读取主密钥文件: %w", err)
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("无法生成主密钥: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("无法写入主密钥文件: %w", err)
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
	gcm, err := newGCM(key)
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

// decryptSecret decrypts a value produced by encryptSecret. Legacy values are
// still readable so existing profiles keep working after an upgrade.
func decryptSecret(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if strings.HasPrefix(stored, legacyDPAPIPrefix) {
		return decryptLegacyDPAPI(stored)
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
	gcm, err := newGCM(key)
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

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
