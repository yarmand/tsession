package main

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// listenWithFallback tries to bind preferred (typically the same address
// tsession serve defaults to, so the native app and a manually-run
// `tsession serve` land on the same familiar URL when only one is
// running). If preferred is already in use — e.g. `tsession serve` is
// separately running, or a second copy of the native app is starting up —
// it falls back to an OS-assigned ephemeral port on the same host so the
// app still starts rather than failing outright. Other listen errors are
// returned as-is so configuration or permission problems are not hidden by
// an unexpected fallback port.
func listenWithFallback(preferred string) (net.Listener, error) {
	l, err := net.Listen("tcp", preferred)
	if err == nil {
		return l, nil
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		return nil, fmt.Errorf("listen on %s: %w", preferred, err)
	}

	host, _, splitErr := net.SplitHostPort(preferred)
	if splitErr != nil {
		return nil, fmt.Errorf("listen on %s: %w", preferred, err)
	}

	fallback, fallbackErr := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if fallbackErr != nil {
		return nil, fmt.Errorf("listen on %s failed (%v), and fallback ephemeral port also failed: %w", preferred, err, fallbackErr)
	}
	return fallback, nil
}
