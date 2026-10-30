package sshcore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// HostStore persists connection profiles as a JSON file next to the
// executable so the application directory stays self-contained.
// Passwords and key passphrases are encrypted before they are written.
type HostStore struct {
	mu   sync.Mutex
	path string
}

// appDir returns the directory the executable lives in.
func appDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法定位程序目录: %w", err)
	}
	return filepath.Dir(exe), nil
}

// NewHostStore creates a store rooted at <executable dir>/hosts.json.
func NewHostStore() (*HostStore, error) {
	dir, err := appDir()
	if err != nil {
		return nil, err
	}
	return &HostStore{path: filepath.Join(dir, "hosts.json")}, nil
}

// Load reads every saved profile. A missing file yields an empty list.
func (s *HostStore) Load() ([]HostConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []HostConfig{}, nil
		}
		return nil, err
	}
	var hosts []HostConfig
	if err := json.Unmarshal(raw, &hosts); err != nil {
		return nil, fmt.Errorf("配置文件损坏: %w", err)
	}
	// A profile must never carry a live session id.
	for i := range hosts {
		hosts[i].SessionID = ""
		// 解密失败（例如换机器/换用户后 DPAPI 无法解出）时留空，用户重新输入即可。
		if hosts[i].Password, err = decryptSecret(hosts[i].Password); err != nil {
			hosts[i].Password = ""
		}
		if hosts[i].KeyPassphrase, err = decryptSecret(hosts[i].KeyPassphrase); err != nil {
			hosts[i].KeyPassphrase = ""
		}
	}
	return hosts, nil
}

// Save overwrites the profile file.
func (s *HostStore) Save(hosts []HostConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hosts == nil {
		hosts = []HostConfig{}
	}
	clean := make([]HostConfig, len(hosts))
	copy(clean, hosts)
	for i := range clean {
		clean[i].SessionID = ""
		pwd, err := encryptSecret(clean[i].Password)
		if err != nil {
			return err
		}
		clean[i].Password = pwd
		pass, err := encryptSecret(clean[i].KeyPassphrase)
		if err != nil {
			return err
		}
		clean[i].KeyPassphrase = pass
	}
	raw, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o600)
}
