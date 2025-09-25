package main

import (
	"context"
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
	log.Printf("🎬 Processing recording task: %s", task.StreamID)

	// Обновить статус на "processing"
	if err := dbManager.UpdateRecordingStatus(task.StreamID, "processing"); err != nil {
		log.Printf("❌ Failed to update status to processing: %v", err)
	}

	// ✅ Конвертация HLS → MP4
	result := convertHLSToMP4(task)
	if !result.Success {
		log.Printf("❌ Conversion failed for %s: %v", task.StreamID, result.Error)
		dbManager.UpdateRecordingStatus(task.StreamID, "failed")
		return
	}

	// ✅ Загрузка в MinIO
	vodPaths, err := storageManager.UploadVODFiles(task.StreamID, result.MP4Path, result.ThumbnailPath)
	if err != nil {
		log.Printf("❌ MinIO upload failed for %s: %v", task.StreamID, err)
		dbManager.UpdateRecordingStatus(task.StreamID, "failed")

		// Очистить локальные файлы при ошибке
		storageManager.CleanupLocalFiles(result.MP4Path, result.ThumbnailPath)
		return
	}

	// ✅ Создание записи в БД с путями к файлам в MinIO
	recording := Recording{
		StreamID:      task.StreamID,
		UserID:        task.UserID,
		Title:         task.Title,
		Duration:      task.Duration,
		FilePath:      vodPaths.MP4URL,       // MinIO URL
		ThumbnailPath: vodPaths.ThumbnailURL, // MinIO URL
		FileSize:      result.FileSize,
		Status:        "ready",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := dbManager.CreateRecording(recording); err != nil {
		log.Printf("❌ Database save failed for %s: %v", task.StreamID, err)
		// Не возвращаем ошибку, т.к. файлы уже в MinIO
	}

	// ✅ Очистить локальные временные файлы после успешной загрузки
	storageManager.CleanupLocalFiles(result.MP4Path, result.ThumbnailPath)

	log.Printf("✅ Successfully processed recording: %s → MinIO:%s", task.StreamID, vodPaths.MP4URL)
}
