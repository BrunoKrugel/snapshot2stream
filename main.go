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
				streamCamera(w, r, client, cameraURL)
			})
			log.Printf("Camera endpoint ready: http://localhost:8081/%s", cameraName)
		}(name, url)
	}

	log.Println("MJPEG server listening on :8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

func streamCamera(w http.ResponseWriter, r *http.Request, client *client.Client, cameraUrl string) {
	// MJPEG headers
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Frame interval — (100ms ≈ 10 FPS)
	frameInterval := 100 * time.Millisecond

	ctx := r.Context()

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

		// Write MJPEG frame header + image
		if _, err := fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(body)); err != nil {
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

		if _, err := fmt.Fprint(w, "\r\n"); err != nil {
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
