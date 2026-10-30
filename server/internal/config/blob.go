package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// BlobBackend selects where raw downloads are kept.
type BlobBackend string

// The blob backends that PRICEDIN_BLOB_BACKEND accepts.
const (
	BlobBackendFS BlobBackend = "fs"
	BlobBackendS3 BlobBackend = "s3"
)

// Blob configures the store of raw downloads.
type Blob struct {
	// Backend is fs (default) or s3.
	Backend BlobBackend
	// Dir is the root folder of the fs backend.
	Dir string
	S3  BlobS3
}

// BlobS3 configures the S3-compatible backend.
type BlobS3 struct {
	Bucket string
	// Endpoint is the http:// or https:// base URL of an S3-compatible
	// service. Empty means AWS S3.
	Endpoint string
	Region   string
	// AccessKeyID and SecretAccessKey are both set or both empty. When
	// empty, the AWS SDK's default credential chain applies.
	AccessKeyID     string
	SecretAccessKey string
	// PathStyle puts the bucket in the URL path instead of the host name.
	PathStyle bool
}

const (
	envBlobBackend       = "PRICEDIN_BLOB_BACKEND"
	envBlobDir           = "PRICEDIN_BLOB_DIR"
	envBlobS3Bucket      = "PRICEDIN_BLOB_S3_BUCKET"
	envBlobS3Endpoint    = "PRICEDIN_BLOB_S3_ENDPOINT"
	envBlobS3Region      = "PRICEDIN_BLOB_S3_REGION"
	envBlobS3AccessKeyID = "PRICEDIN_BLOB_S3_ACCESS_KEY_ID"
	envBlobS3Secret      = "PRICEDIN_BLOB_S3_SECRET_ACCESS_KEY" //nolint:gosec // G101: a variable name, not a credential
	envBlobS3PathStyle   = "PRICEDIN_BLOB_S3_PATH_STYLE"
)

func loadBlob(getenv func(string) string) (Blob, []error) {
	cfg := Blob{
		Backend: BlobBackendFS,
		Dir:     getenv(envBlobDir),
		S3: BlobS3{
			Bucket:          getenv(envBlobS3Bucket),
			Endpoint:        getenv(envBlobS3Endpoint),
			Region:          getenv(envBlobS3Region),
			AccessKeyID:     getenv(envBlobS3AccessKeyID),
			SecretAccessKey: getenv(envBlobS3Secret),
		},
	}
	var errs []error

	switch backend := BlobBackend(strings.ToLower(getenv(envBlobBackend))); backend {
	case "":
	case BlobBackendFS, BlobBackendS3:
		cfg.Backend = backend
	default:
		errs = append(errs, &VarError{Var: envBlobBackend, Err: fmt.Errorf("unknown backend %q, want fs or s3", backend)})
	}

	if v := getenv(envBlobS3PathStyle); v != "" {
		pathStyle, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, &VarError{Var: envBlobS3PathStyle, Err: fmt.Errorf("want true or false, got %q", v)})
		}
		cfg.S3.PathStyle = pathStyle
	}

	if cfg.S3.Endpoint != "" {
		u, err := url.Parse(cfg.S3.Endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, &VarError{Var: envBlobS3Endpoint, Err: errors.New("want an http:// or https:// URL")})
		}
	}

	switch {
	case cfg.S3.AccessKeyID != "" && cfg.S3.SecretAccessKey == "":
		errs = append(errs, &VarError{Var: envBlobS3Secret, Err: fmt.Errorf("must be set with %s", envBlobS3AccessKeyID)})
	case cfg.S3.AccessKeyID == "" && cfg.S3.SecretAccessKey != "":
		errs = append(errs, &VarError{Var: envBlobS3AccessKeyID, Err: fmt.Errorf("must be set with %s", envBlobS3Secret)})
	}

	return cfg, errs
}

// Require returns a *VarError that wraps ErrMissing when a setting that the
// selected backend needs is not set: Dir for fs, Bucket and Region for s3.
func (b Blob) Require() error {
	var missing []string
	switch b.Backend {
	case BlobBackendS3:
		if b.S3.Bucket == "" {
			missing = append(missing, envBlobS3Bucket)
		}
		if b.S3.Region == "" {
			missing = append(missing, envBlobS3Region)
		}
	default:
		if b.Dir == "" {
			missing = append(missing, envBlobDir)
		}
	}
	errs := make([]error, len(missing))
	for i, v := range missing {
		errs[i] = &VarError{Var: v, Err: ErrMissing}
	}
	return errors.Join(errs...)
}
