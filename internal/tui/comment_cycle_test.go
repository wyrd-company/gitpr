package tui

import (
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestAdvanceCommentCycleThroughAnchorThenAppends(t *testing.T) {
	target := commentTarget{filePath: "app.txt", lineStart: 2, lineEnd: 2}
	matches := []anchoredComment{
		{index: 0, comment: model.Comment{Comment: "first"}},
		{index: 2, comment: model.Comment{Comment: "second"}},
	}

	state, editIndex, isNew := advanceCommentCycle(commentCycleState{}, target, matches)
	if isNew || editIndex != 0 {
		t.Fatalf("first press: editIndex=%d isNew=%v, want edit 0", editIndex, isNew)
	}

	state, editIndex, isNew = advanceCommentCycle(state, target, matches)
	if isNew || editIndex != 2 {
		t.Fatalf("second press: editIndex=%d isNew=%v, want edit 2", editIndex, isNew)
	}

	state, editIndex, isNew = advanceCommentCycle(state, target, matches)
	if !isNew || editIndex != -1 {
		t.Fatalf("third press: editIndex=%d isNew=%v, want new comment", editIndex, isNew)
	}

	_, editIndex, isNew = advanceCommentCycle(state, target, matches)
	if isNew || editIndex != 0 {
		t.Fatalf("fourth press: editIndex=%d isNew=%v, want cycle restart at 0", editIndex, isNew)
	}
}

func TestAdvanceCommentCycleResetsOnDifferentAnchor(t *testing.T) {
	first := commentTarget{filePath: "app.txt", lineStart: 2, lineEnd: 2}
	second := commentTarget{filePath: "app.txt", lineStart: 4, lineEnd: 4}
	matches := []anchoredComment{{index: 1, comment: model.Comment{Comment: "only"}}}

	state, editIndex, isNew := advanceCommentCycle(commentCycleState{}, first, matches)
	if isNew || editIndex != 1 {
		t.Fatalf("first anchor: editIndex=%d isNew=%v, want edit 1", editIndex, isNew)
	}

	state, editIndex, isNew = advanceCommentCycle(state, second, []anchoredComment{{index: 3, comment: model.Comment{Comment: "other"}}})
	if isNew || editIndex != 3 {
		t.Fatalf("different anchor: editIndex=%d isNew=%v, want edit 3", editIndex, isNew)
	}
}

func TestAdvanceCommentCycleEmptyAnchorOpensNew(t *testing.T) {
	target := commentTarget{filePath: "app.txt", lineStart: 1, lineEnd: 1}
	_, editIndex, isNew := advanceCommentCycle(commentCycleState{}, target, nil)
	if !isNew || editIndex != -1 {
		t.Fatalf("empty anchor: editIndex=%d isNew=%v, want new comment", editIndex, isNew)
	}
}
