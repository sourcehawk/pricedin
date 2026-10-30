package blob_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/blob"
	"github.com/sourcehawk/pricedin/server/internal/blob/blobtest"
)

func TestFSPassesTheContract(t *testing.T) {
	blobtest.Run(t, func(t *testing.T) blob.Store {
		s, err := blob.NewFS(t.TempDir())
		if err != nil {
			t.Fatalf("NewFS() error = %v", err)
		}
		return s
	})
}

func TestNewFSCreatesTheRootFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data", "blob")
	if _, err := blob.NewFS(root); err != nil {
		t.Fatalf("NewFS() error = %v", err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Errorf("root after NewFS() = %v, %v; want a folder", info, err)
	}
}

func TestNewFSRejectsAnEmptyRoot(t *testing.T) {
	if _, err := blob.NewFS(""); err == nil {
		t.Error("NewFS(\"\") error = nil, want an error")
	}
}

func TestFSStoresAKeyAsAFileBelowTheRoot(t *testing.T) {
	root := t.TempDir()
	s, err := blob.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS() error = %v", err)
	}
	if err := s.Put(context.Background(), "sec/a/b.json", bytes.NewBufferString("body")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "sec", "a", "b.json"))
	if err != nil || string(got) != "body" {
		t.Errorf("file = %q, %v; want %q", got, err, "body")
	}
}

func TestFSLeavesNoTemporaryFileAfterAFailedPut(t *testing.T) {
	root := t.TempDir()
	s, err := blob.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS() error = %v", err)
	}
	broken := readerFunc(func([]byte) (int, error) { return 0, errors.New("broken") })
	if err := s.Put(context.Background(), "sec/a.json", broken); err == nil {
		t.Fatal("Put() error = nil, want an error")
	}
	var files []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if len(files) != 0 {
		t.Errorf("files after a failed Put = %v, want none", files)
	}
}

func TestFSDoesNotFollowASymlinkOutOfTheRoot(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.json"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "sec")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	s, err := blob.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS() error = %v", err)
	}
	if _, err := s.Get(context.Background(), "sec/secret.json"); err == nil {
		t.Error("Get() through a symlink out of the root error = nil, want an error")
	}
	if err := s.Put(context.Background(), "sec/new.json", bytes.NewBufferString("x")); err == nil {
		t.Error("Put() through a symlink out of the root error = nil, want an error")
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }
