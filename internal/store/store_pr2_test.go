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

func TestCrossShapeWritesAreRefusedWithoutChangingStoredRecord(t *testing.T) {
	st, legacy := newStoreTestPR(t)
	_, legacyVersion, err := st.LoadLegacyPR(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, legacyBefore, err := st.loadRecordData(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	wrongPR2 := model.PR2{Schema: 2, ID: legacy.ID, State: model.PRStateOpen}
	if _, err := st.SavePR2(wrongPR2, model.PRStateOpen, legacyVersion); !errors.Is(err, ErrSchema2WriteSchema) {
		t.Fatalf("SavePR2 over legacy error = %v, want ErrSchema2WriteSchema", err)
	}
	_, legacyAfter, _ := st.loadRecordData(legacy.ID)
	if !reflect.DeepEqual(legacyAfter, legacyBefore) {
		t.Fatalf("legacy metadata changed\n before: %s\n after: %s", legacyBefore, legacyAfter)
	}

	pr2 := model.PR2{Schema: 2, ID: "01CROSSSHAPEGUARD000000000", State: model.PRStateOpen}
	if _, err := st.SavePR2(pr2, "", ""); err != nil {
		t.Fatal(err)
	}
	_, pr2Version, err := st.LoadPR2(pr2.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, pr2Before, _ := st.loadRecordData(pr2.ID)
	wrongLegacy := model.PR{ID: pr2.ID, Status: model.StatusOpen}
	if _, err := st.SavePR(wrongLegacy, model.StatusOpen, pr2Version); !errors.Is(err, ErrLegacyWriteSchema) {
		t.Fatalf("SavePR over schema 2 error = %v, want ErrLegacyWriteSchema", err)
	}
	_, pr2After, _ := st.loadRecordData(pr2.ID)
	if !reflect.DeepEqual(pr2After, pr2Before) {
		t.Fatalf("schema-2 metadata changed\n before: %s\n after: %s", pr2Before, pr2After)
	}
}

func TestListPRsSkipsUnsupportedSchemaButLoadPRRejectsIt(t *testing.T) {
	st, legacy := newStoreTestPR(t)
	stubID := "01FORWARDSCHEMA300000000000"
	writeRawPRRecord(t, st, stubID, "open", []byte("schema: 3\nid: "+stubID+"\nstate: open\n"))

	records, err := st.ListPRs("open")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RecordID() != legacy.ID {
		t.Fatalf("open records = %#v, want supported legacy record only", records)
	}
	if _, _, err := st.LoadPR(stubID); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("schema-3 LoadPR error = %v, want ErrUnsupportedSchema", err)
	}
}

func TestListPRsFiltersKeepLegacyAndSchema2VocabulariesDistinct(t *testing.T) {
	st, base, head := newStoreTestHistory(t)
	legacyStates := []model.Status{model.StatusOpen, model.StatusApproved, model.StatusRejected}
	for i, state := range legacyStates {
		pr := model.PR{ID: "01LEGACYFILTER000000000000" + string(rune('A'+i)), Status: state, SourceHeadSHA: head, BaseHeadSHA: base}
		if _, err := st.SavePR(pr, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	newStates := []model.PRState{model.PRStateOpen, model.PRStateMerged, model.PRStateClosed}
	for i, state := range newStates {
		pr := model.PR2{Schema: 2, ID: "01SCHEMA2FILTER00000000000" + string(rune('A'+i)), State: state}
		if state == model.PRStateMerged {
			data, _ := yaml.Marshal(pr)
			writeRawPRRecord(t, st, pr.ID, string(state), data)
		} else if _, err := st.SavePR2(pr, "", ""); err != nil {
			t.Fatal(err)
		}
	}

	wants := map[string][]string{
		"open":     {"01LEGACYFILTER000000000000A", "01SCHEMA2FILTER00000000000A"},
		"approved": {"01LEGACYFILTER000000000000B"},
		"rejected": {"01LEGACYFILTER000000000000C"},
		"merged":   {"01SCHEMA2FILTER00000000000B"},
		"closed":   {"01SCHEMA2FILTER00000000000C"},
	}
	for filter, want := range wants {
		records, err := st.ListPRs(filter)
		if err != nil {
			t.Fatalf("ListPRs(%q): %v", filter, err)
		}
		got := make([]string, len(records))
		for i, record := range records {
			got[i] = record.RecordID()
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ListPRs(%q) IDs = %v, want %v", filter, got, want)
		}
	}
}

func TestEventHistoryUsesInstantEqualityAndRejectsDuplicateIDs(t *testing.T) {
	st, base, head := newStoreTestHistory(t)
	pr := completePR2(st.repo.CommonRoot, base, head)
	if _, err := st.SavePR2(pr, "", ""); err != nil {
		t.Fatal(err)
	}
	loaded, version, err := st.LoadPR2(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	instant := loaded.Events[0].Timestamp
	loaded.Events[0].Timestamp = instant.In(time.FixedZone("equivalent", 0))
	if _, err := st.SavePR2(loaded, loaded.State, version); err != nil {
		t.Fatalf("equivalent timestamp rejected: %v", err)
	}

	loaded, version, err = st.LoadPR2(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := loaded.Events[0]
	duplicate.Verdict = model.VerdictRejected
	loaded.Events = append(loaded.Events, duplicate)
	if _, err := st.SavePR2(loaded, loaded.State, version); !errors.Is(err, ErrDuplicateEventID) {
		t.Fatalf("duplicate event save error = %v, want ErrDuplicateEventID", err)
	}
}

func TestSavePR2RejectsNonCanonicalObjectIDsOnNewEvent(t *testing.T) {
	st, base, head := newStoreTestHistory(t)
	pr := completePR2(st.repo.CommonRoot, base, head)
	if _, err := st.SavePR2(pr, "", ""); err != nil {
		t.Fatal(err)
	}
	loaded, version, _ := st.LoadPR2(pr.ID)
	refsBefore := storeTestGit(t, st.repo.CommonRoot, "for-each-ref", "--format=%(refname) %(objectname)")
	loaded.Events = append(loaded.Events, model.ReviewEvent{ID: "01INVALIDOBJECTEVENT0000000", SourceHeadSHA: "feature", BaseHeadSHA: base, MergeBaseSHA: base, Verdict: model.VerdictRejected, Timestamp: time.Now().UTC(), PredecessorEventID: loaded.Events[0].ID})
	if _, err := st.SavePR2(loaded, loaded.State, version); !errors.Is(err, ErrInvalidEventObjectID) {
		t.Fatalf("invalid event object ID error = %v, want ErrInvalidEventObjectID", err)
	}
	refsAfter := storeTestGit(t, st.repo.CommonRoot, "for-each-ref", "--format=%(refname) %(objectname)")
	if refsAfter != refsBefore {
		t.Fatalf("invalid event mutated refs\nbefore: %s\nafter: %s", refsBefore, refsAfter)
	}
}

func TestSavePR2RejectsNonCanonicalObjectIDsOnInitialEvent(t *testing.T) {
	st, base, head := newStoreTestHistory(t)
	pr := completePR2(st.repo.CommonRoot, base, head)
	pr.Events[0].BaseHeadSHA = "main"
	refsBefore := storeTestGit(t, st.repo.CommonRoot, "for-each-ref", "--format=%(refname) %(objectname)")
	if _, err := st.SavePR2(pr, "", ""); !errors.Is(err, ErrInvalidEventObjectID) {
		t.Fatalf("invalid initial event object ID error = %v, want ErrInvalidEventObjectID", err)
	}
	refsAfter := storeTestGit(t, st.repo.CommonRoot, "for-each-ref", "--format=%(refname) %(objectname)")
	if refsAfter != refsBefore {
		t.Fatalf("invalid initial event mutated refs\nbefore: %s\nafter: %s", refsBefore, refsAfter)
	}
}

func TestSavePR2RefusesMergedStateWithoutAtomicMergeOperation(t *testing.T) {
	st, base, head := newStoreTestHistory(t)
	pr := completePR2(st.repo.CommonRoot, base, head)
	if _, err := st.SavePR2(pr, "", ""); err != nil {
		t.Fatal(err)
	}
	loaded, version, _ := st.LoadPR2(pr.ID)
	refsBefore := storeTestGit(t, st.repo.CommonRoot, "for-each-ref", "--format=%(refname) %(objectname)")
	loaded.State = model.PRStateMerged
	if _, err := st.SavePR2(loaded, model.PRStateOpen, version); !errors.Is(err, ErrMergedStateRequiresMerge) {
		t.Fatalf("merged SavePR2 error = %v", err)
	}
	refsAfter := storeTestGit(t, st.repo.CommonRoot, "for-each-ref", "--format=%(refname) %(objectname)")
	if refsAfter != refsBefore {
		t.Fatalf("forbidden merged save mutated refs\nbefore: %s\nafter: %s", refsBefore, refsAfter)
	}
}

func TestMergePR2DerivesPreviousStateFromExpectedMetadata(t *testing.T) {
	st, base, head := newStoreTestHistory(t)
	storeTestGit(t, st.repo.CommonRoot, "update-ref", "refs/heads/main", base)
	pr := completePR2(st.repo.CommonRoot, base, head)
	if _, err := st.SavePR2(pr, "", ""); err != nil {
		t.Fatal(err)
	}
	closed, version, _ := st.LoadPR2(pr.ID)
	closed.State = model.PRStateClosed
	closed.Closure = &model.Closure{Reason: model.ClosureAbandoned}
	if _, err := st.SavePR2(closed, model.PRStateOpen, version); err != nil {
		t.Fatal(err)
	}
	merging, version, _ := st.LoadPR2(pr.ID)
	merging.State = model.PRStateMerged
	now := time.Now().UTC()
	merging.MergedAt = &now
	merging.MergedEventID = merging.Events[0].ID
	if _, err := st.MergePR2(merging, version); err != nil {
		t.Fatal(err)
	}
	assertMissingRef(t, st, st.indexRef2(model.PRStateClosed, pr.ID))
	meta, _ := st.resolveRef(st.metaRef(pr.ID))
	assertRef(t, st, st.indexRef2(model.PRStateMerged, pr.ID), meta)
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
	loser.State = model.PRStateClosed
	loser.Closure = &model.Closure{Reason: model.ClosureAbandoned}
	loser.Events = completePR2(stA.repo.CommonRoot, base, head).Events
	loser.Threads = []model.Thread{losingAnchorThread(base, head)}
	if _, err := stA.SavePR2(loser, model.PRStateOpen, staleVersion); !errors.Is(err, ErrMetadataConflict) {
		t.Fatalf("losing save error = %v", err)
	}
	loaded, _, _ := stA.LoadPR2(pr.ID)
	if loaded.Title != "winner" || loaded.State != model.PRStateOpen || len(loaded.Events) != 0 {
		t.Fatalf("winner metadata changed: %#v", loaded)
	}
	assertRef(t, stA, stA.indexRef2(model.PRStateOpen, pr.ID), winnerVersionFor(t, stA, pr.ID))
	assertMissingRef(t, stA, stA.indexRef2(model.PRStateClosed, pr.ID))
	assertMissingRef(t, stA, eventRef(pr.ID, loser.Events[0].ID, "head"))
	assertMissingRef(t, stA, anchorRef(pr.ID, loser.Threads[0].ID, "head"))
	assertMissingRef(t, stA, anchorRef(pr.ID, loser.Threads[0].ID, "base"))
}

func TestDeletePR2ReleasesOpenPairOwnership(t *testing.T) {
	st, base, head := newStoreTestHistory(t)
	pr := completePR2(st.repo.CommonRoot, base, head)
	pr.Events = nil
	pr.Threads = nil
	if _, err := st.SavePR2(pr, "", ""); err != nil {
		t.Fatal(err)
	}
	_, version, err := st.LoadPR2(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertRef(t, st, st.openPairRef(pr.SourceBranch, pr.BaseBranch), version)
	if err := st.DeletePR2(pr, version); err != nil {
		t.Fatal(err)
	}
	assertMissingRef(t, st, st.openPairRef(pr.SourceBranch, pr.BaseBranch))
	assertMissingRef(t, st, st.metaRef(pr.ID))
}

func completePR2(root, base, head string) model.PR2 {
	created := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	updated := created.Add(time.Hour)
	return model.PR2{
		Schema: 2, ID: "01SCHEMA2ROUNDTRIP00000000", Title: "Schema two record",
		SourceBranch: "topic", SourceWorktreePath: root + "/topic-worktree", BaseBranch: "main", RepositoryRoot: root,
		Description: "typed storage fixture", State: model.PRStateOpen,
		Events: []model.ReviewEvent{{ID: "01EVENT00000000000000000000", SourceHeadSHA: head, BaseHeadSHA: base, MergeBaseSHA: base, Verdict: model.VerdictAccepted, Timestamp: updated}},
		Threads: []model.Thread{
			{ID: "01THREADANCHORED0000000000", Kind: model.ThreadAnchored, Status: model.ThreadOpen, Outdated: true, Anchor: &model.ThreadAnchor{SourceHeadSHA: head, BaseHeadSHA: base, File: "sample.txt", Side: model.DiffSideSource, LineStart: 2, LineEnd: 4}, Comments: []model.ThreadComment{{Body: "consider this range", Timestamp: updated, SourceHeadSHA: head, BaseHeadSHA: base, PostClosure: false}}},
			{ID: "01THREADPRLEVEL00000000000", Kind: model.ThreadPRLevel, Status: model.ThreadResolved, Comments: []model.ThreadComment{{Body: "record-level disposition", Timestamp: updated, SourceHeadSHA: head, BaseHeadSHA: base, PostClosure: true}}},
		},
		CreatedAt: created, UpdatedAt: updated,
	}
}

func losingAnchorThread(base, head string) model.Thread {
	return model.Thread{ID: "01LOSINGANCHORTHREAD00000000", Kind: model.ThreadAnchored, Status: model.ThreadOpen, Anchor: &model.ThreadAnchor{
		SourceHeadSHA: base, BaseHeadSHA: head, File: "sample.txt", Side: model.DiffSideSource, LineStart: 1, LineEnd: 1,
	}}
}

func writeRawPRRecord(t *testing.T, st *Store, id, state string, data []byte) {
	t.Helper()
	commit, err := st.writeCommit(prFileName, data, "", "test: raw PR record")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.batchUpdateRefs([]refUpdate{
		{Action: "update", Ref: st.metaRef(id), NewOID: commit, OldOID: zeroOID},
		{Action: "update", Ref: indexRefPrefix + "/" + state + "/" + id, NewOID: commit, OldOID: zeroOID},
	}); err != nil {
		t.Fatal(err)
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
