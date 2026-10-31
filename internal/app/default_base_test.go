package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestCreatePRDefaultBaseInWorktree(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured string
		remote     string
		local      string
		override   string
		want       string
		missing    string
	}{
		{name: "main fallback", want: "main"},
		{name: "master fallback", local: "master", want: "master"},
		{name: "saved live base", configured: "epic/current", local: "epic/current", want: "epic/current"},
		{name: "saved base precedes remote", configured: "main", remote: "epic/missing", want: "main"},
		{name: "saved deleted base", configured: "epic/deleted", missing: "epic/deleted"},
		{name: "remote nested branch", remote: "epic/current", local: "epic/current", want: "epic/current"},
		{name: "remote without local branch", remote: "epic/missing", missing: "epic/missing"},
		{name: "dangling remote head", remote: "epic/deleted", missing: "epic/deleted"},
		{name: "explicit recovery", configured: "epic/deleted", override: "main", want: "main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, service := newBranchService(t)
			if tc.local != "" {
				testGit(t, dir, "branch", tc.local, "main")
			}
			if tc.name == "master fallback" {
				testGit(t, dir, "branch", "-D", "main")
			}
			if tc.configured != "" {
				// Simulate a saved default whose branch was subsequently deleted.
				if tc.local == "" && tc.configured != "main" {
					testGit(t, dir, "branch", tc.configured, "main")
					testGit(t, dir, "branch", "-D", tc.configured)
				}
				if err := service.store.SaveConfig(model.Config{DefaultBranch: tc.configured}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.remote != "" {
				if tc.name != "dangling remote head" {
					testGit(t, dir, "update-ref", "refs/remotes/origin/"+tc.remote, "HEAD")
				}
				testGit(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/"+tc.remote)
			}
			worktree := filepath.Join(t.TempDir(), "worktree")
			testGit(t, dir, "checkout", "--detach")
			testGit(t, dir, "worktree", "add", worktree, "feature")
			before := testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/gitpr")
			pr, _, err := service.CreatePR(context.Background(), CreatePRRequest{Title: "Base detection", Worktree: worktree, BaseBranch: tc.override})
			if tc.missing != "" {
				if err == nil || !strings.Contains(err.Error(), tc.missing) || !strings.Contains(err.Error(), "--base") {
					t.Fatalf("create error = %v, want missing %q and recovery command", err, tc.missing)
				}
				if after := testGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/gitpr"); after != before {
					t.Fatalf("failed create mutated refs: before %q, after %q", before, after)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := service.store.LoadConfig()
			if err != nil || cfg.DefaultBranch != tc.want {
				t.Fatalf("saved default = %#v, %v; want %q", cfg, err, tc.want)
			}
			if pr.BaseBranch != tc.want {
				t.Fatalf("base = %q, want %q", pr.BaseBranch, tc.want)
			}
			report, err := service.ReviewPR(context.Background(), pr.ID)
			if err != nil || report.Basis.BaseBranchMissing {
				t.Fatalf("review after create = %#v, %v", report, err)
			}
		})
	}
}
