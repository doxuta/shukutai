package shukutai

import "testing"

// benchInput is what the README says the package is for: a name column. It
// mixes a one-hop fold (髙), a member of the 鶴 cycle, a rune that is already
// a fixed point (橋) and one the table does not mention.
const benchInput = "髙橋鶴橋葛飾"

func BenchmarkKey(b *testing.B) {
	Key(benchInput, Default) // pay for load and the cycle election once
	b.ResetTimer()
	var s string
	for i := 0; i < b.N; i++ {
		s = Key(benchInput, Default)
	}
	_ = s
}

// Folding a column is the bulk case, so the per-call cost has to hold up with
// every core busy: this is the benchmark a package-global lock in the Fold
// path shows up in.
func BenchmarkKeyParallel(b *testing.B) {
	Key(benchInput, Default)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var s string
		for pb.Next() {
			s = Key(benchInput, Default)
		}
		_ = s
	})
}
