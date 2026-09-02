//go:build test

package app

import (
	"context"
	"testing"
)

func TestConcurrentBranchVerdictsRetryIntoOnePredecessorChain(t *testing.T) {
	dir, serviceA := newBranchService(t)
	pr, _, _ := serviceA.CreatePR(context.Background(), CreatePRRequest{Title: "Concurrent verdicts", Worktree: dir})
	report, _ := serviceA.ReviewPR(context.Background(), pr.ID)
	heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}
	serviceB, err := NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	loaded := make(chan struct{})
	release := make(chan struct{})
	serviceA.store.SetBeforeSaveHook(oneShotBlockingHook(serviceA, loaded, release))
	errA := make(chan error, 1)
	go func() { _, _, err := serviceA.ApprovePR(context.Background(), pr.ID, heads); errA <- err }()
	<-loaded
	if _, _, err := serviceB.RejectPR(context.Background(), pr.ID, &heads); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-errA; err != nil {
		t.Fatal(err)
	}
	got, _, _ := serviceA.store.LoadPR(pr.ID)
	if len(got.Events) != 2 {
		t.Fatalf("events = %#v", got.Events)
	}
	if got.Events[0].ID == got.Events[1].ID || got.Events[0].PredecessorEventID != "" || got.Events[1].PredecessorEventID != got.Events[0].ID {
		t.Fatalf("event chain = %#v", got.Events)
	}
}
