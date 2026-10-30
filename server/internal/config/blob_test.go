package config_test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/config"
)

func TestLoadBlobDefaultsToTheFilesystem(t *testing.T) {
	got, err := config.Load(env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if diff := cmp.Diff(config.Blob{Backend: config.BlobBackendFS}, got.Blob); diff != "" {
		t.Errorf("Blob (-want +got):\n%s", diff)
	}
}

func TestLoadBlobReadsEveryVariable(t *testing.T) {
	got, err := config.Load(env(map[string]string{
		"PRICEDIN_BLOB_BACKEND":              "S3",
		"PRICEDIN_BLOB_DIR":                  "/data/blob",
		"PRICEDIN_BLOB_S3_BUCKET":            "pricedin-raw",
		"PRICEDIN_BLOB_S3_ENDPOINT":          "https://fly.storage.tigris.dev",
		"PRICEDIN_BLOB_S3_REGION":            "auto",
		"PRICEDIN_BLOB_S3_ACCESS_KEY_ID":     "key",
		"PRICEDIN_BLOB_S3_SECRET_ACCESS_KEY": "secret",
		"PRICEDIN_BLOB_S3_PATH_STYLE":        "true",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := config.Blob{
		Backend: config.BlobBackendS3,
		Dir:     "/data/blob",
		S3: config.BlobS3{
			Bucket: "pricedin-raw", Endpoint: "https://fly.storage.tigris.dev", Region: "auto",
			AccessKeyID: "key", SecretAccessKey: "secret", PathStyle: true,
		},
	}
	if diff := cmp.Diff(want, got.Blob); diff != "" {
		t.Errorf("Blob (-want +got):\n%s", diff)
	}
}

func TestLoadBlobRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		vars    map[string]string
		wantVar string
	}{
		{"unknown backend", map[string]string{"PRICEDIN_BLOB_BACKEND": "gcs"}, "PRICEDIN_BLOB_BACKEND"},
		{"path style that is not a boolean", map[string]string{"PRICEDIN_BLOB_S3_PATH_STYLE": "maybe"}, "PRICEDIN_BLOB_S3_PATH_STYLE"},
		{"endpoint without a scheme", map[string]string{"PRICEDIN_BLOB_S3_ENDPOINT": "localhost:8333"}, "PRICEDIN_BLOB_S3_ENDPOINT"},
		{"access key without a secret", map[string]string{"PRICEDIN_BLOB_S3_ACCESS_KEY_ID": "key"}, "PRICEDIN_BLOB_S3_SECRET_ACCESS_KEY"},
		{"secret without an access key", map[string]string{"PRICEDIN_BLOB_S3_SECRET_ACCESS_KEY": "secret"}, "PRICEDIN_BLOB_S3_ACCESS_KEY_ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(env(tt.vars))
			var verr *config.VarError
			if !errors.As(err, &verr) {
				t.Fatalf("Load() error = %v, want a *config.VarError", err)
			}
			if verr.Var != tt.wantVar {
				t.Errorf("VarError.Var = %q, want %q", verr.Var, tt.wantVar)
			}
		})
	}
}

func TestBlobRequire(t *testing.T) {
	tests := []struct {
		name    string
		blob    config.Blob
		wantVar string // empty when Require must succeed
	}{
		{"fs with a folder", config.Blob{Backend: config.BlobBackendFS, Dir: "/data/blob"}, ""},
		{"fs without a folder", config.Blob{Backend: config.BlobBackendFS}, "PRICEDIN_BLOB_DIR"},
		{"s3 with bucket and region", config.Blob{Backend: config.BlobBackendS3, S3: config.BlobS3{Bucket: "b", Region: "auto"}}, ""},
		{"s3 without a bucket", config.Blob{Backend: config.BlobBackendS3, S3: config.BlobS3{Region: "auto"}}, "PRICEDIN_BLOB_S3_BUCKET"},
		{"s3 without a region", config.Blob{Backend: config.BlobBackendS3, S3: config.BlobS3{Bucket: "b"}}, "PRICEDIN_BLOB_S3_REGION"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.blob.Require()
			if tt.wantVar == "" {
				if err != nil {
					t.Errorf("Require() error = %v, want nil", err)
				}
				return
			}
			var verr *config.VarError
			if !errors.As(err, &verr) || verr.Var != tt.wantVar || !errors.Is(err, config.ErrMissing) {
				t.Errorf("Require() error = %v, want a missing %s", err, tt.wantVar)
			}
		})
	}
}
