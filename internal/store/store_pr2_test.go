package store

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestSavePR2RoundTripPreservesCompleteRecord(t *testing.T) {
	st, base, head := newStoreTestHistory(t)
	closed := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	pr := completePR2(st.repo.CommonRoot, base, head)
	pr.State = model.PRStateClosed
	pr.ClosedAt = &closed
	pr.Closure = &model.Closure{
		Reason:                    model.ClosureIntegrated,
		DestinationBranch:         "release",
		ResultingCommitSHAs:       []string{head},
		PatchEquivalentIdentities: []string{"patch-id:0123456789abcdef"},
		Note:                      "verified by an external gate",
	}

	if _, err := st.SavePR2(pr, "", ""); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := st.LoadPR2(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, pr) {
		t.Fatalf("round trip mismatch\n got: %#v\nwant: %#v", loaded, pr)
	}
}

func TestLoadPRDispatchesAbsentSchemaToLegacyAndSchema2ToPR2(t *testing.T) {
	st, legacy := newStoreTestPR(t)
	legacy, _, err := st.LoadLegacyPR(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacyRecord, _, err := st.LoadPR(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := legacyRecord.(model.PR); !ok || !reflect.DeepEqual(got, legacy) {
		t.Fatalf("legacy dispatch = %#v", legacyRecord)
	}
	_, legacyData, err := st.loadRecordData(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(legacyData, &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["schema"]; present {
		t.Fatalf("legacy YAML gained schema discriminator: %s", legacyData)
	}

	pr2 := model.PR2{Schema: 2, ID: "01SCHEMA2DISPATCH000000000", State: model.PRStateOpen}
	if _, err := st.SavePR2(pr2, "", ""); err != nil {
		t.Fatal(err)
	}
	record, _, err := st.LoadPR(pr2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := record.(model.PR2); !ok || !reflect.DeepEqual(got, pr2) {
		t.Fatalf("schema-2 dispatch = %#v", record)
	}
	if _, _, err := st.LoadLegacyPR(pr2.ID); !errors.Is(err, ErrRecordSchema) {
		t.Fatalf("legacy-only load error = %v, want ErrRecordSchema", err)
	}

	legacyListed, err := st.ListLegacyPRs("open")
	if err != nil {
		t.Fatal(err)
	}
	if len(legacyListed) != 1 || !reflect.DeepEqual(legacyListed[0], legacy) {
		t.Fatalf("legacy list changed: %#v", legacyListed)
	}
	records, err := st.ListPRs("open")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].RecordSchema()+records[1].RecordSchema() != 3 || records[0].RecordSchema() == records[1].RecordSchema() {
		t.Fatalf("mixed open list = %#v", records)
	}
}

func TestReviewEventRefsPinJudgedCommitsAcrossAggressiveGC(t *testing.T) {
	st, base, head := newStoreTestHistory(t)
	makeHistoryUnreachable(t, st.repo.CommonRoot)
	pr := completePR2(st.repo.CommonRoot, base, head)
	pr.Threads = nil
	if _, err := st.SavePR2(pr, "", ""); err != nil {
		t.Fatal(err)
	}
	assertRef(t, st, eventRef(pr.ID, pr.Events[0].ID, "head"), head)
	assertRef(t, st, eventRef(pr.ID, pr.Events[0].ID, "base"), base)
	gcAndAssertObjects(t, st.repo.CommonRoot, base, head)
}

func TestAnchorRefsPinUnjudgedPairThenReleaseToMatchingEvent(t *testing.T) {
	st, base, head := newStoreTestHistory(t)
	makeHistoryUnreachable(t, st.repo.CommonRoot)
	pr := completePR2(st.repo.CommonRoot, base, head)
	pr.Events = nil
	if _, err := st.SavePR2(pr, "", ""); err != nil {
		t.Fatal(err)
	}
	threadID := pr.Threads[0].ID
	assertRef(t, st, anchorRef(pr.ID, threadID, "head"), head)
	assertRef(t, st, anchorRef(pr.ID, threadID, "base"), base)
	gcAndAssertObjects(t, st.repo.CommonRoot, base, head)

	loaded, version, err := st.LoadPR2(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Events = completePR2(st.repo.CommonRoot, base, head).Events
	if _, err := st.SavePR2(loaded, loaded.State, version); err != nil {
		t.Fatal(err)
	}
	assertMissingRef(t, st, anchorRef(pr.ID, threadID, "head"))
	assertMissingRef(t, st, anchorRef(pr.ID, threadID, "base"))
	assertRef(t, st, eventRef(pr.ID, loaded.Events[0].ID, "head"), head)
	assertRef(t, st, eventRef(pr.ID, loaded.Events[0].ID, "base"), base)
	gcAndAssertObjects(t, st.repo.CommonRoot, base, head)
}

func TestSavePR2RejectsStaleWriterWithoutPartialRefsOrIndex(t *testing.T) {
	stA, base, head := newStoreTestHistory(t)
	pr := completePR2(stA.repo.CommonRoot, base, head)
	pr.Events = nil
	pr.Threads = nil
	if _, err := stA.SavePR2(pr, "", ""); err != nil {
		t.Fatal(err)
	}
	stB, err := New(stA.repo.CommonRoot)
	if err != nil {
		t.Fatal(err)
	}
	loser, staleVersion, _ := stA.LoadPR2(pr.ID)
	winner, winnerVersion, _ := stB.LoadPR2(pr.ID)
	winner.Title = "winner"
	if _, err := stB.SavePR2(winner, winner.State, winnerVersion); err != nil {
		t.Fatal(err)
	}
	loser.State = model.PRStateMerged
	loser.Events = completePR2(stA.repo.CommonRoot, base, head).Events
	if _, err := stA.SavePR2(loser, model.PRStateOpen, staleVersion); !errors.Is(err, ErrMetadataConflict) {
		t.Fatalf("losing save error = %v", err)
	}
	loaded, _, _ := stA.LoadPR2(pr.ID)
	if loaded.Title != "winner" || loaded.State != model.PRStateOpen || len(loaded.Events) != 0 {
		t.Fatalf("winner metadata changed: %#v", loaded)
	}
	assertRef(t, stA, stA.indexRef2(model.PRStateOpen, pr.ID), winnerVersionFor(t, stA, pr.ID))
	assertMissingRef(t, stA, stA.indexRef2(model.PRStateMerged, pr.ID))
	assertMissingRef(t, stA, eventRef(pr.ID, loser.Events[0].ID, "head"))
}

func completePR2(root, base, head string) model.PR2 {
	created := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	updated := created.Add(time.Hour)
	return model.PR2{
		Schema: 2, ID: "01SCHEMA2ROUNDTRIP00000000", Title: "Schema two record",
		SourceBranch: "topic", BaseBranch: "main", RepositoryRoot: root,
		Description: "typed storage fixture", State: model.PRStateOpen,
		Events: []model.ReviewEvent{{ID: "01EVENT00000000000000000000", SourceHeadSHA: head, BaseHeadSHA: base, MergeBaseSHA: base, Verdict: model.VerdictAccepted, Timestamp: updated}},
		Threads: []model.Thread{
			{ID: "01THREADANCHORED0000000000", Kind: model.ThreadAnchored, Status: model.ThreadOpen, Outdated: true, Anchor: &model.ThreadAnchor{SourceHeadSHA: head, BaseHeadSHA: base, File: "sample.txt", Side: model.DiffSideSource, LineStart: 2, LineEnd: 4}, Comments: []model.ThreadComment{{Body: "consider this range", Timestamp: updated, SourceHeadSHA: head, BaseHeadSHA: base, PostClosure: false}}},
			{ID: "01THREADPRLEVEL00000000000", Kind: model.ThreadPRLevel, Status: model.ThreadResolved, Comments: []model.ThreadComment{{Body: "record-level disposition", Timestamp: updated, SourceHeadSHA: head, BaseHeadSHA: base, PostClosure: true}}},
		},
		CreatedAt: created, UpdatedAt: updated,
	}
}

func newStoreTestHistory(t *testing.T) (*Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	storeTestGit(t, dir, "init", "-b", "main")
	storeTestGit(t, dir, "config", "user.name", "gitpr tests")
	storeTestGit(t, dir, "config", "user.email", "gitpr@example.test")
	if err := os.WriteFile(dir+"/sample.txt", []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	storeTestGit(t, dir, "add", "sample.txt")
	storeTestGit(t, dir, "commit", "-m", "base")
	base := storeTestGit(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(dir+"/sample.txt", []byte("base\nchange\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	storeTestGit(t, dir, "add", "sample.txt")
	storeTestGit(t, dir, "commit", "-m", "change")
	head := storeTestGit(t, dir, "rev-parse", "HEAD")
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return st, base, head
}

func makeHistoryUnreachable(t *testing.T, dir string) {
	t.Helper()
	storeTestGit(t, dir, "checkout", "--orphan", "replacement")
	storeTestGit(t, dir, "rm", "-rf", ".")
	storeTestGit(t, dir, "commit", "--allow-empty", "-m", "replacement")
	storeTestGit(t, dir, "branch", "-D", "main")
}

func gcAndAssertObjects(t *testing.T, dir string, oids ...string) {
	t.Helper()
	storeTestGit(t, dir, "reflog", "expire", "--expire=now", "--all")
	storeTestGit(t, dir, "gc", "--prune=now", "--aggressive")
	for _, oid := range oids {
		if got := storeTestGit(t, dir, "cat-file", "-t", oid); got != "commit" {
			t.Fatalf("object %s type = %q", oid, got)
		}
	}
}

func eventRef(prID, eventID, side string) string {
	return prRefPrefix + "/" + prID + "/events/" + eventID + "/" + side
}
func anchorRef(prID, threadID, side string) string {
	return prRefPrefix + "/" + prID + "/anchors/" + threadID + "/" + side
}

func assertRef(t *testing.T, st *Store, ref, want string) {
	t.Helper()
	got, err := st.resolveRef(ref)
	if err != nil || got != want {
		t.Fatalf("ref %s = %q, %v; want %s", ref, got, err, want)
	}
}

func assertMissingRef(t *testing.T, st *Store, ref string) {
	t.Helper()
	_, exists, err := st.resolveRefForLoad(ref)
	if err != nil || exists {
		t.Fatalf("ref %s exists = %v, error = %v; want absent", ref, exists, err)
	}
}

func winnerVersionFor(t *testing.T, st *Store, id string) string {
	t.Helper()
	_, version, err := st.LoadPR2(id)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(version)
}
