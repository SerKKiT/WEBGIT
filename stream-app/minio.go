package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var (
	minioClient *minio.Client
	minioBucket string
)

// Инициализация MinIO клиента
func initMinIO() error {
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

	minioBucket = os.Getenv("MINIO_BUCKET")
	if minioBucket == "" {
		minioBucket = "hls-streams"
	}

	useSSL := os.Getenv("MINIO_USE_SSL") == "true"

	var err error
	minioClient, err = minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize MinIO client: %v", err)
	}

	// Создаем бакет если не существует
	ctx := context.Background()
	exists, err := minioClient.BucketExists(ctx, minioBucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %v", err)
	}

	if !exists {
		err = minioClient.MakeBucket(ctx, minioBucket, minio.MakeBucketOptions{})
		if err != nil {
			return fmt.Errorf("failed to create bucket: %v", err)
		}
		log.Printf("MinIO bucket '%s' created successfully", minioBucket)
	}

	// Устанавливаем политику для публичного чтения HLS файлов
	policy := fmt.Sprintf(`{
        "Version": "2012-10-17",
        "Statement": [
            {
                "Effect": "Allow",
                "Principal": {"AWS": "*"},
                "Action": "s3:GetObject",
                "Resource": "arn:aws:s3:::%s/*"
            }
        ]
    }`, minioBucket)

	err = minioClient.SetBucketPolicy(ctx, minioBucket, policy)
	if err != nil {
		log.Printf("Warning: failed to set bucket policy: %v", err)
	}

	log.Printf("MinIO initialized successfully with bucket: %s", minioBucket)
	return nil
}

// Загрузка файла в MinIO
func uploadToMinIO(streamID, localFilePath, objectName string) error {
	if minioClient == nil {
		return fmt.Errorf("MinIO client not initialized")
	}

	ctx := context.Background()
	objectPath := fmt.Sprintf("%s/%s", streamID, objectName)

	// Определяем Content-Type
	contentType := "application/octet-stream"
	if strings.HasSuffix(objectName, ".m3u8") {
		contentType = "application/vnd.apple.mpegurl"
	} else if strings.HasSuffix(objectName, ".ts") {
		contentType = "video/MP2T"
	}

	file, err := os.Open(localFilePath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %v", localFilePath, err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file stats: %v", err)
	}

	_, err = minioClient.PutObject(ctx, minioBucket, objectPath, file, stat.Size(), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("failed to upload to MinIO: %v", err)
	}

	return nil
}

// Очистка локальных HLS сегментов, оставляя максимум maxChunks
func cleanupLocalHLSSegments(streamID string, maxChunks int) {
	hlsDir := filepath.Join("hls", streamID)

	entries, err := os.ReadDir(hlsDir)
	if err != nil {
		return
	}

	var tsFiles []os.FileInfo

	// Собираем только .ts файлы
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ts") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}
		tsFiles = append(tsFiles, info)
	}

	// Если сегментов не больше лимита - ничего не делаем
	if len(tsFiles) <= maxChunks {
		return
	}

	// Сортируем по времени модификации (старые в начале)
	sort.Slice(tsFiles, func(i, j int) bool {
		return tsFiles[i].ModTime().Before(tsFiles[j].ModTime())
	})

	// Удаляем самые старые сегменты
	toDelete := tsFiles[:len(tsFiles)-maxChunks]

	for _, file := range toDelete {
		filePath := filepath.Join(hlsDir, file.Name())
		if err := os.Remove(filePath); err != nil {
			log.Printf("Failed to remove local HLS segment %s: %v", filePath, err)
		}
	}
}

// Мониторинг и загрузка HLS файлов с локальной очисткой
func startHLSUploader(streamID string) {
	hlsDir := filepath.Join("hls", streamID)
	maxLocalChunks := 5 // Максимум 5 сегментов локально

	go func() {
		uploadCycle := 0

		for {
			// Проверяем, активен ли еще стрим
			streamsMux.Lock()
			_, exists := activeStreams[streamID]
			streamsMux.Unlock()

			if !exists {
				log.Printf("Stopping HLS uploader for stream %s", streamID)
				break
			}

			// Загружаем новые файлы в MinIO
			err := uploadHLSFiles(streamID, hlsDir)
			if err != nil {
				log.Printf("Error uploading HLS files for stream %s: %v", streamID, err)
			}

			// Каждый цикл очищаем локальные старые сегменты
			cleanupLocalHLSSegments(streamID, maxLocalChunks)

			// Каждые 10 циклов (~20 секунд) обновляем плейлист в MinIO
			uploadCycle++
			if uploadCycle%10 == 0 {
				forceUploadPlaylist(streamID, hlsDir)
			}

			time.Sleep(2 * time.Second)
		}

		// Финальная очистка при остановке стрима
		log.Printf("Final cleanup for stream %s", streamID)
		cleanupLocalHLSSegments(streamID, 0) // Удаляем все локальные сегменты
	}()
}

// Принудительная загрузка плейлиста в MinIO
func forceUploadPlaylist(streamID, hlsDir string) {
	playlistPath := filepath.Join(hlsDir, "stream.m3u8")

	if _, err := os.Stat(playlistPath); err == nil {
		err := uploadToMinIO(streamID, playlistPath, "stream.m3u8")
		if err != nil {
			log.Printf("Failed to force upload playlist for stream %s: %v", streamID, err)
		}
	}
}

// Загрузка HLS файлов в MinIO
func uploadHLSFiles(streamID, hlsDir string) error {
	files, err := os.ReadDir(hlsDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()
		if !strings.HasSuffix(fileName, ".m3u8") && !strings.HasSuffix(fileName, ".ts") {
			continue
		}

		localPath := filepath.Join(hlsDir, fileName)

		// Для .ts сегментов проверяем, не загружен ли уже
		if strings.HasSuffix(fileName, ".ts") {
			if isFileUploaded(streamID, fileName, localPath) {
				continue
			}
		}

		// Для .m3u8 плейлистов всегда перезагружаем
		err := uploadToMinIO(streamID, localPath, fileName)
		if err != nil {
			log.Printf("Failed to upload %s: %v", fileName, err)
			continue
		}
	}

	return nil
}

// Простая проверка, загружен ли файл
func isFileUploaded(streamID, fileName, localPath string) bool {
	ctx := context.Background()
	objectPath := fmt.Sprintf("%s/%s", streamID, fileName)

	localStat, err := os.Stat(localPath)
	if err != nil {
		return false
	}

	objInfo, err := minioClient.StatObject(ctx, minioBucket, objectPath, minio.StatObjectOptions{})
	if err != nil {
		return false
	}

	return objInfo.Size == localStat.Size() && !localStat.ModTime().After(objInfo.LastModified)
}

// Очистка файлов стрима из MinIO
func cleanupStreamFromMinIO(streamID string) error {
	if minioClient == nil {
		return nil
	}

	ctx := context.Background()
	objectsCh := minioClient.ListObjects(ctx, minioBucket, minio.ListObjectsOptions{
		Prefix:    streamID + "/",
		Recursive: true,
	})

	for object := range objectsCh {
		if object.Err != nil {
			log.Printf("Error listing objects for stream %s: %v", streamID, object.Err)
			continue
		}

		err := minioClient.RemoveObject(ctx, minioBucket, object.Key, minio.RemoveObjectOptions{})
		if err != nil {
			log.Printf("Failed to remove object %s: %v", object.Key, err)
		}
	}

	return nil
}

// Очистка локальной папки HLS при удалении задачи
func cleanupLocalHLSFolder(streamID string) error {
	hlsDir := filepath.Join("hls", streamID)

	// Проверяем, существует ли папка
	if _, err := os.Stat(hlsDir); os.IsNotExist(err) {
		log.Printf("HLS folder for stream %s does not exist, nothing to cleanup", streamID)
		return nil
	}

	// Удаляем всю папку со всем содержимым
	err := os.RemoveAll(hlsDir)
	if err != nil {
		return fmt.Errorf("failed to remove HLS folder %s: %v", hlsDir, err)
	}

	log.Printf("Removed local HLS folder: %s", hlsDir)
	return nil
}
