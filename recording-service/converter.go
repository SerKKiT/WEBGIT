package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func convertHLSToMP4(task RecordingTask) ProcessingResult {
	log.Printf("🔄 Starting HLS→MP4 conversion for: %s", task.StreamID)

	// ✅ Использовать StorageManager для скачивания HLS
	tempHLSDir, err := storageManager.DownloadHLSFiles(task.StreamID)
	if err != nil {
		return ProcessingResult{
			Success: false,
			Error:   fmt.Errorf("failed to download HLS files: %w", err),
		}
	}
	defer storageManager.CleanupHLSFiles(tempHLSDir) // ✅ Очистка через StorageManager

	// ✅ Получить путь к плейлисту через StorageManager
	hlsPlaylist, err := storageManager.GetHLSPlaylistPath(tempHLSDir)
	if err != nil {
		return ProcessingResult{
			Success: false,
			Error:   fmt.Errorf("HLS playlist not found: %w", err),
		}
	}

	// Пути для выходных файлов
	outputMP4 := fmt.Sprintf("/tmp/%s.mp4", task.StreamID)
	outputThumb := fmt.Sprintf("/tmp/%s.jpg", task.StreamID)

	// ✅ Конвертация (чистая FFmpeg логика)
	if err := convertToMP4(hlsPlaylist, outputMP4); err != nil {
		return ProcessingResult{
			Success: false,
			Error:   fmt.Errorf("MP4 conversion failed: %w", err),
		}
	}

	// ✅ Генерация thumbnail
	generateThumbnail(outputMP4, outputThumb)

	// ✅ Получить размер файла
	fileSize, err := getFileSize(outputMP4)
	if err != nil {
		return ProcessingResult{
			Success: false,
			Error:   fmt.Errorf("could not get file size: %w", err),
		}
	}

	log.Printf("✅ Conversion completed: %s (size: %d bytes)", task.StreamID, fileSize)

	return ProcessingResult{
		Success:       true,
		MP4Path:       outputMP4,
		ThumbnailPath: outputThumb,
		FileSize:      fileSize,
		Error:         nil,
	}
}

func convertToMP4(hlsPlaylist, outputMP4 string) error {
	log.Printf("📋 Analyzing HLS playlist: %s", hlsPlaylist)

	// ✅ Проверить содержимое плейлиста
	if err := validateHLSPlaylist(hlsPlaylist); err != nil {
		return fmt.Errorf("invalid HLS playlist: %w", err)
	}

	// ✅ Улучшенная FFmpeg команда с детальным логированием
	ffmpegCmd := exec.Command("ffmpeg",
		"-loglevel", "info", // Детальные логи
		"-i", hlsPlaylist,
		"-c:v", "libx264", // Принудительное перекодирование видео
		"-c:a", "aac", // Принудительное перекодирование аудио
		"-preset", "fast", // Быстрое кодирование
		"-crf", "23", // Качество видео
		"-movflags", "+faststart", // Оптимизация для веб
		"-f", "mp4", // Принудительный формат MP4
		"-y", // Перезаписать файл
		outputMP4,
	)

	log.Printf("🔧 Running FFmpeg: %v", ffmpegCmd.Args)

	// ✅ Захватить детальный вывод
	var stdout, stderr bytes.Buffer
	ffmpegCmd.Stdout = &stdout
	ffmpegCmd.Stderr = &stderr

	err := ffmpegCmd.Run()

	// ✅ Логировать весь вывод FFmpeg
	if stdout.Len() > 0 {
		log.Printf("FFmpeg stdout: %s", stdout.String())
	}
	if stderr.Len() > 0 {
		log.Printf("FFmpeg stderr: %s", stderr.String())
	}

	if err != nil {
		return fmt.Errorf("FFmpeg failed: %v, stderr: %s", err, stderr.String())
	}

	// ✅ Проверить размер и содержимое выходного файла
	if err := validateOutputMP4(outputMP4); err != nil {
		return fmt.Errorf("output MP4 validation failed: %w", err)
	}

	log.Printf("✅ FFmpeg conversion successful")
	return nil
}

// ✅ Валидация HLS плейлиста
func validateHLSPlaylist(playlistPath string) error {
	content, err := os.ReadFile(playlistPath)
	if err != nil {
		return fmt.Errorf("cannot read playlist: %w", err)
	}

	playlistStr := string(content)
	log.Printf("📄 HLS Playlist content:\n%s", playlistStr)

	// Проверить базовые HLS элементы
	if !strings.Contains(playlistStr, "#EXTM3U") {
		return fmt.Errorf("invalid HLS playlist: missing #EXTM3U header")
	}

	// Подсчитать сегменты
	segmentCount := strings.Count(playlistStr, ".ts")
	if segmentCount == 0 {
		return fmt.Errorf("no .ts segments found in playlist")
	}

	log.Printf("📊 Found %d segments in playlist", segmentCount)

	// Проверить что файлы сегментов существуют
	playlistDir := filepath.Dir(playlistPath)
	lines := strings.Split(playlistStr, "\n")

	validSegments := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ".ts") {
			segmentPath := filepath.Join(playlistDir, line)
			if stat, err := os.Stat(segmentPath); err == nil && stat.Size() > 0 {
				validSegments++
				log.Printf("📦 Valid segment: %s (%d bytes)", line, stat.Size())
			} else {
				log.Printf("⚠️ Missing/empty segment: %s", line)
			}
		}
	}

	if validSegments == 0 {
		return fmt.Errorf("no valid .ts segments found on disk")
	}

	log.Printf("✅ Validated %d/%d segments", validSegments, segmentCount)
	return nil
}

// ✅ Валидация выходного MP4
func validateOutputMP4(mp4Path string) error {
	stat, err := os.Stat(mp4Path)
	if err != nil {
		return fmt.Errorf("output file not created: %s", mp4Path)
	}

	if stat.Size() == 0 {
		return fmt.Errorf("output MP4 file is empty: %s", mp4Path)
	}

	if stat.Size() < 1024 { // Менее 1KB подозрительно
		log.Printf("⚠️ Output MP4 is very small: %d bytes", stat.Size())
	}

	// ✅ Проверить MP4 с помощью ffprobe
	probeCmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		mp4Path,
	)

	output, err := probeCmd.Output()
	if err != nil {
		return fmt.Errorf("ffprobe validation failed: %w", err)
	}

	log.Printf("📋 FFprobe output: %s", string(output))

	// Простая проверка что есть streams
	if !strings.Contains(string(output), `"streams"`) {
		return fmt.Errorf("no streams found in output MP4")
	}

	log.Printf("✅ Output MP4 validated: %d bytes", stat.Size())
	return nil
}

func generateThumbnail(inputMP4, outputThumb string) {
	duration := getVideoDuration(inputMP4)

	var seekTime string
	if duration > 10 {
		seekTime = strconv.Itoa(duration / 2)
	} else {
		seekTime = "2"
	}

	thumbCmd := exec.Command("ffmpeg",
		"-i", inputMP4,
		"-ss", seekTime,
		"-vframes", "1",
		"-vf", "scale=480:360",
		"-q:v", "2",
		"-y",
		outputThumb,
	)

	log.Printf("🖼️ Generating thumbnail at %ss...", seekTime)

	if _, err := thumbCmd.CombinedOutput(); err != nil {
		log.Printf("⚠️ Thumbnail generation failed: %v", err)
		if file, err := os.Create(outputThumb); err == nil {
			file.Close()
		}
	} else {
		log.Printf("✅ Thumbnail generated successfully")
	}
}

func getVideoDuration(videoPath string) int {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "stream=duration",
		"-of", "csv=p=0",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 30
	}

	durationStr := strings.TrimSpace(string(output))
	if duration, err := strconv.ParseFloat(durationStr, 64); err == nil {
		return int(duration)
	}

	return 30
}

func getFileSize(filePath string) (int64, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return fileInfo.Size(), nil
}
