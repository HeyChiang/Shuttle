package sshcore

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// 加密是可选项：passphrase 为空时导出明文数组，方便直接迁移；
// 设置密码后使用独立于本地主密钥的密钥加密，文件拷到其他机器仍可导入。
const (
	exportFileVersion  = 2
	exportCipherPrefix = "aes:v2:"
	exportSaltSize     = 16
	// exportKDFIterations 沿用 OWASP 对 PBKDF2-HMAC-SHA256 的建议值。
	exportKDFIterations = 600_000
)

// exportFile 是导出文件的信封结构。密码与私钥口令各自以 aes:v2: 前缀加密。
type exportFile struct {
	Version   int          `json:"version"`
	Encrypted bool         `json:"encrypted"`
	Salt      string       `json:"salt"`
	Hosts     []HostConfig `json:"hosts"`
}

// ExportJSON 将主机档案序列化为导出文件。passphrase 为空时输出明文，
// 否则用该密码加密其中的密码与私钥口令。
func ExportJSON(hosts []HostConfig, passphrase string) ([]byte, error) {
	cleaned := make([]HostConfig, 0, len(hosts))
	for _, h := range hosts {
		h.SessionID = ""
		cleaned = append(cleaned, h)
	}
	if strings.TrimSpace(passphrase) == "" {
		data, err := json.MarshalIndent(cleaned, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("生成导出文件失败: %w", err)
		}
		return data, nil
	}
	salt := make([]byte, exportSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("生成随机盐失败: %w", err)
	}
	key := pbkdf2.Key([]byte(passphrase), salt, exportKDFIterations, 32, sha256.New)

	sealed := make([]HostConfig, 0, len(cleaned))
	for _, h := range cleaned {
		var err error
		if h.Password, err = sealExport(h.Password, key); err != nil {
			return nil, err
		}
		if h.KeyPassphrase, err = sealExport(h.KeyPassphrase, key); err != nil {
			return nil, err
		}
		sealed = append(sealed, h)
	}

	doc := exportFile{
		Version:   exportFileVersion,
		Encrypted: true,
		Salt:      base64.StdEncoding.EncodeToString(salt),
		Hosts:     sealed,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("生成导出文件失败: %w", err)
	}
	return data, nil
}

// ExportNeedsPassphrase 判断导出文件是否加密，用于导入时按需询问密码。
func ExportNeedsPassphrase(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] == '[' {
		return false
	}
	var doc exportFile
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return false
	}
	return doc.Encrypted
}

// DecryptExport 解析导出文件并用 passphrase 解密凭据。
// 旧版本导出的明文 JSON 数组保持兼容，无需密码即可导入。
func DecryptExport(data []byte, passphrase string) ([]HostConfig, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("导入文件内容为空")
	}
	if trimmed[0] == '[' {
		var hosts []HostConfig
		if err := json.Unmarshal(trimmed, &hosts); err != nil {
			return nil, fmt.Errorf("解析导入文件失败: %w", err)
		}
		return hosts, nil
	}

	var doc exportFile
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return nil, fmt.Errorf("解析导入文件失败: %w", err)
	}
	if !doc.Encrypted {
		return doc.Hosts, nil
	}
	if strings.TrimSpace(passphrase) == "" {
		return nil, errors.New("该文件已加密，请输入导入密码")
	}
	salt, err := base64.StdEncoding.DecodeString(doc.Salt)
	if err != nil || len(salt) != exportSaltSize {
		return nil, errors.New("导入文件已损坏：盐值无效")
	}
	key := pbkdf2.Key([]byte(passphrase), salt, exportKDFIterations, 32, sha256.New)

	hosts := make([]HostConfig, 0, len(doc.Hosts))
	for _, h := range doc.Hosts {
		var err error
		if h.Password, err = openExport(h.Password, key); err != nil {
			return nil, errors.New("导入密码错误，或文件已损坏")
		}
		if h.KeyPassphrase, err = openExport(h.KeyPassphrase, key); err != nil {
			return nil, errors.New("导入密码错误，或文件已损坏")
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

// sealExport 用 AES-256-GCM 加密单个凭据，返回 "aes:v2:<base64(nonce|ciphertext)>"。
func sealExport(plain string, key []byte) (string, error) {
	if plain == "" {
		return "", nil
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return exportCipherPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func openExport(stored string, key []byte) (string, error) {
	if stored == "" {
		return "", nil
	}
	payload, ok := strings.CutPrefix(stored, exportCipherPrefix)
	if !ok {
		return "", errors.New("凭据不是当前导出格式")
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("密文长度无效")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
