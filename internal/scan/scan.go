// Package scan detects secrets that must not leave the machine (audit S1).
//
// The default patterns mirror the Clairvoyance backup engine's secretScanPatterns.
// Binary content (any NUL byte) is skipped; files above MaxBytes are not scanned.
package scan

import (
	"bytes"
	"os"
	"regexp"
)

// Match is a single secret detection.
type Match struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
	Line    int    `json:"line"`
}

// DefaultPatterns mirror the backup engine's secretScanPatterns.
var DefaultPatterns = []string{
	`sk-ant-[A-Za-z0-9_-]{20,}`,
	`ghp_[A-Za-z0-9]{20,}`,
	`github_pat_[A-Za-z0-9_]{20,}`,
	`AKIA[A-Z0-9]{16}`,
	`xox[baprs]-[A-Za-z0-9-]{10,}`,
	`-----BEGIN [A-Z ]*PRIVATE KEY-----`,
	`"(?:client_secret|refresh_token|access_token|api[_-]?key)"\s*:\s*"[^"]{12,}`,
}

// Scanner holds compiled patterns and a per-file size cap.
type Scanner struct {
	res      []*regexp.Regexp
	MaxBytes int64
}

// New compiles patterns (nil = DefaultPatterns) into a Scanner.
func New(patterns []string) (*Scanner, error) {
	if patterns == nil {
		patterns = DefaultPatterns
	}
	s := &Scanner{MaxBytes: 5 << 20} // 5 MiB
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		s.res = append(s.res, re)
	}
	return s, nil
}

// Bytes scans a buffer; binary content (NUL present) returns no matches.
func (s *Scanner) Bytes(path string, data []byte) []Match {
	if bytes.IndexByte(data, 0) >= 0 {
		return nil
	}
	var out []Match
	for i, ln := range bytes.Split(data, []byte("\n")) {
		for _, re := range s.res {
			if re.Match(ln) {
				out = append(out, Match{Path: path, Pattern: re.String(), Line: i + 1})
			}
		}
	}
	return out
}

// File scans a file on disk, respecting MaxBytes.
func (s *Scanner) File(path string) ([]Match, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > s.MaxBytes {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return s.Bytes(path, data), nil
}
