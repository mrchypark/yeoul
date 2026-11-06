//go:build linux || darwin

package yeoul

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestKubernetesDRProvisionBucket(t *testing.T) {
	if os.Getenv("YEOUL_K8S_DR_ENABLE") != "1" {
		t.Skip("set YEOUL_K8S_DR_ENABLE=1 to provision a Kubernetes DR test bucket")
	}
	endpoint := os.Getenv("YEOUL_K8S_DR_S3_ENDPOINT")
	bucket := os.Getenv("YEOUL_K8S_DR_S3_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Fatal("YEOUL_K8S_DR_S3_ENDPOINT and YEOUL_K8S_DR_S3_BUCKET are required")
	}
	if err := requireLoopbackHTTP(endpoint); err != nil {
		t.Fatalf("YEOUL_K8S_DR_S3_ENDPOINT must be an explicit loopback HTTP URL: %v", err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	client, err := minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4("yeoul-local-test-access", "yeoul-local-test-secret", ""),
		Secure: false, Region: "us-east-1", BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("refusing to reuse existing test bucket %q", bucket)
	}
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal(err)
	}
}
