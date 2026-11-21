package main

import (
	"slices"
	"testing"
)

// Every test lands in exactly one shard, and the long ones are spread.
func TestSplit(t *testing.T) {
	tests := []string{"TestA", "TestB", "TestC", "TestD", "TestE", "TestF"}
	times := map[string]float64{"TestA": 100, "TestB": 90, "TestC": 10}
	for n := 1; n <= 7; n++ {
		groups := split(tests, times, n)
		var all []string
		for _, g := range groups {
			for _, x := range g {
				if x != "TestNoTestInThisShard" {
					all = append(all, x)
				}
			}
		}
		slices.Sort(all)
		if !slices.Equal(all, tests) {
			t.Fatalf("%d shards: %v", n, groups)
		}
	}
	g := split(tests, times, 2)
	if slices.Contains(g[0], "TestA") == slices.Contains(g[0], "TestB") {
		t.Fatalf("the two long tests are in the same shard: %v", g)
	}
}
