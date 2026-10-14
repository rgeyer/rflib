package domain

import (
	"fmt"
	"strings"
	"time"
)

// Library is a separate collection of channels, radios, and saved
// searches. Names are unique within a library, not across them.
type Library struct {
	ID          string
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// DefaultLibrary is the library every database starts with, and the one
// used when none is chosen.
const DefaultLibrary = "default"

func ValidateLibrary(l Library) error {
	if strings.TrimSpace(l.Name) == "" {
		return fmt.Errorf("library name is required")
	}
	if l.Name != strings.TrimSpace(l.Name) {
		return fmt.Errorf("library name must not start or end with spaces")
	}
	return nil
}
