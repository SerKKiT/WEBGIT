package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type StorageManager struct {
	minioClient *minio.Client
	bucketName  string
	vodBucket   string
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

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, err
	}

	sm := &StorageManager{
		minioClient: client,
		bucketName:  "hls-streams", // Исходные HLS файлы
		vodBucket:   "recordings",  // VOD файлы
	}

	return sm, nil
}

// ✅ ОРИГИНАЛЬНАЯ ФУНКЦИЯ (для обратной совместимости)
func (sm *StorageManager) DownloadHLSFiles(streamID string) (string, error) {
	return sm.DownloadHLSFilesWithRetry(streamID, 1)
}

// ✅ НОВАЯ ФУНКЦИЯ: скачивание с retry
func (sm *StorageManager) DownloadHLSFilesWithRetry(streamID string, attempt int) (string, error) {
	// Создаем уникальную временную директорию
	tempDir := fmt.Sprintf("/tmp/hls_%s_%d_%d", streamID, attempt, time.Now().Unix())
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp dir: %v", err)
	}

	ctx := context.Background()

	// Список объектов в папке стрима
	objectCh := sm.minioClient.ListObjects(ctx, sm.bucketName, minio.ListObjectsOptions{
		Prefix:    streamID + "/",
		Recursive: true,
	})

	downloadCount := 0

	for object := range objectCh {
		if object.Err != nil {
			log.Printf("❌ Error listing objects: %v", object.Err)
			continue
		}

		// Извлекаем имя файла
		fileName := filepath.Base(object.Key)
		if fileName == "" || fileName == streamID {
			continue
		}

		// Скачиваем только HLS файлы
		if !strings.HasSuffix(fileName, ".ts") && !strings.HasSuffix(fileName, ".m3u8") {
			continue
		}

		localPath := filepath.Join(tempDir, fileName)

		// Скачиваем файл
		err := sm.minioClient.FGetObject(ctx, sm.bucketName, object.Key, localPath, minio.GetObjectOptions{})
		if err != nil {
			log.Printf("❌ Failed to download %s: %v", object.Key, err)
			continue
		}

		downloadCount++
		log.Printf("📥 Downloaded: %s (%d bytes)", fileName, object.Size)
	}

	if downloadCount == 0 {
		os.RemoveAll(tempDir) // Очистить пустую директорию
		return "", fmt.Errorf("no HLS files downloaded from MinIO")
	}

	log.Printf("✅ Downloaded %d HLS files to: %s", downloadCount, tempDir)
	return tempDir, nil
}

// ✅ НОВАЯ ФУНКЦИЯ: fallback скачивание
func (sm *StorageManager) DownloadHLSFilesFromFallback(streamID string) (string, error) {
	log.Printf("🔄 Using fallback method for stream: %s", streamID)

	// Создаем временную директорию для fallback
	tempDir := fmt.Sprintf("/tmp/hls_%s_fallback_%d", streamID, time.Now().Unix())
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create fallback temp dir: %v", err)
	}

	// Возможные пути к shared volume
	possiblePaths := []string{
		fmt.Sprintf("/shared/hls/%s", streamID),     // Docker volume mount
		fmt.Sprintf("/app/hls/%s", streamID),        // Прямое подключение к stream-app
		fmt.Sprintf("/tmp/stream-hls/%s", streamID), // Временная папка
		fmt.Sprintf("/hls/%s", streamID),            // Другой возможный mount
	}

	var sharedPath string
	var found bool

	// Поиск существующей папки
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			sharedPath = path
			found = true
			log.Printf("✅ Found HLS files at: %s", path)
			break
		}
	}

	if !found {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("no shared HLS directory found for stream %s", streamID)
	}

	files, err := os.ReadDir(sharedPath)
	if err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to read shared directory: %v", err)
	}

	copiedFiles := 0

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()

		// Пропускаем временные файлы
		if strings.HasSuffix(fileName, ".tmp") {
			continue
		}

		if !strings.HasSuffix(fileName, ".ts") && !strings.HasSuffix(fileName, ".m3u8") {
			continue
		}

		srcPath := filepath.Join(sharedPath, fileName)
		dstPath := filepath.Join(tempDir, fileName)

		if err := sm.copyFile(srcPath, dstPath); err == nil {
			copiedFiles++
			log.Printf("📁 Copied from shared volume: %s", fileName)
		} else {
			log.Printf("⚠️ Failed to copy %s: %v", fileName, err)
		}
	}

	if copiedFiles == 0 {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("no HLS files copied from shared volume")
	}

	log.Printf("✅ Copied %d files from shared volume to: %s", copiedFiles, tempDir)
	return tempDir, nil
}

// ✅ НОВАЯ ФУНКЦИЯ: подсчет файлов
func (sm *StorageManager) CountHLSFiles(tempDir string) int {
	if tempDir == "" {
		return 0
	}

	files, err := os.ReadDir(tempDir)
	if err != nil {
		return 0
	}

	count := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()
		if strings.HasSuffix(fileName, ".ts") || strings.HasSuffix(fileName, ".m3u8") {
			count++
		}
	}

	return count
}

// ✅ ОБНОВЛЕННАЯ ФУНКЦИЯ: путь к плейлисту
func (sm *StorageManager) GetHLSPlaylistPath(tempDir string) (string, error) {
	// Сначала ищем стандартное имя
	standardPath := filepath.Join(tempDir, "stream.m3u8")
	if _, err := os.Stat(standardPath); err == nil {
		return standardPath, nil
	}

	// Ищем любой .m3u8 файл
	files, err := os.ReadDir(tempDir)
	if err != nil {
		return "", fmt.Errorf("failed to read temp directory: %v", err)
	}

	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".m3u8") {
			return filepath.Join(tempDir, file.Name()), nil
		}
	}

	return "", fmt.Errorf("no .m3u8 playlist found")
}

// ✅ ОБНОВЛЕННАЯ ФУНКЦИЯ: очистка HLS файлов
func (sm *StorageManager) CleanupHLSFiles(tempDir string) {
	if tempDir == "" {
		return
	}

	if err := os.RemoveAll(tempDir); err == nil {
		log.Printf("🧹 Cleaned up HLS temp dir: %s", tempDir)
	} else {
		log.Printf("⚠️ Failed to cleanup HLS temp dir %s: %v", tempDir, err)
	}
}

// ✅ ФУНКЦИЯ ЗАГРУЗКИ VOD В MINIO
func (sm *StorageManager) UploadVODFiles(streamID, mp4Path, thumbnailPath string) (*VODPaths, error) {
	ctx := context.Background()

	// Загрузка MP4
	mp4Key := fmt.Sprintf("vod/%s/video.mp4", streamID)
	_, err := sm.minioClient.FPutObject(ctx, sm.vodBucket, mp4Key, mp4Path, minio.PutObjectOptions{
		ContentType: "video/mp4",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload MP4: %v", err)
	}

	// Получить размер загруженного файла
	mp4Stat, _ := os.Stat(mp4Path)
	log.Printf("📁 Uploaded MP4: %s (size: %d bytes)", mp4Key, mp4Stat.Size())

	// Загрузка thumbnail если существует
	thumbnailKey := fmt.Sprintf("vod/%s/thumbnail.jpg", streamID)
	if _, err := os.Stat(thumbnailPath); err == nil {
		_, err := sm.minioClient.FPutObject(ctx, sm.vodBucket, thumbnailKey, thumbnailPath, minio.PutObjectOptions{
			ContentType: "image/jpeg",
		})
		if err != nil {
			log.Printf("⚠️ Failed to upload thumbnail: %v", err)
		} else {
			thumbStat, _ := os.Stat(thumbnailPath)
			log.Printf("📁 Uploaded thumbnail: %s (size: %d bytes)", thumbnailKey, thumbStat.Size())
		}
	}

	return &VODPaths{
		MP4URL:       fmt.Sprintf("/recordings/%s", mp4Key),
		ThumbnailURL: fmt.Sprintf("/recordings/%s", thumbnailKey),
	}, nil
}

// ✅ ФУНКЦИЯ ОЧИСТКИ ЛОКАЛЬНЫХ ФАЙЛОВ
func (sm *StorageManager) CleanupLocalFiles(mp4Path, thumbnailPath string) {
	if mp4Path != "" {
		if err := os.Remove(mp4Path); err == nil {
			log.Printf("🧹 Cleaned up local file: %s", mp4Path)
		}
	}

	if thumbnailPath != "" {
		if err := os.Remove(thumbnailPath); err == nil {
			log.Printf("🧹 Cleaned up local file: %s", thumbnailPath)
		}
	}
}

// ✅ ВСПОМОГАТЕЛЬНАЯ ФУНКЦИЯ: копирование файлов
func (sm *StorageManager) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
