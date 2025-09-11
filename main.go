package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/BrunoKrugel/snapshot2stream/internal/client"
	"github.com/BrunoKrugel/snapshot2stream/internal/config"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	cfg, err := config.NewConfig()
	if err != nil {
		panic(err)
	}

	client := client.NewRestyClient(cfg)

	cameras := map[string]string{
		"camera1": cfg.Cameras.Camera1,
		"camera2": cfg.Cameras.Camera2,
		// "camera3": cfg.Cameras.Camera3,
		// "camera4": cfg.Cameras.Camera4,
		// "camera5": cfg.Cameras.Camera5,
		// "camera6": cfg.Cameras.Camera6,
	}

	// Register handlers
	for name, url := range cameras {
		func(cameraName, cameraURL string) {
			http.HandleFunc("/"+cameraName, func(w http.ResponseWriter, r *http.Request) {
				streamCamera(w, r, client, cameraURL, cfg)
			})
			log.Printf("Camera endpoint ready: http://localhost:%s/%s", cfg.Server.Port, cameraName)
		}(name, url)
	}

	log.Printf("MJPEG server listening on :%s (FPS: %d)\n", cfg.Server.Port, cfg.Server.FPS)
	log.Fatal(http.ListenAndServe(":"+cfg.Server.Port, nil))
}

func streamCamera(w http.ResponseWriter, r *http.Request, client *client.Client, cameraUrl string, cfg *config.Config) {

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

	// Calculate frame interval from config
	frameInterval := time.Duration(1000/cfg.Server.FPS) * time.Millisecond

	ctx := r.Context()

	// Pre-allocate buffer for frame header to avoid repeated allocations
	headerBuf := make([]byte, 0, 128)

	for {
		// break if client disconnected
		select {
		case <-ctx.Done():
			log.Printf("client disconnected from %s", r.URL.Path)
			return
		default:
		}

		resp, err := client.GetStream(cameraUrl)
		if err != nil {
			log.Println("request error:", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}

		if resp.StatusCode() != http.StatusOK {
			log.Println("bad status:", resp.Status())
			if resp.RawResponse != nil && resp.RawResponse.Body != nil {
				resp.RawResponse.Body.Close()
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		body := resp.Body()
		if len(body) == 0 {
			if resp.RawResponse != nil && resp.RawResponse.Body != nil {
				resp.RawResponse.Body.Close()
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}

		// Build frame header efficiently
		headerBuf = headerBuf[:0]
		headerBuf = append(headerBuf, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: "...)
		headerBuf = append(headerBuf, fmt.Sprintf("%d", len(body))...)
		headerBuf = append(headerBuf, "\r\n\r\n"...)

		// Write frame header + image + separator in fewer syscalls
		if _, err := w.Write(headerBuf); err != nil {
			log.Println("write header error:", err)
			if resp.RawResponse != nil && resp.RawResponse.Body != nil {
				resp.RawResponse.Body.Close()
			}
			return
		}

		if _, err := w.Write(body); err != nil {
			log.Println("write body error:", err)
			if resp.RawResponse != nil && resp.RawResponse.Body != nil {
				resp.RawResponse.Body.Close()
			}
			return
		}

		if _, err := w.Write([]byte("\r\n")); err != nil {
			log.Println("write separator error:", err)
			if resp.RawResponse != nil && resp.RawResponse.Body != nil {
				resp.RawResponse.Body.Close()
			}
			return
		}

		flusher.Flush()

		// close response body to free the connection
		if resp.RawResponse != nil && resp.RawResponse.Body != nil {
			resp.RawResponse.Body.Close()
		}

		time.Sleep(frameInterval)
	}
}
