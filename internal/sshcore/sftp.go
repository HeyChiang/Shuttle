package sshcore

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
)

// ListRemote reads one directory on the remote host.
func (s *session) listRemote(dir string) ([]RemoteFile, error) {
	abs, err := s.resolveRemote(dir)
	if err != nil {
		return nil, err
	}
	c, err := s.sftpClient()
	if err != nil {
		return nil, err
	}
	return listRemoteDir(c, abs)
}

// listRemoteDir reads one directory through an SFTP client.
func listRemoteDir(c *sftp.Client, abs string) ([]RemoteFile, error) {
	entries, err := c.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", abs, err)
	}
	out := make([]RemoteFile, 0, len(entries))
	for _, entry := range entries {
		info := entry
		full := path.Join(abs, entry.Name())
		item := RemoteFile{
			Name:      entry.Name(),
			Path:      full,
			Size:      info.Size(),
			Mode:      info.Mode().String(),
			IsDir:     info.IsDir(),
			IsSymlink: info.Mode()&os.ModeSymlink != 0,
			ModTime:   info.ModTime().UnixMilli(),
		}
		if item.IsSymlink {
			if target, err := c.ReadLink(full); err == nil {
				item.LinkTarget = target
				if st, err := c.Stat(full); err == nil {
					item.IsDir = st.IsDir()
					item.Size = st.Size()
				}
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *session) mkdir(p string) error {
	abs, err := s.resolveRemote(p)
	if err != nil {
		return err
	}
	c, err := s.sftpClient()
	if err != nil {
		return err
	}
	if err := c.Mkdir(abs); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}

func (s *session) remove(p string, recursive bool) error {
	abs, err := s.resolveRemote(p)
	if err != nil {
		return err
	}
	c, err := s.sftpClient()
	if err != nil {
		return err
	}
	if recursive {
		return c.RemoveAll(abs)
	}
	return c.Remove(abs)
}

func (s *session) rename(from, to string) error {
	fromAbs, err := s.resolveRemote(from)
	if err != nil {
		return err
	}
	toAbs, err := s.resolveRemote(to)
	if err != nil {
		return err
	}
	c, err := s.sftpClient()
	if err != nil {
		return err
	}
	return c.Rename(fromAbs, toAbs)
}

// transferProgress throttles progress events so the UI is not flooded.
type transferProgress struct {
	send   Emitter
	id     string
	name   string
	dir    string
	total  int64
	last   time.Time
	copied int64
}

func newProgress(emit Emitter, id, name, direction string, total int64) *transferProgress {
	return &transferProgress{send: emit, id: id, name: name, dir: direction, total: total}
}

func (p *transferProgress) add(n int64) {
	p.copied += n
	if time.Since(p.last) < 150*time.Millisecond {
		return
	}
	p.last = time.Now()
	p.report(false, "")
}

func (p *transferProgress) report(done bool, errMsg string) {
	p.send(EventTransfer, TransferEvent{
		SessionID:   p.id,
		Name:        p.name,
		Direction:   p.dir,
		Transferred: p.copied,
		Total:       p.total,
		Done:        done,
		Error:       errMsg,
	})
}

func (p *transferProgress) finish(err error) {
	if err != nil {
		p.report(true, err.Error())
		return
	}
	p.copied = p.total
	p.report(true, "")
}

// sendLocalFile uploads one local file into absDir through c.
func sendLocalFile(c *sftp.Client, localPath, absDir string, progress *transferProgress) error {
	local, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file: %w", err)
	}
	defer func() { _ = local.Close() }()

	remote, err := c.OpenFile(path.Join(absDir, filepath.Base(localPath)), os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("failed to create remote file: %w", err)
	}
	_, err = io.Copy(io.MultiWriter(remote, progressWriter{progress}), local)
	closeErr := remote.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

// receiveRemoteFile downloads the absolute remote path into localDir through c.
func receiveRemoteFile(c *sftp.Client, abs, localDir string, progress *transferProgress) error {
	remote, err := c.Open(abs)
	if err != nil {
		return fmt.Errorf("failed to open remote file: %w", err)
	}
	defer func() { _ = remote.Close() }()

	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return fmt.Errorf("failed to create local directory: %w", err)
	}
	local, err := os.Create(filepath.Join(localDir, path.Base(abs)))
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	_, err = io.Copy(io.MultiWriter(local, progressWriter{progress}), remote)
	closeErr := local.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

// copyRemoteToRemote streams one remote file from src to dst without touching
// the local disk.
func copyRemoteToRemote(src, dst *sftp.Client, srcPath, dstPath string, progress *transferProgress) error {
	in, err := src.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := dst.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	_, err = io.Copy(io.MultiWriter(out, progressWriter{progress}), in)
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

// upload sends one local file to the remote directory.
func (s *session) upload(localPath, remoteDir string) error {
	absDir, err := s.resolveRemote(remoteDir)
	if err != nil {
		return err
	}
	c, err := s.sftpClient()
	if err != nil {
		return err
	}
	stat, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("failed to read local file: %w", err)
	}
	if stat.IsDir() {
		return fmt.Errorf("%s is a directory; directory upload is not supported", filepath.Base(localPath))
	}

	progress := newProgress(s.emit, s.cfg.SessionID, filepath.Base(localPath), "upload", stat.Size())
	err = sendLocalFile(c, localPath, absDir, progress)
	progress.finish(err)
	if err != nil {
		return fmt.Errorf("failed to upload %s: %w", progress.name, err)
	}
	return nil
}

// download fetch one remote file into a local directory.
func (s *session) download(remotePath, localDir string) error {
	abs, err := s.resolveRemote(remotePath)
	if err != nil {
		return err
	}
	c, err := s.sftpClient()
	if err != nil {
		return err
	}
	stat, err := c.Stat(abs)
	if err != nil {
		return fmt.Errorf("failed to read remote file: %w", err)
	}
	if stat.IsDir() {
		return fmt.Errorf("%s is a directory; directory download is not supported", path.Base(abs))
	}

	progress := newProgress(s.emit, s.cfg.SessionID, path.Base(abs), "download", stat.Size())
	err = receiveRemoteFile(c, abs, localDir, progress)
	progress.finish(err)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", progress.name, err)
	}
	return nil
}

// progressWriter turns every write into a progress tick.
type progressWriter struct{ p *transferProgress }

func (w progressWriter) Write(b []byte) (int, error) {
	w.p.add(int64(len(b)))
	return len(b), nil
}

// --- Manager level file operations -----------------------------------------

func (m *Manager) sessionOrErr(id string) (*session, error) {
	s := m.get(id)
	if s == nil {
		return nil, fmt.Errorf("session does not exist")
	}
	if s.isClosed() {
		return nil, fmt.Errorf("session is closed")
	}
	return s, nil
}

// RemoteHome returns the login directory of a session.
func (m *Manager) RemoteHome(id string) (string, error) {
	s, err := m.sessionOrErr(id)
	if err != nil {
		return "", err
	}
	return s.remoteHome()
}

// ListRemote lists a remote directory.
func (m *Manager) ListRemote(id, dir string) ([]RemoteFile, error) {
	s, err := m.sessionOrErr(id)
	if err != nil {
		return nil, err
	}
	return s.listRemote(dir)
}

// Mkdir creates a remote directory.
func (m *Manager) Mkdir(id, dir string) error {
	s, err := m.sessionOrErr(id)
	if err != nil {
		return err
	}
	return s.mkdir(dir)
}

// Remove deletes a remote file or directory.
func (m *Manager) Remove(id, target string, recursive bool) error {
	s, err := m.sessionOrErr(id)
	if err != nil {
		return err
	}
	return s.remove(target, recursive)
}

// Rename moves a remote file or directory.
func (m *Manager) Rename(id, from, to string) error {
	s, err := m.sessionOrErr(id)
	if err != nil {
		return err
	}
	return s.rename(from, to)
}

// Upload copies local files into a remote directory.
func (m *Manager) Upload(id, remoteDir string, localPaths []string) error {
	s, err := m.sessionOrErr(id)
	if err != nil {
		return err
	}
	var firstErr error
	for _, localPath := range localPaths {
		if err := s.upload(localPath, remoteDir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Download copies remote files into a local directory.
func (m *Manager) Download(id string, remotePaths []string, localDir string) error {
	s, err := m.sessionOrErr(id)
	if err != nil {
		return err
	}
	var firstErr error
	for _, remotePath := range remotePaths {
		if err := s.download(remotePath, localDir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
