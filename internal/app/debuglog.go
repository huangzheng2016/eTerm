package app

import (
	"log"
	"os"
)

func appDebugEnabled() bool {
	return os.Getenv("ETERM_DEBUG_APP") != ""
}

func appDebugf(format string, args ...any) {
	if !appDebugEnabled() {
		return
	}
	log.Printf("[eterm:app] "+format, args...)
}
