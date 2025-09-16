package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/BrunoKrugel/snapshot2stream/internal/client"
	"github.com/BrunoKrugel/snapshot2stream/internal/config"
	_ "github.com/joho/godotenv/autoload"
)

// Frame represents a cached camera frame
type Frame struct {
	timestamp time.Time
	data      []byte
}

// CameraCache holds a ring buffer of frames for smoother playback
type CameraCache struct {
	frames    []*Frame
	writeIndex int
	readIndex  int
	size      int
	mu        sync.RWMutex
}

func newCameraCache(size int) *CameraCache {
	return &CameraCache{
		frames: make([]*Frame, size),
		size:   size,
	}
}

// FrameManager manages frame caching and fetching for all cameras
type FrameManager struct {
	caches map[string]*CameraCache
	client *client.Client
	ctx    context.Context
	cancel context.CancelFunc
}

func main() {
	cfg, err := config.NewConfig()
	if err != nil {
		panic(err)
	}

	client := client.NewRestyClient(cfg)

	// Create frame manager
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frameManager := &FrameManager{
		caches: make(map[string]*CameraCache),
		client: client,
		ctx:    ctx,
		cancel: cancel,
	}

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
		frameManager.caches[name] = newCameraCache(10) // Ring buffer of 10 frames
		if cfg.Server.UseCache {
			// Start background fetcher for each camera
			go frameManager.startFetcher(name, url, cfg)
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

// startFetcher runs in a goroutine to continuously fetch frames for a camera
func (fm *FrameManager) startFetcher(cameraName, cameraURL string, cfg *config.Config) {
	frameInterval := time.Duration(1000/cfg.Server.FetchFPS) * time.Millisecond
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	for {
		select {
		case <-fm.ctx.Done():
			return
		case <-ticker.C:
			fm.fetchFrame(cameraName, cameraURL)
		}
	}
}

// fetchFrame fetches a single frame and updates the cache
func (fm *FrameManager) fetchFrame(cameraName, cameraURL string) {
	resp, err := fm.client.GetStream(cameraURL)
	if err != nil {
		log.Printf("[%s] request error: %v", cameraName, err)
		return
	}

	if resp.RawResponse != nil && resp.RawResponse.Body != nil {
		defer resp.RawResponse.Body.Close()
	}

	if resp.StatusCode() != http.StatusOK {
		log.Printf("[%s] bad status: %s", cameraName, resp.Status())
		return
	}

	body := resp.Body()
	if len(body) == 0 {
		return
	}

	// Validate JPEG frame
	if !isValidJPEG(body) {
		log.Printf("[%s] invalid JPEG frame, skipping", cameraName)
		return
	}

	// Update cache with new frame
	cache := fm.caches[cameraName]
	cache.mu.Lock()
	newFrame := &Frame{
		data:      make([]byte, len(body)),
		timestamp: time.Now(),
	}
	copy(newFrame.data, body)
	
	// Add to ring buffer
	cache.frames[cache.writeIndex] = newFrame
	cache.writeIndex = (cache.writeIndex + 1) % cache.size
	cache.mu.Unlock()
}

// getLatestFrame returns the latest cached frame for a camera
func (fm *FrameManager) getLatestFrame(cameraName string) *Frame {
	cache, exists := fm.caches[cameraName]
	if !exists {
		return nil
	}

	cache.mu.RLock()
	defer cache.mu.RUnlock()
	
	// Get the most recent frame
	latestIndex := (cache.writeIndex - 1 + cache.size) % cache.size
	return cache.frames[latestIndex]
}

// getNextFrame returns the next frame for streaming
func (fm *FrameManager) getNextFrame(cameraName string) *Frame {
	cache, exists := fm.caches[cameraName]
	if !exists {
		return nil
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	
	// If we're caught up to write index, return latest
	if cache.readIndex == cache.writeIndex {
		latestIndex := (cache.writeIndex - 1 + cache.size) % cache.size
		return cache.frames[latestIndex]
	}
	
	// Get next frame and advance read index
	frame := cache.frames[cache.readIndex]
	cache.readIndex = (cache.readIndex + 1) % cache.size
	return frame
}

// streamCameraFromCache serves cached frames to clients
func streamCameraFromCache(w http.ResponseWriter, r *http.Request, fm *FrameManager, cameraName string, cfg *config.Config) {
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
		frame := fm.getNextFrame(cameraName)
		if frame == nil {
			// No frame available yet, wait and retry
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// Build frame header efficiently
		headerBuf = headerBuf[:0]
		headerBuf = append(headerBuf, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: "...)
		headerBuf = append(headerBuf, fmt.Sprintf("%d", len(frame.data))...)
		headerBuf = append(headerBuf, "\r\n\r\n"...)

		// Write frame header + image + separator
		if _, err := w.Write(headerBuf); err != nil {
			log.Printf("[%s] write header error: %v", cameraName, err)
			return
		}

		if _, err := w.Write(frame.data); err != nil {
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
func streamCameraDirect(w http.ResponseWriter, r *http.Request, fm *FrameManager, cameraName, cameraURL string, cfg *config.Config) {
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
		resp, err := fm.client.GetStream(cameraURL)
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
		if _, err := w.Write(headerBuf); err != nil {
			log.Printf("[%s] write header error: %v", cameraName, err)
			return
		}

		if _, err := w.Write(body); err != nil {
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
