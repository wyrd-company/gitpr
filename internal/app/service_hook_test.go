//go:build test

package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/store"
)

func TestConcurrentAddCommentRetriesAndPreservesBothComments(t *testing.T) {
	serviceA, pr := newTestPR(t)
	serviceB := secondService(t, pr.RepositoryRoot)
	loaded := make(chan struct{})
	release := make(chan struct{})
	serviceA.store.SetBeforeSaveHook(func() {
		close(loaded)
		<-release
		serviceA.store.SetBeforeSaveHook(nil)
	})

	errA := make(chan error, 1)
	go func() {
		_, err := serviceA.AddComment(pr.ID, testComment("alpha"))
		errA <- err
	}()
	<-loaded
	if _, err := serviceB.AddComment(pr.ID, testComment("bravo")); err != nil {
		t.Fatalf("service B AddComment() error = %v", err)
	}
	close(release)
	if err := <-errA; err != nil {
		t.Fatalf("service A AddComment() error = %v", err)
	}

	assertCommentTextsExactlyOnce(t, serviceA, pr.ID, "alpha", "bravo")
}
func TestConcurrentAddAndUpdatePreserveBothMutations(t *testing.T) {
	serviceA, pr := newTestPR(t)
	if _, err := serviceA.AddComment(pr.ID, testComment("original")); err != nil {
		t.Fatal(err)
	}
	serviceB := secondService(t, pr.RepositoryRoot)
	loaded := make(chan struct{})
	release := make(chan struct{})
	serviceA.store.SetBeforeSaveHook(oneShotBlockingHook(serviceA, loaded, release))

	errA := make(chan error, 1)
	go func() {
		_, err := serviceA.AddComment(pr.ID, testComment("appended"))
		errA <- err
	}()
	<-loaded
	replacement := testComment("updated")
	if _, err := serviceB.UpdateComment(pr.ID, 0, replacement); err != nil {
		t.Fatalf("service B UpdateComment() error = %v", err)
	}
	close(release)
	if err := <-errA; err != nil {
		t.Fatalf("service A AddComment() error = %v", err)
	}

	assertCommentTextsExactlyOnce(t, serviceA, pr.ID, "updated", "appended")
}
func TestConcurrentUpdatesRevalidateAndPreserveBothMutations(t *testing.T) {
	serviceA, pr := newTestPR(t)
	if _, err := serviceA.AddComment(pr.ID, testComment("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := serviceA.AddComment(pr.ID, testComment("second")); err != nil {
		t.Fatal(err)
	}
	serviceB := secondService(t, pr.RepositoryRoot)
	loaded := make(chan struct{})
	release := make(chan struct{})
	serviceA.store.SetBeforeSaveHook(oneShotBlockingHook(serviceA, loaded, release))

	errA := make(chan error, 1)
	go func() {
		_, err := serviceA.UpdateComment(pr.ID, 0, testComment("first updated"))
		errA <- err
	}()
	<-loaded
	if _, err := serviceB.UpdateComment(pr.ID, 1, testComment("second updated")); err != nil {
		t.Fatalf("service B UpdateComment() error = %v", err)
	}
	close(release)
	if err := <-errA; err != nil {
		t.Fatalf("service A UpdateComment() error = %v", err)
	}

	assertCommentTextsExactlyOnce(t, serviceA, pr.ID, "first updated", "second updated")
}
func TestConcurrentUpdatesOnSameIndexAreLastWriteWins(t *testing.T) {
	serviceA, pr := newTestPR(t)
	if _, err := serviceA.AddComment(pr.ID, testComment("original")); err != nil {
		t.Fatal(err)
	}
	serviceB := secondService(t, pr.RepositoryRoot)
	loaded := make(chan struct{})
	release := make(chan struct{})
	serviceA.store.SetBeforeSaveHook(oneShotBlockingHook(serviceA, loaded, release))

	errA := make(chan error, 1)
	go func() {
		_, err := serviceA.UpdateComment(pr.ID, 0, testComment("second writer"))
		errA <- err
	}()
	<-loaded
	if _, err := serviceB.UpdateComment(pr.ID, 0, testComment("first writer")); err != nil {
		t.Fatalf("first writer UpdateComment() error = %v", err)
	}
	close(release)
	if err := <-errA; err != nil {
		t.Fatalf("second writer UpdateComment() error = %v", err)
	}

	assertCommentTextsExactlyOnce(t, serviceA, pr.ID, "second writer")
	loadedPR, _, err := serviceA.LoadPR(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedPR.Comments) != 1 || loadedPR.Comments[0].Comment != "second writer" {
		t.Fatalf("comments = %v, want exactly the second writer", commentTexts(loadedPR.Comments))
	}
}
func TestUpdateCommentRetryUsesWinningReloadCommitSHA(t *testing.T) {
	serviceA, pr := newTestPR(t)
	original := testComment("original")
	original.CommitSHA = "first-attempt-sha"
	if _, err := serviceA.AddComment(pr.ID, original); err != nil {
		t.Fatal(err)
	}
	serviceB := secondService(t, pr.RepositoryRoot)
	loaded := make(chan struct{})
	release := make(chan struct{})
	serviceA.store.SetBeforeSaveHook(oneShotBlockingHook(serviceA, loaded, release))

	errA := make(chan error, 1)
	go func() {
		_, err := serviceA.UpdateComment(pr.ID, 0, testComment("winning text"))
		errA <- err
	}()
	<-loaded
	winner := testComment("interleaved text")
	winner.CommitSHA = "winning-reload-sha"
	if _, err := serviceB.UpdateComment(pr.ID, 0, winner); err != nil {
		t.Fatalf("interleaved UpdateComment() error = %v", err)
	}
	close(release)
	if err := <-errA; err != nil {
		t.Fatalf("retrying UpdateComment() error = %v", err)
	}

	loadedPR, _, err := serviceA.LoadPR(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := loadedPR.Comments[0].CommitSHA; got != winner.CommitSHA {
		t.Fatalf("CommitSHA = %q, want winning reload SHA %q", got, winner.CommitSHA)
	}
}
func TestSuccessorMetadataRefForcesReloadBeforeRetry(t *testing.T) {
	serviceA, pr := newTestPR(t)
	serviceB := secondService(t, pr.RepositoryRoot)
	serviceA.store.SetBeforeSaveHook(func() {
		serviceA.store.SetBeforeSaveHook(nil)
		if _, err := serviceB.AddComment(pr.ID, testComment("successor")); err != nil {
			t.Errorf("install successor metadata: %v", err)
		}
	})

	if _, err := serviceA.AddComment(pr.ID, testComment("retry")); err != nil {
		t.Fatalf("service A AddComment() error = %v", err)
	}
	assertCommentTextsExactlyOnce(t, serviceA, pr.ID, "successor", "retry")
}
func TestMetadataConflictExhaustionRequestsRetry(t *testing.T) {
	serviceA, pr := newTestPR(t)
	serviceB := secondService(t, pr.RepositoryRoot)
	n := 0
	serviceA.store.SetBeforeSaveHook(func() {
		n++
		if _, err := serviceB.AddComment(pr.ID, testComment(fmt.Sprintf("winner %d", n))); err != nil {
			t.Errorf("service B AddComment() error = %v", err)
		}
	})

	_, err := serviceA.AddComment(pr.ID, testComment("loser"))
	if !errors.Is(err, store.ErrMetadataConflict) {
		t.Fatalf("AddComment() error = %v, want ErrMetadataConflict", err)
	}
	if !strings.Contains(err.Error(), "retry the command") {
		t.Fatalf("AddComment() error = %q, want retry guidance", err)
	}
	if n != metadataMutationAttempts {
		t.Fatalf("conflicting attempts = %d, want %d", n, metadataMutationAttempts)
	}
	loaded, _, loadErr := serviceA.LoadPR(pr.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	for _, comment := range loaded.Comments {
		if comment.Comment == "loser" {
			t.Fatal("failed mutation was stored")
		}
	}
}
func TestMergePRReportsSuccessfulMergeWhenConcurrentRejectWinsMetadata(t *testing.T) {
	repoPath, _, serviceA, pr := newMergeTestPR(t)
	serviceB := secondService(t, repoPath)
	serviceA.store.SetBeforeSaveHook(func() {
		serviceA.store.SetBeforeSaveHook(nil)
		if _, _, err := serviceB.RejectPR(pr.ID); err != nil {
			t.Errorf("concurrent RejectPR() error = %v", err)
		}
	})

	merged, _, err := serviceA.MergePR(context.Background(), pr.ID, false)
	if err == nil {
		t.Fatal("MergePR() error = nil, want metadata repair error")
	}
	for _, want := range []string{"merge succeeded", pr.ID, pr.SourceHeadSHA, "repair", "retry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("MergePR() error = %q, want %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "already closed") || strings.Contains(err.Error(), "merge failed") {
		t.Fatalf("MergePR() error misstates merge outcome: %q", err)
	}
	if merged.ID == "" || merged.ID != pr.ID || merged.SourceHeadSHA != pr.SourceHeadSHA {
		t.Fatalf("returned PR identity = %q at %q, want %q at merged SHA %q", merged.ID, merged.SourceHeadSHA, pr.ID, pr.SourceHeadSHA)
	}
	if got := testGit(t, repoPath, "rev-parse", "main"); got != pr.SourceHeadSHA {
		t.Fatalf("main = %s, want merged SHA %s", got, pr.SourceHeadSHA)
	}
}
func TestMergePRConflictRetryRedetectsAgainstCurrentBase(t *testing.T) {
	repoPath, serviceA, pr := newMergeConflictTestPR(t)
	serviceB := secondService(t, repoPath)
	serviceA.store.SetBeforeSaveHook(func() {
		serviceA.store.SetBeforeSaveHook(nil)
		testGit(t, repoPath, "update-ref", "refs/heads/main", pr.SourceHeadSHA)
		if _, err := serviceB.AddComment(pr.ID, testComment("metadata successor")); err != nil {
			t.Errorf("AddComment() error = %v", err)
		}
	})

	_, _, err := serviceA.MergePR(context.Background(), pr.ID, false)
	if err == nil || !strings.Contains(err.Error(), "merge conflicts detected") {
		t.Fatalf("MergePR() error = %v, want conflict refusal", err)
	}
	loaded, _, loadErr := serviceA.LoadPR(pr.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(loaded.MergeConflicts) != 0 {
		t.Fatalf("persisted conflicts = %v, want retry re-detection against advanced base", loaded.MergeConflicts)
	}
}
func oneShotBlockingHook(service *Service, loaded chan<- struct{}, release <-chan struct{}) func() {
	return func() {
		close(loaded)
		<-release
		service.store.SetBeforeSaveHook(nil)
	}
}
