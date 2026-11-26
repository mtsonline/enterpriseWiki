//go:build !unix

package shellcmd

import (
	"context"
	"errors"
	"time"
)

// Run is not available outside Unix systems.
func Run(context.Context, string, []string, time.Duration, string) (string, int, error) {
	return "", -1, errors.New("shell commands are only supported on Unix systems")
}
