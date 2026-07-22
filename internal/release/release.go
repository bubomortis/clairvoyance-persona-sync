// Package release queries the GitHub Releases API for the latest clvsync release
// and compares it against the running version. Public releases need no token.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/version"
)

// Asset is one downloadable release file.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Release is the subset of a GitHub release clvsync uses.
type Release struct {
	Tag     string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

// AssetURL returns the download URL for the named asset, if present.
func (r *Release) AssetURL(name string) (string, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.URL, true
		}
	}
	return "", false
}

// Latest fetches the latest published release for the clvsync repo.
func Latest(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", version.Owner, version.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "clvsync/"+version.Version)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases API: %s", resp.Status)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.Tag == "" {
		return nil, fmt.Errorf("latest release has no tag")
	}
	return &rel, nil
}

// core parses a version into its numeric components, dropping a leading 'v' and any
// pre-release/build suffix. Returns nil if any component is non-numeric (e.g. "dev").
func core(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		nums = append(nums, n)
	}
	return nums
}

// Compare returns (-1|0|1, true) for a<b, a==b, a>b using dotted-numeric semantics,
// or (0, false) if either version is not a parseable release version.
func Compare(a, b string) (int, bool) {
	na, nb := core(a), core(b)
	if na == nil || nb == nil {
		return 0, false
	}
	for i := 0; i < len(na) || i < len(nb); i++ {
		var x, y int
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		if x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	// Equal numeric core: a pre-release (e.g. 0.2.5-rc1) precedes the final release with
	// the same core (SemVer §11), so an rc build correctly sees the final as newer and is
	// offered the update instead of being told "up to date". Build metadata (+...) does
	// not affect precedence.
	switch pa, pb := isPrerelease(a), isPrerelease(b); {
	case pa && !pb:
		return -1, true
	case !pa && pb:
		return 1, true
	}
	return 0, true
}

// isPrerelease reports whether v carries a SemVer pre-release marker (a '-' that is not
// part of trailing build metadata). "0.2.5-rc1" is a pre-release; "0.2.5" and "0.2.5+ci"
// are not.
func isPrerelease(v string) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	dash := strings.IndexByte(v, '-')
	if dash < 0 {
		return false
	}
	if plus := strings.IndexByte(v, '+'); plus >= 0 && dash > plus {
		return false // the '-' lives inside build metadata, not a pre-release tag
	}
	return true
}
