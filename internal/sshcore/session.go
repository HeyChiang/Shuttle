package sshcore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/text/transform"
)

// session is a single interactive shell on a remote host.
type session struct {
	cfg  HostConfig
	emit Emitter

	client  *ssh.Client
	shell   *ssh.Session
	stdin   io.WriteCloser
	decoder *transform.Reader

	sftpMu sync.Mutex
	sftp   *sftp.Client

	mu     sync.Mutex
	closed bool
	reason string
}

func newSession(cfg HostConfig, emit Emitter) *session {
	return &session{cfg: cfg, emit: emit}
}

// applyDefaults fills the optional fields with sane values.
func applyDefaults(cfg HostConfig) HostConfig {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.User == "" {
		cfg.User = "root"
	}
	if cfg.Name == "" {
		cfg.Name = fmt.Sprintf("%s@%s", cfg.User, cfg.Host)
	}
	if cfg.TermType == "" {
		cfg.TermType = "xterm-256color"
	}
	if cfg.Cols <= 0 {
		cfg.Cols = 80
	}
	if cfg.Rows <= 0 {
		cfg.Rows = 24
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 15
	}
	if cfg.AuthMethod == "" {
		cfg.AuthMethod = AuthPassword
	}
	cfg.Encoding = normalizeEncoding(cfg.Encoding)
	return cfg
}

func (s *session) info() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := StatusConnected
	message := ""
	if s.closed {
		status = StatusClosed
		message = s.reason
	}
	return SessionInfo{
		ID:          s.cfg.SessionID,
		Name:        s.cfg.Name,
		Host:        s.cfg.Host,
		Port:        s.cfg.Port,
		User:        s.cfg.User,
		Encoding:    s.cfg.Encoding,
		Status:      status,
		Message:     message,
		Cols:        s.cfg.Cols,
		Rows:        s.cfg.Rows,
		ConnectedAt: time.Now().UnixMilli(),
	}
}

func loadSigner(keyPath, passphrase string) (ssh.Signer, error) {
	if keyPath == "" {
		return nil, fmt.Errorf("请选择私钥文件")
	}
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("读取私钥失败: %w", err)
	}
	if passphrase != "" {
		signer, err := ssh.ParsePrivateKeyWithPassphrase(raw, []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("解析私钥失败: %w", err)
		}
		return signer, nil
	}
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			return nil, fmt.Errorf("私钥已加密，请填写私钥口令")
		}
		return nil, fmt.Errorf("解析私钥失败: %w", err)
	}
	return signer, nil
}

// start dials the host, opens the PTY and launches the interactive shell.
func (s *session) start() error {
	client, err := dialClient(s.cfg)
	if err != nil {
		return err
	}
	s.client = client

	shell, err := s.client.NewSession()
	if err != nil {
		_ = s.client.Close()
		return fmt.Errorf("创建会话失败: %w", err)
	}
	s.shell = shell

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := shell.RequestPty(s.cfg.TermType, s.cfg.Rows, s.cfg.Cols, modes); err != nil {
		_ = s.client.Close()
		return fmt.Errorf("申请 PTY 失败: %w", err)
	}

	rawStdin, err := shell.StdinPipe()
	if err != nil {
		_ = s.client.Close()
		return err
	}
	stdout, err := shell.StdoutPipe()
	if err != nil {
		_ = s.client.Close()
		return err
	}
	stderr, err := shell.StderrPipe()
	if err != nil {
		_ = s.client.Close()
		return err
	}

	s.stdin = s.encoderWriter(rawStdin)

	// stdout and stderr are streamed concurrently into one pipe so that the
	// order of the output is preserved and a quiet stderr never blocks.
	pr, pw := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(pw, stdout) }()
	go func() { defer wg.Done(); _, _ = io.Copy(pw, stderr) }()
	go func() { wg.Wait(); _ = pw.Close() }()
	s.decoder = transform.NewReader(pr, s.decoderTransform())

	if err := shell.Shell(); err != nil {
		_ = s.client.Close()
		return fmt.Errorf("启动 shell 失败: %w", err)
	}

	go s.pump()
	go s.wait()
	return nil
}

// decoderTransform returns the transformer that converts remote bytes to UTF-8.
func (s *session) decoderTransform() transform.Transformer {
	if isUTF8(s.cfg.Encoding) {
		return transform.Nop
	}
	return encodings[strings.ToUpper(s.cfg.Encoding)].NewDecoder()
}

// encoderWriter wraps SSH stdin so UTF-8 typed by the user is converted to the
// session charset before being sent to the remote host.
func (s *session) encoderWriter(w io.WriteCloser) io.WriteCloser {
	if isUTF8(s.cfg.Encoding) {
		return w
	}
	return &transformWriter{
		w: w,
		t: encodings[strings.ToUpper(s.cfg.Encoding)].NewEncoder(),
	}
}

// transformWriter encodes everything written through it.
type transformWriter struct {
	w io.WriteCloser
	t transform.Transformer
}

func (t *transformWriter) Write(p []byte) (int, error) {
	out, _, err := transform.Bytes(t.t, p)
	if err != nil {
		return 0, err
	}
	if _, err := t.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *transformWriter) Close() error { return t.w.Close() }

// pump forwards decoded remote output to the frontend.
func (s *session) pump() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.decoder.Read(buf)
		if n > 0 {
			s.emit(EventData, DataEvent{SessionID: s.cfg.SessionID, Data: string(buf[:n])})
		}
		if err != nil {
			return
		}
	}
}

// wait blocks until the remote shell exits and reports the reason.
func (s *session) wait() {
	err := s.shell.Wait()
	reason := "连接已关闭"
	normal := err == nil
	if err != nil {
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			// 远端 shell 正常退出（用户执行 exit / logout），不算连接故障。
			normal = true
			if code := exitErr.ExitStatus(); code != 0 {
				reason = fmt.Sprintf("退出码 %d", code)
			}
		} else {
			reason = err.Error()
		}
	}
	s.mu.Lock()
	s.closed = true
	s.reason = reason
	s.mu.Unlock()
	s.emit(EventClosed, ClosedEvent{SessionID: s.cfg.SessionID, Reason: reason, Normal: normal})
	s.close()
}

func (s *session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *session) write(data []byte) error {
	if s.isClosed() || s.stdin == nil {
		return fmt.Errorf("会话已关闭")
	}
	_, err := s.stdin.Write(data)
	return err
}

func (s *session) resize(cols, rows int) error {
	if s.isClosed() || s.shell == nil {
		return fmt.Errorf("会话已关闭")
	}
	return s.shell.WindowChange(rows, cols)
}

func (s *session) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.sftp != nil {
		_ = s.sftp.Close()
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.shell != nil {
		_ = s.shell.Close()
	}
	if s.client != nil {
		_ = s.client.Close()
	}
}

// sftpClient lazily creates the SFTP subsystem client.
func (s *session) sftpClient() (*sftp.Client, error) {
	s.sftpMu.Lock()
	defer s.sftpMu.Unlock()
	if s.sftp != nil {
		return s.sftp, nil
	}
	if s.client == nil {
		return nil, fmt.Errorf("会话未连接")
	}
	c, err := sftp.NewClient(s.client)
	if err != nil {
		return nil, fmt.Errorf("打开 SFTP 失败: %w", err)
	}
	s.sftp = c
	return c, nil
}

// resolveRemote turns a possibly relative path into an absolute one.
func (s *session) resolveRemote(p string) (string, error) {
	if p == "" {
		return s.remoteHome()
	}
	if strings.HasPrefix(p, "/") {
		return path.Clean(p), nil
	}
	home, err := s.remoteHome()
	if err != nil {
		return "", err
	}
	return path.Join(home, p), nil
}

func (s *session) remoteHome() (string, error) {
	c, err := s.sftpClient()
	if err != nil {
		return "", err
	}
	home, err := c.Getwd()
	if err != nil || home == "" {
		return "/", nil
	}
	return home, nil
}