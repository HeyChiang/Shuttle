package sshcore

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// storeFileName is the SQLite database holding connection profiles. It lives
// next to the executable so the application directory stays self-contained.
const storeFileName = "hosts.db"

// HostStore persists connection profiles in SQLite. Passwords and key
// passphrases are encrypted before they are written.
type HostStore struct {
	mu sync.Mutex
	db *sql.DB
}

// appDir returns the directory the executable lives in.
func appDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法定位程序目录: %w", err)
	}
	return filepath.Dir(exe), nil
}

// NewHostStore opens (and creates if needed) <executable dir>/hosts.db.
func NewHostStore() (*HostStore, error) {
	dir, err := appDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, storeFileName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开主机数据库失败: %w", err)
	}
	// SQLite 只允许单写者，单连接可以避免并发写锁冲突。
	db.SetMaxOpenConns(1)
	s := &HostStore{db: db}
	if err := s.init(path); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *HostStore) init(path string) error {
	if _, err := s.db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return fmt.Errorf("初始化数据库失败: %w", err)
	}
	if _, err := s.db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		return fmt.Errorf("初始化数据库失败: %w", err)
	}
	if err := s.migrate(); err != nil {
		return err
	}
	// 数据库内含加密凭据，尽量收紧权限（Windows 上由 ACL 决定，无副作用）。
	_ = os.Chmod(path, 0o600)
	return nil
}

func (s *HostStore) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("读取数据库版本失败: %w", err)
	}
	if version >= 1 {
		return nil
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS hosts (
		position         INTEGER PRIMARY KEY,
		id               TEXT NOT NULL,
		name             TEXT NOT NULL DEFAULT '',
		grp              TEXT NOT NULL DEFAULT '',
		host             TEXT NOT NULL DEFAULT '',
		port             INTEGER NOT NULL DEFAULT 22,
		user             TEXT NOT NULL DEFAULT '',
		auth_method      TEXT NOT NULL DEFAULT '',
		password         TEXT NOT NULL DEFAULT '',
		private_key_path TEXT NOT NULL DEFAULT '',
		key_passphrase   TEXT NOT NULL DEFAULT '',
		encoding         TEXT NOT NULL DEFAULT '',
		term_type        TEXT NOT NULL DEFAULT '',
		cols             INTEGER NOT NULL DEFAULT 0,
		rows             INTEGER NOT NULL DEFAULT 0,
		timeout_sec      INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		return fmt.Errorf("创建数据表失败: %w", err)
	}
	if _, err := s.db.Exec(`PRAGMA user_version = 1`); err != nil {
		return fmt.Errorf("写入数据库版本失败: %w", err)
	}
	return nil
}

// Load reads every saved profile in display order.
func (s *HostStore) Load() ([]HostConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, name, grp, host, port, user, auth_method,
		password, private_key_path, key_passphrase, encoding, term_type,
		cols, rows, timeout_sec
		FROM hosts ORDER BY position`)
	if err != nil {
		return nil, fmt.Errorf("读取主机档案失败: %w", err)
	}
	defer rows.Close()
	hosts := []HostConfig{}
	for rows.Next() {
		var h HostConfig
		if err := rows.Scan(&h.ID, &h.Name, &h.Group, &h.Host, &h.Port, &h.User,
			&h.AuthMethod, &h.Password, &h.PrivateKeyPath, &h.KeyPassphrase,
			&h.Encoding, &h.TermType, &h.Cols, &h.Rows, &h.TimeoutSec); err != nil {
			return nil, fmt.Errorf("读取主机档案失败: %w", err)
		}
		// 解密失败（例如换机器/换用户后无法解出）时留空，用户重新输入即可。
		if h.Password, err = decryptSecret(h.Password); err != nil {
			h.Password = ""
		}
		if h.KeyPassphrase, err = decryptSecret(h.KeyPassphrase); err != nil {
			h.KeyPassphrase = ""
		}
		hosts = append(hosts, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取主机档案失败: %w", err)
	}
	return hosts, nil
}

// Save replaces the stored profiles with the given list.
func (s *HostStore) Save(hosts []HostConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replaceAll(hosts)
}

// replaceAll writes the whole list in one transaction. Callers must hold s.mu.
func (s *HostStore) replaceAll(hosts []HostConfig) error {
	if hosts == nil {
		hosts = []HostConfig{}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("保存主机档案失败: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM hosts`); err != nil {
		return fmt.Errorf("保存主机档案失败: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO hosts (
		position, id, name, grp, host, port, user, auth_method,
		password, private_key_path, key_passphrase, encoding, term_type,
		cols, rows, timeout_sec
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("保存主机档案失败: %w", err)
	}
	defer stmt.Close()
	for i, h := range hosts {
		pwd, err := encryptSecret(h.Password)
		if err != nil {
			return err
		}
		pass, err := encryptSecret(h.KeyPassphrase)
		if err != nil {
			return err
		}
		if _, err := stmt.Exec(i, h.ID, h.Name, h.Group, h.Host, h.Port, h.User,
			h.AuthMethod, pwd, h.PrivateKeyPath, pass, h.Encoding, h.TermType,
			h.Cols, h.Rows, h.TimeoutSec); err != nil {
			return fmt.Errorf("保存主机档案失败: %w", err)
		}
	}
	return tx.Commit()
}

// Close releases the database handle.
func (s *HostStore) Close() error {
	return s.db.Close()
}
