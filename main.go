package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/BrunoKrugel/snapshot2stream/internal/client"
	"github.com/BrunoKrugel/snapshot2stream/internal/config"
	"github.com/BrunoKrugel/snapshot2stream/internal/frame"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	cfg, err := config.NewConfig()
	if err != nil {
		panic(err)
	}

	client := client.NewRestyClient(cfg)

	// Create frame manager
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frameManager := frame.NewFrameManager(cfg, client)

	cameras := map[string]string{
		"rua":        cfg.Cameras.Camera1,
		"piscina":    cfg.Cameras.Camera2,
		"encomendas": cfg.Cameras.Camera3,
		"externo":    cfg.Cameras.Camera4,
		"hall":       cfg.Cameras.Camera5,
		"elevador":   cfg.Cameras.Camera6,
	}

	// Initialize caches and start fetchers (only if cache is enabled)
	for name, url := range cameras {
		frameManager.Caches[name] = frame.NewCameraCache(10) // Ring buffer of 10 frames
		if cfg.Server.UseCache {
			// Start background fetcher for each camera
			go frameManager.StartFetcher(ctx, name, url, cfg)
		}
	}

	// Register handlers
	for name, url := range cameras {
		cameraName := name
		cameraURL := url
		http.HandleFunc("/"+cameraName, func(w http.ResponseWriter, r *http.Request) {
			if cfg.Server.UseCache {
				streamCameraFromCache(w, r, frameManager, cameraName, cfg)
			} else {
				streamCameraDirect(w, r, frameManager, cameraName, cameraURL, cfg)
			}
		})
		log.Printf("Camera endpoint ready: http://localhost:%s/%s", cfg.Server.Port, cameraName)
	}

	cacheStatus := "enabled"
	if !cfg.Server.UseCache {
		cacheStatus = "disabled"
	}
	log.Printf("MJPEG server listening on :%s (Serve FPS: %d, Fetch FPS: %d, Cache: %s)\n", cfg.Server.Port, cfg.Server.FPS, cfg.Server.FetchFPS, cacheStatus)
	log.Fatal(http.ListenAndServe(":"+cfg.Server.Port, nil))
}

// streamCameraFromCache serves cached frames to clients
func streamCameraFromCache(w http.ResponseWriter, r *http.Request, fm *frame.FrameManager, cameraName string, cfg *config.Config) {
	// MJPEG headers
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	frameInterval := time.Duration(1000/cfg.Server.FPS) * time.Millisecond

	// Pre-allocate buffer for frame header to avoid repeated allocations
	headerBuf := make([]byte, 0, 128)

	for {
		// Check if client disconnected
		select {
		case <-ctx.Done():
			log.Printf("[%s] client disconnected", cameraName)
			return
		default:
		}

		// Get next frame from cache
		frame := fm.GetNextFrame(cameraName)
		if frame == nil {
			// No frame available yet, wait and retry
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// Build frame header efficiently
		headerBuf = headerBuf[:0]
		headerBuf = append(headerBuf, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: "...)
		headerBuf = append(headerBuf, fmt.Sprintf("%d", len(frame.Data))...)
		headerBuf = append(headerBuf, "\r\n\r\n"...)

		// Write frame header + image + separator
		if _, err := w.Write(headerBuf); err != nil {
			log.Printf("[%s] write header error: %v", cameraName, err)
			return
		}

		if _, err := w.Write(frame.Data); err != nil {
			log.Printf("[%s] write body error: %v", cameraName, err)
			return
		}

		if _, err := w.Write([]byte("\r\n")); err != nil {
			log.Printf("[%s] write separator error: %v", cameraName, err)
			return
		}

		flusher.Flush()

		// Control output rate
		time.Sleep(frameInterval)
	}
}

// isValidJPEG checks if the data is a valid JPEG image
func isValidJPEG(data []byte) bool {
	if len(data) < 10 {
		return false
	}
	// Check JPEG magic bytes (SOI marker: FF D8)
	if data[0] != 0xFF || data[1] != 0xD8 {
		return false
	}
	// Check for EOI marker (FF D9) at the end
	if data[len(data)-2] != 0xFF || data[len(data)-1] != 0xD9 {
		return false
	}
	// Basic size check - very small images are likely corrupt
	if len(data) < 1000 {
		return false
	}
	return true
}

// streamCameraDirect streams directly from camera without caching
func streamCameraDirect(w http.ResponseWriter, r *http.Request, fm *frame.FrameManager, cameraName, cameraURL string, cfg *config.Config) {
	// MJPEG headers
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	frameInterval := time.Duration(1000/cfg.Server.FPS) * time.Millisecond

	// Pre-allocate buffer for frame header to avoid repeated allocations
	headerBuf := make([]byte, 0, 128)

	for {
		// Check if client disconnected
		select {
		case <-ctx.Done():
			log.Printf("[%s] client disconnected", cameraName)
			return
		default:
		}

		// Fetch frame directly
		resp, err := fm.Client.GetStream(cameraURL)
		if err != nil {
			log.Printf("[%s] request error: %v", cameraName, err)
			time.Sleep(frameInterval)
			continue
		}

		if resp.RawResponse != nil && resp.RawResponse.Body != nil {
			defer resp.RawResponse.Body.Close()
		}

		if resp.StatusCode() != http.StatusOK {
			log.Printf("[%s] bad status: %s", cameraName, resp.Status())
			time.Sleep(frameInterval)
			continue
		}

		body := resp.Body()
		if len(body) == 0 {
			time.Sleep(frameInterval)
			continue
		}

		// Validate JPEG frame
		if !isValidJPEG(body) {
			log.Printf("[%s] invalid JPEG frame, skipping", cameraName)
			time.Sleep(frameInterval)
			continue
		}

		// Build frame header efficiently
		headerBuf = headerBuf[:0]
		headerBuf = append(headerBuf, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: "...)
		headerBuf = append(headerBuf, fmt.Sprintf("%d", len(body))...)
		headerBuf = append(headerBuf, "\r\n\r\n"...)

		// Write frame header + image + separator
		if _, errHeader := w.Write(headerBuf); errHeader != nil {
			log.Printf("[%s] write header error: %v", cameraName, errHeader)
			return
		}

		if _, errBody := w.Write(body); errBody != nil {
			log.Printf("[%s] write body error: %v", cameraName, errBody)
			return
		}

		if _, errSeparator := w.Write([]byte("\r\n")); errSeparator != nil {
			log.Printf("[%s] write separator error: %v", cameraName, errSeparator)
			return
		}

		flusher.Flush()

		// Control output rate
		time.Sleep(frameInterval)
	}
}
