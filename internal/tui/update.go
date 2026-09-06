package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AikonCWD/ytcli/internal/queue"
	"github.com/AikonCWD/ytcli/internal/track"
)

type tickMsg time.Time
type endFileMsg struct{}
type playerLostMsg struct{}
type searchResultMsg struct {
	tracks []track.Track
	err    error
}

func tickCmd() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func listenEnd(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg { <-ch; return endFileMsg{} }
}

func listenLost(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg { <-ch; return playerLostMsg{} }
}

func (m Model) searchCmd(query string) tea.Cmd {
	yt := m.yt
	return func() tea.Msg {
		tracks, err := yt.Search(query, searchN)
		return searchResultMsg{tracks: tracks, err: err}
	}
}

func (m Model) Init() tea.Cmd {
	// ClearScreen anchors the player at the terminal's first row instead of
	// leaving it below the shell prompt and previous output.
	return tea.Batch(tea.ClearScreen, tickCmd(),
		listenEnd(m.player.EndCh()), listenLost(m.player.LostCh()))
}

func nextRepeat(r queue.RepeatMode) queue.RepeatMode {
	switch r {
	case queue.RepeatOff:
		return queue.RepeatAll
	case queue.RepeatAll:
		return queue.RepeatOne
	default:
		return queue.RepeatOff
	}
}

// playCurrent loads the queue's current track and records it in history.
func (m *Model) playCurrent() {
	t, ok := m.q.Current()
	if !ok {
		return
	}
	if err := m.player.Load(t.URL); err != nil {
		m.status = "Error al reproducir: " + err.Error()
		return
	}
	m.status = ""
	m.store.AppendHistory(t)
}

// savePlaylist persists the queue to playlist.txt next to the exe. It saves
// the insertion order, not the playback order, so an active shuffle never
// rewrites the user's curated file in scrambled order.
func (m *Model) savePlaylist() {
	if err := m.store.SavePlaylist(m.q.OriginalTracks()); err != nil {
		m.status = "Error al guardar la playlist: " + err.Error()
	}
}

// setMode switches compact/expanded and keeps the terminal screen in sync:
// expanded mode lives in the alt screen because growing the inline view in
// place is unreliable (ConPTY scroll desync eats the bottom border).
func (m *Model) setMode(newMode mode) tea.Cmd {
	if m.mode == newMode {
		return nil
	}
	m.mode = newMode
	if newMode == modeExpanded {
		return tea.EnterAltScreen
	}
	if m.pendingClear {
		// The terminal was resized while in the alt screen: the main buffer
		// content may have rewrapped or scrolled, so redraw it after switching back.
		m.pendingClear = false
		return tea.Sequence(tea.ExitAltScreen, tea.ClearScreen)
	}
	return tea.ExitAltScreen
}

// clampCursor keeps the cursor inside a list of length n.
func (m *Model) clampCursor(n int) {
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m Model) activeList() []track.Track {
	switch m.tab {
	case tabSearch:
		return m.results
	case tabHistory:
		return m.history
	case tabFavorites:
		return m.favorites
	default:
		return m.q.Tracks()
	}
}

// selectTab switches to tab t in expanded mode, loading store-backed lists.
func (m Model) selectTab(t tab) (tea.Model, tea.Cmd) {
	cmd := m.setMode(modeExpanded)
	m.tab = t
	m.cursor = 0
	switch t {
	case tabHistory:
		m.history, _ = m.store.LoadHistory()
	case tabFavorites:
		m.favorites, _ = m.store.LoadFavorites()
	}
	return m, cmd
}

// toggleFavorite favorites the selected list item (expanded, non-queue tab) or
// else the current track, and refreshes the favorites list if it's showing.
func (m Model) toggleFavorite() (tea.Model, tea.Cmd) {
	var t track.Track
	var ok bool
	list := m.activeList()
	if m.mode == modeExpanded && m.tab != tabQueue && m.cursor >= 0 && m.cursor < len(list) {
		t, ok = list[m.cursor], true
	} else {
		t, ok = m.q.Current()
	}
	if !ok {
		return m, nil
	}
	added, err := m.store.ToggleFavorite(t)
	if err != nil {
		m.status = "Error al marcar favorito: " + err.Error()
		return m, nil
	}
	if added {
		m.favIDs[t.ID] = true
	} else {
		delete(m.favIDs, t.ID)
	}
	if m.tab == tabFavorites {
		m.favorites, _ = m.store.LoadFavorites()
		m.clampCursor(len(m.favorites))
	}
	return m, nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Width changes can rewrap lines; height changes can scroll or restore
		// rows in tmux. Both can move the inline renderer's cursor anchor and
		// leave stale copies of the player, so clear after either changes.
		sizeChanged := m.sizeKnown && (msg.Width != m.width || msg.Height != m.height)
		m.sizeKnown = true
		m.width, m.height = msg.Width, msg.Height
		if !sizeChanged {
			return m, nil
		}
		if m.mode == modeCompact {
			return m, tea.ClearScreen
		}
		m.pendingClear = true // redraw the main buffer when leaving the alt screen
		return m, nil

	case tickMsg:
		m.st = m.player.State()
		return m, tickCmd()

	case endFileMsg:
		if _, ok := m.q.Next(); ok {
			m.playCurrent()
		} else {
			m.status = "Fin de la cola"
		}
		return m, listenEnd(m.player.EndCh())

	case playerLostMsg:
		m.status = "Conexión con mpv perdida"
		return m, nil

	case searchResultMsg:
		if msg.err != nil {
			m.status = "Error de búsqueda: " + msg.err.Error()
			return m, nil
		}
		m.results = msg.tracks
		m.cursor = 0
		m.status = ""
		if len(msg.tracks) == 0 {
			m.status = "Sin resultados"
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}
