package importer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bubomortis/clairvoyance-persona-sync/internal/clv"
)

// TestIsAppOwnedAggregate pins the §23.4 classification: only the profile staff.json
// roster and agent-history transcripts are app-owned aggregates; curated memory,
// templates, and workspace content are static and must verify strictly.
func TestIsAppOwnedAggregate(t *testing.T) {
	cases := map[string]bool{
		filepath.Join("d", "profiles", "p", "staff.json"):                            true,
		filepath.Join("d", "profiles", "p", "agent-history", "staff-abc.json"):       true,
		filepath.Join("d", ".clairvoyance", "staff", "reegor", "current-context.md"): false,
		filepath.Join("d", "profiles", "p", "staff-names.json"):                      false,
		filepath.Join("d", "neurons", "personas", "Sync Operator.md"):                false,
	}
	for p, want := range cases {
		if got := IsAppOwnedAggregate(p); got != want {
			t.Errorf("IsAppOwnedAggregate(%q) = %v, want %v", p, got, want)
		}
	}
}

// TestVerifyReceipt_AggregateRewriteIsAdvisory proves §23.4: an app-owned aggregate that
// the app rewrote on reopen becomes an advisory NOTE (OK stays true), while a static-file
// tamper and a missing aggregate are both still hard failures.
func TestVerifyReceipt_AggregateRewriteIsAdvisory(t *testing.T) {
	dir := t.TempDir()
	prof := filepath.Join(dir, "profiles", "p")
	if err := os.MkdirAll(filepath.Join(prof, "agent-history"), 0o755); err != nil {
		t.Fatal(err)
	}
	staff := filepath.Join(prof, "staff.json")
	hist := filepath.Join(prof, "agent-history", "staff-x.json")
	mem := filepath.Join(dir, ".clairvoyance", "staff", "r", "context.md")
	if err := os.MkdirAll(filepath.Dir(mem), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, staff, `{"a":1}`)
	mustWrite(t, hist, `[1]`)
	mustWrite(t, mem, "memory")

	r := &Receipt{}
	r.addFile(staff)
	r.addFile(hist)
	r.addFile(mem)
	if len(r.Files) != 3 {
		t.Fatalf("want 3 recorded files, got %d", len(r.Files))
	}
	// The aggregate flag is stamped at record time.
	for _, f := range r.Files {
		wantAgg := f.Path != mem
		if f.Aggregate != wantAgg {
			t.Errorf("%s Aggregate=%v, want %v", f.Path, f.Aggregate, wantAgg)
		}
	}

	in := &clv.Instance{DataDir: dir}

	// Clean state: everything matches, no advisories.
	if res := VerifyReceipt(r, in); !res.OK || res.Advisories != 0 {
		t.Fatalf("clean verify: OK=%v advisories=%d, want OK=true advisories=0", res.OK, res.Advisories)
	}

	// The app rewrites both aggregates on reopen → hashes differ, but reconciliation holds.
	mustWrite(t, staff, `{"a":2,"b":3}`)
	mustWrite(t, hist, `[1,2,3]`)
	res := VerifyReceipt(r, in)
	if !res.OK {
		t.Fatal("aggregate rewrite must NOT fail reconciliation (§23.4)")
	}
	if res.Advisories != 2 {
		t.Fatalf("want 2 advisories for the two rewritten aggregates, got %d", res.Advisories)
	}

	// Tampering the static curated-memory file IS a real failure.
	mustWrite(t, mem, "tampered")
	if VerifyReceipt(r, in).OK {
		t.Fatal("a tampered static memory file must fail reconciliation")
	}
	mustWrite(t, mem, "memory") // restore

	// A MISSING aggregate is still a failure — the app rewrites it, it does not delete it.
	if err := os.Remove(hist); err != nil {
		t.Fatal(err)
	}
	if VerifyReceipt(r, in).OK {
		t.Fatal("a missing app-owned aggregate must fail reconciliation")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
