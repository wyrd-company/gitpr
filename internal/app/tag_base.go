// relationships:
//
//	implements: branch-based-pull-requests
package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/wyrd-company/gitpr/internal/gitutil"
	"github.com/wyrd-company/gitpr/internal/model"
)

// Unqualified names prefer branches. An explicit refs/tags/ name selects a tag
// even when a branch has the same short name.
func resolveBase(ctx context.Context, repo *gitutil.Repo, name string) (string, *model.TagBase, error) {
	if strings.HasPrefix(name, "refs/heads/") {
		return strings.TrimPrefix(name, "refs/heads/"), nil, nil
	}
	if !strings.HasPrefix(name, "refs/tags/") {
		exists, err := repo.BranchExists(ctx, name)
		if err != nil {
			return "", nil, err
		}
		if exists {
			return name, nil, nil
		}
		ref := "refs/tags/" + name
		if _, err := repo.HeadSHA(ctx, ref); err != nil {
			return name, nil, nil
		}
		name = ref
	}
	object, commit, err := repo.TagIdentity(ctx, name)
	if err != nil {
		return "", nil, fmt.Errorf("resolve base tag %q: %w", name, err)
	}
	return name, &model.TagBase{Ref: name, ObjectSHA: object, CommitSHA: commit}, nil
}

func validateTagBase(ctx context.Context, repo *gitutil.Repo, pr model.PR2) error {
	if pr.BaseTag == nil {
		return nil
	}
	tag := pr.BaseTag
	object, commit, err := repo.TagIdentity(ctx, tag.Ref)
	if err != nil {
		return fmt.Errorf("base tag %q is missing or no longer resolves to a commit: %w", tag.Ref, err)
	}
	if object != tag.ObjectSHA || commit != tag.CommitSHA {
		return fmt.Errorf("base tag %q drifted: expected object %s commit %s, live object %s commit %s; create a new review for the changed tag", tag.Ref, tag.ObjectSHA, tag.CommitSHA, object, commit)
	}
	return nil
}
