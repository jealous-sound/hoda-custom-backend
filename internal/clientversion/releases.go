package clientversion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

const (
	releaseFeedUserAgent = "hoda-custom-backend"
	releaseFeedAccept    = "application/vnd.github+json"
	releaseBodyLimit     = 1 << 20
)

type ReleaseFeed struct {
	url    string
	client *http.Client

	mu     sync.RWMutex
	latest Version
	etag   string
}

func NewReleaseFeed(url string, client *http.Client) *ReleaseFeed {
	return &ReleaseFeed{url: url, client: client}
}

func (f *ReleaseFeed) Latest() Version {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.latest
}

func (f *ReleaseFeed) Refresh(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", releaseFeedUserAgent)
	request.Header.Set("Accept", releaseFeedAccept)
	f.mu.RLock()
	if f.etag != "" {
		request.Header.Set("If-None-Match", f.etag)
	}
	f.mu.RUnlock()
	response, err := f.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return nil
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("latest release: %s", response.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, releaseBodyLimit)).Decode(&release); err != nil {
		return fmt.Errorf("latest release: %w", err)
	}
	version, ok := Parse(release.TagName)
	if !ok {
		return fmt.Errorf("latest release tag %q is not a version", release.TagName)
	}
	f.mu.Lock()
	f.latest, f.etag = version, response.Header.Get("ETag")
	f.mu.Unlock()
	return nil
}
