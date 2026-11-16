package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/update"
)

func fakeRelease(t *testing.T, tag string, binary []byte) *httptest.Server {
	t.Helper()
	var tgz bytes.Buffer
	gz := gzip.NewWriter(&tgz)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "lid", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()

	asset := update.AssetName(tag, runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(tgz.Bytes())
	checksums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/test/lid/releases/latest":
			_ = json.NewEncoder(w).Encode(update.Release{TagName: tag, Assets: []update.Asset{
				{Name: asset, URL: srv.URL + "/asset"},
				{Name: "checksums.txt", URL: srv.URL + "/checksums"},
			}})
		case "/asset":
			_, _ = w.Write(tgz.Bytes())
		case "/checksums":
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runSourceUpdate(t *testing.T, version, stdin string) (string, string, error) {
	t.Helper()
	srv := fakeRelease(t, "v9.9.9", []byte("released lid"))
	target := filepath.Join(t.TempDir(), "lid")
	if err := os.WriteFile(target, []byte("source build"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldTarget, oldClient := updateTarget, newUpdateClient
	t.Cleanup(func() { updateTarget, newUpdateClient = oldTarget, oldClient })
	updateTarget = func() (string, error) { return target, nil }
	newUpdateClient = func() *update.Client {
		c := update.NewClient("test/lid")
		c.BaseURL = srv.URL
		return c
	}

	var out, errBuf bytes.Buffer
	a := &app.App{Version: version, In: strings.NewReader(stdin), Out: &out, Err: &errBuf, Cwd: t.TempDir()}
	root := NewRootCommand(a)
	root.SetArgs([]string{"update"})
	err := root.Execute()

	installed, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(installed), errBuf.String(), err
}

func TestUpdateAsksBeforeReplacingASourceBuildWithTheRelease(t *testing.T) {
	installed, stderr, err := runSourceUpdate(t, "dev", "y\n")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "v9.9.9") || !strings.Contains(stderr, "[y/N]") {
		t.Errorf("the prompt does not name the release it would install:\n%s", stderr)
	}
	if installed != "released lid" {
		t.Errorf("after a yes the binary is %q, want the release", installed)
	}
}

func TestUpdateKeepsASourceBuildUnlessTheAnswerIsYes(t *testing.T) {
	for _, stdin := range []string{"", "n\n", "\n", "sure\n"} {
		for _, version := range []string{"dev", "v1.2.3-4-g8a17b2a-dirty"} {
			installed, stderr, err := runSourceUpdate(t, version, stdin)
			if err != nil {
				t.Fatalf("update on %s with %q: %v\n%s", version, stdin, err, stderr)
			}
			if installed != "source build" {
				t.Errorf("update on %s with answer %q overwrote the developer's own build", version, stdin)
			}
		}
	}
}
