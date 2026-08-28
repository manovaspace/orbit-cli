package assets

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Store struct {
	client *s3.Client
	bucket string
}

func NewS3Store(ctx context.Context, cfg R2Config) (*S3Store, error) {
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
	return &S3Store{client: client, bucket: cfg.Bucket}, nil
}

func objectKey(sha256 string) string {
	return "objects/" + sha256
}

func (s *S3Store) Put(ctx context.Context, sha256 string, r io.Reader, size int64, contentType, sourcePath string) error {
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
	_, err := s.client.PutObject(ctx, in)
	if err != nil {
		return fmt.Errorf("r2 put %s: %w", sha256, err)
	}
	return nil
}

func (s *S3Store) Get(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey(sha256)),
	})
	if err != nil {
		return nil, fmt.Errorf("r2 get %s: %w", sha256, err)
	}
	return out.Body, nil
}

func (s *S3Store) Exists(ctx context.Context, sha256 string) (bool, error) {
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

func (s *S3Store) Ping(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err != nil {
		return fmt.Errorf("r2 head bucket %s: %w", s.bucket, err)
	}
	return nil
}

func OpenStore(ctx context.Context) (Store, error) {
	cfg, err := LoadR2Env(DefaultR2EnvPath())
	if err != nil {
		return nil, err
	}
	return NewS3Store(ctx, cfg)
}
