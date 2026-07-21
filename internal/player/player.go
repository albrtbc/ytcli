// Package player runs an mpv process and drives it over mpv's JSON IPC. The
// transport is OS-specific — a Windows named pipe or a Unix domain socket, see
// ipc_windows.go / ipc_unix.go. Audio only; mpv resolves YouTube via yt-dlp.
package player

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"
)

type State struct {
	Position int
	Duration int
	Volume   int
	Paused   bool
	Title    string
}

// Player runs and controls a single mpv process over its JSON IPC pipe.
//
// Concurrency: State() and the internal readLoop are mutex-guarded and safe to
// run concurrently. The setter methods (Load, Seek, SetVolume, SetPaused,
// TogglePause) write to the IPC connection WITHOUT a write mutex, so they must
// all be called from one goroutine — in ytcli that is the Bubbletea Update
// loop. readLoop is the sole reader of the connection.
type Player struct {
	mpvPath  string
	pipeName string
	cmd      *exec.Cmd
	conn     net.Conn
	mu       sync.Mutex
	state    State
	endCh    chan struct{}
	lostCh   chan struct{}
}

func New(mpvPath string) *Player {
	return &Player{mpvPath: mpvPath, pipeName: defaultIPCName, endCh: make(chan struct{}, 1), lostCh: make(chan struct{}, 1)}
}

func (p *Player) Start() error {
	if err := prepareIPC(p.pipeName); err != nil {
		return fmt.Errorf("preparando el IPC de mpv: %w", err)
	}
	p.cmd = exec.Command(p.mpvPath,
		"--no-video", "--idle=yes", "--no-terminal",
		"--input-ipc-server="+p.pipeName,
		"--volume=80",
	)
	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("lanzando mpv: %w", err)
	}

	var conn net.Conn
	var err error
	for i := 0; i < 50; i++ { // mpv tarda un momento en crear el pipe/socket
		conn, err = dialIPC(p.pipeName, 500*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		p.Close()
		return fmt.Errorf("conectando al IPC de mpv: %w", err)
	}
	p.conn = conn

	for i, name := range []string{"time-pos", "duration", "volume", "pause", "media-title"} {
		if _, err := p.conn.Write(cmdObserve(i+1, name)); err != nil {
			p.Close()
			return fmt.Errorf("observando propiedades de mpv: %w", err)
		}
	}
	go p.readLoop()
	return nil
}

func (p *Player) readLoop() {
	sc := bufio.NewScanner(p.conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		p.mu.Lock()
		ev, reason, _ := applyLine(line, &p.state)
		p.mu.Unlock()
		if ev == "end-file" && reason == "eof" {
			select {
			case p.endCh <- struct{}{}:
			default:
			}
		}
	}
	select {
	case p.lostCh <- struct{}{}:
	default:
	}
}

func (p *Player) send(b []byte) error {
	if p.conn == nil {
		return errors.New("player no iniciado")
	}
	_, err := p.conn.Write(b)
	return err
}

func (p *Player) Load(url string) error  { return p.send(cmdLoad(url)) }

// Stop ends playback and returns mpv to idle; end-file fires with a non-eof
// reason, so it does not trigger queue auto-advance.
func (p *Player) Stop() error { return p.send(cmdStop()) }
func (p *Player) SetPaused(v bool) error { return p.send(cmdSetPause(v)) }
func (p *Player) Seek(d int) error       { return p.send(cmdSeek(d)) }
func (p *Player) SetVolume(v int) error  { return p.send(cmdSetVolume(v)) }

func (p *Player) TogglePause() error {
	p.mu.Lock()
	paused := p.state.Paused
	p.mu.Unlock()
	return p.SetPaused(!paused)
}

func (p *Player) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *Player) EndCh() <-chan struct{} { return p.endCh }

// LostCh signals once when the IPC read loop ends (mpv/pipe gone).
func (p *Player) LostCh() <-chan struct{} { return p.lostCh }

func (p *Player) Close() error {
	if p.conn != nil {
		p.conn.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}
