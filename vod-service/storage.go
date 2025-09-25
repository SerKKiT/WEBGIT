package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Storage struct {
	client     *minio.Client
	bucketName string
}

func NewStorage() (*Storage, error) {
	endpoint := getEnv("MINIO_ENDPOINT", "localhost:9000")
	accessKey := getEnv("MINIO_ACCESS_KEY", "minioadmin")
	secretKey := getEnv("MINIO_SECRET_KEY", "minioadmin123")
	bucketName := getEnv("MINIO_BUCKET", "recordings")

	// Создание клиента MinIO
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false, // Используем HTTP для локальной разработки
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	storage := &Storage{
		client:     client,
		bucketName: bucketName,
	}

	return storage, nil
}

func (s *Storage) TestConnection() error {
	ctx := context.Background()

	// Проверка доступности MinIO
	_, err := s.client.ListBuckets(ctx)
	if err != nil {
		return fmt.Errorf("MinIO connection failed: %w", err)
	}

	// Создание bucket если не существует
	exists, err := s.client.BucketExists(ctx, s.bucketName)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		err = s.client.MakeBucket(ctx, s.bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
		log.Printf("✅ Created MinIO bucket: %s", s.bucketName)
	}

	log.Println("✅ MinIO connection test successful")
	return nil
}

func (s *Storage) GetPresignedStreamURL(ctx context.Context, streamID string, expiry time.Duration) (string, error) {
	objectName := fmt.Sprintf("streams/%s/video.m3u8", streamID)

	url, err := s.client.PresignedGetObject(ctx, s.bucketName, objectName, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return url.String(), nil
}

func (s *Storage) GetPresignedThumbnailURL(ctx context.Context, streamID string, expiry time.Duration) (string, error) {
	objectName := fmt.Sprintf("streams/%s/thumbnail.jpg", streamID)

	url, err := s.client.PresignedGetObject(ctx, s.bucketName, objectName, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned thumbnail URL: %w", err)
	}

	return url.String(), nil
}
