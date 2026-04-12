package s3store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"etalon-server/internal/infra/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

var ErrObjectNotFound = errors.New("объект S3 не найден")

type ObjectInfo struct {
	Size         int64
	LastModified time.Time
}

type Service struct {
	client *s3.Client
}

type BucketStore struct {
	client *s3.Client
	bucket string
}

func New(ctx context.Context, cfg config.S3Config) (*Service, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("S3 endpoint обязателен")
	}
	if cfg.AccessKey == "" {
		return nil, errors.New("S3 access key обязателен")
	}
	if strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, errors.New("S3 secret key обязателен")
	}

	awsConfig, err := awscfg.LoadDefaultConfig(
		ctx,
		awscfg.WithRegion(cfg.Region),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("не удалось инициализировать S3-конфигурацию: %w", err)
	}

	return &Service{
		client: s3.NewFromConfig(awsConfig, func(options *s3.Options) {
			options.UsePathStyle = true
			options.BaseEndpoint = aws.String(cfg.Endpoint)
		}),
	}, nil
}

func (s *Service) Bucket(ctx context.Context, bucket string, ensureBucket bool) (*BucketStore, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("S3-сервис не инициализирован")
	}
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return nil, errors.New("S3 bucket обязателен")
	}

	store := &BucketStore{
		client: s.client,
		bucket: bucket,
	}
	if ensureBucket {
		if err := store.ensureBucket(ctx); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *BucketStore) GetObject(ctx context.Context, key string) ([]byte, error) {
	response, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, mapObjectStoreError(err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать объект %s: %w", key, err)
	}
	return body, nil
}

func (s *BucketStore) PutObject(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(defaultContentType(contentType)),
	})
	if err != nil {
		return fmt.Errorf("не удалось записать объект %s: %w", key, err)
	}
	return nil
}

func (s *BucketStore) PutFile(ctx context.Context, key string, filePath string, contentType string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("не удалось открыть файл %s: %w", filePath, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("не удалось получить размер файла %s: %w", filePath, err)
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          file,
		ContentLength: aws.Int64(info.Size()),
		ContentType:   aws.String(defaultContentType(contentType)),
	})
	if err != nil {
		return fmt.Errorf("не удалось загрузить файл %s в объект %s: %w", filePath, key, err)
	}
	return nil
}

func (s *BucketStore) StatObject(ctx context.Context, key string) (ObjectInfo, error) {
	response, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return ObjectInfo{}, mapObjectStoreError(err)
	}

	return ObjectInfo{
		Size:         aws.ToInt64(response.ContentLength),
		LastModified: aws.ToTime(response.LastModified),
	}, nil
}

func (s *BucketStore) DeleteObject(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("не удалось удалить объект %s: %w", key, err)
	}
	return nil
}

func (s *BucketStore) ensureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err == nil {
		return nil
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchBucket", "404":
			_, createErr := s.client.CreateBucket(ctx, &s3.CreateBucketInput{
				Bucket: aws.String(s.bucket),
			})
			if createErr != nil {
				return fmt.Errorf("не удалось создать S3 bucket %s: %w", s.bucket, createErr)
			}
			return nil
		}
	}

	return fmt.Errorf("не удалось проверить bucket %s: %w", s.bucket, err)
}

func mapObjectStoreError(err error) error {
	var noSuchKey *s3types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return fmt.Errorf("%w: %s", ErrObjectNotFound, noSuchKey.Error())
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchBucket":
			return fmt.Errorf("%w: %s", ErrObjectNotFound, apiErr.ErrorMessage())
		}
	}

	return err
}

func defaultContentType(contentType string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}
