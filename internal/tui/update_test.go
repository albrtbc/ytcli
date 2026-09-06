package tui

import (
	"errors"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AikonCWD/ytcli/internal/player"
	"github.com/AikonCWD/ytcli/internal/queue"
	"github.com/AikonCWD/ytcli/internal/track"
)

type fakePlayer struct {
	loaded   string
	toggled  int
	seeked   int
	volume   int
	stopped  int
	endCh    chan struct{}
	lostCh   chan struct{}
	curState player.State
	loadErr  error
}

func newFakePlayer() *fakePlayer {
	return &fakePlayer{endCh: make(chan struct{}, 1), lostCh: make(chan struct{}, 1)}
}

func (f *fakePlayer) Load(u string) error     { f.loaded = u; return f.loadErr }
func (f *fakePlayer) Stop() error             { f.stopped++; return nil }
func (f *fakePlayer) TogglePause() error      { f.toggled++; return nil }
func (f *fakePlayer) Seek(d int) error        { f.seeked += d; return nil }
func (f *fakePlayer) SetVolume(v int) error   { f.volume = v; return nil }
func (f *fakePlayer) State() player.State     { return f.curState }
func (f *fakePlayer) EndCh() <-chan struct{}  { return f.endCh }
func (f *fakePlayer) LostCh() <-chan struct{} { return f.lostCh }

type fakeSearch struct{ result []track.Track }

func (f *fakeSearch) Search(string, int) ([]track.Track, error) { return f.result, nil }
func (f *fakeSearch) Resolve(string) ([]track.Track, error)     { return f.result, nil }

type fakeStore struct {
	appended, favToggled int
	hist, favs, playlist []track.Track
	playlistSaves        int
}

func (f *fakeStore) AppendHistory(track.Track) error          { f.appended++; return nil }
func (f *fakeStore) ToggleFavorite(track.Track) (bool, error) { f.favToggled++; return true, nil }
func (f *fakeStore) LoadHistory() ([]track.Track, error)      { return f.hist, nil }
func (f *fakeStore) LoadFavorites() ([]track.Track, error)    { return f.favs, nil }
func (f *fakeStore) SavePlaylist(ts []track.Track) error {
	f.playlist = ts
	f.playlistSaves++
	return nil
}

func newTestModel() (Model, *fakePlayer, *fakeStore) {
	q := queue.New()
	q.Add(track.Track{ID: "a", URL: "ua"}, track.Track{ID: "b", URL: "ub"})
	fp := newFakePlayer()
	fs := &fakeStore{}
	m := New(q, fp, &fakeSearch{}, fs, 80)
	return m, fp, fs
}

func key(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestSpaceTogglesPause(t *testing.T) {
	m, fp, _ := newTestModel()
	m.Update(key(' '))
	// space is a special key, not a rune; send it as such:
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if fp.toggled == 0 {
		t.Fatal("space should toggle pause")
	}
}

func TestSeekKeys(t *testing.T) {
	m, fp, _ := newTestModel()
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if fp.seeked != 0 { // +10 then -10
		t.Fatalf("seeked = %d; want 0", fp.seeked)
	}
}

func TestNextLoadsAndRecordsHistory(t *testing.T) {
	m, fp, fs := newTestModel()
	m.Update(key('n'))
	if fp.loaded != "ub" {
		t.Fatalf("loaded = %q; want ub", fp.loaded)
	}
	if fs.appended == 0 {
		t.Fatal("next should append to history")
	}
}

func TestVolumeKeys(t *testing.T) {
	m, fp, _ := newTestModel()
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if fp.volume != 85 {
		t.Fatalf("volume = %d; want 85", fp.volume)
	}
}

func TestRepeatCycles(t *testing.T) {
	m, _, _ := newTestModel()
	m2, _ := m.Update(key('r'))
	if m2.(Model).q.Repeat() != queue.RepeatAll {
		t.Fatal("r should cycle to RepeatAll")
	}
}

func TestShuffleToggles(t *testing.T) {
	m, _, _ := newTestModel()
	m2, _ := m.Update(key('s'))
	if !m2.(Model).q.Shuffle() {
		t.Fatal("s should enable shuffle")
	}
}

func TestTabTogglesModeAndAltScreen(t *testing.T) {
	m, _, _ := newTestModel()
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m2.(Model).mode != modeExpanded {
		t.Fatal("tab should expand")
	}
	if cmd == nil {
		t.Fatal("expanding should enter the alt screen")
	}
	m3, cmd := m2.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	if m3.(Model).mode != modeCompact {
		t.Fatal("second tab should collapse")
	}
	if cmd == nil {
		t.Fatal("collapsing should exit the alt screen")
	}
}

func TestEscCollapsesExpandedMode(t *testing.T) {
	m, _, _ := newTestModel()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m3, cmd := m2.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m3.(Model).mode != modeCompact {
		t.Fatal("esc should collapse expanded mode")
	}
	if cmd == nil {
		t.Fatal("esc should exit the alt screen")
	}
}

func TestDeleteRemovesFromQueueAndSavesPlaylist(t *testing.T) {
	m, _, fs := newTestModel()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m3, _ := m2.(Model).Update(key('d'))
	mm := m3.(Model)
	if mm.q.Len() != 1 {
		t.Fatalf("queue len = %d; want 1", mm.q.Len())
	}
	if fs.playlistSaves == 0 {
		t.Fatal("removal should persist the playlist")
	}
	if len(fs.playlist) != 1 || fs.playlist[0].ID != "b" {
		t.Fatalf("saved playlist = %+v; want [b]", fs.playlist)
	}
}

func TestDeleteIgnoredOutsideQueueTab(t *testing.T) {
	m, _, fs := newTestModel()
	m2, _ := m.Update(key('3')) // history tab
	m3, _ := m2.(Model).Update(key('d'))
	if m3.(Model).q.Len() != 2 || fs.playlistSaves != 0 {
		t.Fatal("d must only act on the queue tab")
	}
}

func TestEmptySearchQueryDoesNotSearch(t *testing.T) {
	m, _, _ := newTestModel()
	m2, _ := m.Update(key('/'))
	m3, cmd := m2.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter on empty query should not launch a search")
	}
	if m3.(Model).searching {
		t.Fatal("enter should leave search input mode")
	}
}

func TestPlaySelectionFromSearchSavesPlaylist(t *testing.T) {
	m, _, fs := newTestModel()
	m.mode = modeExpanded
	m.tab = tabSearch
	m.results = []track.Track{{ID: "s1", URL: "us1", Title: "S"}}
	m.cursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if fs.playlistSaves == 0 {
		t.Fatal("adding a search result to the queue should persist the playlist")
	}
}

func TestDeleteCurrentTrackLoadsNext(t *testing.T) {
	m, fp, _ := newTestModel() // queue [a, b], current a
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m3, _ := m2.(Model).Update(key('d')) // cursor 0 == current
	mm := m3.(Model)
	if fp.loaded != "ub" {
		t.Fatalf("removing the playing track should load the next one; loaded=%q", fp.loaded)
	}
	if cur, _ := mm.q.Current(); cur.ID != "b" {
		t.Fatalf("current = %q; want b", cur.ID)
	}
}

func TestDeleteLastRemainingTrackStopsPlayer(t *testing.T) {
	m, fp, _ := newTestModel()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m3, _ := m2.(Model).Update(key('d'))
	m4, _ := m3.(Model).Update(key('d'))
	if m4.(Model).q.Len() != 0 {
		t.Fatal("queue should be empty")
	}
	if fp.stopped == 0 {
		t.Fatal("emptying the queue should stop playback")
	}
}

func TestPlaySelectionJumpsToExistingInsteadOfDuplicating(t *testing.T) {
	m, fp, fs := newTestModel() // queue [a, b]
	m.mode = modeExpanded
	m.tab = tabHistory
	m.history = []track.Track{{ID: "b", URL: "ub"}} // already queued
	m.cursor = 0
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := m2.(Model)
	if mm.q.Len() != 2 {
		t.Fatalf("queue len = %d; want 2 (no duplicate)", mm.q.Len())
	}
	if cur, _ := mm.q.Current(); cur.ID != "b" || fp.loaded != "ub" {
		t.Fatalf("should jump to existing entry; cur=%q loaded=%q", cur.ID, fp.loaded)
	}
	if fs.playlistSaves != 0 {
		t.Fatal("no queue change → no playlist rewrite")
	}
}

func size(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }

func TestFirstResizeDoesNotClearScreen(t *testing.T) {
	m, _, _ := newTestModel()
	m2, cmd := m.Update(size(120, 30))
	if cmd != nil {
		t.Fatal("the initial WindowSizeMsg must not clear the screen (would wipe startup output)")
	}
	if m2.(Model).width != 120 {
		t.Fatal("width not stored")
	}
}

func TestResizeInCompactClearsScreen(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"narrower", 80, 30},
		{"wider", 160, 30},
		{"shorter", 120, 14},
		{"taller", 120, 50},
		{"both", 80, 14},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _ := newTestModel()
			m2, _ := m.Update(size(120, 30))
			m3, cmd := m2.(Model).Update(size(tc.w, tc.h))
			if cmd == nil || !reflect.DeepEqual(cmd(), tea.ClearScreen()) {
				t.Fatal("resizing inline mode must clear the screen to remove stale rows")
			}
			if mm := m3.(Model); mm.width != tc.w || mm.height != tc.h {
				t.Fatal("terminal dimensions not updated")
			}
		})
	}
}

func TestUnchangedSizeDoesNotClearScreen(t *testing.T) {
	for _, mode := range []mode{modeCompact, modeExpanded} {
		m, _, _ := newTestModel()
		m.mode = mode
		m2, _ := m.Update(size(120, 30))
		m3, cmd := m2.(Model).Update(size(120, 30))
		if cmd != nil || m3.(Model).pendingClear {
			t.Fatal("repeated dimensions must not request a screen clear")
		}
	}
}

func TestResizeWhileExpandedClearsMainScreenOnCollapse(t *testing.T) {
	for _, dimensions := range []tea.WindowSizeMsg{size(90, 30), size(120, 14), size(120, 50)} {
		for _, help := range []bool{false, true} {
			m, _, _ := newTestModel()
			m2, _ := m.Update(size(120, 30))
			open := tea.KeyMsg{Type: tea.KeyTab}
			if help {
				open = key('?')
			}
			m3, _ := m2.(Model).Update(open) // alt screen
			m4, cmd := m3.(Model).Update(dimensions)
			if cmd != nil {
				t.Fatal("resize in alt screen needs no immediate clear (renderer repaints the alt buffer)")
			}
			if !m4.(Model).pendingClear {
				t.Fatal("resize under the alt screen should mark the main buffer dirty")
			}
			m5, cmd := m4.(Model).Update(tea.KeyMsg{Type: tea.KeyTab}) // collapse
			if cmd == nil {
				t.Fatal("collapsing after a resize must exit alt screen and clear the main buffer")
			}
			if m5.(Model).pendingClear {
				t.Fatal("pendingClear should reset after the collapse")
			}
		}
	}
}

func TestEndOfQueueSetsStatus(t *testing.T) {
	m, _, _ := newTestModel()
	m.q.JumpTo(1) // last track
	m2, _ := m.Update(endFileMsg{})
	if m2.(Model).status == "" {
		t.Fatal("end of queue should set a status message")
	}
}

func TestSlashEntersSearch(t *testing.T) {
	m, _, _ := newTestModel()
	m2, _ := m.Update(key('/'))
	mm := m2.(Model)
	if !mm.searching || mm.tab != tabSearch {
		t.Fatal("/ should enter search mode on the Search tab")
	}
}

func TestSpaceRuneTogglesPause(t *testing.T) {
	m, fp, _ := newTestModel()
	m.Update(key(' '))
	if fp.toggled == 0 {
		t.Fatal("space rune should toggle pause")
	}
}

func TestPlayLoadErrorSetsStatusAndSkipsHistory(t *testing.T) {
	m, fp, fs := newTestModel()
	fp.loadErr = errors.New("boom")
	m2, _ := m.Update(key('n'))
	mm := m2.(Model)
	if mm.status == "" {
		t.Fatal("load error should set status")
	}
	if fs.appended != 0 {
		t.Fatal("history should not be appended when load fails")
	}
}

func TestNumberKeyOpensAndLoadsHistoryTab(t *testing.T) {
	m, _, fs := newTestModel()
	fs.hist = []track.Track{{ID: "h1", Title: "Hist"}}
	m2, _ := m.Update(key('3'))
	mm := m2.(Model)
	if mm.mode != modeExpanded || mm.tab != tabHistory {
		t.Fatalf("'3' should open expanded Historial tab; mode=%v tab=%v", mm.mode, mm.tab)
	}
	if len(mm.history) != 1 || mm.history[0].ID != "h1" {
		t.Fatalf("history not loaded from store: %+v", mm.history)
	}
}

func TestNumberKeyOpensFavoritesTab(t *testing.T) {
	m, _, fs := newTestModel()
	fs.favs = []track.Track{{ID: "f1"}}
	m2, _ := m.Update(key('4'))
	mm := m2.(Model)
	if mm.tab != tabFavorites || len(mm.favorites) != 1 {
		t.Fatalf("'4' should open Favoritos and load favs; tab=%v favs=%+v", mm.tab, mm.favorites)
	}
}

func TestQuestionMarkOpensModalHelp(t *testing.T) {
	m, _, _ := newTestModel()
	m2, cmd := m.Update(key('?'))
	mm := m2.(Model)
	if !mm.showHelp || mm.mode != modeExpanded {
		t.Fatalf("? should open the help in the alt screen; showHelp=%v mode=%v", mm.showHelp, mm.mode)
	}
	if cmd == nil {
		t.Fatal("opening help from compact must enter the alt screen")
	}
	m3, cmd := mm.Update(key('x')) // any key closes
	mm = m3.(Model)
	if mm.showHelp || mm.mode != modeCompact {
		t.Fatalf("any key should close help and restore compact; showHelp=%v mode=%v", mm.showHelp, mm.mode)
	}
	if cmd == nil {
		t.Fatal("closing help back to compact must exit the alt screen")
	}
}

func TestHelpRestoresExpandedModeAndQQuits(t *testing.T) {
	m, _, _ := newTestModel()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab}) // expanded
	m3, _ := m2.(Model).Update(key('?'))
	m4, _ := m3.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm := m4.(Model)
	if mm.showHelp || mm.mode != modeExpanded {
		t.Fatalf("help opened from expanded should restore expanded; showHelp=%v mode=%v", mm.showHelp, mm.mode)
	}
	m5, cmd := mm.Update(key('?'))
	m6, cmd := m5.(Model).Update(key('q'))
	if !m6.(Model).quit || cmd == nil {
		t.Fatal("q inside the help should quit")
	}
}

func TestToggleFavoriteUpdatesStarSet(t *testing.T) {
	m, _, _ := newTestModel()
	m2, _ := m.Update(key('f')) // fakeStore.ToggleFavorite returns true (added)
	if !m2.(Model).favIDs["a"] {
		t.Fatal("favoriting the current track should register its ID for the ⭐ marker")
	}
}

func TestPlayerLostSetsStatus(t *testing.T) {
	m, _, _ := newTestModel()
	m2, _ := m.Update(playerLostMsg{})
	if m2.(Model).status == "" {
		t.Fatal("playerLostMsg should set a status message")
	}
}
