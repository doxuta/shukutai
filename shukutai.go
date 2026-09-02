// Package shukutai folds Japanese kanji variants (異体字) to a canonical
// JIS X 0213 form using IPA's MJ縮退マップ, so that 髙橋 and 高橋 compare equal.
//
// The table maps one code point to its shrink candidates together with the
// evidence category (Basis) the map records for each link. Fold follows those
// links to a fixed point and refuses to guess when the evidence points two
// ways.
package shukutai

import (
	"bufio"
	"bytes"
	_ "embed"
	"strconv"
	"strings"
	"sync"
)

// Basis is the kind of evidence MJ縮退マップ gives for a link. It is a bit set.
type Basis uint8

const (
	// JIS: JIS X 0213 包摂規準 / UCS unification rules (glyph-shape unification).
	JIS Basis = 1 << iota
	// Koseki: 法務省戸籍法関連通達・通知 (family-register notices).
	Koseki
	// Notice582: 法務省告示582号別表第四 (residence-card name notation).
	Notice582
	// Dictionary: 辞書類等による関連字 (大漢和辞典 and four other dictionaries).
	Dictionary
	// Analogy: 読み・字形による類推 (reading/shape analogy) — the weakest category.
	Analogy
	// Unicode: NFC canonical decomposition of a CJK compatibility ideograph.
	// Not from the shrink map; derived from Unicode so callers need not normalise.
	Unicode

	// Default is every basis except Analogy.
	Default = JIS | Koseki | Notice582 | Dictionary | Unicode
	// All includes Analogy.
	All = Default | Analogy
)

var basisNames = []string{"jis", "koseki", "notice582", "dictionary", "analogy", "unicode"}

// String lists the set bits, e.g. "koseki|dictionary".
func (b Basis) String() string {
	var parts []string
	for i, n := range basisNames {
		if b&(1<<i) != 0 {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, "|")
}

// Candidate is one shrink target for a rune.
type Candidate struct {
	To    rune
	Basis Basis
}

//go:embed table.tsv
var tableTSV []byte

var (
	once  sync.Once
	table map[rune][]Candidate
)

func load() {
	table = map[rune][]Candidate{}
	sc := bufio.NewScanner(bytes.NewReader(tableTSV))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		from, _ := strconv.ParseUint(f[0], 16, 32)
		to, _ := strconv.ParseUint(f[1], 16, 32)
		b, _ := strconv.ParseUint(f[2], 10, 8)
		table[rune(from)] = append(table[rune(from)], Candidate{rune(to), Basis(b)})
	}
}

// Candidates returns the one-hop shrink targets recorded for r, in code point
// order, with every basis that supports each link. Nil when r has none.
func Candidates(r rune) []Candidate {
	once.Do(load)
	return table[r]
}

// maxDepth bounds a fold chain. The deepest chain in the shipped table is 4
// hops; a cycle or a much deeper chain means the data changed, and we would
// rather report "unresolved" than loop. ponytail: the depth bound is the only
// cycle guard — there is no visited set, because a bounded walk terminates
// regardless and the table has no cycles today.
const maxDepth = 16

// Fold follows shrink links supported by any basis in b until it reaches a
// rune with no further link. When every branch ends at the same rune, that
// rune is returned with ok=true. When branches disagree, a cycle is met, or
// the chain is too deep, r itself is returned with ok=false.
func Fold(r rune, b Basis) (result rune, ok bool) {
	once.Do(load)
	final, ok := walk(r, b, 0)
	if !ok {
		return r, false
	}
	return final, true
}

// walk returns the unique fixed point reachable from r, or ok=false.
func walk(r rune, b Basis, depth int) (rune, bool) {
	if depth > maxDepth {
		return 0, false
	}
	var final rune
	found := false
	for _, c := range table[r] {
		if c.Basis&b == 0 {
			continue
		}
		f, ok := walk(c.To, b, depth+1)
		if !ok {
			return 0, false
		}
		if found && f != final {
			return 0, false // branches disagree
		}
		final, found = f, true
	}
	if !found {
		return r, true // fixed point
	}
	return final, true
}

// Key folds every rune of s with Fold and drops variation selectors
// (U+FE00–FE0F, U+E0100–E01EF), so two spellings of the same name produce
// the same string. Runes that cannot be folded unambiguously are kept as-is.
// Input is not otherwise normalised; apply NFKC first if width or kana
// differences should also collapse.
func Key(s string, b Basis) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if isVS(r) {
			continue
		}
		f, _ := Fold(r, b)
		sb.WriteRune(f)
	}
	return sb.String()
}

// Equal reports whether a and b have the same Key.
func Equal(a, b string, basis Basis) bool {
	return Key(a, basis) == Key(b, basis)
}

func isVS(r rune) bool {
	return (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF)
}
