//go:build test

package store

import (
	"errors"
	"testing"

	"github.com/wyrd-company/gitpr/internal/model"
)

func TestSavePR2EventAppendIsAtomicOnInterleavedConflict(t *testing.T) {
	losingStore, base, head := newStoreTestHistory(t)
	pr := completePR2(losingStore.repo.CommonRoot, base, head)
	pr.Events = nil
	pr.Threads = nil
	if _, err := losingStore.SavePR2(pr, "", ""); err != nil {
		t.Fatal(err)
	}
	winnerStore, err := New(losingStore.repo.CommonRoot)
	if err != nil {
		t.Fatal(err)
	}

	loser, staleVersion, err := losingStore.LoadPR2(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	loser.State = model.PRStateMerged
	loser.Events = completePR2(losingStore.repo.CommonRoot, base, head).Events

	losingStore.SetBeforeSaveHook(func() {
		winner, version, loadErr := winnerStore.LoadPR2(pr.ID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		winner.Title = "interleaving winner"
		if _, saveErr := winnerStore.SavePR2(winner, winner.State, version); saveErr != nil {
			t.Fatal(saveErr)
		}
	})

	if _, err := losingStore.SavePR2(loser, model.PRStateOpen, staleVersion); !errors.Is(err, ErrMetadataConflict) {
		t.Fatalf("losing append error = %v, want ErrMetadataConflict", err)
	}
	loaded, version, err := losingStore.LoadPR2(pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != "interleaving winner" || loaded.State != model.PRStateOpen || len(loaded.Events) != 0 {
		t.Fatalf("metadata contains losing append: %#v", loaded)
	}
	assertRef(t, losingStore, losingStore.indexRef2(model.PRStateOpen, pr.ID), version)
	assertMissingRef(t, losingStore, losingStore.indexRef2(model.PRStateMerged, pr.ID))
	assertMissingRef(t, losingStore, eventRef(pr.ID, loser.Events[0].ID, "head"))
	assertMissingRef(t, losingStore, eventRef(pr.ID, loser.Events[0].ID, "base"))
}
