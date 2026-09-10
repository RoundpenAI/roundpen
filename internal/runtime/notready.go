package runtime

import (
	"errors"
	"fmt"
)

// NotReady is returned when the user picked an engine the host cannot run yet.
type NotReady struct {
	Engine  string
	Message string
	Setup   []SetupStep
}

func (e *NotReady) Error() string {
	if e == nil {
		return "engine is not ready"
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("%s environment is not ready", e.Engine)
}

// IsNotReady reports whether err is (or wraps) NotReady.
func IsNotReady(err error) bool {
	var n *NotReady
	return errors.As(err, &n)
}

// AsNotReady extracts *NotReady from err.
func AsNotReady(err error) *NotReady {
	var n *NotReady
	if errors.As(err, &n) {
		return n
	}
	return nil
}
