package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/wyrd-company/gitpr/internal/gitutil"
	"github.com/wyrd-company/gitpr/internal/model"
)

const (
	configRefPrefix = "refs/gitpr/config"
	prRefPrefix     = "refs/gitpr/pr"
	indexRefPrefix  = "refs/gitpr/index"
	prFileName      = "pr.yaml"
	configFileName  = "config.yaml"
	zeroOID         = "0000000000000000000000000000000000000000"
)

type Store struct {
	repo *gitutil.Repo
}

func New(root string) (*Store, error) {
	repo, err := gitutil.Open(root)
	if err != nil {
		return nil, err
	}
	return &Store{repo: repo}, nil
}

func (s *Store) LoadConfig() (model.Config, error) {
	data, err := s.showFileFromRef(configRefPrefix+"/meta", configFileName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.Config{}, nil
		}
		return model.Config{}, err
	}

	var cfg model.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return model.Config{}, err
	}
	return cfg, nil
}

func (s *Store) SaveConfig(cfg model.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	metaRef := configRefPrefix + "/meta"
	oldMeta, _ := s.resolveRef(metaRef)
	commit, err := s.writeCommit(configFileName, data, oldMeta, "gitpr: update config")
	if err != nil {
		return err
	}

	return s.batchUpdateRefs([]refUpdate{
		{
			Action: "update",
			Ref:    metaRef,
			NewOID: commit,
			OldOID: oidOrZero(oldMeta),
		},
	})
}

func (s *Store) SavePR(pr model.PR, previousStatus model.Status) (string, error) {
	if strings.TrimSpace(pr.ID) == "" {
		return "", errors.New("PR ID is required")
	}

	metaRef := s.metaRef(pr.ID)
	oldMeta, _ := s.resolveRef(metaRef)
	oldHead, _ := s.resolveRef(s.headRef(pr.ID))
	oldBase, _ := s.resolveRef(s.baseRef(pr.ID))
	currentStatusRef := s.indexRef(pr.Status, pr.ID)
	oldCurrentIndex, _ := s.resolveRef(currentStatusRef)

	data, err := yaml.Marshal(pr)
	if err != nil {
		return "", err
	}

	message := "gitpr: update " + pr.ID
	if oldMeta == "" {
		message = "gitpr: create " + pr.ID
	}

	metaCommit, err := s.writeCommit(prFileName, data, oldMeta, message)
	if err != nil {
		return "", err
	}

	updates := []refUpdate{
		{
			Action: "update",
			Ref:    metaRef,
			NewOID: metaCommit,
			OldOID: oidOrZero(oldMeta),
		},
		{
			Action: "update",
			Ref:    s.headRef(pr.ID),
			NewOID: pr.SourceHeadSHA,
			OldOID: oidOrZero(oldHead),
		},
		{
			Action: "update",
			Ref:    s.baseRef(pr.ID),
			NewOID: pr.BaseHeadSHA,
			OldOID: oidOrZero(oldBase),
		},
		{
			Action: "update",
			Ref:    currentStatusRef,
			NewOID: metaCommit,
			OldOID: oidOrZero(oldCurrentIndex),
		},
	}

	if previousStatus != "" && previousStatus != pr.Status {
		previousStatusRef := s.indexRef(previousStatus, pr.ID)
		oldPreviousIndex, _ := s.resolveRef(previousStatusRef)
		if oldPreviousIndex != "" {
			updates = append(updates, refUpdate{
				Action: "delete",
				Ref:    previousStatusRef,
				OldOID: oldPreviousIndex,
			})
		}
	}

	if err := s.batchUpdateRefs(updates); err != nil {
		return "", err
	}

	return metaRef, nil
}

func (s *Store) LoadPR(id string) (model.PR, string, error) {
	resolvedID, err := s.resolvePRID(id)
	if err != nil {
		return model.PR{}, "", err
	}

	metaRef := s.metaRef(resolvedID)
	data, err := s.showFileFromRef(metaRef, prFileName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.PR{}, "", fmt.Errorf("PR %q not found", id)
		}
		return model.PR{}, "", err
	}

	var pr model.PR
	if err := yaml.Unmarshal(data, &pr); err != nil {
		return model.PR{}, "", err
	}

	return pr, metaRef, nil
}

func (s *Store) ListPRs(filter string) ([]model.PR, error) {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		filter = string(model.StatusOpen)
	}

	ids, err := s.listIDsForFilter(filter)
	if err != nil {
		return nil, err
	}

	prs := make([]model.PR, 0, len(ids))
	for _, id := range ids {
		pr, _, err := s.LoadPR(id)
		if err != nil {
			return nil, err
		}
		prs = append(prs, pr)
	}

	sort.Slice(prs, func(i, j int) bool {
		return prs[i].ID < prs[j].ID
	})
	return prs, nil
}

func (s *Store) ExportPR(id, which, targetDir string) error {
	resolvedID, err := s.resolvePRID(id)
	if err != nil {
		return err
	}

	var ref string
	switch strings.ToLower(strings.TrimSpace(which)) {
	case "", "meta":
		ref = s.metaRef(resolvedID)
	case "head":
		ref = s.headRef(resolvedID)
	case "base":
		ref = s.baseRef(resolvedID)
	default:
		return fmt.Errorf("unsupported export ref %q", which)
	}

	resolvedRef, err := s.resolveRef(ref)
	if err != nil {
		return err
	}

	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absTarget, 0o755); err != nil {
		return err
	}

	cmdArchive := exec.Command("git", "-C", s.repo.CommonRoot, "archive", "--format=tar", resolvedRef)
	cmdExtract := exec.Command("tar", "-x", "-C", absTarget)

	reader, writer := io.Pipe()
	cmdArchive.Stdout = writer
	cmdExtract.Stdin = reader

	var archiveStderr bytes.Buffer
	var extractStderr bytes.Buffer
	cmdArchive.Stderr = &archiveStderr
	cmdExtract.Stderr = &extractStderr

	if err := cmdExtract.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return err
	}
	if err := cmdArchive.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		_ = cmdExtract.Wait()
		return err
	}

	archiveErr := cmdArchive.Wait()
	_ = writer.Close()
	extractErr := cmdExtract.Wait()
	_ = reader.Close()

	if archiveErr != nil {
		return fmt.Errorf("git archive: %s: %w", strings.TrimSpace(archiveStderr.String()), archiveErr)
	}
	if extractErr != nil {
		return fmt.Errorf("tar extract: %s: %w", strings.TrimSpace(extractStderr.String()), extractErr)
	}

	return nil
}

func (s *Store) resolvePRID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("PR ID is required")
	}

	if _, err := s.resolveRef(s.metaRef(id)); err == nil {
		return id, nil
	}

	refs, err := s.listRefs(prRefPrefix + "/" + id + "*/meta")
	if err != nil {
		return "", err
	}

	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, prIDFromMetaRef(ref.Name))
	}

	switch len(ids) {
	case 0:
		return "", fmt.Errorf("PR %q not found", id)
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("PR ID prefix %q is ambiguous", id)
	}
}

func (s *Store) listIDsForFilter(filter string) ([]string, error) {
	refNames := map[string]struct{}{}

	addRefs := func(pattern string) error {
		refs, err := s.listRefs(pattern)
		if err != nil {
			return err
		}
		for _, ref := range refs {
			refNames[ref.Name] = struct{}{}
		}
		return nil
	}

	switch filter {
	case "all":
		if err := addRefs(prRefPrefix + "/*/meta"); err != nil {
			return nil, err
		}
	case "closed":
		if err := addRefs(indexRefPrefix + "/approved/*"); err != nil {
			return nil, err
		}
		if err := addRefs(indexRefPrefix + "/rejected/*"); err != nil {
			return nil, err
		}
	case "open", "approved", "rejected":
		if err := addRefs(indexRefPrefix + "/" + filter + "/*"); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported status filter %q", filter)
	}

	ids := make([]string, 0, len(refNames))
	for ref := range refNames {
		ids = append(ids, prIDFromAnyRef(ref))
	}
	sort.Strings(ids)
	return ids, nil
}

type gitRef struct {
	Name string
	Oid  string
}

func (s *Store) listRefs(pattern string) ([]gitRef, error) {
	out, err := runGit(context.Background(), s.repo.CommonRoot, "for-each-ref", "--format=%(refname)%09%(objectname)", pattern)
	if err != nil {
		return nil, err
	}

	lines := splitLines(out)
	refs := make([]gitRef, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		refs = append(refs, gitRef{Name: parts[0], Oid: parts[1]})
	}
	return refs, nil
}

func (s *Store) resolveRef(ref string) (string, error) {
	out, err := runGit(context.Background(), s.repo.CommonRoot, "rev-parse", "--verify", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (s *Store) showFileFromRef(ref, fileName string) ([]byte, error) {
	out, err := runGit(context.Background(), s.repo.CommonRoot, "show", ref+":"+fileName)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "invalid object name") || strings.Contains(msg, "does not exist") || strings.Contains(msg, "exists on disk, but not in") {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return []byte(out), nil
}

func (s *Store) writeCommit(fileName string, data []byte, parent, message string) (string, error) {
	blob, err := hashObject(s.repo.CommonRoot, data)
	if err != nil {
		return "", err
	}

	treeInput := fmt.Sprintf("100644 blob %s\t%s\n", blob, fileName)
	tree, err := runGitWithStdin(context.Background(), s.repo.CommonRoot, treeInput, "mktree")
	if err != nil {
		return "", err
	}
	tree = strings.TrimSpace(tree)

	args := []string{"commit-tree", tree, "-m", message}
	if parent != "" {
		args = append(args, "-p", parent)
	}

	env, err := commitEnv(s.repo.CommonRoot)
	if err != nil {
		return "", err
	}

	commit, err := runGitWithEnv(context.Background(), s.repo.CommonRoot, env, "", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(commit), nil
}

func (s *Store) metaRef(id string) string {
	return prRefPrefix + "/" + id + "/meta"
}

func (s *Store) headRef(id string) string {
	return prRefPrefix + "/" + id + "/head"
}

func (s *Store) baseRef(id string) string {
	return prRefPrefix + "/" + id + "/base"
}

func (s *Store) indexRef(status model.Status, id string) string {
	return indexRefPrefix + "/" + string(status) + "/" + id
}

func prIDFromMetaRef(ref string) string {
	ref = strings.TrimPrefix(ref, prRefPrefix+"/")
	return strings.TrimSuffix(ref, "/meta")
}

func prIDFromAnyRef(ref string) string {
	if strings.HasPrefix(ref, prRefPrefix+"/") {
		return prIDFromMetaRef(ref)
	}
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

func hashObject(dir string, data []byte) (string, error) {
	out, err := runGitWithStdin(context.Background(), dir, string(data), "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	return runGitWithEnv(ctx, dir, os.Environ(), "", args...)
}

func runGitWithEnv(ctx context.Context, dir string, env []string, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return stdout.String(), nil
}

func runGitWithStdin(ctx context.Context, dir, stdin string, args ...string) (string, error) {
	return runGitWithEnv(ctx, dir, os.Environ(), stdin, args...)
}

func splitLines(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

type refUpdate struct {
	Action string
	Ref    string
	NewOID string
	OldOID string
}

func (s *Store) batchUpdateRefs(updates []refUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	var script strings.Builder
	script.WriteString("start\n")
	for _, update := range updates {
		switch update.Action {
		case "update":
			script.WriteString(fmt.Sprintf("update %s %s %s\n", update.Ref, update.NewOID, oidOrZero(update.OldOID)))
		case "delete":
			script.WriteString(fmt.Sprintf("delete %s %s\n", update.Ref, oidOrZero(update.OldOID)))
		default:
			return fmt.Errorf("unsupported ref update action %q", update.Action)
		}
	}
	script.WriteString("prepare\ncommit\n")

	_, err := runGitWithStdin(context.Background(), s.repo.CommonRoot, script.String(), "update-ref", "--stdin")
	return err
}

func oidOrZero(oid string) string {
	if strings.TrimSpace(oid) == "" {
		return zeroOID
	}
	return oid
}

func commitEnv(dir string) ([]string, error) {
	name, _ := runGit(context.Background(), dir, "config", "user.name")
	email, _ := runGit(context.Background(), dir, "config", "user.email")

	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	if name == "" {
		name = "gitpr"
	}
	if email == "" {
		email = "gitpr@local"
	}

	env := os.Environ()
	env = append(env,
		"GIT_AUTHOR_NAME="+name,
		"GIT_AUTHOR_EMAIL="+email,
		"GIT_COMMITTER_NAME="+name,
		"GIT_COMMITTER_EMAIL="+email,
	)
	return env, nil
}
