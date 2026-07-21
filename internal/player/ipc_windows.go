//go:build windows

package player

import (
	"net"
	"time"

	winio "github.com/Microsoft/go-winio"
)

// defaultIPCName is the named pipe mpv exposes its JSON IPC on.
const defaultIPCName = `\\.\pipe\ytcli-mpv`

func dialIPC(name string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(name, &timeout)
}

// prepareIPC is a no-op on Windows; named pipes need no pre-run cleanup.
func prepareIPC(string) error { return nil }
