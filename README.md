# snapshot2stream

A lightweight Go application that converts static camera snapshots into MJPEG streams.

This tool is designed to solve the common problem where camera manufacturers or applications don't expose proper streaming endpoints that can be consumed by modern video management systems.

## Problem

Many security cameras and IoT devices only provide snapshot URLs instead of proper video streams, or require authentication for streaming.

This creates integration challenges when trying to use these cameras with popular video management systems like Frigate, go2rtc or Home Assistant.

## Solution

snapshot2stream bridges this gap by:

1. Continuously fetching images from snapshot URLs
2. Converting them into a proper MJPEG stream
3. Able to use authentication (cookies and tokens)
4. Serving the stream via HTTP endpoints that can be consumed by any application

## Features

- Converts static snapshots to live MJPEG streams
- High performance with optimized HTTP transport and connection pooling
- Configurable via environment variables (FPS, port, timeouts)
- Support for multiple cameras simultaneously
- Authentication support (cookies and tokens)
- Automatic reconnection and error handling with exponential backoff
- Configurable FPS (1-30 FPS, default 10)
- Optimized memory allocation and reduced syscalls

## Usage

1. Set up your environment variables:
```bash
export camera1="https://your-camera-ip/snapshot"
export camera2="https://another-camera-ip/snapshot"
export token="your-bearer-token"      # Optional
export cookie="session-cookie"        # Optional
export PORT="8081"                    # Optional (default: 8081)
export FPS="15"                       # Optional (default: 10, max: 30)
export LOG_LEVEL="info"               # Optional (default: info)
```

2. Run the application:
```bash
make run
```

3. Access your camera streams:
- Camera 1: `http://localhost:8081/camera1`
- Camera 2: `http://localhost:8081/camera2`

## Integration Examples

### Frigate
```yaml
cameras:
  backyard:
    ffmpeg:
      inputs:
        - path: http://localhost:8081/camera1
          roles:
            - detect
```

### go2rtc
```yaml
streams:
  backyard: http://localhost:8081/camera1
```

### Home Assistant
```yaml
camera:
  - platform: mjpeg
    mjpeg_url: http://localhost:8081/camera1
    name: Backyard Camera
```
