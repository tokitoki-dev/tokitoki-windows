// Package appupdate asks the TokiToki server whether a newer Windows app
// build exists. It only answers the question — the download itself happens in
// the user's browser, so the app never has to swap its own running executable.
package appupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"time"
)

// Channel is the release channel the Windows app reads on the TokiToki server.
const Channel = "windows"

const checkTimeout = 15 * time.Second

// ErrDevBuild marks a local build, which has no meaningful version to compare.
var ErrDevBuild = errors.New("development build; update checks are disabled")

// semverRE accepts what the server's update check accepts.
var semverRE = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+`)

// Update describes an available newer build.
type Update struct {
	// Version is the offered release version.
	Version string
	// DownloadURL is the absolute URL a browser can fetch the build from.
	DownloadURL string
}

// Check returns the available update, or nil when current is already the
// newest published build.
func Check(ctx context.Context, baseURL, current string) (*Update, error) {
	if !semverRE.MatchString(current) {
		return nil, ErrDevBuild
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	checkURL := fmt.Sprintf(
		"%s/api/updates/check?channel=%s&platform=windows&arch=%s&version=%s",
		baseURL, Channel, runtime.GOARCH, url.QueryEscape(current),
	)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "tokitoki-windows")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("check for update: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("check for update: server returned %s", response.Status)
	}

	var decoded struct {
		Available bool   `json:"available"`
		Version   string `json:"version"`
		URL       string `json:"url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("check for update: %w", err)
	}
	if !decoded.Available {
		return nil, nil
	}
	if decoded.URL == "" {
		return nil, fmt.Errorf("check for update: update %s has no download URL", decoded.Version)
	}
	return &Update{
		Version:     decoded.Version,
		DownloadURL: baseURL + decoded.URL,
	}, nil
}
