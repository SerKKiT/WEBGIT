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
// Загрузка файла в MinIO с детальным логированием
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

	// Проверить что файл существует и читается
	file, err := os.Open(localFilePath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %v", localFilePath, err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file stats: %v", err)
	}

	if stat.Size() == 0 {
		return fmt.Errorf("file is empty: %s", localFilePath)
	}

	// Попытка загрузки в MinIO
	info, err := minioClient.PutObject(ctx, minioBucket, objectPath, file, stat.Size(), minio.PutObjectOptions{
		ContentType: contentType,
	})

	if err != nil {
		return fmt.Errorf("failed to upload to MinIO: %v", err)
	}

	// ✅ УБРАЛИ ИЗБЫТОЧНЫЙ ЛОГ - теперь логируется только в uploadNewHLSFiles
	_ = info // Избегаем unused variable warning
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
// Мониторинг и загрузка HLS файлов с локальной очисткой
// Оптимизированный HLS Uploader без спама
func startHLSUploader(streamID string) {
	log.Printf("🚀 Starting optimized HLS uploader for stream: %s", streamID)

	hlsDir := filepath.Join("hls", streamID)
	maxLocalChunks := 8 // Увеличено для буферизации

	// Трекинг загруженных файлов
	uploadedFiles := make(map[string]time.Time)
	lastPlaylistHash := ""

	go func() {
		log.Printf("📡 HLS uploader goroutine started for stream: %s", streamID)

		uploadCycle := 0

		for {
			// Проверяем, активен ли еще стрим
			streamsMux.Lock()
			_, exists := activeStreams[streamID]
			streamsMux.Unlock()

			if !exists {
				log.Printf("🛑 Stopping HLS uploader for stream %s (stream not active)", streamID)
				// Финальная загрузка всех файлов
				uploadAllHLSFiles(streamID, hlsDir)
				break
			}

			uploadCycle++

			// ✅ УМНАЯ ЗАГРУЗКА - только новые файлы
			newFilesCount := uploadNewHLSFiles(streamID, hlsDir, uploadedFiles, &lastPlaylistHash)

			// Логируем только если есть активность
			if newFilesCount > 0 {
				log.Printf("📊 HLS upload cycle #%d: %d new files uploaded for %s", uploadCycle, newFilesCount, streamID)
			}

			// Локальная очистка старых сегментов каждые 10 циклов
			if uploadCycle%10 == 0 {
				cleanupLocalHLSSegments(streamID, maxLocalChunks)
				// Очистка трекинга старых файлов
				cleanupUploadedFilesTracker(uploadedFiles)
			}

			// ✅ УВЕЛИЧЕН ИНТЕРВАЛ - каждые 5 секунд вместо 2
			time.Sleep(5 * time.Second)
		}

		log.Printf("✅ HLS uploader stopped for stream %s", streamID)
	}()

	log.Printf("✅ Optimized HLS uploader launched for stream: %s", streamID)
}

// ✅ НОВАЯ ФУНКЦИЯ: умная загрузка только новых файлов
func uploadNewHLSFiles(streamID, hlsDir string, uploadedFiles map[string]time.Time, lastPlaylistHash *string) int {
	// Проверить что папка существует
	if _, err := os.Stat(hlsDir); os.IsNotExist(err) {
		return 0 // Тихо возвращаем, папка еще не создалась
	}

	files, err := os.ReadDir(hlsDir)
	if err != nil {
		log.Printf("❌ Failed to read HLS directory %s: %v", hlsDir, err)
		return 0
	}

	if len(files) == 0 {
		return 0
	}

	uploadCount := 0

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()

		// Пропускаем временные файлы
		if strings.HasSuffix(fileName, ".tmp") {
			continue
		}

		if !strings.HasSuffix(fileName, ".m3u8") && !strings.HasSuffix(fileName, ".ts") {
			continue
		}

		localPath := filepath.Join(hlsDir, fileName)

		// Получаем информацию о файле
		fileInfo, err := os.Stat(localPath)
		if err != nil {
			continue
		}

		// ✅ УМНАЯ ПРОВЕРКА: загружать только если файл новый или изменился
		shouldUpload := false

		if strings.HasSuffix(fileName, ".ts") {
			// .ts сегменты загружаем только один раз
			if lastUploaded, exists := uploadedFiles[fileName]; !exists {
				shouldUpload = true
			} else if fileInfo.ModTime().After(lastUploaded) {
				shouldUpload = true
			}
		} else if strings.HasSuffix(fileName, ".m3u8") {
			// .m3u8 загружаем только если содержимое изменилось
			currentHash := getFileHash(localPath)
			if currentHash != *lastPlaylistHash {
				shouldUpload = true
				*lastPlaylistHash = currentHash
			}
		}

		if !shouldUpload {
			continue // Тихо пропускаем без логов
		}

		// Загружаем файл
		err = uploadToMinIO(streamID, localPath, fileName)
		if err != nil {
			log.Printf("❌ Failed to upload %s: %v", fileName, err)
			continue
		}

		// Отмечаем как загруженный
		uploadedFiles[fileName] = fileInfo.ModTime()
		uploadCount++

		// Логируем только новые загрузки
		log.Printf("✅ Uploaded new file: %s", fileName)
	}

	return uploadCount
}

// ✅ НОВАЯ ФУНКЦИЯ: получение хеша файла для проверки изменений
func getFileHash(filePath string) string {
	file, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer file.Close()

	// Читаем первые 512 байт для быстрой проверки изменений
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && n == 0 {
		return ""
	}

	// Простой хеш на основе размера файла и содержимого
	stat, _ := file.Stat()
	return fmt.Sprintf("%d_%x", stat.Size(), buffer[:n])
}

// ✅ НОВАЯ ФУНКЦИЯ: очистка трекера загруженных файлов
func cleanupUploadedFilesTracker(uploadedFiles map[string]time.Time) {
	cutoff := time.Now().Add(-10 * time.Minute) // Удаляем записи старше 10 минут

	for fileName, uploadTime := range uploadedFiles {
		if uploadTime.Before(cutoff) {
			delete(uploadedFiles, fileName)
		}
	}
}

// ✅ ОБНОВЛЕННАЯ ФУНКЦИЯ: uploadAllHLSFiles для финальной загрузки
func uploadAllHLSFiles(streamID, hlsDir string) {
	log.Printf("🔄 Final upload of all remaining HLS files for stream %s", streamID)

	// Простая загрузка всех файлов без трекинга при остановке
	files, err := os.ReadDir(hlsDir)
	if err != nil {
		log.Printf("⚠️ Error reading HLS dir for final upload: %v", err)
		return
	}

	uploadCount := 0
	for _, file := range files {
		if file.IsDir() || strings.HasSuffix(file.Name(), ".tmp") {
			continue
		}

		fileName := file.Name()
		if !strings.HasSuffix(fileName, ".m3u8") && !strings.HasSuffix(fileName, ".ts") {
			continue
		}

		localPath := filepath.Join(hlsDir, fileName)
		err := uploadToMinIO(streamID, localPath, fileName)
		if err == nil {
			uploadCount++
		}
	}

	if uploadCount > 0 {
		log.Printf("✅ Final upload completed: %d files for stream %s", uploadCount, streamID)
	}
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
// Обновить uploadHLSFiles с детальным логированием
// Обновленная функция uploadHLSFiles с улучшенным логированием
func uploadHLSFiles(streamID, hlsDir string) error {
	log.Printf("📂 Scanning HLS directory for stream %s: %s", streamID, hlsDir)

	// Проверить что папка существует
	if _, err := os.Stat(hlsDir); os.IsNotExist(err) {
		log.Printf("📂 HLS directory does not exist yet: %s", hlsDir)
		return nil // Не ошибка, папка еще не создалась
	}

	files, err := os.ReadDir(hlsDir)
	if err != nil {
		log.Printf("❌ Failed to read HLS directory %s: %v", hlsDir, err)
		return err
	}

	if len(files) == 0 {
		log.Printf("📂 No files in HLS directory: %s", hlsDir)
		return nil
	}

	log.Printf("📂 Found %d files in HLS directory %s", len(files), hlsDir)

	uploadCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()

		// Пропускаем временные файлы
		if strings.HasSuffix(fileName, ".tmp") {
			log.Printf("⏭️ Skipping temporary file: %s", fileName)
			continue
		}

		if !strings.HasSuffix(fileName, ".m3u8") && !strings.HasSuffix(fileName, ".ts") {
			log.Printf("⏭️ Skipping non-HLS file: %s", fileName)
			continue
		}

		localPath := filepath.Join(hlsDir, fileName)

		// Для .ts сегментов проверяем, не загружен ли уже
		if strings.HasSuffix(fileName, ".ts") {
			if isFileUploaded(streamID, fileName, localPath) {
				log.Printf("✅ File already uploaded: %s", fileName)
				continue
			}
		}

		log.Printf("📤 Uploading to MinIO: %s", fileName)

		// Для .m3u8 плейлистов всегда перезагружаем
		err := uploadToMinIO(streamID, localPath, fileName)
		if err != nil {
			log.Printf("❌ Failed to upload %s: %v", fileName, err)
			continue
		}

		uploadCount++
		log.Printf("✅ Successfully uploaded: %s", fileName)
	}

	if uploadCount > 0 {
		log.Printf("📊 HLS upload completed for %s: %d files uploaded", streamID, uploadCount)
	} else {
		log.Printf("📂 No new files to upload for stream %s", streamID)
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
