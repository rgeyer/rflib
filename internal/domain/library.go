package domain

import (
	"strings"
	"time"
)

// Library is a separate collection of channels, radios, and saved
// searches. Names are unique within a library, not across them.
type Library struct {
	ID          string
	Name        string
	Description string
	// Shared marks the one library of channels shared with everyone (see
	// SharedLibrary). Other libraries link to its channels.
	Shared    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DefaultLibrary is the library every database starts with, and the one
// used when none is chosen.
const DefaultLibrary = "default"

// SharedLibrary is the name of the library of shared channels.
const SharedLibrary = "shared"

func ValidateLibrary(l Library) error {
	if strings.TrimSpace(l.Name) == "" {
		return Invalidf("library name is required")
	}
	if l.Name != strings.TrimSpace(l.Name) {
		return Invalidf("library name must not start or end with spaces")
	}
	return nil
}
