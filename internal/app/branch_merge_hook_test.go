//go:build test

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/store"
)

func TestMergePR2BaseMovementDuringTransactionLeavesRecordOpen(t *testing.T) {
	dir, service, pr, event := newAcceptedBranchPR(t)
	service.store.SetBeforeSaveHook(func() {
		service.store.SetBeforeSaveHook(nil)
		testGit(t, dir, "update-ref", "refs/heads/main", event.SourceHeadSHA, event.BaseHeadSHA)
	})
	_, _, err := service.mergeBranchPR(context.Background(), pr.ID, false)
	if err == nil || !errors.Is(err, store.ErrMergeConflict) || !strings.Contains(err.Error(), "review") || !strings.Contains(err.Error(), "retry") {
		t.Fatalf("racing merge error = %v", err)
	}
	loaded, _, _ := service.store.LoadPR2(pr.ID)
	if loaded.State != model.PRStateOpen || loaded.MergedAt != nil || loaded.MergedEventID != "" {
		t.Fatalf("racing merge metadata = %#v", loaded)
	}
	assertAppRefExists(t, dir, "refs/gitpr/index/open/"+pr.ID)
	if got := testGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/index/merged/"+pr.ID); got != "" {
		t.Fatalf("merged index exists: %s", got)
	}
	if got := testGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/openpair"); got == "" {
		t.Fatal("open-pair ref was released")
	}
}

func TestMergePR2RefreshFailureReportsCompletedMergeAndRepairCommand(t *testing.T) {
	dir, service, pr, event := newAcceptedBranchPR(t)
	basePath := filepath.Join(t.TempDir(), "base")
	testGit(t, dir, "worktree", "add", basePath, "main")
	service.store.SetBeforeSaveHook(func() {
		service.store.SetBeforeSaveHook(nil)
		testGit(t, dir, "worktree", "remove", "--force", basePath)
	})
	merged, ref, err := service.mergeBranchPR(context.Background(), pr.ID, false)
	if err == nil || merged.State != model.PRStateMerged || ref == "" || !strings.Contains(err.Error(), "merge succeeded") || !strings.Contains(err.Error(), basePath) || !strings.Contains(err.Error(), "reset --hard "+event.SourceHeadSHA) {
		t.Fatalf("refresh failure result = %#v, ref=%q, err=%v", merged, ref, err)
	}
	if got := testGit(t, dir, "rev-parse", "refs/heads/main"); got != event.SourceHeadSHA {
		t.Fatalf("base = %s", got)
	}
}

func TestMergePR2ConcurrentVerdictWinsWithoutBaseAdvance(t *testing.T) {
	dir, serviceA, pr, event := newAcceptedBranchPR(t)
	serviceB, err := NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	heads := ExpectedHeads{Source: event.SourceHeadSHA, Base: event.BaseHeadSHA}
	serviceA.store.SetBeforeSaveHook(func() {
		serviceA.store.SetBeforeSaveHook(nil)
		if _, _, err := serviceB.RejectRecord(context.Background(), pr.ID, &heads); err != nil {
			t.Errorf("concurrent reject: %v", err)
		}
	})
	_, _, err = serviceA.mergeBranchPR(context.Background(), pr.ID, false)
	if err == nil || !errors.Is(err, store.ErrMergeConflict) {
		t.Fatalf("concurrent verdict merge error = %v", err)
	}
	loaded, _, _ := serviceA.store.LoadPR2(pr.ID)
	if loaded.State != model.PRStateOpen || len(loaded.Events) != 2 || loaded.Events[1].Verdict != model.VerdictRejected {
		t.Fatalf("winner record = %#v", loaded)
	}
	if got := testGit(t, dir, "rev-parse", "refs/heads/main"); got != event.BaseHeadSHA {
		t.Fatalf("base advanced to %s", got)
	}
}

func TestMergePR2NewDirtyWorktreeSkipsRefreshAfterCompletedMerge(t *testing.T) {
	dir, service, pr, event := newAcceptedBranchPR(t)
	basePath := filepath.Join(t.TempDir(), "base")
	testGit(t, dir, "worktree", "add", basePath, "main")
	service.store.SetBeforeSaveHook(func() {
		service.store.SetBeforeSaveHook(nil)
		writeTestFile(t, basePath, "late.txt", "late change\n")
	})
	merged, ref, err := service.mergeBranchPR(context.Background(), pr.ID, false)
	if err == nil || merged.State != model.PRStateMerged || ref == "" || !strings.Contains(err.Error(), "merge succeeded") || !strings.Contains(err.Error(), "newly dirty") || !strings.Contains(err.Error(), "reset --hard "+event.SourceHeadSHA) {
		t.Fatalf("dirty-window result = %#v ref=%q err=%v", merged, ref, err)
	}
	if _, statErr := os.Stat(filepath.Join(basePath, "late.txt")); statErr != nil {
		t.Fatalf("late change lost: %v", statErr)
	}
}
