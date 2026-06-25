package main

import (
	"log"
	"os"
	"strings"
)

func businessDebugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BUSINESS_DEBUG"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func businessDebugf(format string, args ...any) {
	if !businessDebugEnabled() {
		return
	}
	log.Printf("[business-debug] "+format, args...)
}
