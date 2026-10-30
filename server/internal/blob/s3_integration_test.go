//go:build integration

package blob_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sourcehawk/pricedin/server/internal/blob"
	"github.com/sourcehawk/pricedin/server/internal/blob/blobtest"
	"github.com/sourcehawk/pricedin/server/internal/config"
)

// seaweedImage is an S3-compatible server. MinIO stopped publishing
// community images in 2025, so the tests use SeaweedFS.
const seaweedImage = "chrislusf/seaweedfs:4.51@sha256:f7a49e3f3972ebed5be2ed4befa4a0c68eb7bdddba6e692d42729ceb545134b8"

// startS3 starts SeaweedFS with its S3 gateway and returns the endpoint.
// With no identities configured, SeaweedFS accepts any credentials.
func startS3(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.Run(ctx, seaweedImage,
		testcontainers.WithCmd("server", "-s3", "-dir=/data"),
		testcontainers.WithExposedPorts("8333/tcp"),
		testcontainers.WithWaitStrategyAndDeadline(2*time.Minute, wait.ForListeningPort("8333/tcp")),
	)
	testcontainers.CleanupContainer(t, c)
	if err != nil {
		t.Fatalf("start seaweedfs: %v", err)
	}
	endpoint, err := c.PortEndpoint(ctx, "8333/tcp", "http")
	if err != nil {
		t.Fatalf("seaweedfs endpoint: %v", err)
	}
	return endpoint
}

func s3Config(endpoint, bucket string) config.BlobS3 {
	return config.BlobS3{
		Bucket: bucket, Endpoint: endpoint, Region: "us-east-1",
		AccessKeyID: "test", SecretAccessKey: "test", PathStyle: true,
	}
}

// createBucket creates bucket with a plain S3 client. It retries until a
// write succeeds, because the volume store of SeaweedFS starts some seconds
// after the port opens.
func createBucket(t *testing.T, endpoint, bucket string) {
	t.Helper()
	ctx := context.Background()
	client := s3.New(s3.Options{
		BaseEndpoint: aws.String(endpoint),
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		UsePathStyle: true,
	})
	probe := func() error {
		if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
			var owned *types.BucketAlreadyOwnedByYou
			if !errors.As(err, &owned) {
				return err
			}
		}
		if _, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket), Key: aws.String("probe"), Body: strings.NewReader("ok"),
		}); err != nil {
			return err
		}
		_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("probe")})
		return err
	}
	deadline := time.Now().Add(time.Minute)
	for {
		err := probe()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("bucket %s not writable: %v", bucket, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestS3(t *testing.T) {
	endpoint := startS3(t)

	t.Run("PassesTheContract", func(t *testing.T) {
		var n atomic.Int64
		blobtest.Run(t, func(t *testing.T) blob.Store {
			bucket := fmt.Sprintf("contract-%d", n.Add(1))
			createBucket(t, endpoint, bucket)
			s, err := blob.NewS3(context.Background(), s3Config(endpoint, bucket))
			if err != nil {
				t.Fatalf("NewS3() error = %v", err)
			}
			return s
		})
	})

	t.Run("ListInAMissingBucketFails", func(t *testing.T) {
		s, err := blob.NewS3(context.Background(), s3Config(endpoint, "no-such-bucket"))
		if err != nil {
			t.Fatalf("NewS3() error = %v", err)
		}
		if _, err := s.List(context.Background(), ""); err == nil {
			t.Error("List() in a missing bucket error = nil, want an error")
		}
	})
}
