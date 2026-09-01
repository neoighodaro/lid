package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func clientFor(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c := NewClient("neoighodaro/lid")
	c.BaseURL = server.URL
	return c
}

func TestTheLatestReleaseComesBackWithSomewhereToDownloadItFrom(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/neoighodaro/lid/releases/latest" {
			t.Errorf("asked for %s, want the latest release of the configured repository", r.URL.Path)
		}
		w.Write([]byte(`{"tag_name": "v1.3.0", "assets": [
			{"name": "lid-v1.3.0-darwin-arm64.tar.gz", "browser_download_url": "https://example.test/mac"},
			{"name": "checksums.txt", "browser_download_url": "https://example.test/sums"}
		]}`))
	})

	release, err := c.LatestRelease(context.Background())
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if release.TagName != "v1.3.0" {
		t.Errorf("TagName = %q, want v1.3.0", release.TagName)
	}
	if url, ok := release.AssetURL("lid-v1.3.0-darwin-arm64.tar.gz"); !ok || url != "https://example.test/mac" {
		t.Errorf("asset url = %q (found %v), want the one github listed", url, ok)
	}
	if _, ok := release.AssetURL("lid-v1.3.0-plan9-arm64.tar.gz"); ok {
		t.Error("an asset that is not in the release was found anyway")
	}
}

func TestARepositoryWithNoReleasesSaysSoInsteadOfFailingObscurely(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "Not Found"}`, http.StatusNotFound)
	})

	if _, err := c.LatestRelease(context.Background()); !errors.Is(err, ErrNoReleases) {
		t.Errorf("err = %v, want ErrNoReleases so 'lid update' can explain that nothing is published yet", err)
	}
}

func TestAFailedDownloadIsNotMistakenForAnEmptyFile(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusInternalServerError)
	})

	if _, err := c.Download(context.Background(), c.BaseURL+"/asset"); err == nil {
		t.Fatal("a 500 was returned as bytes, which would be installed as the lid binary")
	}
}

func TestTheAssetNameMatchesWhatTheMakefilePacks(t *testing.T) {
	if got := AssetName("v1.3.0", "darwin", "arm64"); got != "lid-v1.3.0-darwin-arm64.tar.gz" {
		t.Errorf("AssetName = %q, want the name 'make assets' writes into dist/", got)
	}
}
