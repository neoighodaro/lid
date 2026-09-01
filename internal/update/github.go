package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"
)

const (
	defaultBaseURL  = "https://api.github.com"
	apiTimeout      = 5 * time.Second
	downloadTimeout = 60 * time.Second
	maxDownload     = 256 << 20
)

var ErrNoReleases = errors.New("no releases published")

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

func (r Release) AssetURL(name string) (string, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.URL, true
		}
	}
	return "", false
}

type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	Repo       string
}

func NewClient(repo string) *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: downloadTimeout},
		BaseURL:    defaultBaseURL,
		Repo:       repo,
	}
}

func (c *Client) LatestRelease(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/repos/%s/releases/latest", c.BaseURL, c.Repo), nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Release{}, ErrNoReleases
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github answered %s", resp.Status)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, err
	}
	return rel, nil
}

func (c *Client) Download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the download answered %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownload))
}

func AssetName(version, goos, goarch string) string {
	return fmt.Sprintf("lid-%s-%s-%s.tar.gz", version, goos, goarch)
}

func PlatformAsset(version string) string {
	return AssetName(version, runtime.GOOS, runtime.GOARCH)
}
