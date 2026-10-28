package sshcore

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SftpManager owns SFTP connections that do not depend on an interactive
// terminal session. Every connection is identified by a caller supplied token,
// which is also used as the routing key for progress events.
type SftpManager struct {
	emit  Emitter
	mu    sync.Mutex
	conns map[string]*sftpConn
}

type sftpConn struct {
	token string
	cfg   HostConfig
	home  string
	emit  Emitter

	mu     sync.Mutex
	client *ssh.Client
	sftp   *sftp.Client
}

// NewSftpManager creates an empty standalone SFTP connection pool.
func NewSftpManager(emit Emitter) *SftpManager {
	if emit == nil {
		emit = func(string, any) {}
	}
	return &SftpManager{emit: emit, conns: make(map[string]*sftpConn)}
}

// Connect opens (or replaces) the SFTP connection identified by token.
func (m *SftpManager) Connect(cfg HostConfig, token string) error {
	cfg = applyDefaults(cfg)
	if strings.TrimSpace(cfg.Host) == "" {
		return fmt.Errorf("请填写主机地址")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("缺少连接标识")
	}

	m.Close(token)

	client, err := dialClient(cfg)
	if err != nil {
		return err
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("打开 SFTP 失败: %w", err)
	}
	home, err := sc.Getwd()
	if err != nil || home == "" {
		home = "/"
	}

	m.mu.Lock()
	m.conns[token] = &sftpConn{
		token:  token,
		cfg:    cfg,
		home:   home,
		emit:   m.emit,
		client: client,
		sftp:   sc,
	}
	m.mu.Unlock()
	return nil
}

// Close drops one connection.
func (m *SftpManager) Close(token string) {
	m.mu.Lock()
	conn, ok := m.conns[token]
	if ok {
		delete(m.conns, token)
	}
	m.mu.Unlock()
	if ok {
		conn.close()
	}
}

// CloseAll drops every connection.
func (m *SftpManager) CloseAll() {
	m.mu.Lock()
	all := m.conns
	m.conns = make(map[string]*sftpConn)
	m.mu.Unlock()
	for _, conn := range all {
		conn.close()
	}
}

func (c *sftpConn) close() {
	if c.sftp != nil {
		_ = c.sftp.Close()
	}
	if c.client != nil {
		_ = c.client.Close()
	}
}

func (m *SftpManager) get(token string) (*sftpConn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, ok := m.conns[token]
	if !ok {
		return nil, fmt.Errorf("SFTP 连接不存在或已关闭")
	}
	return conn, nil
}

// resolve turns a possibly relative remote path into an absolute one.
func (c *sftpConn) resolve(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return c.home
	}
	if !strings.HasPrefix(p, "/") {
		return path.Join(c.home, p)
	}
	return path.Clean(p)
}

// Home returns the login directory of the connection.
func (m *SftpManager) Home(token string) (string, error) {
	conn, err := m.get(token)
	if err != nil {
		return "", err
	}
	return conn.home, nil
}

// List reads one remote directory.
func (m *SftpManager) List(token, dir string) ([]RemoteFile, error) {
	conn, err := m.get(token)
	if err != nil {
		return nil, err
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return listRemoteDir(conn.sftp, conn.resolve(dir))
}

// Mkdir creates one remote directory.
func (m *SftpManager) Mkdir(token, dir string) error {
	conn, err := m.get(token)
	if err != nil {
		return err
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if err := conn.sftp.Mkdir(conn.resolve(dir)); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	return nil
}

// Remove deletes a remote file or directory.
func (m *SftpManager) Remove(token, target string, recursive bool) error {
	conn, err := m.get(token)
	if err != nil {
		return err
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	abs := conn.resolve(target)
	if recursive {
		return conn.sftp.RemoveAll(abs)
	}
	return conn.sftp.Remove(abs)
}

// Upload copies local files into a remote directory.
func (m *SftpManager) Upload(token, remoteDir string, localPaths []string) error {
	conn, err := m.get(token)
	if err != nil {
		return err
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()

	absDir := conn.resolve(remoteDir)
	if err := conn.sftp.MkdirAll(absDir); err != nil {
		return fmt.Errorf("创建远程目录失败: %w", err)
	}
	var firstErr error
	for _, localPath := range localPaths {
		stat, err := os.Stat(localPath)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("读取本地文件失败: %w", err)
			}
			continue
		}
		if stat.IsDir() {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s 是目录，暂不支持目录上传", filepath.Base(localPath))
			}
			continue
		}
		progress := newProgress(conn.emit, conn.token, filepath.Base(localPath), "upload", stat.Size())
		err = sendLocalFile(conn.sftp, localPath, absDir, progress)
		progress.finish(err)
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("上传 %s 失败: %w", filepath.Base(localPath), err)
		}
	}
	return firstErr
}

// Download copies remote files into a local directory.
func (m *SftpManager) Download(token string, remotePaths []string, localDir string) error {
	conn, err := m.get(token)
	if err != nil {
		return err
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()

	var firstErr error
	for _, remotePath := range remotePaths {
		abs := conn.resolve(remotePath)
		stat, err := conn.sftp.Stat(abs)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("读取远程文件失败: %w", err)
			}
			continue
		}
		if stat.IsDir() {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s 是目录，暂不支持目录下载", path.Base(abs))
			}
			continue
		}
		progress := newProgress(conn.emit, conn.token, path.Base(abs), "download", stat.Size())
		err = receiveRemoteFile(conn.sftp, abs, localDir, progress)
		progress.finish(err)
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("下载 %s 失败: %w", path.Base(abs), err)
		}
	}
	return firstErr
}

// Transfer streams remote files directly from one host to another.
func (m *SftpManager) Transfer(srcToken, dstToken string, remotePaths []string, dstDir string) error {
	src, err := m.get(srcToken)
	if err != nil {
		return err
	}
	dst, err := m.get(dstToken)
	if err != nil {
		return err
	}
	if src == dst {
		return fmt.Errorf("源主机与目标主机相同")
	}

	// Lock in a stable order so two opposite transfers cannot deadlock.
	first, second := src, dst
	if srcToken > dstToken {
		first, second = dst, src
	}
	first.mu.Lock()
	defer first.mu.Unlock()
	second.mu.Lock()
	defer second.mu.Unlock()

	absDstDir := dst.resolve(dstDir)
	if err := dst.sftp.MkdirAll(absDstDir); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}

	var firstErr error
	for _, remotePath := range remotePaths {
		abs := src.resolve(remotePath)
		stat, err := src.sftp.Stat(abs)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("读取源文件失败: %w", err)
			}
			continue
		}
		if stat.IsDir() {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s 是目录，暂不支持目录传输", path.Base(abs))
			}
			continue
		}
		progress := newProgress(src.emit, src.token, path.Base(abs), "transfer", stat.Size())
		err = copyRemoteToRemote(src.sftp, dst.sftp, abs, path.Join(absDstDir, path.Base(abs)), progress)
		progress.finish(err)
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("传输 %s 失败: %w", path.Base(abs), err)
		}
	}
	return firstErr
}
