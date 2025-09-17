package utils

// isValidJPEG checks if the data is a valid JPEG image
func IsValidJPEG(data []byte) bool {
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
