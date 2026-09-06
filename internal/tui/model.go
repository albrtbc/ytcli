package tui

import (
	"github.com/AikonCWD/ytcli/internal/player"
	"github.com/AikonCWD/ytcli/internal/queue"
	"github.com/AikonCWD/ytcli/internal/track"
)

// Ports keep the model testable with fakes and decouple it from concrete deps.
type playerPort interface {
	Load(url string) error
	Stop() error
	TogglePause() error
	Seek(delta int) error
	SetVolume(v int) error
	State() player.State
	EndCh() <-chan struct{}
	LostCh() <-chan struct{}
}

type searchPort interface {
	Search(query string, n int) ([]track.Track, error)
	Resolve(url string) ([]track.Track, error)
}

type storePort interface {
	AppendHistory(t track.Track) error
	ToggleFavorite(t track.Track) (bool, error)
	LoadHistory() ([]track.Track, error)
	LoadFavorites() ([]track.Track, error)
	SavePlaylist(ts []track.Track) error
}

type mode int

const (
	modeCompact mode = iota
	modeExpanded
)

type tab int

const (
	tabQueue tab = iota
	tabSearch
	tabHistory
	tabFavorites
)

const (
	seekStep   = 10
	volumeStep = 5
	searchN    = 15
)

type Model struct {
	q      *queue.Queue
	player playerPort
	yt     searchPort
	store  storePort

	st      player.State
	vol     int
	prevVol int // for mute restore

	mode      mode
	tab       tab
	searching bool
	query     string
	cursor    int

	showHelp bool // modal help panel (alt screen)
	helpFrom mode // mode to restore when the help closes

	favIDs map[string]bool // favorite IDs, for the ⭐ marker in lists

	results   []track.Track
	history   []track.Track
	favorites []track.Track

	status string
	width  int
	height int
	quit   bool

	sizeKnown    bool // first WindowSizeMsg received
	pendingClear bool // terminal resized while in the alt screen
}

func New(q *queue.Queue, p playerPort, yt searchPort, st storePort, vol int) Model {
	favIDs := make(map[string]bool)
	if favs, err := st.LoadFavorites(); err == nil {
		for _, t := range favs {
			favIDs[t.ID] = true
		}
	}
	return Model{q: q, player: p, yt: yt, store: st, vol: vol, prevVol: vol,
		width: 46, favIDs: favIDs}
}
