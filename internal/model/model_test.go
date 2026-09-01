package model

import "testing"

func TestRecordDisplayStateUsesEachSchemaVocabulary(t *testing.T) {
	records := []struct {
		record Record
		want   string
	}{
		{record: PR{Status: StatusApproved}, want: "approved"},
		{record: PR2{State: PRStateMerged}, want: "merged"},
		{record: PR2{State: PRStateClosed, Closure: &Closure{Reason: ClosureAbandoned}}, want: "closed"},
	}
	for _, test := range records {
		if got := test.record.RecordDisplayState(); got != test.want {
			t.Errorf("RecordDisplayState() = %q, want %q", got, test.want)
		}
	}
}
