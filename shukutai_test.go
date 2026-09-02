package shukutai

import (
	"bufio"
	"bytes"
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
	want := []int{22601, 28515, 19497, 3104, 1002, 6857, 21599, 0, 2129}
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

// hops counts the longest single path from r along Default links.
func hops(r rune, b Basis) int {
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

// A synthetic cycle must be reported as unresolved, not loop forever.
func TestCycleIsUnresolved(t *testing.T) {
	once.Do(load)
	const a, b = 0x10FFF0, 0x10FFF1 // private-use, never in the real table
	table[a] = []Candidate{{b, JIS}}
	table[b] = []Candidate{{a, JIS}}
	defer func() { delete(table, a); delete(table, b) }()
	if got, ok := Fold(a, Default); ok || got != a {
		t.Errorf("Fold on a cycle = %U, %v; want %U, false", got, ok, a)
	}
}

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
