// Package deps locates mpv and yt-dlp, downloading them into %LOCALAPPDATA%\ytcli\bin
// on first run when they are not already available on PATH.
package deps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"
)

type Paths struct {
	Mpv   string
	YtDlp string
}

const (
	ytDlpURL   = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp.exe"
	mpvRelease = "https://api.github.com/repos/shinchiro/mpv-winbuild-cmake/releases/latest"
)

// httpClient bounds how long a stalled download can hang.
var httpClient = &http.Client{Timeout: 10 * time.Minute}

// BinDir returns the directory where ytcli keeps auto-downloaded dependencies:
// %LOCALAPPDATA%\ytcli\bin on Windows, else $XDG_CACHE_HOME/ytcli/bin (o
// ~/.cache/ytcli/bin). En Linux/macOS mpv y yt-dlp suelen estar en el PATH, así
// que este directorio solo actúa de reserva y rara vez llega a usarse.
func BinDir() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA no está definido")
		}
		return filepath.Join(base, "ytcli", "bin"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ytcli", "bin"), nil
}

// exeName appends the platform executable suffix (".exe" only on Windows).
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// resolveBinary returns an existing path for name, preferring PATH then binDir.
func resolveBinary(name, binDir string,
	lookPath func(string) (string, error),
	exists func(string) bool) (string, bool) {
	if p, err := lookPath(name); err == nil {
		return p, true
	}
	cand := filepath.Join(binDir, exeName(name))
	if exists(cand) {
		return cand, true
	}
	return "", false
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type release struct {
	Assets []asset `json:"assets"`
}

func parseReleaseAssets(r io.Reader) ([]asset, error) {
	var rel release
	if err := json.NewDecoder(r).Decode(&rel); err != nil {
		return nil, err
	}
	return rel.Assets, nil
}

// mpvAssetRE matches the plain 64-bit build, excluding "-dev-" and "-v3-" variants.
var mpvAssetRE = regexp.MustCompile(`^mpv-x86_64-\d[\w.-]*\.7z$`)

func pickMpvAsset(assets []asset) (asset, error) {
	for _, a := range assets {
		if mpvAssetRE.MatchString(a.Name) {
			return a, nil
		}
	}
	return asset{}, errors.New("no se encontró un asset mpv compatible")
}

func download(url, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("descarga %s: HTTP %d", url, resp.StatusCode)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// Ensure returns paths to mpv and yt-dlp, downloading any that are missing.
func Ensure(binDir string, progress func(string)) (Paths, error) {
	if progress == nil {
		progress = func(string) {}
	}
	var p Paths

	if path, ok := resolveBinary("yt-dlp", binDir, exec.LookPath, fileExists); ok {
		p.YtDlp = path
	} else {
		progress("Descargando yt-dlp…")
		dst := filepath.Join(binDir, "yt-dlp.exe")
		if err := download(ytDlpURL, dst); err != nil {
			return p, fmt.Errorf("descargando yt-dlp: %w", err)
		}
		p.YtDlp = dst
	}

	if path, ok := resolveBinary("mpv", binDir, exec.LookPath, fileExists); ok {
		p.Mpv = path
	} else {
		progress("Buscando la última versión de mpv…")
		path, err := downloadMpv(binDir, progress)
		if err != nil {
			return p, fmt.Errorf("descargando mpv: %w", err)
		}
		p.Mpv = path
	}
	return p, nil
}

func downloadMpv(binDir string, progress func(string)) (string, error) {
	resp, err := httpClient.Get(mpvRelease)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("consultando release de mpv: HTTP %d", resp.StatusCode)
	}
	assets, err := parseReleaseAssets(resp.Body)
	if err != nil {
		return "", err
	}
	a, err := pickMpvAsset(assets)
	if err != nil {
		return "", err
	}
	progress("Descargando mpv (" + a.Name + ")…")
	archive := filepath.Join(binDir, a.Name)
	if err := download(a.URL, archive); err != nil {
		return "", err
	}
	progress("Extrayendo mpv…")
	dst := filepath.Join(binDir, "mpv.exe")
	if err := extractMpvExe(archive, dst); err != nil {
		return "", err
	}
	_ = os.Remove(archive)
	return dst, nil
}
