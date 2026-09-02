package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

// shellFragileDescription mirrors the content observed corrupting shell-quoted
// --description values in tasks 298, 549, and 658: backticks, a $() command
// substitution marker, embedded quotes, and multiple lines.
const shellFragileDescription = "line one has a backtick `date` inside\n" +
	"line two with $(command) and \"quotes\" and 'single quotes'\n" +
	"line three trailing"

func TestCreatePRStoresDescriptionByteForByteWithoutTrimmingOrInterpretation(t *testing.T) {
	repoPath, service := newBranchService(t)
	pr, _, err := service.CreatePR(context.Background(), CreatePRRequest{
		Title: "Byte preserving", Description: shellFragileDescription, Worktree: repoPath, BaseBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Description != shellFragileDescription {
		t.Fatalf("stored description = %q, want %q", pr.Description, shellFragileDescription)
	}
	loaded, _, err := service.store.LoadPR(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Description != shellFragileDescription {
		t.Fatalf("round-tripped description = %q, want %q", loaded.Description, shellFragileDescription)
	}
}

func TestEditPRReplacesDescriptionOnOpenPRWithoutNewRecordOrHeadChange(t *testing.T) {
	repoPath, service := newBranchService(t)
	created, ref, err := service.CreatePR(context.Background(), CreatePRRequest{
		Title: "Correctable", Description: "", Worktree: repoPath, BaseBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	newDescription := shellFragileDescription
	updated, newRef, err := service.EditPR(created.ID, EditPRRequest{Description: &newDescription})
	if err != nil {
		t.Fatal(err)
	}

	if updated.ID != created.ID {
		t.Fatalf("edit produced a different record: %s vs %s", updated.ID, created.ID)
	}
	if updated.Description != shellFragileDescription {
		t.Fatalf("edited description = %q, want %q", updated.Description, shellFragileDescription)
	}
	if newRef != ref {
		t.Fatalf("edit ref = %q, want same metadata ref %q", newRef, ref)
	}

	// Unrelated metadata, heads, and identifiers survive untouched.
	if updated.SourceBranch != created.SourceBranch || updated.BaseBranch != created.BaseBranch ||
		updated.SourceWorktreePath != created.SourceWorktreePath || updated.RepositoryRoot != created.RepositoryRoot ||
		updated.State != created.State || updated.Title != created.Title || !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("edit changed unrelated metadata: before=%#v after=%#v", created, updated)
	}

	loaded, _, err := service.store.LoadPR(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Description != shellFragileDescription {
		t.Fatalf("persisted description = %q, want %q", loaded.Description, shellFragileDescription)
	}

	// Only one PR record exists: no second snapshot was created.
	open, _, err := service.store.ListPRs(string(model.PRStateOpen))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("open PRs after edit = %d, want 1", len(open))
	}
}

// TestEditPRChangesOnlyTitleDescriptionAndUpdatedAtOnARecordCarryingEventsThreadsAndAnchors
// pins the "touches nothing else" guarantee against a record with real
// review-event and thread/anchor state, not just a bare open record: an
// approve event, an anchored thread with a reply, and their pinned anchor
// refs. Every field is compared explicitly (not via reflect.DeepEqual)
// so a future field added to model.PR2 fails this test by name instead of
// silently passing unchecked.
func TestEditPRChangesOnlyTitleDescriptionAndUpdatedAtOnARecordCarryingEventsThreadsAndAnchors(t *testing.T) {
	dir, service, pr, _ := newAcceptedBranchPR(t)
	if _, _, err := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{File: "sample.txt", Side: model.DiffSideSource, LineStart: 2, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	before, beforeVersion, err := service.store.LoadPR(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Events) == 0 || len(before.Threads) == 0 {
		t.Fatalf("fixture is missing events/threads: events=%d threads=%d", len(before.Events), len(before.Threads))
	}
	// Capture every pinned ref under this PR (events and anchors), excluding
	// the meta pointer itself, which legitimately advances on every save.
	pinnedRefs := func() string {
		return testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)",
			"refs/gitpr/pr/"+pr.ID+"/events", "refs/gitpr/pr/"+pr.ID+"/anchors")
	}
	beforePins := pinnedRefs()
	if beforePins == "" {
		t.Fatal("fixture pinned no event/anchor refs")
	}

	newTitle, newDescription := "Corrected title", shellFragileDescription
	updated, _, err := service.EditPR(pr.ID, EditPRRequest{Title: &newTitle, Description: &newDescription})
	if err != nil {
		t.Fatal(err)
	}

	if updated.Title != newTitle {
		t.Fatalf("title = %q, want %q", updated.Title, newTitle)
	}
	if updated.Description != newDescription {
		t.Fatalf("description = %q, want %q", updated.Description, newDescription)
	}
	if !updated.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("updated_at = %s, want after %s", updated.UpdatedAt, before.UpdatedAt)
	}

	// Every other field, field by field: byte-identical to the pre-edit record.
	if updated.Schema != before.Schema {
		t.Fatalf("schema changed: %d -> %d", before.Schema, updated.Schema)
	}
	if updated.ID != before.ID {
		t.Fatalf("id changed: %s -> %s", before.ID, updated.ID)
	}
	if updated.SourceBranch != before.SourceBranch {
		t.Fatalf("source_branch changed: %s -> %s", before.SourceBranch, updated.SourceBranch)
	}
	if updated.SourceWorktreePath != before.SourceWorktreePath {
		t.Fatalf("source_worktree_path changed: %s -> %s", before.SourceWorktreePath, updated.SourceWorktreePath)
	}
	if updated.RepositoryRoot != before.RepositoryRoot {
		t.Fatalf("repository_root changed: %s -> %s", before.RepositoryRoot, updated.RepositoryRoot)
	}
	if updated.BaseBranch != before.BaseBranch {
		t.Fatalf("base_branch changed: %s -> %s", before.BaseBranch, updated.BaseBranch)
	}
	if updated.State != before.State {
		t.Fatalf("state changed: %s -> %s", before.State, updated.State)
	}
	if !updated.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("created_at changed: %s -> %s", before.CreatedAt, updated.CreatedAt)
	}
	if updated.MergedAt != nil || before.MergedAt != nil || updated.MergedEventID != before.MergedEventID {
		t.Fatalf("merge provenance changed: before=%#v after=%#v", before, updated)
	}
	if updated.ClosedAt != nil || before.ClosedAt != nil || !reflect.DeepEqual(updated.Closure, before.Closure) {
		t.Fatalf("closure changed: before=%#v after=%#v", before, updated)
	}
	if !reflect.DeepEqual(updated.Events, before.Events) {
		t.Fatalf("events changed:\n before=%#v\n after=%#v", before.Events, updated.Events)
	}
	if !reflect.DeepEqual(updated.Threads, before.Threads) {
		t.Fatalf("threads changed:\n before=%#v\n after=%#v", before.Threads, updated.Threads)
	}

	loaded, loadedVersion, err := service.store.LoadPR(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedVersion == beforeVersion {
		t.Fatalf("edit did not advance the metadata ref: version stayed %s", beforeVersion)
	}
	if !reflect.DeepEqual(loaded.Events, before.Events) || !reflect.DeepEqual(loaded.Threads, before.Threads) {
		t.Fatalf("persisted record diverges from the pre-edit events/threads")
	}

	if after := pinnedRefs(); after != beforePins {
		t.Fatalf("edit changed pinned event/anchor refs:\n before: %s\n after:  %s", beforePins, after)
	}
}

func TestEditPRReplacesTitle(t *testing.T) {
	repoPath, service := newBranchService(t)
	created, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "Old title", Worktree: repoPath, BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	newTitle := "New title"
	updated, _, err := service.EditPR(created.ID, EditPRRequest{Title: &newTitle})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "New title" {
		t.Fatalf("title = %q, want %q", updated.Title, "New title")
	}
	if updated.SourceBranch != created.SourceBranch || updated.Description != created.Description {
		t.Fatalf("edit of title changed unrelated fields: %#v", updated)
	}
}

func TestEditPRRejectsClosedRecordWithoutWriting(t *testing.T) {
	repoPath, service := newBranchService(t)
	created, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "To close", Worktree: repoPath, BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	stored, version, err := service.store.LoadPR(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.State = model.PRStateClosed
	stored.Closure = &model.Closure{Reason: model.ClosureAbandoned}
	if _, err := service.store.SavePR2(stored, model.PRStateOpen, version); err != nil {
		t.Fatal(err)
	}
	beforeEdit, beforeVersion, err := service.store.LoadPR(created.ID)
	if err != nil {
		t.Fatal(err)
	}

	newDescription := "attempted correction"
	if _, _, err := service.EditPR(created.ID, EditPRRequest{Description: &newDescription}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("edit on closed PR error = %v, want a closed-state refusal", err)
	}

	afterEdit, afterVersion, err := service.store.LoadPR(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterVersion != beforeVersion || afterEdit.Description != beforeEdit.Description {
		t.Fatalf("rejected edit wrote to the closed record: before=%#v after=%#v", beforeEdit, afterEdit)
	}
}

func TestEditPRRejectsMergedRecordWithoutWriting(t *testing.T) {
	_, service, pr, _ := newAcceptedBranchPR(t)
	merged, _, err := service.MergePR(context.Background(), pr.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	beforeEdit, beforeVersion, err := service.store.LoadPR(merged.ID)
	if err != nil {
		t.Fatal(err)
	}

	newDescription := "attempted correction after merge"
	if _, _, err := service.EditPR(merged.ID, EditPRRequest{Description: &newDescription}); err == nil || !strings.Contains(err.Error(), "merged") {
		t.Fatalf("edit on merged PR error = %v, want a merged-state refusal", err)
	}

	afterEdit, afterVersion, err := service.store.LoadPR(merged.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterVersion != beforeVersion || afterEdit.Description != beforeEdit.Description {
		t.Fatalf("rejected edit wrote to the merged record: before=%#v after=%#v", beforeEdit, afterEdit)
	}
}

func TestEditPRRequiresAtLeastOneField(t *testing.T) {
	repoPath, service := newBranchService(t)
	created, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "No-op", Worktree: repoPath, BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EditPR(created.ID, EditPRRequest{}); err == nil {
		t.Fatal("expected an error when neither title nor description is provided")
	}
}

func TestEditPRRejectsUnknownRecord(t *testing.T) {
	_, service := newBranchService(t)
	desc := "x"
	if _, _, err := service.EditPR("01DOESNOTEXIST00000000000", EditPRRequest{Description: &desc}); err == nil {
		t.Fatal("expected an error for an unknown PR id")
	}
}
