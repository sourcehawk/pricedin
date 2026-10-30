package blob_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/blob"
	"github.com/sourcehawk/pricedin/server/internal/config"
)

func TestNewPicksTheConfiguredBackend(t *testing.T) {
	ctx := context.Background()
	fsStore, err := blob.New(ctx, config.Blob{Backend: config.BlobBackendFS, Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New(fs) error = %v", err)
	}
	if _, ok := fsStore.(*blob.FS); !ok {
		t.Errorf("New(fs) = %T, want *blob.FS", fsStore)
	}
	s3Store, err := blob.New(ctx, config.Blob{Backend: config.BlobBackendS3, S3: config.BlobS3{Bucket: "b", Region: "auto"}})
	if err != nil {
		t.Fatalf("New(s3) error = %v", err)
	}
	if _, ok := s3Store.(*blob.S3); !ok {
		t.Errorf("New(s3) = %T, want *blob.S3", s3Store)
	}
}

func TestNewRequiresTheBackendsSettings(t *testing.T) {
	for _, cfg := range []config.Blob{
		{Backend: config.BlobBackendFS},
		{Backend: config.BlobBackendS3, S3: config.BlobS3{Region: "auto"}},
	} {
		if _, err := blob.New(context.Background(), cfg); !errors.Is(err, config.ErrMissing) {
			t.Errorf("New(%+v) error = %v, want ErrMissing", cfg, err)
		}
	}
}
