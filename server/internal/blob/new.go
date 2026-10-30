package blob

import (
	"context"

	"github.com/sourcehawk/pricedin/server/internal/config"
)

// New returns the store that cfg selects. It returns the error of
// cfg.Require when a setting that the backend needs is missing.
func New(ctx context.Context, cfg config.Blob) (Store, error) {
	if err := cfg.Require(); err != nil {
		return nil, err
	}
	if cfg.Backend == config.BlobBackendS3 {
		return NewS3(ctx, cfg.S3)
	}
	return NewFS(cfg.Dir)
}
