package main

import (
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"shuttle/internal/sshcore"
)

// SSHService is the object exposed to the frontend. Every exported method
// becomes a callable binding; every sshcore event is forwarded to the Wails
// event bus.
type SSHService struct {
	mgr   *sshcore.Manager
	sftp  *sshcore.SftpManager
	store *sshcore.HostStore
}

// NewSSHService wires the session manager to the Wails event bus.
func NewSSHService() (*SSHService, error) {
	store, err := sshcore.NewHostStore()
	if err != nil {
		return nil, err
	}
	emit := func(event string, payload any) {
		if app := application.Get(); app != nil {
			app.Event.Emit(event, payload)
		}
	}
	svc := &SSHService{store: store}
	svc.mgr = sshcore.NewManager(emit)
	svc.sftp = sshcore.NewSftpManager(emit)
	return svc, nil
}

// Encodings lists the character sets offered in the connection form.
func (s *SSHService) Encodings() []string {
	return sshcore.SupportedEncodings()
}

// --- profile persistence ---------------------------------------------------

// LoadHosts returns the saved connection profiles.
func (s *SSHService) LoadHosts() ([]sshcore.HostConfig, error) {
	return s.store.Load()
}

// SaveHosts overwrites the saved connection profiles.
func (s *SSHService) SaveHosts(hosts []sshcore.HostConfig) error {
	return s.store.Save(hosts)
}

// --- session lifecycle -----------------------------------------------------

// Connect opens a session. The frontend supplies cfg.SessionID so it can route
// the terminal output before the connection is established.
func (s *SSHService) Connect(cfg sshcore.HostConfig) (sshcore.SessionInfo, error) {
	return s.mgr.Connect(cfg)
}

// Disconnect closes a single session.
func (s *SSHService) Disconnect(id string) {
	s.mgr.Disconnect(id)
}

// DisconnectAll closes every session.
func (s *SSHService) DisconnectAll() {
	s.mgr.DisconnectAll()
}

// Sessions returns the live session snapshot.
func (s *SSHService) Sessions() []sshcore.SessionInfo {
	return s.mgr.Sessions()
}

// Write sends keystrokes to the given sessions. An empty list broadcasts to all
// connected sessions - this powers the "sync input" switch.
func (s *SSHService) Write(ids []string, data string) error {
	return s.mgr.Write(ids, data)
}

// Resize applies a terminal size to the given sessions.
func (s *SSHService) Resize(ids []string, cols, rows int) {
	s.mgr.Resize(ids, cols, rows)
}

// Exec runs a command on every selected session and streams the result back
// through the "ssh:exec" event.
func (s *SSHService) Exec(ids []string, command string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("命令不能为空")
	}
	s.mgr.Exec(ids, command)
	return nil
}

// --- remote files ----------------------------------------------------------

// RemoteHome returns the login directory of a session.
func (s *SSHService) RemoteHome(id string) (string, error) {
	return s.mgr.RemoteHome(id)
}

// ListRemote lists a remote directory.
func (s *SSHService) ListRemote(id, dir string) ([]sshcore.RemoteFile, error) {
	return s.mgr.ListRemote(id, dir)
}

// Mkdir creates a remote directory.
func (s *SSHService) Mkdir(id, dir string) error {
	return s.mgr.Mkdir(id, dir)
}

// Remove deletes a remote file, or a directory when recursive is true.
func (s *SSHService) Remove(id, target string, recursive bool) error {
	return s.mgr.Remove(id, target, recursive)
}

// Rename moves or renames a remote entry.
func (s *SSHService) Rename(id, from, to string) error {
	return s.mgr.Rename(id, from, to)
}

// Upload copies the given local paths into remoteDir of one session.
func (s *SSHService) Upload(id, remoteDir string, localPaths []string) error {
	return s.mgr.Upload(id, remoteDir, localPaths)
}

// UploadMany copies the given local paths into remoteDir of several sessions.
func (s *SSHService) UploadMany(ids []string, remoteDir string, localPaths []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("请至少选择一个会话")
	}
	var firstErr error
	for _, id := range ids {
		if err := s.mgr.Upload(id, remoteDir, localPaths); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Download copies the given remote paths of a session into a local directory.
func (s *SSHService) Download(id string, remotePaths []string, localDir string) error {
	return s.mgr.Download(id, remotePaths, localDir)
}

// --- standalone SFTP -------------------------------------------------------
//
// These methods work on their own SFTP connection identified by token, so file
// transfers do not require an interactive terminal session to be open.

// SftpOpen opens a standalone SFTP connection for cfg under token.
func (s *SSHService) SftpOpen(cfg sshcore.HostConfig, token string) error {
	return s.sftp.Connect(cfg, token)
}

// SftpClose closes one standalone SFTP connection.
func (s *SSHService) SftpClose(token string) {
	s.sftp.Close(token)
}

// SftpCloseAll closes every standalone SFTP connection.
func (s *SSHService) SftpCloseAll() {
	s.sftp.CloseAll()
}

// SftpHome returns the login directory of a standalone connection.
func (s *SSHService) SftpHome(token string) (string, error) {
	return s.sftp.Home(token)
}

// SftpList lists a directory on a standalone connection.
func (s *SSHService) SftpList(token, dir string) ([]sshcore.RemoteFile, error) {
	return s.sftp.List(token, dir)
}

// SftpMkdir creates a remote directory.
func (s *SSHService) SftpMkdir(token, dir string) error {
	return s.sftp.Mkdir(token, dir)
}

// SftpRemove deletes a remote file, or a directory when recursive is true.
func (s *SSHService) SftpRemove(token, target string, recursive bool) error {
	return s.sftp.Remove(token, target, recursive)
}

// SftpUpload copies local files into a remote directory.
func (s *SSHService) SftpUpload(token, remoteDir string, localPaths []string) error {
	return s.sftp.Upload(token, remoteDir, localPaths)
}

// SftpDownload copies remote files into a local directory.
func (s *SSHService) SftpDownload(token string, remotePaths []string, localDir string) error {
	return s.sftp.Download(token, remotePaths, localDir)
}

// SftpTransfer streams remote files from one host to another.
func (s *SSHService) SftpTransfer(srcToken, dstToken string, remotePaths []string, dstDir string) error {
	return s.sftp.Transfer(srcToken, dstToken, remotePaths, dstDir)
}

// --- native dialogs --------------------------------------------------------

func openFileDialog() *application.OpenFileDialogStruct {
	if app := application.Get(); app != nil {
		return app.Dialog.OpenFile()
	}
	return nil
}

// PickFiles opens the native file picker and returns the selected paths.
func (s *SSHService) PickFiles(title string) ([]string, error) {
	dialog := openFileDialog()
	if dialog == nil {
		return nil, fmt.Errorf("应用尚未就绪")
	}
	dialog.SetTitle(orDefault(title, "选择要上传的文件"))
	result, err := dialog.PromptForMultipleSelection()
	if err != nil {
		return nil, err
	}
	return result, nil
}

// PickFile opens the native file picker for a single path (e.g. a private key).
func (s *SSHService) PickFile(title string) (string, error) {
	dialog := openFileDialog()
	if dialog == nil {
		return "", fmt.Errorf("应用尚未就绪")
	}
	dialog.SetTitle(orDefault(title, "选择文件"))
	result, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return result, nil
}

// PickDirectory opens the native folder picker.
func (s *SSHService) PickDirectory(title string) (string, error) {
	dialog := openFileDialog()
	if dialog == nil {
		return "", fmt.Errorf("应用尚未就绪")
	}
	dialog.CanChooseDirectories(true)
	dialog.CanChooseFiles(false)
	dialog.SetTitle(orDefault(title, "选择目录"))
	result, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return result, nil
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}