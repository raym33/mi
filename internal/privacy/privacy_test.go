package privacy

import "testing"

func TestTiersForMode(t *testing.T) {
	tests := map[string][]string{
		"":        {Community, Private, Public},
		Private:   {Private, Community, Public},
		Community: {Community, Public},
		Public:    {Public},
	}
	for mode, want := range tests {
		got, err := TiersForMode(mode)
		if err != nil {
			t.Fatalf("TiersForMode(%q): %v", mode, err)
		}
		for _, tier := range want {
			if !Accepts(got, tier) {
				t.Fatalf("TiersForMode(%q) = %v, want it to accept %q", mode, got, tier)
			}
		}
	}
}

func TestRejectsInvalidTier(t *testing.T) {
	if _, err := NormalizeTier("secret"); err == nil {
		t.Fatal("NormalizeTier should reject unknown tiers")
	}
	if _, err := TiersForMode("secret"); err == nil {
		t.Fatal("TiersForMode should reject unknown modes")
	}
}
