package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"pilotserver/internal/store"
)

func TestSSHAuditInsertListAndCap(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.InsertSSHAudit(store.SSHAudit{DongleID: "d1", Action: "tunnel", Port: 41001}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertSSHAudit(store.SSHAudit{DongleID: "d2", Action: "pty", Port: 41002, CreatedAt: 100}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ListSSHAudit(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].DongleID != "d2" || entries[0].Action != "pty" || entries[1].DongleID != "d1" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestUploadPolicyPruneExpiredAndCapKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().Unix()
	old := store.Segment{DongleID: "d1", RouteName: "old", SegmentName: "0", RelPath: "old/0/a.ts", Size: 100, UploadedAt: now - 10}
	mid := store.Segment{DongleID: "d1", RouteName: "mid", SegmentName: "0", RelPath: "mid/0/b.ts", Size: 100, UploadedAt: now - 5}
	fresh := store.Segment{DongleID: "d1", RouteName: "new", SegmentName: "0", RelPath: "new/0/c.ts", Size: 50, UploadedAt: now}
	for _, segment := range []store.Segment{old, mid, fresh} {
		if err := s.InsertSegment(segment); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "uploads", segment.DongleID, filepath.FromSlash(segment.RelPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	expired, err := s.ListSegmentsUploadedBefore(now - 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0].RelPath != old.RelPath {
		t.Fatalf("expired = %+v", expired)
	}
	if err := s.DeleteSegment(old.DongleID, old.RelPath); err != nil {
		t.Fatal(err)
	}
	total, err := s.TotalUploadBytes()
	if err != nil {
		t.Fatal(err)
	}
	if total != 150 {
		t.Fatalf("total = %d", total)
	}
	oldest, ok, err := s.OldestSegmentExcept("", "")
	if err != nil || !ok || oldest.RelPath != mid.RelPath {
		t.Fatalf("oldest = %+v ok=%v err=%v", oldest, ok, err)
	}
}
