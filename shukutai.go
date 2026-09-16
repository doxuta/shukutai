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
	"sort"
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
// hops. Cycles are resolved before the walk (see representatives), so this
// bound is left only as a guard against a future table whose chains run far
// deeper than this data ever has.
const maxDepth = 16

// Fold follows shrink links supported by any basis in b until it reaches a
// rune with no further link. When every branch ends at the same rune, that
// rune is returned with ok=true. A rune inside a cycle folds to that cycle's
// elected representative (see representatives). When branches disagree or the
// chain is too deep, r itself is returned with ok=false.
func Fold(r rune, b Basis) (result rune, ok bool) {
	once.Do(load)
	final, ok := walk(r, b, representatives(b), 0)
	if !ok {
		return r, false
	}
	return final, true
}

// walk returns the unique fixed point reachable from r, or ok=false.
func walk(r rune, b Basis, rep map[rune]rune, depth int) (rune, bool) {
	if x, ok := rep[r]; ok {
		return x, true // r is in a cycle; the cycle has one canonical form
	}
	if depth > maxDepth {
		return 0, false
	}
	var final rune
	found := false
	for _, c := range table[r] {
		if c.Basis&b == 0 {
			continue
		}
		f, ok := walk(c.To, b, rep, depth+1)
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

// basisRank orders the evidence categories strongest first. The five
// shrink-map categories are in the order MJ縮退マップ itself lists them
// (JIS包摂規準, 戸籍法関連通達, 告示582号, 辞書類等, 読み・字形による類推).
// Unicode is placed first as a documented convention — a canonical
// decomposition is normative rather than editorial — but no Unicode link lies
// inside a cycle of the shipped table, so nothing here measures that choice.
var basisRank = [...]Basis{Unicode, JIS, Koseki, Notice582, Dictionary, Analogy}

// rank returns the position of the strongest category set in b, 0 being
// strongest. Callers pass an already basis-filtered value.
func rank(b Basis) int {
	for i, bit := range basisRank {
		if b&bit != 0 {
			return i
		}
	}
	return len(basisRank)
}

// repCache holds the elected representatives for each Basis seen so far.
// Fold consults it once per rune, so Key over a name column hits it once per
// character: a package-global lock here serialises the whole library, and
// measurably did — with a mutex the parallel benchmark ran 3.4x slower than
// the same work on a single goroutine. sync.Map's read path takes no lock.
// Two goroutines racing to fill the same basis both compute from the
// immutable table and elect the same representatives, so whichever entry
// LoadOrStore keeps is the same map.
var repCache sync.Map // Basis -> map[rune]rune

// representatives maps every rune that lies on a cycle of the b-filtered fold
// graph to the canonical form elected for that cycle. Result is cached per
// basis; the map is read-only once returned.
//
// MJ縮退マップ records some links in both directions — 靎 and 靏 carry a
// family-register link to 鶴 while 鶴 carries dictionary-tier links back to
// them — so the fold graph is not a DAG. Under Default the shipped table has
// six strongly connected components spanning 13 glyphs and 7 elementary
// cycles. A component is a set of glyphs the map declares mutually reducible,
// so one of them has to be elected:
//
//  1. Strongest evidence wins. Take the strongest basis carried by any link
//     between two members; the candidates are the glyphs those links point at.
//     In 靎/靏/鶴 the strongest such tier is Koseki and both Koseki links point
//     at 鶴, so 鶴 is elected and the dictionary-tier links back are never
//     followed.
//  2. A remaining tie goes to the lowest code point. The other five components
//     are symmetric Koseki pairs (址↔阯 and four more) for which the map
//     records no ranking at all; see "No ranking" in README.md. That choice is
//     arbitrary, and is pinned to the code point only so it is stable across
//     builds and platforms rather than dependent on map iteration order.
//
// Every member of a component folds to the representative and the
// representative folds to itself: walk follows no link out of any member, not
// even one leaving the component, because the election has already fixed the
// class's canonical form. 鶴 → 靍 U+974D, the only such link in the shipped
// table, is dropped this way, so 靍 stays a separate key.
//
// The election runs on the b-filtered graph, so restricting the basis can
// never import a representative elected from links the caller excluded.
func representatives(b Basis) map[rune]rune {
	if m, ok := repCache.Load(b); ok {
		return m.(map[rune]rune)
	}
	m, _ := repCache.LoadOrStore(b, elect(b))
	return m.(map[rune]rune)
}

// elect runs the election described on representatives, once per Basis.
func elect(b Basis) map[rune]rune {
	m := map[rune]rune{}
	for _, comp := range cycles(b) {
		member := make(map[rune]bool, len(comp))
		for _, r := range comp {
			member[r] = true
		}
		best := len(basisRank)
		for _, r := range comp {
			for _, c := range table[r] {
				if member[c.To] && c.Basis&b != 0 {
					if k := rank(c.Basis & b); k < best {
						best = k
					}
				}
			}
		}
		rep, found := rune(0), false
		for _, r := range comp {
			for _, c := range table[r] {
				if member[c.To] && c.Basis&b != 0 && rank(c.Basis&b) == best {
					if !found || c.To < rep {
						rep, found = c.To, true
					}
				}
			}
		}
		if !found {
			// Unreachable, and asserted rather than tolerated because the
			// silent form of this bug is every member of comp folding to
			// U+0000. Any path between two members of a strongly connected
			// component stays inside it, so a component cycles(b) returned
			// with more than one member has at least one internal link, that
			// link passed the same c.Basis&b != 0 filter during the search,
			// and rank of a non-zero masked basis is always < len(basisRank).
			panic("shukutai: cycle " + string(comp) + " has no internal link; cycles is broken")
		}
		for _, r := range comp {
			m[r] = rep
		}
	}
	return m
}

// cycles returns the strongly connected components of the b-filtered fold
// graph that have more than one member, by Tarjan's algorithm. Recursion depth
// is bounded by the longest simple path, which is 4 hops in the shipped table.
func cycles(b Basis) [][]rune {
	var (
		index   = map[rune]int{}
		lowlink = map[rune]int{}
		onStack = map[rune]bool{}
		stack   []rune
		next    int
		out     [][]rune
		visit   func(rune)
	)
	visit = func(r rune) {
		index[r], lowlink[r] = next, next
		next++
		stack = append(stack, r)
		onStack[r] = true
		for _, c := range table[r] {
			if c.Basis&b == 0 {
				continue
			}
			if _, seen := index[c.To]; !seen {
				visit(c.To)
				if lowlink[c.To] < lowlink[r] {
					lowlink[r] = lowlink[c.To]
				}
			} else if onStack[c.To] {
				if index[c.To] < lowlink[r] {
					lowlink[r] = index[c.To]
				}
			}
		}
		if lowlink[r] != index[r] {
			return
		}
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
	// Seed in code point order so the walk, and therefore the component list,
	// does not depend on map iteration order.
	roots := make([]rune, 0, len(table))
	for r := range table {
		roots = append(roots, r)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
	for _, r := range roots {
		if _, seen := index[r]; !seen {
			visit(r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
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
