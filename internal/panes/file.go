package panes

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

var errNotPaneFile = errors.New("not a lid pane file")

// Root is lid's cache dir, which holds the pane dir. It is not created.
func Root() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "lid"), nil
}

// Path is where the pane dir is. It is not created.
func Path() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "panes"), nil
}

func Dir() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		return "", err
	}
	if err := os.Mkdir(root, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if _, err := realDir(root); err != nil {
		return "", err
	}
	dir := filepath.Join(root, "panes")
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := realDir(dir)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm() != 0o700 {
		return "", fmt.Errorf("%s must be mode 0700, is %04o", dir, info.Mode().Perm())
	}
	return dir, nil
}

func realDir(dir string) (os.FileInfo, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a plain directory", dir)
	}
	if !ownedByUser(info) {
		return nil, fmt.Errorf("%s is not owned by you", dir)
	}
	return info, nil
}

func WriteArgv(argv []string) (string, error) {
	data, err := json.Marshal(argv)
	if err != nil {
		return "", err
	}
	var name [16]byte
	if _, err := rand.Read(name[:]); err != nil {
		return "", err
	}
	return Create(hex.EncodeToString(name[:]), data)
}

// Create writes a new private file named name into the pane dir.
func Create(name string, body []byte) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if filepath.Dir(path) != dir {
		return "", fmt.Errorf("%q is not a file name", name)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return "", err
	}
	err = f.Chmod(0o600)
	if err == nil {
		_, err = f.Write(body)
	}
	if err == nil {
		err = checkFd(f)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// Verify reports whether path is still a private file in the pane dir that
// holds exactly body.
func Verify(path string, body []byte) error {
	f, err := openPrivate(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(len(body))+1))
	if err != nil {
		return err
	}
	if !bytes.Equal(data, body) {
		return fmt.Errorf("%s has changed", path)
	}
	return nil
}

func TakeArgv(path string) ([]string, error) {
	f, err := openPrivate(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := os.Remove(f.Name()); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var argv []string
	if err := json.Unmarshal(data, &argv); err != nil || len(argv) == 0 {
		return nil, errNotPaneFile
	}
	return argv, nil
}

func openPrivate(path string) (*os.File, error) {
	path = filepath.Clean(path)
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	if filepath.Dir(path) != dir {
		return nil, errNotPaneFile
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errNotPaneFile
	}
	if err := checkFd(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func checkFd(f *os.File) error {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByUser(info) {
		return errNotPaneFile
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Nlink != 1 {
		return errNotPaneFile
	}
	return nil
}

func ownedByUser(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
