package frame

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/BrunoKrugel/snapshot2stream/internal/client"
	"github.com/BrunoKrugel/snapshot2stream/internal/config"
	"github.com/BrunoKrugel/snapshot2stream/internal/model"
	"github.com/BrunoKrugel/snapshot2stream/internal/utils"
)

// CameraCache holds a ring buffer of frames for smoother playback
type CameraCache struct {
	frames     []*model.Frame
	writeIndex int
	readIndex  int
	size       int
	mu         sync.RWMutex
}

func NewCameraCache(size int) *CameraCache {
	return &CameraCache{
		frames: make([]*model.Frame, size),
		size:   size,
	}
}

// FrameManager manages frame caching and fetching for all cameras
type FrameManager struct {
	Caches map[string]*CameraCache
	Client *client.Client
}

func NewFrameManager(cfg *config.Config, client *client.Client) *FrameManager {
	return &FrameManager{
		Caches: make(map[string]*CameraCache),
		Client: client,
	}
}

// StartFetcher runs in a goroutine to continuously fetch frames for a camera
func (fm *FrameManager) StartFetcher(ctx context.Context, cameraName, cameraURL string, cfg *config.Config) {
	frameInterval := time.Duration(1000/cfg.Server.FetchFPS) * time.Millisecond
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fm.FetchFrame(cameraName, cameraURL)
		}
	}
}

// FetchFrame fetches a single frame and updates the cache
func (fm *FrameManager) FetchFrame(cameraName, cameraURL string) {
	resp, err := fm.Client.GetStream(cameraURL)
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
	if !utils.IsValidJPEG(body) {
		log.Printf("[%s] invalid JPEG frame, skipping", cameraName)
		return
	}

	// Update cache with new frame
	cache := fm.Caches[cameraName]
	cache.mu.Lock()
	newFrame := &model.Frame{
		Data:      make([]byte, len(body)),
		Timestamp: time.Now(),
	}
	copy(newFrame.Data, body)

	// Add to ring buffer
	cache.frames[cache.writeIndex] = newFrame
	cache.writeIndex = (cache.writeIndex + 1) % cache.size
	cache.mu.Unlock()
}

// GetLatestFrame returns the latest cached frame for a camera
func (fm *FrameManager) GetLatestFrame(cameraName string) *model.Frame {
	cache, exists := fm.Caches[cameraName]
	if !exists {
		return nil
	}

	cache.mu.RLock()
	defer cache.mu.RUnlock()

	// Get the most recent frame
	latestIndex := (cache.writeIndex - 1 + cache.size) % cache.size
	return cache.frames[latestIndex]
}

// GetNextFrame returns the next frame for streaming
func (fm *FrameManager) GetNextFrame(cameraName string) *model.Frame {
	cache, exists := fm.Caches[cameraName]
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
