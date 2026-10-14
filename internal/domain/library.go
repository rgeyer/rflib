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
	Shared bool
	// Default marks the library used when none is chosen. There is always
	// exactly one, and it is never the shared library.
	Default   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DefaultLibrary is the name of the library every database starts with,
// which is also the default library (see Library.Default) until the user
// renames it or chooses another.
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
