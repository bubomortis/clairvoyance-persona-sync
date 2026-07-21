// Package scan detects secrets that must not leave the machine (audit S1).
//
// The default patterns mirror the Clairvoyance backup engine's secretScanPatterns.
// Binary content (any NUL byte) is skipped; files above MaxBytes are not scanned.
package scan

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"regexp"
)

// Match is a single secret detection.
type Match struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
	Line    int    `json:"line"`
}

// Skip records a file the scanner could not fully inspect, so a skip is never
// silently mistaken for "clean" (audit P4).
type Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
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

// File scans a file on disk line-by-line so files of any size are inspected with
// bounded memory (P4: MaxBytes no longer causes a silent skip). Binary files
// (a NUL byte in the first 8 KiB) are reported as a Skip rather than passed as
// clean. A non-nil *Skip means the file was not fully text-scanned.
func (s *Scanner) File(path string) ([]Match, *Skip, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	// Sniff for binary content up front (mirrors git's 8 KiB heuristic).
	head := make([]byte, 8192)
	n, rerr := io.ReadFull(f, head)
	if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
		return nil, nil, rerr
	}
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return nil, &Skip{Path: path, Reason: "binary (NUL byte) — not text-scanned"}, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, nil, err
	}

	var out []Match
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20) // tolerate long lines up to 8 MiB
	line := 0
	for sc.Scan() {
		line++
		ln := sc.Bytes()
		for _, re := range s.res {
			if re.Match(ln) {
				out = append(out, Match{Path: path, Pattern: re.String(), Line: line})
			}
		}
	}
	if err := sc.Err(); err != nil {
		// e.g. a single line longer than the buffer: surface it instead of hiding it.
		return out, &Skip{Path: path, Reason: "partial scan: " + err.Error()}, nil
	}
	return out, nil, nil
}
