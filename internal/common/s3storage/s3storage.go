package s3storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config holds S3-compatible storage configuration.
type Config struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	BasePath  string // optional prefix for all keys
	UseSSL    bool
}

// Client wraps an S3 client for software repository operations.
type Client struct {
	s3     *s3.Client
	bucket string
	base   string
	presig *s3.PresignClient
}

// New creates a new S3 storage client.
func New(cfg Config) (*Client, error) {
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	opts := []func(*s3.Options){
		func(o *s3.Options) {
			o.Region = region
			o.Credentials = credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
			o.UsePathStyle = true // required for MinIO and most S3-compatible storage
		},
	}

	if cfg.Endpoint != "" {
		opts = append(opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}

	client := s3.New(s3.Options{}, opts...)
	presign := s3.NewPresignClient(client)

	return &Client{
		s3:     client,
		bucket: cfg.Bucket,
		base:   cfg.BasePath,
		presig: presign,
	}, nil
}

// fullKey returns the full S3 key with optional base path prefix.
func (c *Client) fullKey(key string) string {
	if c.base == "" {
		return key
	}
	return c.base + "/" + key
}

// Upload uploads a file to S3.
func (c *Client) Upload(ctx context.Context, key string, body io.Reader, contentType string) error {
	input := &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(c.fullKey(key)),
		Body:        body,
		ContentType: aws.String(contentType),
	}

	_, err := c.s3.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("s3 upload %s: %w", key, err)
	}
	return nil
}

// Download downloads a file from S3.
func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.fullKey(key)),
	}

	out, err := c.s3.GetObject(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("s3 download %s: %w", key, err)
	}
	return out.Body, nil
}

// Delete deletes a file from S3.
func (c *Client) Delete(ctx context.Context, key string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.fullKey(key)),
	}

	_, err := c.s3.DeleteObject(ctx, input)
	if err != nil {
		return fmt.Errorf("s3 delete %s: %w", key, err)
	}
	return nil
}

// PresignedGetURL generates a pre-signed URL for downloading a file.
func (c *Client) PresignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.fullKey(key)),
	}

	out, err := c.presig.PresignGetObject(ctx, input, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("s3 presign %s: %w", key, err)
	}
	return out.URL, nil
}

// Exists checks if a key exists in the bucket.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.fullKey(key)),
	}

	_, err := c.s3.HeadObject(ctx, input)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// TestConnection verifies the S3 connection by listing bucket contents.
func (c *Client) TestConnection(ctx context.Context) error {
	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(c.bucket),
		MaxKeys: aws.Int32(1),
	}

	_, err := c.s3.ListObjectsV2(ctx, input)
	if err != nil {
		return fmt.Errorf("s3 connection test failed: %w", err)
	}
	return nil
}

// ListObjects lists objects with a given prefix.
func (c *Client) ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(c.fullKey(prefix)),
	}

	out, err := c.s3.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("s3 list %s: %w", prefix, err)
	}

	var objects []ObjectInfo
	for _, obj := range out.Contents {
		objects = append(objects, ObjectInfo{
			Key:          aws.ToString(obj.Key),
			Size:         aws.ToInt64(obj.Size),
			LastModified: aws.ToTime(obj.LastModified),
		})
	}
	return objects, nil
}

// ObjectInfo holds basic information about an S3 object.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}
