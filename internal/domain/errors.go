package domain

import (
	"errors"
	"fmt"
)

// ErrInvalid marks an error in what was asked, such as a malformed value or
// a request the rules refuse, as opposed to a failure in rflib or its
// storage. Front ends report it as the caller's to fix (HTTP 400).
var ErrInvalid = errors.New("invalid")

type invalidError struct{ msg string }

func (e invalidError) Error() string        { return e.msg }
func (e invalidError) Is(target error) bool { return target == ErrInvalid }

// Invalidf formats an error that is ErrInvalid; its message is the
// formatted text alone. It doesn't wrap: use it for errors made from
// scratch, and fmt.Errorf with %w to add context to another error.
func Invalidf(format string, args ...any) error {
	return invalidError{fmt.Sprintf(format, args...)}
}
