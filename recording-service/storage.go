package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type StorageManager struct {
	client    *minio.Client
	bucket    string
	hlsBucket string // ✅ Bucket для HLS файлов
}

type VODPaths struct {
	MP4URL       string
	ThumbnailURL string
}

func NewStorageManager() (*StorageManager, error) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "minio:9000"
	}

	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "minioadmin"
	}

	secretKey := os.Getenv("MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "minioadmin123"
	}

	bucket := os.Getenv("MINIO_BUCKET")
	if bucket == "" {
		bucket = "recordings"
	}

	// ✅ Добавить HLS bucket
	hlsBucket := os.Getenv("MINIO_HLS_BUCKET")
	if hlsBucket == "" {
		hlsBucket = "hls-streams"
	}

	log.Printf("📁 Connecting to MinIO: %s, VOD bucket: %s, HLS bucket: %s", endpoint, bucket, hlsBucket)

	// Инициализация MinIO клиента
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false, // HTTP для локальной разработки
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	sm := &StorageManager{
		client:    client,
		bucket:    bucket,
		hlsBucket: hlsBucket, // ✅ Инициализировать HLS bucket
	}

	// ✅ Создать оба buckets
	if err := sm.ensureBucket(); err != nil {
		return nil, fmt.Errorf("failed to ensure VOD bucket: %w", err)
	}

	if err := sm.ensureHLSBucket(); err != nil {
		return nil, fmt.Errorf("failed to ensure HLS bucket: %w", err)
	}

	log.Println("✅ MinIO storage manager initialized")
	return sm, nil
}

func (sm *StorageManager) ensureBucket() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := sm.client.BucketExists(ctx, sm.bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		log.Printf("📁 Creating VOD bucket: %s", sm.bucket)
		if err := sm.client.MakeBucket(ctx, sm.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
		log.Printf("✅ Created VOD bucket: %s", sm.bucket)
	}

	return nil
}

// ✅ Новый метод для HLS bucket
func (sm *StorageManager) ensureHLSBucket() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := sm.client.BucketExists(ctx, sm.hlsBucket)
	if err != nil {
		return fmt.Errorf("failed to check HLS bucket existence: %w", err)
	}

	if !exists {
		log.Printf("📁 Creating HLS bucket: %s", sm.hlsBucket)
		if err := sm.client.MakeBucket(ctx, sm.hlsBucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create HLS bucket: %w", err)
		}
		log.Printf("✅ Created HLS bucket: %s", sm.hlsBucket)
	}

	return nil
}

// ✅ Метод для скачивания HLS файлов из MinIO
func (sm *StorageManager) DownloadHLSFiles(streamID string) (string, error) {
	log.Printf("📥 Downloading HLS files for stream: %s", streamID)

	// Создать временную папку для HLS файлов
	tempDir := fmt.Sprintf("/tmp/hls_%s_%d", streamID, time.Now().Unix())
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	prefix := fmt.Sprintf("%s/", streamID)

	// Список объектов в HLS bucket
	objectCh := sm.client.ListObjects(ctx, sm.hlsBucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	downloadCount := 0
	playlistFound := false

	for object := range objectCh {
		if object.Err != nil {
			return "", fmt.Errorf("error listing HLS objects: %w", object.Err)
		}

		// Определить локальный путь
		relativePath := strings.TrimPrefix(object.Key, prefix)
		if relativePath == "" {
			continue // Пропустить папку
		}

		localPath := filepath.Join(tempDir, relativePath)

		// Создать папки если нужно
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			log.Printf("⚠️ Failed to create dir for %s: %v", localPath, err)
			continue
		}

		// Скачать файл
		if err := sm.client.FGetObject(ctx, sm.hlsBucket, object.Key, localPath, minio.GetObjectOptions{}); err != nil {
			log.Printf("⚠️ Failed to download %s: %v", object.Key, err)
			continue
		}

		downloadCount++
		log.Printf("📥 Downloaded: %s → %s", object.Key, relativePath)

		// Проверить что это плейлист
		if strings.HasSuffix(relativePath, ".m3u8") {
			playlistFound = true
		}
	}

	if downloadCount == 0 {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("no HLS files found for stream %s in bucket %s", streamID, sm.hlsBucket)
	}

	if !playlistFound {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("no HLS playlist (.m3u8) found for stream %s", streamID)
	}

	log.Printf("✅ Downloaded %d HLS files for stream %s to %s", downloadCount, streamID, tempDir)
	return tempDir, nil
}

// ✅ Метод для получения пути к HLS плейлисту
func (sm *StorageManager) GetHLSPlaylistPath(tempDir string) (string, error) {
	// Поиск .m3u8 файлов в временной папке
	playlistPaths := []string{
		filepath.Join(tempDir, "stream.m3u8"),
		filepath.Join(tempDir, "playlist.m3u8"),
		filepath.Join(tempDir, "index.m3u8"),
	}

	for _, path := range playlistPaths {
		if _, err := os.Stat(path); err == nil {
			log.Printf("📄 Found HLS playlist: %s", path)
			return path, nil
		}
	}

	// Поиск любого .m3u8 файла
	files, err := filepath.Glob(filepath.Join(tempDir, "*.m3u8"))
	if err == nil && len(files) > 0 {
		log.Printf("📄 Found HLS playlist: %s", files[0])
		return files[0], nil
	}

	return "", fmt.Errorf("no HLS playlist found in %s", tempDir)
}

// ✅ Метод для очистки временных HLS файлов
func (sm *StorageManager) CleanupHLSFiles(tempDir string) {
	if tempDir != "" {
		if err := os.RemoveAll(tempDir); err != nil {
			log.Printf("⚠️ Failed to cleanup HLS temp dir %s: %v", tempDir, err)
		} else {
			log.Printf("🧹 Cleaned up HLS temp dir: %s", tempDir)
		}
	}
}

func (sm *StorageManager) UploadVODFiles(streamID, mp4Path, thumbnailPath string) (VODPaths, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var paths VODPaths

	// ✅ Загрузить MP4 файл
	if mp4Path != "" {
		mp4ObjectName := fmt.Sprintf("vod/%s/video.mp4", streamID)

		// Проверить что файл существует
		if _, err := os.Stat(mp4Path); os.IsNotExist(err) {
			return paths, fmt.Errorf("MP4 file not found: %s", mp4Path)
		}

		info, err := sm.client.FPutObject(ctx, sm.bucket, mp4ObjectName, mp4Path, minio.PutObjectOptions{
			ContentType: "video/mp4",
		})
		if err != nil {
			return paths, fmt.Errorf("failed to upload MP4: %w", err)
		}

		paths.MP4URL = fmt.Sprintf("/%s/%s", sm.bucket, mp4ObjectName)
		log.Printf("📁 Uploaded MP4: %s (size: %d bytes)", mp4ObjectName, info.Size)
	}

	// ✅ Загрузить thumbnail
	if thumbnailPath != "" {
		if _, err := os.Stat(thumbnailPath); err == nil {
			thumbObjectName := fmt.Sprintf("vod/%s/thumbnail.jpg", streamID)

			info, err := sm.client.FPutObject(ctx, sm.bucket, thumbObjectName, thumbnailPath, minio.PutObjectOptions{
				ContentType: "image/jpeg",
			})
			if err != nil {
				log.Printf("⚠️ Failed to upload thumbnail (non-critical): %v", err)
			} else {
				paths.ThumbnailURL = fmt.Sprintf("/%s/%s", sm.bucket, thumbObjectName)
				log.Printf("📁 Uploaded thumbnail: %s (size: %d bytes)", thumbObjectName, info.Size)
			}
		} else {
			log.Printf("⚠️ Thumbnail file not found: %s", thumbnailPath)
		}
	}

	return paths, nil
}

func (sm *StorageManager) GetPresignedURL(objectName string, expiry time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url, err := sm.client.PresignedGetObject(ctx, sm.bucket, objectName, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return url.String(), nil
}

func (sm *StorageManager) DeleteVODFiles(streamID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Список файлов для удаления
	objectNames := []string{
		fmt.Sprintf("vod/%s/video.mp4", streamID),
		fmt.Sprintf("vod/%s/thumbnail.jpg", streamID),
	}

	for _, objectName := range objectNames {
		if err := sm.client.RemoveObject(ctx, sm.bucket, objectName, minio.RemoveObjectOptions{}); err != nil {
			log.Printf("⚠️ Failed to delete %s: %v", objectName, err)
		} else {
			log.Printf("🗑️ Deleted: %s", objectName)
		}
	}

	return nil
}

// Утилитарная функция для очистки локальных временных файлов
func (sm *StorageManager) CleanupLocalFiles(files ...string) {
	for _, file := range files {
		if file != "" {
			if err := os.Remove(file); err != nil {
				log.Printf("⚠️ Failed to cleanup local file %s: %v", file, err)
			} else {
				log.Printf("🧹 Cleaned up local file: %s", file)
			}
		}
	}
}

func (sm *StorageManager) TestConnection() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exists, err := sm.client.BucketExists(ctx, sm.bucket)
	if err != nil {
		return fmt.Errorf("failed to test MinIO connection: %w", err)
	}

	if !exists {
		return fmt.Errorf("bucket %s does not exist", sm.bucket)
	}

	log.Printf("✅ MinIO connection test successful")
	return nil
}
