// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transport

import (
	"os"
)

// removeIfSocket removes path only if it exists and is a Unix-domain socket.
// It is used before binding a "unix" listener to clear a stale socket left
// by a crashed predecessor, without ever deleting a regular file or
// directory a misconfiguration might point at.
func removeIfSocket(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return nil // nothing there (or unstat-able) — let net.Listen decide
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return nil // not a socket — refuse to touch it
	}
	return os.Remove(path)
}
