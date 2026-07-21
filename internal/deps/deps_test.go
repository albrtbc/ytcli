package deps

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveBinaryPreferPath(t *testing.T) {
	look := func(n string) (string, error) { return `C:\tools\` + n + ".exe", nil }
	exists := func(string) bool { return false }
	got, ok := resolveBinary("mpv", `C:\bin`, look, exists)
	if !ok || got != `C:\tools\mpv.exe` {
		t.Fatalf("resolve = %q,%v; want C:\\tools\\mpv.exe,true", got, ok)
	}
}

func TestResolveBinaryFallbackBinDir(t *testing.T) {
	look := func(string) (string, error) { return "", errors.New("not found") }
	want := filepath.Join("bin", exeName("yt-dlp"))
	exists := func(p string) bool { return strings.HasSuffix(p, want) }
	got, ok := resolveBinary("yt-dlp", "bin", look, exists)
	if !ok || !strings.HasSuffix(got, want) {
		t.Fatalf("resolve = %q,%v; want suffix %q,true", got, ok, want)
	}
}

func TestResolveBinaryMissing(t *testing.T) {
	look := func(string) (string, error) { return "", errors.New("no") }
	exists := func(string) bool { return false }
	if _, ok := resolveBinary("mpv", `C:\bin`, look, exists); ok {
		t.Fatal("missing binary should return false")
	}
}

func TestParseReleaseAssets(t *testing.T) {
	js := strings.NewReader(`{"assets":[
		{"name":"mpv-x86_64-20250101-git-abc.7z","browser_download_url":"https://x/a.7z"},
		{"name":"mpv-dev-x86_64-20250101.7z","browser_download_url":"https://x/dev.7z"}
	]}`)
	assets, err := parseReleaseAssets(js)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 || assets[0].Name != "mpv-x86_64-20250101-git-abc.7z" {
		t.Fatalf("assets = %+v", assets)
	}
}

func TestPickMpvAsset(t *testing.T) {
	assets := []asset{
		{Name: "mpv-dev-x86_64-20250101.7z", URL: "dev"},
		{Name: "mpv-x86_64-v3-20250101-git-abc.7z", URL: "v3"},
		{Name: "mpv-x86_64-20250101-git-abc.7z", URL: "good"},
	}
	got, err := pickMpvAsset(assets)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "good" {
		t.Fatalf("picked %q; want the plain x86_64 build", got.URL)
	}
}

func TestDownloadWritesFileAndNoPartLeft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello-binary"))
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "tool.exe")
	if err := download(srv.URL, dst); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "hello-binary" {
		t.Fatalf("downloaded content = %q, err %v", string(b), err)
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal(".part temp file should not remain after success")
	}
}

func TestDownloadHTTPErrorLeavesNoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "tool.exe")
	if err := download(srv.URL, dst); err == nil {
		t.Fatal("expected error on HTTP 404")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("no file should be created on HTTP error")
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal("no .part file should remain on HTTP error")
	}
}
