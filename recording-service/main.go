package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	dbManager      *DatabaseManager
	storageManager *StorageManager
	workerPool     *WorkerPool
)

func main() {
	log.Println("🎬 Starting Recording Service...")

	// Инициализация компонентов
	if err := initializeServices(); err != nil {
		log.Fatalf("Failed to initialize services: %v", err)
	}

	// Создание worker pool
	workerPool = NewWorkerPool(3) // 3 параллельных воркера
	workerPool.Start()

	// Создание и запуск Kafka consumer
	consumer := NewKafkaConsumer()
	ctx, cancel := context.WithCancel(context.Background())

	// Запуск consumer в отдельной горутине
	go func() {
		if err := consumer.Start(ctx, workerPool.JobChannel); err != nil {
			log.Printf("Consumer error: %v", err)
		}
	}()

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	log.Println("✅ Recording Service is running...")
	<-c

	log.Println("🛑 Shutting down Recording Service...")
	cancel()
	workerPool.Stop()

	// ✅ Закрытие database connection
	if dbManager != nil {
		dbManager.Close()
	}

	log.Println("✅ Recording Service stopped")
}

func initializeServices() error {
	var err error

	// Инициализация Database Manager
	dbManager, err = NewDatabaseManager()
	if err != nil {
		return err
	}

	// Инициализация Storage Manager
	storageManager, err = NewStorageManager()
	if err != nil {
		return err
	}

	log.Println("✅ All services initialized")
	return nil
}

// WorkerPool для параллельной обработки задач
type WorkerPool struct {
	workerCount int
	JobChannel  chan RecordingTask
	wg          sync.WaitGroup
	quit        chan bool
}

func NewWorkerPool(workerCount int) *WorkerPool {
	return &WorkerPool{
		workerCount: workerCount,
		JobChannel:  make(chan RecordingTask, 100), // Буфер на 100 задач
		quit:        make(chan bool),
	}
}

func (wp *WorkerPool) Start() {
	for i := 0; i < wp.workerCount; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
}

func (wp *WorkerPool) Stop() {
	close(wp.quit)
	wp.wg.Wait()
	close(wp.JobChannel)
}

func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	log.Printf("🔧 Worker %d started", id)

	for {
		select {
		case job := <-wp.JobChannel:
			log.Printf("🔧 Worker %d processing stream: %s", id, job.StreamID)
			wp.processRecordingTask(job)

		case <-wp.quit:
			log.Printf("🔧 Worker %d stopping", id)
			return
		}
	}
}

func (wp *WorkerPool) processRecordingTask(task RecordingTask) {
	log.Printf("🎬 Processing recording task: %s for user %s (ID: %d)",
		task.StreamID, task.Username, task.UserID)

	// ✅ ВАЛИДАЦИЯ: проверить что user_id передан
	if task.UserID == 0 {
		log.Printf("⚠️ Warning: UserID is 0 for stream %s - using fallback", task.StreamID)
		task.UserID = 1 // Fallback для legacy стримов
		task.Username = "system"
	}

	if task.Username == "" {
		log.Printf("⚠️ Warning: Username is empty for stream %s", task.StreamID)
		task.Username = fmt.Sprintf("user_%d", task.UserID)
	}

	// Создать начальную запись в БД с информацией о пользователе
	initialRecording := Recording{
		StreamID:  task.StreamID,
		UserID:    task.UserID,   // ✅ СОХРАНЯЕМ ВЛАДЕЛЬЦА
		Username:  task.Username, // ✅ СОХРАНЯЕМ USERNAME
		Title:     task.Title,
		Duration:  0,
		FilePath:  "",
		FileSize:  0,
		Status:    "processing",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := dbManager.CreateRecording(initialRecording); err != nil {
		log.Printf("❌ Failed to create initial recording for %s: %v", task.StreamID, err)
		return
	}

	log.Printf("📊 DB: Created/Updated recording for %s (owner: %s, user_id: %d, duration: %ds)",
		task.StreamID, task.Username, task.UserID, task.Duration)

	// ✅ НОВАЯ ЛОГИКА: RETRY MECHANISM ДЛЯ MINIO
	var tempHLSDir string
	var err error

	// Пробуем скачать файлы из MinIO с retry
	for attempt := 1; attempt <= 4; attempt++ {
		log.Printf("📥 Attempt %d/4: Looking for HLS files in MinIO for stream: %s", attempt, task.StreamID)

		tempHLSDir, err = storageManager.DownloadHLSFilesWithRetry(task.StreamID, attempt)

		if err == nil && tempHLSDir != "" {
			// Проверим что файлы действительно скачались
			if fileCount := storageManager.CountHLSFiles(tempHLSDir); fileCount > 0 {
				log.Printf("✅ Found %d HLS files in MinIO on attempt %d", fileCount, attempt)
				break
			} else {
				log.Printf("⚠️ HLS directory empty on attempt %d", attempt)
				storageManager.CleanupHLSFiles(tempHLSDir)
				tempHLSDir = ""
				err = fmt.Errorf("no files downloaded")
			}
		}

		if attempt < 4 {
			waitTime := time.Duration(attempt * 2) // 2s, 4s, 6s
			log.Printf("⚠️ Attempt %d failed: %v. Retrying in %v...", attempt, err, waitTime*time.Second)
			time.Sleep(waitTime * time.Second)
		}
	}

	// Если все retry не удались - final attempt с fallback
	if tempHLSDir == "" || err != nil {
		log.Printf("⚠️ All MinIO retry attempts failed, using fallback method for stream: %s", task.StreamID)
		tempHLSDir, err = storageManager.DownloadHLSFilesFromFallback(task.StreamID)

		if err != nil || tempHLSDir == "" {
			log.Printf("❌ Both MinIO and fallback methods failed for %s: %v", task.StreamID, err)
			dbManager.UpdateRecordingStatus(task.StreamID, "failed")
			return
		}

		log.Printf("✅ Fallback successful: %d HLS files found", storageManager.CountHLSFiles(tempHLSDir))
	}

	// Убедимся что временная директория будет очищена
	defer storageManager.CleanupHLSFiles(tempHLSDir)

	// ✅ Конвертация HLS → MP4 с найденными файлами
	result := convertHLSToMP4WithTempDir(task, tempHLSDir)
	if !result.Success {
		log.Printf("❌ Conversion failed for %s: %v", task.StreamID, result.Error)
		dbManager.UpdateRecordingStatus(task.StreamID, "failed")
		return
	}

	// ✅ Загрузка в MinIO VOD bucket
	vodPaths, err := storageManager.UploadVODFiles(task.StreamID, result.MP4Path, result.ThumbnailPath)
	if err != nil {
		log.Printf("❌ MinIO upload failed for %s: %v", task.StreamID, err)
		dbManager.UpdateRecordingStatus(task.StreamID, "failed")
		storageManager.CleanupLocalFiles(result.MP4Path, result.ThumbnailPath)
		return
	}

	// ✅ Обновление записи с финальными данными
	finalRecording := Recording{
		StreamID:      task.StreamID,
		UserID:        task.UserID,   // ✅ СОХРАНЯЕМ ВЛАДЕЛЬЦА
		Username:      task.Username, // ✅ СОХРАНЯЕМ USERNAME
		Title:         task.Title,
		Duration:      task.Duration,
		FilePath:      vodPaths.MP4URL,
		ThumbnailPath: vodPaths.ThumbnailURL,
		FileSize:      result.FileSize,
		Status:        "ready",
		UpdatedAt:     time.Now(),
	}

	if err := dbManager.UpdateRecordingComplete(finalRecording); err != nil {
		log.Printf("❌ Database final update failed for %s: %v", task.StreamID, err)
		// Не возвращаем ошибку, т.к. файлы уже в MinIO
	} else {
		log.Printf("📊 DB: Updated recording complete for %s (owner: %s, rows affected: 1)", task.StreamID, task.Username)
	}

	// ✅ Очистить локальные временные файлы после успешной загрузки
	storageManager.CleanupLocalFiles(result.MP4Path, result.ThumbnailPath)

	log.Printf("✅ Successfully processed recording: %s → MinIO:%s (owner: %s)",
		task.StreamID, vodPaths.MP4URL, task.Username)
}
