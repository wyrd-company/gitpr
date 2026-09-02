//go:build test

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
	"github.com/wyrd-company/gitpr/internal/store"
)

func TestConcurrentCreatePRClaimsOneOpenPair(t *testing.T) {
	repoPath, first := newBranchService(t)
	if err := first.store.SaveConfig(model.Config{DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	second, err := NewService(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	hook := func() { ready <- struct{}{}; <-release }
	first.store.SetBeforeSaveHook(hook)
	second.store.SetBeforeSaveHook(hook)

	results := make(chan error, 2)
	for i, service := range []*Service{first, second} {
		go func(index int, service *Service) {
			_, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "Concurrent", Worktree: repoPath, BaseBranch: "main"})
			results <- err
		}(i, service)
	}
	<-ready
	<-ready
	close(release)

	var successes, duplicates int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrOpenPairConflict):
			duplicates++
		default:
			t.Errorf("concurrent create error = %v", err)
		}
	}
	if successes != 1 || duplicates != 1 {
		t.Fatalf("concurrent results: successes=%d duplicates=%d", successes, duplicates)
	}
	records, _, err := first.ListPRs("open")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("open PR count = %d, want 1", len(records))
	}
}
