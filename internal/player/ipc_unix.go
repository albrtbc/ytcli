//go:build !windows

package player

import (
	"net"
	"os"
	"path/filepath"
	"time"
)

// defaultIPCName is the Unix domain socket mpv exposes its JSON IPC on. It lives
// in $XDG_RUNTIME_DIR when available (per-user, wiped on logout) and falls back
// to the temp dir.
var defaultIPCName = func() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "ytcli-mpv.sock")
}()

func dialIPC(name string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.Dial("unix", name)
}

// prepareIPC removes a stale socket left by a previous run so mpv can bind.
func prepareIPC(name string) error {
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
