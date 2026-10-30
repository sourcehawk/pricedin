package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/sourcehawk/pricedin/server/internal/config"
)

// S3 is a Store in a bucket of S3 or of an S3-compatible service, such as
// Tigris or Cloudflare R2. The bucket must exist.
type S3 struct {
	client   *s3.Client
	uploader *transfermanager.Client
	bucket   string
}

// NewS3 returns a store in cfg.Bucket. It does not contact the service.
func NewS3(ctx context.Context, cfg config.BlobS3) (*S3, error) {
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.Region)}
	if cfg.AccessKeyID != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("blob s3: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.PathStyle
		// Several S3-compatible services reject the checksums that the SDK
		// adds by default.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	uploader := transfermanager.New(client, func(o *transfermanager.Options) {
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	return &S3{client: client, uploader: uploader, bucket: cfg.Bucket}, nil
}

// Put uploads r in parts when it is large. A failed upload is aborted, so
// the key keeps its earlier object.
func (s *S3) Put(ctx context.Context, key string, r io.Reader) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if _, err := s.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   r,
	}); err != nil {
		return fmt.Errorf("blob s3: put %s: %w", key, err)
	}
	return nil
}

// Get opens the object under key.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if isNotFound(err) {
		return nil, fmt.Errorf("blob s3: get %s: %w", key, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("blob s3: get %s: %w", key, err)
	}
	return out.Body, nil
}

// Exists reports whether an object is stored under key. A missing bucket
// also reads as false, because a HEAD response has no error code.
func (s *S3) Exists(ctx context.Context, key string) (bool, error) {
	if err := ValidateKey(key); err != nil {
		return false, err
	}
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("blob s3: exists %s: %w", key, err)
	}
	return true, nil
}

// List pages through the bucket listing.
func (s *S3) List(ctx context.Context, prefix string) ([]string, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return nil, err
	}
	var keys []string
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("blob s3: list %s: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
	}
	slices.Sort(keys)
	return keys, nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var noKey *types.NoSuchKey
	var notFound *types.NotFound
	if errors.As(err, &noKey) || errors.As(err, &notFound) {
		return true
	}
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound")
}
