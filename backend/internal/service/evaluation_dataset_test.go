package service

import (
	"reflect"
	"testing"
)

func TestGenerateSyntheticDataset_SameSeedIsIdentical(t *testing.T) {
	a := GenerateSyntheticDataset(12345, 200)
	b := GenerateSyntheticDataset(12345, 200)

	if !reflect.DeepEqual(a.Opportunities, b.Opportunities) {
		t.Fatal("same seed produced different opportunities")
	}
	if !reflect.DeepEqual(a.groundTruths, b.groundTruths) {
		t.Fatal("same seed produced different ground truths")
	}
	if a.Type != "synthetic" || b.Type != "synthetic" {
		t.Fatalf("dataset must be labeled synthetic, got %q and %q", a.Type, b.Type)
	}
}

func TestGenerateSyntheticDataset_DifferentSeedIsDifferent(t *testing.T) {
	a := GenerateSyntheticDataset(12345, 200)
	b := GenerateSyntheticDataset(54321, 200)

	if reflect.DeepEqual(a.Opportunities, b.Opportunities) {
		t.Fatal("different seeds produced identical opportunities")
	}
}

func TestGenerateSyntheticDataset_CountRespected(t *testing.T) {
	d := GenerateSyntheticDataset(1, 537)
	if len(d.Opportunities) != 537 {
		t.Fatalf("expected 537 opportunities, got %d", len(d.Opportunities))
	}
	if len(d.groundTruths) != 537 {
		t.Fatalf("expected 537 ground truths, got %d", len(d.groundTruths))
	}
}

func TestGenerateSyntheticDataset_ZeroCases(t *testing.T) {
	d := GenerateSyntheticDataset(1, 0)
	if len(d.Opportunities) != 0 {
		t.Fatalf("expected 0 opportunities, got %d", len(d.Opportunities))
	}
}

func TestGenerateSyntheticDataset_AllFieldsInBounds(t *testing.T) {
	d := GenerateSyntheticDataset(999, 500)
	for _, opp := range d.Opportunities {
		if opp.AmountMinorUnits <= 0 {
			t.Fatalf("opportunity %s has non-positive amount %d", opp.ID, opp.AmountMinorUnits)
		}
		if !opp.FailureCategory.Valid() {
			t.Fatalf("opportunity %s has invalid failure category %q", opp.ID, opp.FailureCategory)
		}
		if opp.PreviousAttempts < 1 {
			t.Fatalf("opportunity %s has PreviousAttempts < 1: %d", opp.ID, opp.PreviousAttempts)
		}
		if opp.PreviousRecoveryActions < 0 {
			t.Fatalf("opportunity %s has negative PreviousRecoveryActions", opp.ID)
		}
		if opp.HoursSinceFailure < 0 {
			t.Fatalf("opportunity %s has negative HoursSinceFailure", opp.ID)
		}
	}
}

func TestGenerateSyntheticDataset_UniqueIDs(t *testing.T) {
	d := GenerateSyntheticDataset(7, 300)
	seen := make(map[string]bool, len(d.Opportunities))
	for _, opp := range d.Opportunities {
		if seen[opp.ID] {
			t.Fatalf("duplicate opportunity ID %s", opp.ID)
		}
		seen[opp.ID] = true
	}
}
