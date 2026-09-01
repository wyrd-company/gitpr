package store

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	configRefPrefix   = "refs/gitpr/config"
	prRefPrefix       = "refs/gitpr/pr"
	indexRefPrefix    = "refs/gitpr/index"
	openPairRefPrefix = "refs/gitpr/openpair"
	prFileName        = "pr.yaml"
	configFileName    = "config.yaml"
	zeroOID           = "0000000000000000000000000000000000000000"
)

type Store struct {
	repo           *gitutil.Repo
	beforeSaveHook func()
}

var ErrMetadataConflict = errors.New("gitpr metadata changed concurrently")
var ErrRecordSchema = errors.New("gitpr record has a different schema")
var ErrLegacyWriteSchema = errors.New("legacy write requires a legacy record")
var ErrSchema2WriteSchema = errors.New("schema-2 write requires a schema-2 record")
var ErrUnsupportedSchema = errors.New("unsupported PR schema")
var ErrDuplicateEventID = errors.New("duplicate review event ID")
var ErrInvalidEventObjectID = errors.New("review event has an invalid object ID")
var ErrOpenPairConflict = errors.New("an open branch-based PR already tracks this branch pair")
var ErrMergeConflict = errors.New("branch-based merge transaction conflicted")
var ErrMergedStateRequiresMerge = errors.New("merged state requires the atomic branch merge operation")

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

func (s *Store) SavePR(pr model.PR, previousStatus model.Status, expectedMeta string) (string, error) {
	if strings.TrimSpace(pr.ID) == "" {
		return "", errors.New("PR ID is required")
	}
	if s.beforeSaveHook != nil {
		s.beforeSaveHook()
	}
	if err := s.validateLegacyExpectedMeta(expectedMeta); err != nil {
		return "", err
	}

	metaRef := s.metaRef(pr.ID)
	oldHead, _ := s.resolveRef(s.headRef(pr.ID))
	oldBase, _ := s.resolveRef(s.baseRef(pr.ID))
	currentStatusRef := s.indexRef(pr.Status, pr.ID)
	oldCurrentIndex, _ := s.resolveRef(currentStatusRef)

	data, err := yaml.Marshal(pr)
	if err != nil {
		return "", err
	}

	message := "gitpr: update " + pr.ID
	if expectedMeta == "" {
		message = "gitpr: create " + pr.ID
	}

	metaCommit, err := s.writeCommit(prFileName, data, expectedMeta, message)
	if err != nil {
		return "", err
	}

	updates := []refUpdate{
		{
			Action: "update",
			Ref:    metaRef,
			NewOID: metaCommit,
			OldOID: oidOrZero(expectedMeta),
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
		if isRefConflict(err) {
			return "", fmt.Errorf("%w for PR %s", ErrMetadataConflict, pr.ID)
		}
		return "", err
	}

	return metaRef, nil
}

func (s *Store) SavePR2(pr model.PR2, previousState model.PRState, expectedMeta string) (string, error) {
	return s.savePR2(pr, previousState, expectedMeta, nil)
}

func (s *Store) MergePR2(pr model.PR2, expectedMeta string) (string, error) {
	if pr.State != model.PRStateMerged || len(pr.Events) == 0 {
		return "", errors.New("merged schema-2 PR requires a review event")
	}
	data, err := s.showFileFromRef(expectedMeta, prFileName)
	if err != nil {
		return "", err
	}
	var previous model.PR2
	if err := yaml.Unmarshal(data, &previous); err != nil {
		return "", err
	}
	latest := pr.Events[len(pr.Events)-1]
	baseUpdate := &refUpdate{Action: "update", Ref: "refs/heads/" + pr.BaseBranch, NewOID: latest.SourceHeadSHA, OldOID: latest.BaseHeadSHA}
	return s.savePR2(pr, previous.State, expectedMeta, baseUpdate)
}

func (s *Store) savePR2(pr model.PR2, previousState model.PRState, expectedMeta string, baseUpdate *refUpdate) (string, error) {
	if strings.TrimSpace(pr.ID) == "" {
		return "", errors.New("PR ID is required")
	}
	if pr.Schema != 2 {
		return "", errors.New("schema-2 PR must have schema: 2")
	}
	if pr.State == model.PRStateMerged && baseUpdate == nil {
		return "", fmt.Errorf("%w for PR %s", ErrMergedStateRequiresMerge, pr.ID)
	}
	if s.beforeSaveHook != nil {
		s.beforeSaveHook()
	}
	if err := s.validateEventHistory(pr, expectedMeta); err != nil {
		return "", err
	}

	data, err := yaml.Marshal(pr)
	if err != nil {
		return "", err
	}
	message := "gitpr: update " + pr.ID
	if expectedMeta == "" {
		message = "gitpr: create " + pr.ID
	}
	metaCommit, err := s.writeCommit(prFileName, data, expectedMeta, message)
	if err != nil {
		return "", err
	}

	metaRef := s.metaRef(pr.ID)
	currentIndexRef := s.indexRef2(pr.State, pr.ID)
	oldCurrentIndex, _ := s.resolveRef(currentIndexRef)
	updates := make([]refUpdate, 0, 8)
	if baseUpdate != nil {
		updates = append(updates, *baseUpdate)
	}
	updates = append(updates, refUpdate{Action: "update", Ref: metaRef, NewOID: metaCommit, OldOID: oidOrZero(expectedMeta)}, refUpdate{
		Action: "update", Ref: currentIndexRef, NewOID: metaCommit, OldOID: oidOrZero(oldCurrentIndex),
	})
	// Legacy PRs deliberately do not participate: their frozen snapshots do not
	// own a live source/base branch pair.
	openPairRef := s.openPairRef(pr.SourceBranch, pr.BaseBranch)
	if expectedMeta == "" && pr.State == model.PRStateOpen {
		updates = append(updates, refUpdate{Action: "update", Ref: openPairRef, NewOID: metaCommit, OldOID: zeroOID})
	}
	if previousState == model.PRStateOpen && pr.State != model.PRStateOpen {
		if oid, err := s.resolveRef(openPairRef); err == nil {
			updates = append(updates, refUpdate{Action: "delete", Ref: openPairRef, OldOID: oid})
		}
	}
	if previousState != "" && previousState != pr.State {
		oldRef := s.indexRef2(previousState, pr.ID)
		if oid, err := s.resolveRef(oldRef); err == nil {
			updates = append(updates, refUpdate{Action: "delete", Ref: oldRef, OldOID: oid})
		}
	}

	pins := s.desiredPR2Pins(pr)
	existing, err := s.listRefs(prRefPrefix + "/" + pr.ID + "/events")
	if err != nil {
		return "", err
	}
	anchors, err := s.listRefs(prRefPrefix + "/" + pr.ID + "/anchors")
	if err != nil {
		return "", err
	}
	existing = append(existing, anchors...)
	for _, ref := range existing {
		desired, keep := pins[ref.Name]
		if !keep {
			updates = append(updates, refUpdate{Action: "delete", Ref: ref.Name, OldOID: ref.Oid})
			continue
		}
		delete(pins, ref.Name)
		if desired != ref.Oid {
			updates = append(updates, refUpdate{Action: "update", Ref: ref.Name, NewOID: desired, OldOID: ref.Oid})
		}
	}
	for ref, oid := range pins {
		updates = append(updates, refUpdate{Action: "update", Ref: ref, NewOID: oid, OldOID: zeroOID})
	}

	if err := s.batchUpdateRefs(updates); err != nil {
		if isRefConflict(err) {
			if baseUpdate != nil {
				return "", fmt.Errorf("%w for PR %s", ErrMergeConflict, pr.ID)
			}
			if expectedMeta == "" && pr.State == model.PRStateOpen {
				return "", fmt.Errorf("%w: %s into %s", ErrOpenPairConflict, pr.SourceBranch, pr.BaseBranch)
			}
			return "", fmt.Errorf("%w for PR %s", ErrMetadataConflict, pr.ID)
		}
		return "", err
	}
	return metaRef, nil
}

func (s *Store) DeletePR2(pr model.PR2, expectedMeta string) error {
	updates := []refUpdate{{Action: "delete", Ref: s.metaRef(pr.ID), OldOID: expectedMeta}}
	if oid, err := s.resolveRef(s.indexRef2(pr.State, pr.ID)); err == nil {
		updates = append(updates, refUpdate{Action: "delete", Ref: s.indexRef2(pr.State, pr.ID), OldOID: oid})
	}
	if pr.State == model.PRStateOpen {
		ref := s.openPairRef(pr.SourceBranch, pr.BaseBranch)
		if oid, err := s.resolveRef(ref); err == nil {
			updates = append(updates, refUpdate{Action: "delete", Ref: ref, OldOID: oid})
		}
	}
	refs, err := s.listRefs(prRefPrefix + "/" + pr.ID)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if ref.Name != s.metaRef(pr.ID) {
			updates = append(updates, refUpdate{Action: "delete", Ref: ref.Name, OldOID: ref.Oid})
		}
	}
	if err := s.batchUpdateRefs(updates); err != nil {
		if isRefConflict(err) {
			return fmt.Errorf("%w for PR %s", ErrMetadataConflict, pr.ID)
		}
		return err
	}
	return nil
}

func (s *Store) DeletePR(pr model.PR, expectedMeta string) error {
	updates := []refUpdate{{Action: "delete", Ref: s.metaRef(pr.ID), OldOID: expectedMeta}}
	for _, ref := range []string{s.headRef(pr.ID), s.baseRef(pr.ID), s.indexRef(pr.Status, pr.ID)} {
		if oid, err := s.resolveRef(ref); err == nil {
			updates = append(updates, refUpdate{Action: "delete", Ref: ref, OldOID: oid})
		}
	}
	if err := s.batchUpdateRefs(updates); err != nil {
		if isRefConflict(err) {
			return fmt.Errorf("%w for PR %s", ErrMetadataConflict, pr.ID)
		}
		return err
	}
	return nil
}

func (s *Store) validateEventHistory(pr model.PR2, expectedMeta string) error {
	seen := make(map[string]struct{}, len(pr.Events))
	for _, event := range pr.Events {
		if _, duplicate := seen[event.ID]; duplicate {
			return fmt.Errorf("%w %q", ErrDuplicateEventID, event.ID)
		}
		seen[event.ID] = struct{}{}
	}
	previousEventCount := 0
	if expectedMeta == "" {
		return validateNewEventObjectIDs(pr.Events, previousEventCount)
	}
	data, err := s.showFileFromRef(expectedMeta, prFileName)
	if err != nil {
		return err
	}
	var discriminator struct {
		Schema *int `yaml:"schema"`
	}
	if err := yaml.Unmarshal(data, &discriminator); err != nil {
		return err
	}
	if discriminator.Schema == nil || *discriminator.Schema != 2 {
		return fmt.Errorf("%w: prior metadata is not schema 2", ErrSchema2WriteSchema)
	}
	var previous model.PR2
	if err := yaml.Unmarshal(data, &previous); err != nil {
		return err
	}
	if len(pr.Events) < len(previous.Events) {
		return errors.New("review events are append-only")
	}
	previousEventCount = len(previous.Events)
	for i := range previous.Events {
		if !sameReviewEvent(pr.Events[i], previous.Events[i]) {
			return errors.New("review events are immutable")
		}
	}
	return validateNewEventObjectIDs(pr.Events, previousEventCount)
}

func validateNewEventObjectIDs(events []model.ReviewEvent, start int) error {
	for _, event := range events[start:] {
		for field, value := range map[string]string{
			"source_head_sha": event.SourceHeadSHA,
			"base_head_sha":   event.BaseHeadSHA,
			"merge_base_sha":  event.MergeBaseSHA,
		} {
			if !isFullObjectID(value) {
				return fmt.Errorf("%w: event %q field %s must be 40 lowercase hexadecimal characters", ErrInvalidEventObjectID, event.ID, field)
			}
		}
	}
	return nil
}

func isFullObjectID(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func (s *Store) validateLegacyExpectedMeta(expectedMeta string) error {
	if expectedMeta == "" {
		return nil
	}
	data, err := s.showFileFromRef(expectedMeta, prFileName)
	if err != nil {
		return err
	}
	var discriminator struct {
		Schema *int `yaml:"schema"`
	}
	if err := yaml.Unmarshal(data, &discriminator); err != nil {
		return err
	}
	if discriminator.Schema != nil {
		return fmt.Errorf("%w: prior metadata has schema %d", ErrLegacyWriteSchema, *discriminator.Schema)
	}
	return nil
}

func sameReviewEvent(left, right model.ReviewEvent) bool {
	return left.ID == right.ID &&
		left.SourceHeadSHA == right.SourceHeadSHA &&
		left.BaseHeadSHA == right.BaseHeadSHA &&
		left.MergeBaseSHA == right.MergeBaseSHA &&
		left.Verdict == right.Verdict &&
		left.Timestamp.Equal(right.Timestamp) &&
		left.PredecessorEventID == right.PredecessorEventID
}

func (s *Store) desiredPR2Pins(pr model.PR2) map[string]string {
	pins := make(map[string]string)
	pairs := make(map[string]struct{}, len(pr.Events))
	for _, event := range pr.Events {
		prefix := fmt.Sprintf("%s/%s/events/%s", prRefPrefix, pr.ID, event.ID)
		pins[prefix+"/head"] = event.SourceHeadSHA
		pins[prefix+"/base"] = event.BaseHeadSHA
		pairs[event.SourceHeadSHA+"\x00"+event.BaseHeadSHA] = struct{}{}
	}
	for _, thread := range pr.Threads {
		if thread.Kind != model.ThreadAnchored || thread.Anchor == nil {
			continue
		}
		pair := thread.Anchor.SourceHeadSHA + "\x00" + thread.Anchor.BaseHeadSHA
		if _, recorded := pairs[pair]; recorded {
			continue
		}
		prefix := fmt.Sprintf("%s/%s/anchors/%s", prRefPrefix, pr.ID, thread.ID)
		pins[prefix+"/head"] = thread.Anchor.SourceHeadSHA
		pins[prefix+"/base"] = thread.Anchor.BaseHeadSHA
	}
	return pins
}

func (s *Store) loadRecordData(id string) (string, []byte, error) {
	resolvedID, err := s.resolvePRID(id)
	if err != nil {
		return "", nil, err
	}

	metaRef := s.metaRef(resolvedID)
	metaVersion, exists, err := s.resolveRefForLoad(metaRef)
	if err != nil {
		return "", nil, err
	}
	if !exists {
		return "", nil, fmt.Errorf("PR %q not found", id)
	}
	data, err := s.showFileFromRef(metaVersion, prFileName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf("PR %q not found", id)
		}
		return "", nil, err
	}
	return metaVersion, data, nil
}

func (s *Store) LoadPR(id string) (model.Record, string, error) {
	metaVersion, data, err := s.loadRecordData(id)
	if err != nil {
		return nil, "", err
	}

	var discriminator struct {
		Schema *int `yaml:"schema"`
	}
	if err := yaml.Unmarshal(data, &discriminator); err != nil {
		return nil, "", err
	}
	if discriminator.Schema == nil {
		var pr model.PR
		if err := yaml.Unmarshal(data, &pr); err != nil {
			return nil, "", err
		}
		return pr, metaVersion, nil
	}
	if *discriminator.Schema != 2 {
		return nil, "", fmt.Errorf("%w %d", ErrUnsupportedSchema, *discriminator.Schema)
	}
	var pr model.PR2
	if err := yaml.Unmarshal(data, &pr); err != nil {
		return nil, "", err
	}
	return pr, metaVersion, nil
}

func (s *Store) LoadLegacyPR(id string) (model.PR, string, error) {
	record, version, err := s.LoadPR(id)
	if err != nil {
		return model.PR{}, "", err
	}
	pr, ok := record.(model.PR)
	if !ok {
		return model.PR{}, "", fmt.Errorf("%w: PR %q is schema 2, not legacy", ErrRecordSchema, id)
	}
	return pr, version, nil
}

func (s *Store) LoadPR2(id string) (model.PR2, string, error) {
	record, version, err := s.LoadPR(id)
	if err != nil {
		return model.PR2{}, "", err
	}
	pr, ok := record.(model.PR2)
	if !ok {
		return model.PR2{}, "", fmt.Errorf("%w: PR %q is legacy, not schema 2", ErrRecordSchema, id)
	}
	return pr, version, nil
}

func (s *Store) ListPRs(filter string) ([]model.Record, error) {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		filter = string(model.StatusOpen)
	}

	ids, err := s.listIDsForFilter(filter)
	if err != nil {
		return nil, err
	}

	prs := make([]model.Record, 0, len(ids))
	for _, id := range ids {
		pr, _, err := s.LoadPR(id)
		if err != nil {
			if errors.Is(err, ErrUnsupportedSchema) {
				continue
			}
			return nil, err
		}
		prs = append(prs, pr)
	}

	sort.Slice(prs, func(i, j int) bool {
		return prs[i].RecordID() < prs[j].RecordID()
	})
	return prs, nil
}

func (s *Store) ListLegacyPRs(filter string) ([]model.PR, error) {
	// Temporary compatibility narrowing: increment 6 rewires service listing to
	// consume schema-dispatched records before schema-2 creation becomes public.
	records, err := s.ListPRs(filter)
	if err != nil {
		return nil, err
	}
	prs := make([]model.PR, 0, len(records))
	for _, record := range records {
		if pr, ok := record.(model.PR); ok {
			prs = append(prs, pr)
		}
	}
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
	case "open", "approved", "rejected", "merged", "closed":
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

func (s *Store) resolveRefForLoad(ref string) (string, bool, error) {
	out, err := runGit(context.Background(), s.repo.CommonRoot, "rev-parse", "--verify", "--quiet", ref)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
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

func (s *Store) indexRef2(state model.PRState, id string) string {
	return indexRefPrefix + "/" + string(state) + "/" + id
}

func (s *Store) openPairRef(sourceBranch, baseBranch string) string {
	digest := sha256.Sum256([]byte(sourceBranch + "\x00" + baseBranch))
	return fmt.Sprintf("%s/%x", openPairRefPrefix, digest)
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
	return runGitWithEnv(ctx, dir, stableGitEnv(), "", args...)
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
	return runGitWithEnv(ctx, dir, stableGitEnv(), stdin, args...)
}

func stableGitEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "LC_ALL=") {
			env = append(env, value)
		}
	}
	return append(env, "LC_ALL=C")
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

func isRefConflict(err error) bool {
	message := err.Error()
	return strings.Contains(message, "cannot lock ref") ||
		strings.Contains(message, "reference already exists")
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
