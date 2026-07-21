// Package store persists history, favorites and the playlist as plain-text
// files next to the executable, so the whole player is self-contained and the
// lists can be edited by hand: one track per line, TAB-separated fields
// URL <TAB> título <TAB> canal <TAB> duración_s|live <TAB> id. Every field
// after the URL is optional, so pasting a bare URL on a line is enough.
package store

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/AikonCWD/ytcli/internal/track"
)

const (
	historyCap    = 200
	historyFile   = "history.txt"
	favoritesFile = "favorites.txt"
	playlistFile  = "playlist.txt"
)

var fileHeaders = map[string]string{
	historyFile:   "historial (más reciente primero)",
	favoritesFile: "favoritos",
	playlistFile:  "lista de reproducción (la cola)",
}

type Store struct{ dir string }

func New(dir string) *Store { return &Store{dir: dir} }

// DefaultDir returns where ytcli keeps its text lists. On Windows that is the
// directory of the running executable (self-contained, as originally designed);
// elsewhere it is $XDG_CONFIG_HOME/ytcli (o ~/.config/ytcli), which se crea si
// no existe, para no ensuciar el directorio del binario (p.ej. ~/.local/bin).
func DefaultDir() (string, error) {
	if runtime.GOOS == "windows" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.Dir(exe), nil
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfg, "ytcli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

// videoIDFromURL extracts the YouTube video id (watch?v=, youtu.be/,
// /shorts/, /live/, /embed/), falling back to the URL itself so hand-added
// entries still get a stable identity. It must agree with the ids yt-dlp
// returns or dedup against resolved tracks breaks.
func videoIDFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if id := u.Query().Get("v"); id != "" {
		return id
	}
	seg := strings.Split(strings.Trim(u.Path, "/"), "/")
	if strings.Contains(u.Host, "youtu.be") && len(seg) > 0 && seg[0] != "" {
		return seg[0]
	}
	if len(seg) >= 2 && seg[len(seg)-1] != "" {
		switch seg[len(seg)-2] {
		case "shorts", "live", "embed", "v":
			return seg[len(seg)-1]
		}
	}
	return raw
}

// LooksLikeURL reports whether a string plausibly is a link (with or without
// scheme). The store uses it so stray hand-edited text doesn't become phantom
// tracks; main uses it to tell URL args from search words.
func LooksLikeURL(s string) bool {
	return strings.Contains(s, "://") || strings.HasPrefix(s, "www.") ||
		strings.HasPrefix(s, "youtube.com/") || strings.HasPrefix(s, "youtu.be/")
}

// sanitizeField keeps the TAB-separated line format intact.
func sanitizeField(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\r':
			return ' '
		}
		return r
	}, s)
}

func encodeLine(t track.Track) string {
	dur := strconv.Itoa(t.Duration)
	if t.Live {
		dur = "live"
	}
	fields := []string{t.URL, t.Title, t.Channel, dur, t.ID}
	for i, f := range fields {
		fields[i] = sanitizeField(f)
	}
	return strings.Join(fields, "\t")
}

// decodeLine parses one text line; ok is false for blanks and # comments.
func decodeLine(line string) (track.Track, bool) {
	line = strings.TrimRight(line, "\r\n")
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return track.Track{}, false
	}
	f := strings.Split(line, "\t")
	t := track.Track{URL: strings.TrimSpace(f[0])}
	if !LooksLikeURL(t.URL) {
		return track.Track{}, false
	}
	if !strings.Contains(t.URL, "://") {
		t.URL = "https://" + t.URL // mpv needs a scheme or it tries a local path
	}
	if len(f) > 1 {
		t.Title = strings.TrimSpace(f[1])
	}
	if len(f) > 2 {
		t.Channel = strings.TrimSpace(f[2])
	}
	if len(f) > 3 {
		d := strings.TrimSpace(f[3])
		if d == "live" {
			t.Live = true
		} else if n, err := strconv.Atoi(d); err == nil {
			t.Duration = n
		}
	}
	if len(f) > 4 {
		t.ID = strings.TrimSpace(f[4])
	}
	if t.ID == "" {
		t.ID = videoIDFromURL(t.URL)
	}
	return t, true
}

func (s *Store) load(name string) ([]track.Track, error) {
	b, err := os.ReadFile(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ts []track.Track
	for _, line := range strings.Split(string(b), "\n") {
		if t, ok := decodeLine(line); ok {
			ts = append(ts, t)
		}
	}
	return ts, nil
}

func (s *Store) save(name string, ts []track.Track) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# ytcli — " + fileHeaders[name] + "\n")
	b.WriteString("# formato: URL <TAB> título <TAB> canal <TAB> duración_s|live <TAB> id\n")
	for _, t := range ts {
		b.WriteString(encodeLine(t))
		b.WriteString("\n")
	}
	return os.WriteFile(s.path(name), []byte(b.String()), 0o644)
}

func (s *Store) LoadHistory() ([]track.Track, error) { return s.load(historyFile) }

func (s *Store) AppendHistory(t track.Track) error {
	ts, err := s.load(historyFile)
	if err != nil {
		return err
	}
	out := make([]track.Track, 0, len(ts)+1)
	out = append(out, t)
	for _, x := range ts {
		if x.ID != t.ID {
			out = append(out, x)
		}
	}
	if len(out) > historyCap {
		out = out[:historyCap]
	}
	return s.save(historyFile, out)
}

func (s *Store) LoadFavorites() ([]track.Track, error) { return s.load(favoritesFile) }

func (s *Store) IsFavorite(id string) (bool, error) {
	ts, err := s.load(favoritesFile)
	if err != nil {
		return false, err
	}
	for _, x := range ts {
		if x.ID == id {
			return true, nil
		}
	}
	return false, nil
}

// ToggleFavorite adds or removes t and returns the new favorite state.
func (s *Store) ToggleFavorite(t track.Track) (bool, error) {
	ts, err := s.load(favoritesFile)
	if err != nil {
		return false, err
	}
	for i, x := range ts {
		if x.ID == t.ID {
			ts = append(ts[:i], ts[i+1:]...)
			return false, s.save(favoritesFile, ts)
		}
	}
	ts = append([]track.Track{t}, ts...)
	return true, s.save(favoritesFile, ts)
}

func (s *Store) LoadPlaylist() ([]track.Track, error) { return s.load(playlistFile) }

func (s *Store) SavePlaylist(ts []track.Track) error {
	return s.save(playlistFile, ts)
}

// MigrateJSON imports history.json/favorites.json from oldDir (the pre-v1.1
// %APPDATA% store) into the text files, only when these don't exist yet.
func (s *Store) MigrateJSON(oldDir string) error {
	pairs := []struct{ old, new string }{
		{"history.json", historyFile},
		{"favorites.json", favoritesFile},
	}
	for _, p := range pairs {
		if _, err := os.Stat(s.path(p.new)); err == nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(oldDir, p.old))
		if err != nil {
			continue
		}
		var ts []track.Track
		if json.Unmarshal(b, &ts) != nil {
			continue
		}
		if err := s.save(p.new, ts); err != nil {
			return err
		}
	}
	return nil
}
