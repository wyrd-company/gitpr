package tui

import "github.com/wyrd-company/gitpr/internal/model"

type anchoredComment struct {
	index   int
	comment model.Comment
}

type commentCycleState struct {
	target commentTarget
	next   int
}

func (t commentTarget) equal(other commentTarget) bool {
	return t.filePath == other.filePath && t.lineStart == other.lineStart && t.lineEnd == other.lineEnd
}

func commentsAtExactTarget(comments []model.Comment, target commentTarget) []anchoredComment {
	var matched []anchoredComment
	for idx, comment := range comments {
		if comment.FilePath == target.filePath && comment.LineStart == target.lineStart && comment.LineEnd == target.lineEnd {
			matched = append(matched, anchoredComment{index: idx, comment: comment})
		}
	}
	return matched
}

// advanceCommentCycle picks the next edit or append action when the user
// presses c repeatedly on the same anchor. It cycles through existing
// comments at that anchor before opening a new-comment buffer.
func advanceCommentCycle(state commentCycleState, target commentTarget, matches []anchoredComment) (commentCycleState, int, bool) {
	if len(matches) == 0 {
		return commentCycleState{target: target}, -1, true
	}

	if !state.target.equal(target) {
		state = commentCycleState{target: target, next: 0}
	}

	if state.next < len(matches) {
		editIndex := matches[state.next].index
		state.next++
		return state, editIndex, false
	}

	state.next = 0
	return state, -1, true
}
