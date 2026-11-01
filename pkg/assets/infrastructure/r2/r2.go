// Package r2 implements the asset store port with private Cloudflare R2.
package r2

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/manovaspace/orbit-cli/pkg/assets/application"
)

// Config describes one R2 bucket.
type Config struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Endpoint        string
}

// DefaultEnvPath is the R2 credentials file.
func DefaultEnvPath() string {
	if p := strings.TrimSpace(os.Getenv("ORBIT_R2_ENV")); p != "" {
		return p
	}
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".config", "orbit", "r2.env")
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "orbit", "r2.env")
}

// LoadEnv reads and validates an R2 credentials file.
func LoadEnv(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer func() { _ = f.Close() }()

	vals := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vals[strings.TrimSpace(key)] = strings.TrimSpace(strings.Trim(val, `"'`))
	}
	if err := sc.Err(); err != nil {
		return Config{}, err
	}

	cfg := Config{
		AccountID:       vals["R2_ACCOUNT_ID"],
		AccessKeyID:     vals["R2_ACCESS_KEY_ID"],
		SecretAccessKey: vals["R2_SECRET_ACCESS_KEY"],
		Bucket:          vals["R2_BUCKET"],
		Endpoint:        vals["R2_ENDPOINT"],
	}
	if cfg.Bucket == "" {
		cfg.Bucket = "manova-assets"
	}
	if cfg.AccountID == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return Config{}, fmt.Errorf("%s: R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, and R2_SECRET_ACCESS_KEY are required", path)
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)
	}
	return cfg, nil
}

// Store is the S3-compatible R2 object store.
type Store struct {
	client *s3.Client
	bucket string
}

// NewStore builds a Store over cfg.
func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
		config.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("r2 aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
	})
	return &Store{client: client, bucket: cfg.Bucket}, nil
}

// OpenStore loads the default credentials file and builds a Store.
func OpenStore(ctx context.Context) (application.Store, error) {
	cfg, err := LoadEnv(DefaultEnvPath())
	if err != nil {
		return nil, err
	}
	return NewStore(ctx, cfg)
}

func objectKey(sha256 string) string {
	return "objects/" + sha256
}

// Put stores one object.
func (s *Store) Put(ctx context.Context, sha256 string, r io.Reader, size int64, contentType, sourcePath string) error {
	in := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(objectKey(sha256)),
		Body:          r,
		ContentLength: aws.Int64(size),
		Metadata:      map[string]string{"path": sourcePath},
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	if _, err := s.client.PutObject(ctx, in); err != nil {
		return fmt.Errorf("r2 put %s: %w", sha256, err)
	}
	return nil
}

// Get retrieves one object.
func (s *Store) Get(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey(sha256)),
	})
	if err != nil {
		return nil, fmt.Errorf("r2 get %s: %w", sha256, err)
	}
	return out.Body, nil
}

// Exists reports whether the object is present.
func (s *Store) Exists(ctx context.Context, sha256 string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey(sha256)),
	})
	if err != nil {
		var nsk *types.NotFound
		var nfe *types.NoSuchKey
		if errors.As(err, &nsk) || errors.As(err, &nfe) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Ping verifies that the bucket is reachable.
func (s *Store) Ping(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err != nil {
		return fmt.Errorf("r2 head bucket %s: %w", s.bucket, err)
	}
	return nil
}
