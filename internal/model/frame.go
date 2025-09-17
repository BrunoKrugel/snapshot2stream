package model

import "time"

// Frame represents a cached camera frame
type Frame struct {
	Timestamp time.Time
	Data      []byte
}
