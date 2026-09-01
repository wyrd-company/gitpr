package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestPickerRendersBranchAndTitleForBothRecordShapes(t *testing.T) {
	view := ansi.Strip(pickerModel{title: "Select", prs: []model.Record{
		model.PR{ID: "01LEGACYPICKER000000000000", Status: model.StatusApproved, SourceBranch: "legacy-topic", Title: "Legacy title"},
		model.PR2{Schema: 2, ID: "01BRANCHPICKER000000000000", State: model.PRStateOpen, SourceBranch: "branch-topic", Title: "Branch title"},
	}}.View())
	for _, want := range []string{
		"01LEGACYPICK  approved   legacy-topic       Legacy title",
		"01BRANCHPICK  open       branch-topic       Branch title",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("picker view missing %q:\n%s", want, view)
		}
	}
}
