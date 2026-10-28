package sshcore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// HostStore persists connection profiles as a JSON file in the user config
// directory so that saved hosts survive restarts.
type HostStore struct {
	mu   sync.Mutex
	path string
}

// NewHostStore creates a store rooted at <user config>/Shuttle/hosts.json.
func NewHostStore() (*HostStore, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("无法定位配置目录: %w", err)
	}
	base := filepath.Join(dir, "Shuttle")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, fmt.Errorf("无法创建配置目录: %w", err)
	}
	return &HostStore{path: filepath.Join(base, "hosts.json")}, nil
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
	}
	raw, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o600)
}