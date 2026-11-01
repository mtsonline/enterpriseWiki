//go:build !unix

package macro_shell

import (
	"context"
	"errors"
	"time"
)

func run(context.Context, string, time.Duration, string) (string, int, error) {
	return "", -1, errors.New("shell macros are only supported on Unix systems")
}
