package shukutai

import (
	"bufio"
	"bytes"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// Family-name variants that Unicode normalisation does not touch.
func TestFoldCommonNameVariants(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"髙", "高"}, // U+9AD9 → U+9AD8
		{"﨑", "崎"}, // U+FA11 → U+5D0E
		{"德", "徳"}, // U+5FB7 → U+5FB3
		{"濵", "浜"}, // U+6FF5 → U+6D5C
		{"栁", "柳"}, // U+6801 → U+67F3
		{"齋", "斎"}, // U+9F4B → U+658E
		{"邊", "辺"}, // U+908A → U+8FBA
		{"邉", "辺"}, // U+9089 → {辺, 邊}; 邊 → 辺, so both branches agree
		{"高", "高"}, // already canonical: fixed point
		{"あ", "あ"}, // not in the table at all
	}
	for _, c := range cases {
		got, ok := Fold([]rune(c.in)[0], Default)
		if !ok || string(got) != c.want {
			t.Errorf("Fold(%s) = %s, %v; want %s, true", c.in, string(got), ok, c.want)
		}
	}
}

func TestEqualAcrossSpellings(t *testing.T) {
	for _, pair := range [][2]string{
		{"髙橋", "高橋"},
		{"渡邉", "渡辺"},
		{"渡邊", "渡邉"},
		{"齋藤", "斎藤"},
		{"山﨑", "山崎"},
		{"塚本", "塚本"}, // U+FA10 compatibility ideograph vs U+585A
	} {
		if !Equal(pair[0], pair[1], Default) {
			t.Errorf("Equal(%q, %q) = false; keys %q vs %q", pair[0], pair[1], Key(pair[0], Default), Key(pair[1], Default))
		}
	}
	if Equal("斎藤", "斉藤", Default) {
		t.Errorf("斎 and 斉 are distinct characters in JIS and must not compare equal")
	}
}

// 㐄 U+3404 is listed under 告示582 with two targets, 井 and 牛, that are
// both fixed points. There is no evidence in the data to prefer one, so
// Fold must refuse rather than pick the first, and Key must keep the rune.
func TestDivergentBranchesAreUnresolved(t *testing.T) {
	got, ok := Fold('㐄', Default)
	if ok || got != '㐄' {
		t.Errorf("Fold(㐄) = %c, %v; want 㐄, false", got, ok)
	}
	if Key("㐄田", Default) != "㐄田" {
		t.Errorf("Key must keep an unresolved rune as-is, got %q", Key("㐄田", Default))
	}
	cs := Candidates('㐄')
	if len(cs) != 2 || cs[0].To != '井' || cs[1].To != '牛' {
		t.Errorf("Candidates(㐄) = %+v", cs)
	}
}

// Numbers quoted in README.md. A data update is expected to break this test;
// re-measure and update both together.
func TestReadmeClaims(t *testing.T) {
	once.Do(load)
	pairs, resolved, ambiguous, unicodeOnly, fixedPoints := 0, 0, 0, 0, map[rune]bool{}
	for from, cs := range table {
		pairs += len(cs)
		for _, c := range cs {
			if c.Basis == Unicode {
				unicodeOnly++
			}
		}
		if f, ok := Fold(from, Default); ok {
			resolved++
			fixedPoints[f] = true
		} else {
			ambiguous++
		}
	}
	// "NFKC changes zero of the 21,599 shrink-map source characters": every
	// source with a non-Unicode link must be untouched by NFKC.
	shrinkSrcs, nfkcTouched, ambBMP := 0, 0, 0
	for from, cs := range table {
		fromMap := false
		for _, c := range cs {
			if c.Basis&^Unicode != 0 {
				fromMap = true
			}
		}
		if fromMap {
			shrinkSrcs++
			if norm.NFKC.String(string(from)) != string(from) {
				nfkcTouched++
			}
		}
		if _, ok := Fold(from, Default); !ok && from <= 0xFFFF {
			ambBMP++
		}
	}
	got := []int{len(table), pairs, resolved, ambiguous, unicodeOnly, len(fixedPoints), shrinkSrcs, nfkcTouched, ambBMP}
	want := []int{22601, 28515, 19523, 3078, 1002, 6863, 21599, 0, 2107}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("claim %d: got %d, README says %d", i, got[i], w)
		}
	}
}

func TestKeyDropsVariationSelectors(t *testing.T) {
	if Key("葛\U000E0100城", Default) != Key("葛城", Default) {
		t.Errorf("IVS not dropped: %q", Key("葛\U000E0100城", Default))
	}
	if Key("芦︀", Default) != Key("芦", Default) {
		t.Errorf("SVS not dropped")
	}
}

// Restricting the basis must make fewer links available, never more.
func TestBasisFilterIsMonotone(t *testing.T) {
	// 﨑 → 崎 is supported only by a family-register notice.
	if got, _ := Fold('﨑', JIS); got != '﨑' {
		t.Errorf("Fold(﨑, JIS) = %c; expected no JIS link", got)
	}
	if got, _ := Fold('﨑', Koseki); got != '崎' {
		t.Errorf("Fold(﨑, Koseki) = %c; want 崎", got)
	}
	once.Do(load)
	for from := range table {
		d, dok := Fold(from, Default)
		a, aok := Fold(from, All)
		if dok && aok && d != a && d != from {
			// Adding Analogy may only redirect a rune that Default left alone
			// or add a further hop; it must not change a resolved target
			// unless it extends that chain.
			if f, _ := Fold(d, All); f != a {
				t.Errorf("%U: Default→%U but All→%U, and All does not extend the Default chain", from, d, a)
			}
		}
	}
}

func TestCandidatesCarryBasis(t *testing.T) {
	cs := Candidates('髙')
	if len(cs) != 1 || cs[0].To != '高' || cs[0].Basis&Koseki == 0 || cs[0].Basis&JIS == 0 {
		t.Fatalf("Candidates(髙) = %+v", cs)
	}
	if cs[0].Basis.String() != "jis|koseki" {
		t.Errorf("String() = %q", cs[0].Basis.String())
	}
	if Candidates('あ') != nil {
		t.Errorf("あ should have no candidates")
	}
}

// Every rune in the table must terminate, and a successful fold must land
// on a rune that folds to itself. Key must be idempotent.
func TestEveryEntryTerminatesAndKeyIsIdempotent(t *testing.T) {
	once.Do(load)
	var resolved, ambiguous, maxHops int
	for from := range table {
		got, ok := Fold(from, Default)
		if !ok {
			ambiguous++
			continue
		}
		resolved++
		if again, ok2 := Fold(got, Default); !ok2 || again != got {
			t.Errorf("%U folds to %U which is not a fixed point (→ %U, %v)", from, got, again, ok2)
		}
		if h := hops(from, Default); h > maxHops {
			maxHops = h
		}
		k := Key(string(from), Default)
		if Key(k, Default) != k {
			t.Errorf("Key not idempotent for %U", from)
		}
	}
	t.Logf("table runes: %d, resolved: %d, ambiguous: %d, longest chain: %d hops", len(table), resolved, ambiguous, maxHops)
	if resolved == 0 || maxHops >= maxDepth {
		t.Fatalf("suspicious stats: resolved=%d maxHops=%d", resolved, maxHops)
	}
}

// hops counts the longest single path from r along b-supported links. A rune
// that lies on a cycle terminates at its representative, exactly as walk does,
// so this does not loop on the shipped table's six components.
func hops(r rune, b Basis) int {
	if _, ok := representatives(b)[r]; ok {
		return 0
	}
	best := 0
	for _, c := range table[r] {
		if c.Basis&b == 0 {
			continue
		}
		if h := 1 + hops(c.To, b); h > best {
			best = h
		}
	}
	return best
}

// A cycle the shipped table does not contain must still resolve, to the same
// rune from either end. Both links here carry the same basis, so the election
// falls through to the lowest code point.
func TestSyntheticCycleResolvesToLowestCodePoint(t *testing.T) {
	once.Do(load)
	const a, b = 0x10FFF0, 0x10FFF1 // private-use, never in the real table
	table[a] = []Candidate{{b, JIS}}
	table[b] = []Candidate{{a, JIS}}
	// representatives caches per basis, so the synthetic edges have to be in
	// place before the election runs for Default.
	clearRepCache()
	defer func() { delete(table, a); delete(table, b); clearRepCache() }()
	for _, from := range []rune{a, b} {
		if got, ok := Fold(from, Default); !ok || got != a {
			t.Errorf("Fold(%U) on a cycle = %U, %v; want %U, true", from, got, ok, rune(a))
		}
	}
}

// A cycle whose links carry different evidence must be decided by the
// evidence, not by the code point: here the Koseki link points at the higher
// code point and must still win over the Dictionary link back.
func TestSyntheticCycleElectsStrongestEvidence(t *testing.T) {
	once.Do(load)
	const a, b = 0x10FFF2, 0x10FFF3 // private-use, never in the real table
	table[a] = []Candidate{{b, Koseki}}
	table[b] = []Candidate{{a, Dictionary}}
	clearRepCache()
	defer func() { delete(table, a); delete(table, b); clearRepCache() }()
	for _, from := range []rune{a, b} {
		if got, ok := Fold(from, Default); !ok || got != b {
			t.Errorf("Fold(%U) = %U, %v; want %U, true (Koseki beats Dictionary)", from, got, ok, rune(b))
		}
	}
}

func clearRepCache() { repCache.Clear() }

// The shipped TSV must be well-formed: three hex/decimal fields, sorted,
// no self-pairs, no duplicate (from,to).
func TestTableIntegrity(t *testing.T) {
	sc := bufio.NewScanner(bytes.NewReader(tableTSV))
	var prev [2]uint64
	n := 0
	for sc.Scan() {
		n++
		f := strings.Split(sc.Text(), "\t")
		if len(f) != 3 {
			t.Fatalf("line %d: %d fields", n, len(f))
		}
		from, err1 := strconv.ParseUint(f[0], 16, 32)
		to, err2 := strconv.ParseUint(f[1], 16, 32)
		b, err3 := strconv.ParseUint(f[2], 10, 8)
		if err1 != nil || err2 != nil || err3 != nil {
			t.Fatalf("line %d: parse error", n)
		}
		if from == to {
			t.Errorf("line %d: self-pair %X", n, from)
		}
		if b == 0 || Basis(b) > All {
			t.Errorf("line %d: basis %d out of range", n, b)
		}
		cur := [2]uint64{from, to}
		if n > 1 && (cur[0] < prev[0] || (cur[0] == prev[0] && cur[1] <= prev[1])) {
			t.Errorf("line %d: not strictly sorted (%X,%X after %X,%X)", n, from, to, prev[0], prev[1])
		}
		prev = cur
	}
	if n < 20000 {
		t.Fatalf("only %d lines; table looks truncated", n)
	}
}

// The shipped table is not a DAG. MJ縮退マップ records links in both
// directions for a few glyph families — 靎/靏 carry a family-register link to
// 鶴 and 鶴 carries dictionary-tier links back — so the fold graph has
// strongly connected components. Folding must still land every member of a
// component on one representative, whichever member the walk starts from.
func TestCyclesResolveDeterministically(t *testing.T) {
	once.Do(load)
	for _, b := range []Basis{Default, All, Koseki} {
		comps := components(b)
		if len(comps) == 0 {
			t.Fatalf("basis %s: no component found; the finder is broken", b)
		}
		if b == Default {
			// Pinned in README.md, like TestReadmeClaims: a data update that
			// adds or removes a cycle is supposed to break this.
			members := 0
			for _, comp := range comps {
				members += len(comp)
			}
			if len(comps) != 6 || members != 13 {
				t.Errorf("shipped table has %d components over %d glyphs; README says 6 / 13", len(comps), members)
			}
		}
		for _, comp := range comps {
			member := map[rune]bool{}
			for _, r := range comp {
				member[r] = true
			}
			want, ok := Fold(comp[0], b)
			if !ok {
				t.Errorf("basis %s: component %s: Fold(%c U+%04X) = %c U+%04X, false; want a representative, true",
					b, string(comp), comp[0], comp[0], want, want)
				continue
			}
			if !member[want] {
				t.Errorf("basis %s: component %s: representative %c U+%04X is outside the component",
					b, string(comp), want, want)
			}
			for _, r := range comp {
				got, ok := Fold(r, b)
				if !ok || got != want {
					t.Errorf("basis %s: component %s: Fold(%c U+%04X) = %c U+%04X, %v; want %c U+%04X, true",
						b, string(comp), r, r, got, got, ok, want, want)
				}
			}
			if again, ok := Fold(want, b); !ok || again != want {
				t.Errorf("basis %s: representative %c U+%04X is not a fixed point (= %c U+%04X, %v)",
					b, want, want, again, again, ok)
			}
		}
	}
}

// 鶴 is the component that matters in a name column. 寉 U+5BC9 has exactly one
// recorded link, a family-register notice pointing at 鶴, and 鶴 U+FA2D is a
// CJK compatibility ideograph whose NFC canonical form is 鶴 U+9DB4.
func TestTsuruFamilyFolds(t *testing.T) {
	for _, c := range []struct{ in, want rune }{
		{'靎', '鶴'}, {'靏', '鶴'}, {'寉', '鶴'}, {'䳽', '鶴'}, {'鶴', '鶴'}, {'鶴', '鶴'},
	} {
		got, ok := Fold(c.in, Default)
		if !ok || got != c.want {
			t.Errorf("Fold(%c U+%04X, Default) = %c U+%04X, %v; want %c U+%04X, true",
				c.in, c.in, got, got, ok, c.want, c.want)
		}
	}
	if !Equal("鶴橋", "鶴橋", Default) {
		t.Errorf("Equal(U+FA2D橋, 鶴橋) = false; keys %U vs %U",
			[]rune(Key("鶴橋", Default)), []rune(Key("鶴橋", Default)))
	}
	// Adding the dictionary tier must not withdraw the family-register fold.
	k, kok := Fold('靎', Koseki)
	d, dok := Fold('靎', Default)
	if k != d || !kok || !dok {
		t.Errorf("Fold(靎, Koseki) = %c, %v but Fold(靎, Default) = %c, %v; Default ⊃ Koseki",
			k, kok, d, dok)
	}
}

// components returns the non-trivial strongly connected components of the
// b-filtered fold graph, each sorted by code point, the whole list sorted by
// first member. Tarjan, written out here so the test does not depend on the
// implementation it is checking.
func components(b Basis) [][]rune {
	var (
		idx     = map[rune]int{}
		low     = map[rune]int{}
		onStack = map[rune]bool{}
		stack   []rune
		next    int
		out     [][]rune
		visit   func(rune)
	)
	visit = func(r rune) {
		idx[r], low[r] = next, next
		next++
		stack = append(stack, r)
		onStack[r] = true
		for _, c := range table[r] {
			if c.Basis&b == 0 {
				continue
			}
			switch {
			case !visited(idx, c.To):
				visit(c.To)
				if low[c.To] < low[r] {
					low[r] = low[c.To]
				}
			case onStack[c.To]:
				if idx[c.To] < low[r] {
					low[r] = idx[c.To]
				}
			}
		}
		if low[r] == idx[r] {
			var comp []rune
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == r {
					break
				}
			}
			if len(comp) > 1 {
				sort.Slice(comp, func(i, j int) bool { return comp[i] < comp[j] })
				out = append(out, comp)
			}
		}
	}
	nodes := map[rune]bool{}
	for from, cs := range table {
		nodes[from] = true
		for _, c := range cs {
			if c.Basis&b != 0 {
				nodes[c.To] = true
			}
		}
	}
	keys := make([]rune, 0, len(nodes))
	for r := range nodes {
		keys = append(keys, r)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, r := range keys {
		if !visited(idx, r) {
			visit(r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func visited(idx map[rune]int, r rune) bool { _, ok := idx[r]; return ok }
