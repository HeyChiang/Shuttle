package sshcore

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LocalHome returns the current user's home directory.
func LocalHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取本地家目录失败: %w", err)
	}
	return home, nil
}

// LocalDrives lists the local filesystem roots so the user can switch disks
// (for example C:\ D:\ E:\) without typing a path.
func LocalDrives() ([]string, error) {
	if runtime.GOOS != "windows" {
		return []string{"/"}, nil
	}
	roots := make([]string, 0, 8)
	for c := 'A'; c <= 'Z'; c++ {
		root := fmt.Sprintf("%c:\\", c)
		if _, err := os.Stat(root); err == nil {
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("未找到可用的磁盘")
	}
	return roots, nil
}

// LocalList reads one local directory for the SFTP dual-pane browser.
func LocalList(dir string) ([]RemoteFile, error) {
	if strings.TrimSpace(dir) == "" {
		home, err := LocalHome()
		if err != nil {
			return nil, err
		}
		dir = home
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("解析本地路径失败: %w", err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("读取目录 %s 失败: %w", abs, err)
	}

	out := make([]RemoteFile, 0, len(entries))
	for _, entry := range entries {
		full := filepath.Join(abs, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		item := RemoteFile{
			Name:      entry.Name(),
			Path:      full,
			Size:      info.Size(),
			Mode:      info.Mode().String(),
			IsDir:     info.IsDir(),
			IsSymlink: entry.Type()&os.ModeSymlink != 0,
			ModTime:   info.ModTime().UnixMilli(),
		}
		if item.IsSymlink {
			if target, err := os.Readlink(full); err == nil {
				item.LinkTarget = target
			}
			if st, err := os.Stat(full); err == nil {
				item.IsDir = st.IsDir()
				item.Size = st.Size()
			}
		}
		out = append(out, item)
	}
	return out, nil
}
