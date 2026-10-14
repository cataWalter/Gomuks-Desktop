package ui

import "testing"

func TestFuzzyScoreMatchesSubsequencesAndPrefersContiguousText(t *testing.T) {
	subsequenceScore, ok := fuzzyScore("ac", "Alice Example")
	if !ok {
		t.Fatal("expected a subsequence match")
	}
	contiguousScore, ok := fuzzyScore("ali", "Alice Example")
	if !ok {
		t.Fatal("expected a contiguous match")
	}
	if contiguousScore <= subsequenceScore {
		t.Fatalf("contiguous score %d should exceed subsequence score %d", contiguousScore, subsequenceScore)
	}
	if _, ok = fuzzyScore("zz", "Alice Example"); ok {
		t.Fatal("unexpected match for absent characters")
	}
}
