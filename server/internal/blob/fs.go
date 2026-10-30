package blob

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"syscall"
)

// tmpDir holds files that Put is writing. Keys cannot start with a dot, so
// no key reaches it.
const tmpDir = ".tmp"

// FS is a Store in a local folder. Each key is a file at the key's path
// below the root. It does not follow symbolic links out of the root.
type FS struct {
	root string
}

// NewFS returns a store in the folder root and creates the folder when it
// does not exist.
func NewFS(root string) (*FS, error) {
	if root == "" {
		return nil, errors.New("blob fs: empty root folder")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("blob fs: %w", err)
	}
	return &FS{root: root}, nil
}

// open returns the root as an os.Root, which refuses paths that leave it.
func (s *FS) open() (*os.Root, error) {
	r, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, fmt.Errorf("blob fs: %w", err)
	}
	return r, nil
}

// Put writes to a temporary file and renames it over the key, so a reader
// never sees a partial object.
func (s *FS) Put(ctx context.Context, key string, r io.Reader) (err error) {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("blob fs: put %s: %w", key, err)
	}
	root, err := s.open()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	if err := root.MkdirAll(tmpDir, 0o750); err != nil {
		return fmt.Errorf("blob fs: put %s: %w", key, err)
	}
	if dir := path.Dir(key); dir != "." {
		if err := root.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("blob fs: put %s: %w", key, err)
		}
	}
	tmp := path.Join(tmpDir, rand.Text())
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("blob fs: put %s: %w", key, err)
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = root.Remove(tmp)
		}
	}()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("blob fs: put %s: %w", key, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("blob fs: put %s: %w", key, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("blob fs: put %s: %w", key, err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("blob fs: put %s: %w", key, err)
	}
	if err := root.Rename(tmp, key); err != nil {
		return fmt.Errorf("blob fs: put %s: %w", key, err)
	}
	return nil
}

// Get opens the file of key.
func (s *FS) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	root, err := s.open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(key)
	if isAbsent(err) {
		return nil, fmt.Errorf("blob fs: get %s: %w", key, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("blob fs: get %s: %w", key, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("blob fs: get %s: %w", key, err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("blob fs: get %s: %w", key, ErrNotFound)
	}
	return f, nil
}

// Exists reports whether key is a regular file.
func (s *FS) Exists(_ context.Context, key string) (bool, error) {
	if err := ValidateKey(key); err != nil {
		return false, err
	}
	root, err := s.open()
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Stat(key)
	if isAbsent(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("blob fs: exists %s: %w", key, err)
	}
	return info.Mode().IsRegular(), nil
}

// List walks the deepest folder that the prefix names in full.
func (s *FS) List(ctx context.Context, prefix string) ([]string, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return nil, err
	}
	root, err := s.open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	start := "."
	if i := strings.LastIndex(prefix, "/"); i >= 0 {
		start = prefix[:i]
	}
	var keys []string
	err = fs.WalkDir(root.FS(), start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == start && isAbsent(err) {
				return fs.SkipAll
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && p != "." {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && strings.HasPrefix(p, prefix) {
			keys = append(keys, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("blob fs: list %s: %w", prefix, err)
	}
	slices.Sort(keys)
	return keys, nil
}

// isAbsent reports whether err means that no file is at the path: the path
// is missing, or it runs through a file, which a key or a prefix can do.
func isAbsent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}
