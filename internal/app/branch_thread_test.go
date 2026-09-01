package app

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestBranchThreadCRUDAndAnchorLifecycle(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Discussion", Worktree: dir})
	created, _, err := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{File: "sample.txt", Side: model.DiffSideSource, LineStart: 2, Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	thread := created.Threads[0]
	if thread.Kind != model.ThreadAnchored || thread.Status != model.ThreadOpen || thread.Anchor.LineEnd != 2 || len(thread.Comments) != 1 {
		t.Fatalf("thread=%#v", thread)
	}
	for _, leaf := range []string{"head", "base"} {
		assertAppRefExists(t, dir, "refs/gitpr/pr/"+pr.ID+"/anchors/"+thread.ID+"/"+leaf)
	}
	resolved, _, err := service.SetThreadStatus(pr.ID, thread.ID, model.ThreadResolved)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Threads[0].Status != model.ThreadResolved {
		t.Fatal("thread did not resolve")
	}
	replied, _, err := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{ThreadID: thread.ID, Text: "reply while resolved"})
	if err != nil {
		t.Fatal(err)
	}
	if replied.Threads[0].Status != model.ThreadResolved || len(replied.Threads[0].Comments) != 2 {
		t.Fatalf("reply=%#v", replied.Threads[0])
	}
	reopened, _, err := service.SetThreadStatus(pr.ID, thread.ID, model.ThreadOpen)
	if err != nil || reopened.Threads[0].Status != model.ThreadOpen {
		t.Fatalf("reopen=%#v err=%v", reopened.Threads, err)
	}
	report, _ := service.ReviewPR(context.Background(), pr.ID)
	if _, _, err := service.ApprovePR(context.Background(), pr.ID, ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}); err != nil {
		t.Fatal(err)
	}
	if refs := testGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/gitpr/pr/"+pr.ID+"/anchors/"+thread.ID); refs != "" {
		t.Fatalf("anchor refs remain: %s", refs)
	}
	for _, leaf := range []string{"head", "base"} {
		assertAppRefExists(t, dir, "refs/gitpr/pr/"+pr.ID+"/events/"+mustLatestEvent(t, service, pr.ID).ID+"/"+leaf)
	}
}

func TestCommentTimeRemapMovesUnchangedRangeAndMarksChangedOrMissingContentOutdated(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Remap", Worktree: dir})
	created, _, _ := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{File: "sample.txt", LineStart: 2, Text: "anchor"})
	id := created.Threads[0].ID
	writeTestFile(t, dir, "sample.txt", "intro\nbase\nfeature\n")
	testGit(t, dir, "add", "sample.txt")
	testGit(t, dir, "commit", "-m", "move range")
	moved, _, err := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{ThreadID: id, Text: "after move"})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Threads[0].Outdated || moved.Threads[0].Anchor.LineStart != 3 || moved.Threads[0].Anchor.LineEnd != 3 {
		t.Fatalf("moved thread=%#v", moved.Threads[0])
	}
	writeTestFile(t, dir, "sample.txt", "intro\nbase\nchanged\n")
	testGit(t, dir, "add", "sample.txt")
	testGit(t, dir, "commit", "-m", "change range")
	changed, _, _ := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{ThreadID: id, Text: "after edit"})
	if !changed.Threads[0].Outdated {
		t.Fatal("content change did not outdate thread")
	}
	writeTestFile(t, dir, "sample.txt", "intro\nbase\nfeature\n")
	testGit(t, dir, "add", "sample.txt")
	testGit(t, dir, "commit", "-m", "restore")
	current, _, _ := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{File: "sample.txt", LineStart: 3, Text: "delete target"})
	deleteID := current.Threads[len(current.Threads)-1].ID
	testGit(t, dir, "rm", "sample.txt")
	testGit(t, dir, "commit", "-m", "delete file")
	deleted, _, err := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{ThreadID: deleteID, Text: "after delete"})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Threads[len(deleted.Threads)-1].Outdated || deleted.Threads[len(deleted.Threads)-1].Comments[0].Body != "delete target" {
		t.Fatalf("deleted thread=%#v", deleted.Threads[len(deleted.Threads)-1])
	}
}

func TestOutdatedVerdictKeepsOriginalAnchorPairPinnedThroughGC(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Pinned outdated identity", Worktree: dir})
	originalSource := testGit(t, dir, "rev-parse", "refs/heads/feature")
	originalBase := testGit(t, dir, "rev-parse", "refs/heads/main")
	created, _, err := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{File: "sample.txt", LineStart: 2, Text: "keep identity"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := created.Threads[0].ID
	testGit(t, dir, "reset", "--hard", "refs/heads/main")
	writeTestFile(t, dir, "other.txt", "replacement\n")
	testGit(t, dir, "add", "-A")
	testGit(t, dir, "commit", "-m", "replace source history")
	report, err := service.ReviewPR(context.Background(), pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	judged, _, err := service.ApprovePR(context.Background(), pr.ID, ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA})
	if err != nil {
		t.Fatal(err)
	}
	anchor := judged.Threads[0].Anchor
	if !judged.Threads[0].Outdated || anchor.SourceHeadSHA != originalSource || anchor.BaseHeadSHA != originalBase {
		t.Fatalf("outdated anchor=%#v thread=%#v", anchor, judged.Threads[0])
	}
	for _, pair := range []struct{ leaf, want string }{{"head", originalSource}, {"base", originalBase}} {
		ref := "refs/gitpr/pr/" + pr.ID + "/anchors/" + threadID + "/" + pair.leaf
		if got := testGit(t, dir, "rev-parse", ref); got != pair.want {
			t.Fatalf("%s=%s want %s", ref, got, pair.want)
		}
	}
	testGit(t, dir, "reflog", "expire", "--expire=now", "--all")
	testGit(t, dir, "gc", "--prune=now", "--aggressive")
	if got := testGit(t, dir, "cat-file", "-t", originalSource); got != "commit" {
		t.Fatalf("original source object type=%q", got)
	}
}

func TestMapUnchangedRangeUsesInclusiveThresholdAfterEdgeTrim(t *testing.T) {
	atLimit := make([]string, remapLineLimit+1)
	for i := 0; i < remapLineLimit; i++ {
		atLimit[i] = "removed"
	}
	atLimit[remapLineLimit] = "anchor"
	if start, end, ok := mapUnchangedRange(atLimit, []string{"anchor"}, remapLineLimit+1, remapLineLimit+1); !ok || start != 1 || end != 1 {
		t.Fatalf("at limit=(%d,%d,%v)", start, end, ok)
	}
	above := append([]string{"removed"}, atLimit...)
	if _, _, ok := mapUnchangedRange(above, []string{"anchor"}, len(above), len(above)); ok {
		t.Fatal("range above remap threshold was mapped")
	}
}

func TestVerdictRemapsEveryThreadButPRLevelNeverOutdates(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Verdict remap", Worktree: dir})
	anchored, _, _ := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{File: "sample.txt", LineStart: 2, Text: "anchor"})
	_, _, _ = service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{PRLevel: true, Text: "overall"})
	testGit(t, dir, "rm", "sample.txt")
	testGit(t, dir, "commit", "-m", "remove reviewed file")
	report, _ := service.ReviewPR(context.Background(), pr.ID)
	judged, _, err := service.ApprovePR(context.Background(), pr.ID, ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA})
	if err != nil {
		t.Fatal(err)
	}
	if !judged.Threads[0].Outdated {
		t.Fatal("verdict did not remap anchored thread")
	}
	if judged.Threads[1].Kind != model.ThreadPRLevel || judged.Threads[1].Outdated {
		t.Fatalf("PR-level thread=%#v", judged.Threads[1])
	}
	if judged.Threads[0].ID != anchored.Threads[0].ID {
		t.Fatal("thread identity changed")
	}
}

func TestCommentsUseLiveOrExplicitPairAndMarkPostClosure(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Pairs", Worktree: dir})
	base := testGit(t, dir, "rev-parse", "refs/heads/main")
	source := testGit(t, dir, "rev-parse", "refs/heads/feature")
	live, _, _ := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{PRLevel: true, Text: "live"})
	if live.Threads[0].Comments[0].SourceHeadSHA != source || live.Threads[0].Comments[0].BaseHeadSHA != base {
		t.Fatalf("live pair=%#v", live.Threads[0].Comments[0])
	}
	testGit(t, dir, "checkout", "main")
	testGit(t, dir, "branch", "-D", "feature")
	if _, _, err := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{PRLevel: true, Text: "missing"}); err == nil || !strings.Contains(err.Error(), "explicit pair") {
		t.Fatalf("missing source error=%v", err)
	}
	explicit, _, err := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{PRLevel: true, Text: "explicit", Heads: &ExpectedHeads{Source: source, Base: base}})
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Threads[1].Comments[0].SourceHeadSHA != source {
		t.Fatal("explicit pair lost")
	}
	closed, _, err := service.ClosePR(pr.ID, ClosePRRequest{Reason: model.ClosureAbandoned})
	if err != nil {
		t.Fatal(err)
	}
	post, _, err := service.CommentPR2(context.Background(), closed.ID, ThreadCommentRequest{PRLevel: true, Text: "after close", Heads: &ExpectedHeads{Source: source, Base: base}})
	if err != nil {
		t.Fatal(err)
	}
	if !post.Threads[len(post.Threads)-1].Comments[0].PostClosure {
		t.Fatal("closed comment missing post-closure marker")
	}
}

func TestCommentAfterMergedStateCarriesPostClosureMarker(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Merged discussion", Worktree: dir})
	report, _ := service.ReviewPR(context.Background(), pr.ID)
	heads := ExpectedHeads{Source: report.Basis.SourceHeadSHA, Base: report.Basis.BaseHeadSHA}
	if _, _, err := service.ApprovePR(context.Background(), pr.ID, heads); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.MergePR(context.Background(), pr.ID, false); err != nil {
		t.Fatal(err)
	}
	commented, _, err := service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{PRLevel: true, Text: "after merge"})
	if err != nil {
		t.Fatal(err)
	}
	if !commented.Threads[0].Comments[0].PostClosure {
		t.Fatal("merged comment missing post-closure marker")
	}
}

func TestReviewWithThreadsProjectsWithoutPersistence(t *testing.T) {
	dir, service := newBranchService(t)
	pr, _, _ := service.CreatePR(context.Background(), CreatePRRequest{Title: "Pure projection", Worktree: dir})
	_, _, _ = service.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{File: "sample.txt", LineStart: 2, Text: "anchor"})
	writeTestFile(t, dir, "sample.txt", "base\nchanged\n")
	testGit(t, dir, "add", "sample.txt")
	testGit(t, dir, "commit", "-m", "edit")
	beforeRefs := testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)")
	beforeStatus := testGit(t, dir, "status", "--porcelain")
	before, version, _ := service.store.LoadPR(pr.ID)
	report, err := service.ReviewPR(context.Background(), pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Threads) != 1 || !report.Threads[0].Outdated {
		t.Fatalf("projected threads=%#v", report.Threads)
	}
	after, afterVersion, _ := service.store.LoadPR(pr.ID)
	if afterVersion != version || !reflect.DeepEqual(before, after) || testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)") != beforeRefs || testGit(t, dir, "status", "--porcelain") != beforeStatus {
		t.Fatal("review persisted projected thread state")
	}
}

func TestConcurrentBranchCommentsAreRetriedExactlyOnce(t *testing.T) {
	dir, serviceA := newBranchService(t)
	pr, _, _ := serviceA.CreatePR(context.Background(), CreatePRRequest{Title: "Concurrent", Worktree: dir})
	serviceB, _ := NewService(dir)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i, svc := range []*Service{serviceA, serviceB} {
		wg.Add(1)
		go func(i int, svc *Service) {
			defer wg.Done()
			<-start
			_, _, err := svc.CommentPR2(context.Background(), pr.ID, ThreadCommentRequest{PRLevel: true, Text: []string{"first", "second"}[i]})
			errs <- err
		}(i, svc)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, _, _ := serviceA.store.LoadPR(pr.ID)
	if len(got.Threads) != 2 {
		t.Fatalf("threads=%#v", got.Threads)
	}
	bodies := []string{got.Threads[0].Comments[0].Body, got.Threads[1].Comments[0].Body}
	if !(contains(bodies, "first") && contains(bodies, "second")) {
		t.Fatalf("bodies=%v", bodies)
	}
}

func mustLatestEvent(t *testing.T, service *Service, id string) model.ReviewEvent {
	t.Helper()
	pr, _, err := service.store.LoadPR(id)
	if err != nil {
		t.Fatal(err)
	}
	return pr.Events[len(pr.Events)-1]
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
