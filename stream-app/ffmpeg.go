package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	portStart = 10000
	portEnd   = 10100
	portPool  = make(map[int]bool)
	poolMux   sync.Mutex

	processes    = make(map[string]*StreamProcess)
	processesMux sync.Mutex
)

type StreamProcess struct {
	Cmd         *exec.Cmd
	StopChan    chan bool
	IsRunning   bool
	IsConnected bool
	StreamID    string
}

func acquirePort() (int, error) {
	poolMux.Lock()
	defer poolMux.Unlock()
	for p := portStart; p <= portEnd; p++ {
		if !portPool[p] {
			portPool[p] = true
			return p, nil
		}
	}
	return 0, errors.New("no ports available")
}

func releasePort(port int) {
	poolMux.Lock()
	defer poolMux.Unlock()
	portPool[port] = false
}

func startFFmpegProcess(streamID, srtAddr string) error {
	hlsDir := filepath.Join("hls", streamID)
	if err := os.MkdirAll(hlsDir, 0755); err != nil {
		return fmt.Errorf("failed to create HLS directory: %v", err)
	}

	processesMux.Lock()
	defer processesMux.Unlock()

	if proc, exists := processes[streamID]; exists && proc.IsRunning {
		return nil
	}

	stopChan := make(chan bool)

	processes[streamID] = &StreamProcess{
		StopChan:    stopChan,
		IsRunning:   true,
		IsConnected: false,
		StreamID:    streamID,
	}

	go func() {
		for {
			select {
			case <-stopChan:
				log.Printf("Stopping ffmpeg loop for stream %s", streamID)
				return
			default:
				if err := runFFmpegInstance(streamID, srtAddr, stopChan); err != nil {
					log.Printf("FFmpeg instance error for stream %s: %v", streamID, err)
				}

				select {
				case <-stopChan:
					log.Printf("Stopping ffmpeg loop for stream %s", streamID)
					return
				default:
					log.Printf("Restarting ffmpeg for stream %s in 2 seconds...", streamID)
					time.Sleep(2 * time.Second)
				}
			}
		}
	}()

	return nil
}

func runFFmpegInstance(streamID, srtAddr string, stopChan chan bool) error {
	hlsDir := filepath.Join("hls", streamID)
	output := filepath.Join(hlsDir, "stream.m3u8")

	cmd := exec.Command("ffmpeg",
		"-hide_banner",
		"-loglevel", "info",
		"-fflags", "+nobuffer+genpts", // ✅ Добавить genpts для PTS
		"-analyzeduration", "2000000", // ✅ Увеличить анализ до 2 сек
		"-probesize", "2000000", // ✅ Увеличить размер пробы
		"-timeout", "5000000",
		"-i", srtAddr,

		// ✅ ПРИНУДИТЕЛЬНОЕ ПЕРЕКОДИРОВАНИЕ ВИДЕО
		"-c:v", "libx264", // Вместо copy
		"-preset", "faster", // Быстрое кодирование для live
		"-crf", "23", // Качество видео
		"-maxrate", "5000k", // Ограничение битрейта
		"-bufsize", "6000k", // Размер буфера
		"-pix_fmt", "yuv420p", // Совместимый формат пикселей
		"-g", "60", // GOP size (keyframe каждые 2 сек при 25fps)
		"-keyint_min", "30", // Минимальный интервал ключевых кадров
		"-sc_threshold", "0", // Отключить scene change detection
		"-r", "30", // Принудительный framerate

		// ✅ АУДИО БЕЗ ИЗМЕНЕНИЙ
		"-c:a", "aac",
		"-b:a", "128k",
		"-ar", "48000", // ✅ Фиксированная частота
		"-ac", "2", // ✅ Стерео

		// ✅ HLS ПАРАМЕТРЫ
		"-f", "hls",
		"-hls_time", "4",
		"-hls_list_size", "0",
		"-hls_flags", "append_list+independent_segments", // ✅ Добавить independent_segments
		"-hls_playlist_type", "event",
		"-hls_allow_cache", "0",
		"-hls_segment_filename", filepath.Join(hlsDir, "segment_%03d.ts"),
		output)

	// Правильное использование StderrPipe
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	log.Printf("Starting ffmpeg instance for stream %s", streamID)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %v", err)
	}

	processesMux.Lock()
	if proc, exists := processes[streamID]; exists {
		proc.Cmd = cmd
	}
	processesMux.Unlock()

	// Мониторинг логов ffmpeg (теперь с правильным типом)
	go monitorFFmpegLogs(streamID, stderr)

	// Горутина для остановки по сигналу
	go func() {
		<-stopChan
		if cmd.Process != nil {
			log.Printf("Killing ffmpeg process for stream %s", streamID)
			cmd.Process.Kill()
		}
	}()

	err = cmd.Wait()

	processesMux.Lock()
	if proc, exists := processes[streamID]; exists {
		proc.Cmd = nil
		if proc.IsConnected {
			proc.IsConnected = false
			go notifyMainAppStatusChange(streamID, "waiting")
		}
	}
	processesMux.Unlock()

	if err != nil {
		log.Printf("FFmpeg process for stream %s finished with error: %v", streamID, err)
		return err
	}

	log.Printf("FFmpeg process for stream %s finished normally", streamID)
	return nil
}

func monitorFFmpegLogs(streamID string, stderr io.ReadCloser) {
	defer stderr.Close()
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("FFmpeg [%s]: %s", streamID, line)

		lowerLine := strings.ToLower(line)

		// Обнаружение подключения SRT
		if strings.Contains(lowerLine, "stream #0") ||
			strings.Contains(lowerLine, "input #0") ||
			(strings.Contains(lowerLine, "video:") && strings.Contains(lowerLine, "fps")) {

			processesMux.Lock()
			if proc, exists := processes[streamID]; exists && !proc.IsConnected {
				proc.IsConnected = true
				log.Printf("SRT connection detected for stream %s", streamID)
				go notifyMainAppStatusChange(streamID, "running")
			}
			processesMux.Unlock()
		}

		// Обнаружение разрыва соединения
		if strings.Contains(lowerLine, "connection timed out") ||
			strings.Contains(lowerLine, "connection failed") ||
			strings.Contains(lowerLine, "connection closed") ||
			strings.Contains(lowerLine, "no more input") ||
			strings.Contains(lowerLine, "end of file") {

			processesMux.Lock()
			if proc, exists := processes[streamID]; exists && proc.IsConnected {
				proc.IsConnected = false
				log.Printf("SRT connection lost for stream %s", streamID)
				go notifyMainAppStatusChange(streamID, "waiting")
			}
			processesMux.Unlock()
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading ffmpeg stderr for stream %s: %v", streamID, err)
	}
}

func stopFFmpegProcess(streamID string) {
	processesMux.Lock()
	defer processesMux.Unlock()

	if proc, ok := processes[streamID]; ok {
		proc.IsRunning = false
		close(proc.StopChan)

		if proc.Cmd != nil && proc.Cmd.Process != nil {
			log.Printf("Killing current ffmpeg process for stream %s", streamID)
			proc.Cmd.Process.Kill()
		}

		delete(processes, streamID)
	}
}
