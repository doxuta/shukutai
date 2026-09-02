// Command gen builds table.tsv from the two IPA source files.
//
//	go run ./internal/gen -shrink MJShrinkMap.1.2.0.json -mji mji.00602.xlsx -o table.tsv
//
// The shrink map is keyed by MJ glyph name (MJ000001...), not by code point,
// so the glyph name has to be resolved through the MJ文字情報一覧表 first.
// Only glyphs that have an 実装したUCS (a plain code point of their own) are
// kept; glyphs reachable only through an IVS are skipped and counted.
package main

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/doxuta/shukutai"
)

// Shrink-map category name -> basis bit. Order is the public Basis order.
var categories = []struct {
	key   string
	basis shukutai.Basis
}{
	{"JIS包摂規準・UCS統合規則", shukutai.JIS},
	{"法務省戸籍法関連通達・通知", shukutai.Koseki},
	{"法務省告示582号別表第四", shukutai.Notice582},
	{"辞書類等による関連字", shukutai.Dictionary},
	{"読み・字形による類推", shukutai.Analogy},
}

type entry struct {
	Name string `json:"MJ文字図形名"`
	// Everything else is decoded lazily per category below.
}

func main() {
	shrink := flag.String("shrink", "", "path to MJShrinkMap JSON")
	mji := flag.String("mji", "", "path to MJ文字情報一覧表 xlsx")
	out := flag.String("o", "table.tsv", "output path")
	flag.Parse()
	if *shrink == "" || *mji == "" {
		flag.Usage()
		os.Exit(2)
	}

	ucsByName, err := readMJI(*mji)
	if err != nil {
		log.Fatalf("mji: %v", err)
	}
	log.Printf("mji: %d glyphs with 実装したUCS", len(ucsByName))

	pairs, stats, err := readShrink(*shrink, ucsByName)
	if err != nil {
		log.Fatalf("shrink: %v", err)
	}
	log.Printf("shrink: %d entries, %d with candidates, %d skipped (no 実装したUCS), %d self-pairs dropped",
		stats.entries, stats.withCandidates, stats.noUCS, stats.self)

	addCompat(pairs)

	if err := write(*out, pairs); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d pairs to %s", len(pairs), *out)
}

type pairKey struct{ from, to rune }

type stats struct{ entries, withCandidates, noUCS, self int }

// readShrink returns basis bits per (from,to) pair, unioned across every MJ
// glyph that shares the same 実装したUCS. Both ends are NFC-normalised so a
// target that is a decomposable CJK compatibility ideograph (e.g. U+FA10)
// lands on its canonical code point.
func readShrink(path string, ucsByName map[string]rune) (map[pairKey]shukutai.Basis, stats, error) {
	var st stats
	f, err := os.Open(path)
	if err != nil {
		return nil, st, err
	}
	defer f.Close()

	var doc struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.NewDecoder(f).Decode(&doc); err != nil {
		return nil, st, err
	}
	pairs := map[pairKey]shukutai.Basis{}
	for _, raw := range doc.Content {
		st.entries++
		var e entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, st, err
		}
		var cats map[string]json.RawMessage
		if err := json.Unmarshal(raw, &cats); err != nil {
			return nil, st, err
		}
		from, ok := ucsByName[e.Name]
		hasCandidate := false
		for _, c := range categories {
			list, present := cats[c.key]
			if !present {
				continue
			}
			var items []struct {
				UCS string `json:"UCS"`
			}
			if err := json.Unmarshal(list, &items); err != nil {
				return nil, st, fmt.Errorf("%s %s: %w", e.Name, c.key, err)
			}
			for _, it := range items {
				hasCandidate = true
				if !ok {
					continue
				}
				to, err := parseUCS(it.UCS)
				if err != nil {
					return nil, st, fmt.Errorf("%s: %w", e.Name, err)
				}
				from, to = nfc(from), nfc(to)
				if from == to {
					st.self++
					continue
				}
				pairs[pairKey{from, to}] |= c.basis
			}
		}
		if hasCandidate {
			st.withCandidates++
			if !ok {
				st.noUCS++
			}
		}
	}
	return pairs, st, nil
}

// addCompat adds one pair per CJK compatibility ideograph that NFC maps to a
// single canonical code point, so callers do not need to normalise input.
func addCompat(pairs map[pairKey]shukutai.Basis) {
	for _, rng := range [][2]rune{{0xF900, 0xFAFF}, {0x2F800, 0x2FA1F}} {
		for r := rng[0]; r <= rng[1]; r++ {
			if c := nfc(r); c != r {
				pairs[pairKey{r, c}] |= shukutai.Unicode
			}
		}
	}
}

// nfc returns the NFC form of r when that form is a single rune, else r.
func nfc(r rune) rune {
	s := norm.NFC.String(string(r))
	if rs := []rune(s); len(rs) == 1 {
		return rs[0]
	}
	return r
}

func parseUCS(s string) (rune, error) {
	if !strings.HasPrefix(s, "U+") {
		return 0, fmt.Errorf("bad UCS %q", s)
	}
	v, err := strconv.ParseUint(s[2:], 16, 32)
	return rune(v), err
}

func write(path string, pairs map[pairKey]shukutai.Basis) error {
	keys := make([]pairKey, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].from != keys[j].from {
			return keys[i].from < keys[j].from
		}
		return keys[i].to < keys[j].to
	})
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, k := range keys {
		fmt.Fprintf(w, "%X\t%X\t%d\n", k.from, k.to, pairs[k])
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return f.Close()
}

// ---- xlsx ----------------------------------------------------------------

const ssNS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"

// readMJI streams the first worksheet and returns MJ文字図形名 -> 実装したUCS.
func readMJI(path string) (map[string]rune, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	open := func(name string) (io.ReadCloser, error) {
		for _, f := range z.File {
			if f.Name == name {
				return f.Open()
			}
		}
		return nil, fmt.Errorf("%s: not in archive", name)
	}
	ss, err := open("xl/sharedStrings.xml")
	if err != nil {
		return nil, err
	}
	strs, err := sharedStrings(ss)
	ss.Close()
	if err != nil {
		return nil, err
	}
	sh, err := open("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	defer sh.Close()
	return sheetToUCS(sh, strs)
}

// sharedStrings collects every <si> as one string. Ruby (<rPh>) runs are
// furigana Excel stores next to the value and are not part of it.
func sharedStrings(r io.Reader) ([]string, error) {
	dec := xml.NewDecoder(r)
	var out []string
	var cur strings.Builder
	depth, inSI, inRPh := 0, false, 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "si":
				inSI, inRPh = true, 0
				cur.Reset()
			case "rPh":
				inRPh++
			}
		case xml.EndElement:
			depth--
			switch t.Name.Local {
			case "si":
				out = append(out, cur.String())
				inSI = false
			case "rPh":
				inRPh--
			}
		case xml.CharData:
			if inSI && inRPh == 0 {
				cur.Write(t)
			}
		}
	}
}

// sheetToUCS walks rows; the first row is the header and names the columns.
func sheetToUCS(r io.Reader, strs []string) (map[string]rune, error) {
	dec := xml.NewDecoder(r)
	out := map[string]rune{}
	var (
		header   map[string]int // column name -> index
		row      = map[int]string{}
		colIdx   int
		cellType string
		inV, inT bool
		val      strings.Builder
		nameCol  = -1
		ucsCol   = -1
	)
	flush := func() error {
		if header == nil {
			header = map[string]int{}
			for i, v := range row {
				header[v] = i
			}
			var ok1, ok2 bool
			nameCol, ok1 = header["MJ文字図形名"]
			ucsCol, ok2 = header["実装したUCS"]
			if !ok1 || !ok2 {
				return fmt.Errorf("header missing MJ文字図形名/実装したUCS: %v", row)
			}
			return nil
		}
		name, ucs := row[nameCol], row[ucsCol]
		if name == "" || ucs == "" {
			return nil
		}
		r, err := parseUCS(ucs)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		out[name] = r
		return nil
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row = map[int]string{}
			case "c":
				cellType = ""
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						colIdx = colIndex(a.Value)
					case "t":
						cellType = a.Value
					}
				}
				val.Reset()
			case "v":
				inV = true
			case "t":
				inT = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "row":
				if err := flush(); err != nil {
					return nil, err
				}
			case "c":
				s := val.String()
				if cellType == "s" {
					i, err := strconv.Atoi(s)
					if err != nil || i < 0 || i >= len(strs) {
						return nil, fmt.Errorf("bad shared string index %q", s)
					}
					s = strs[i]
				}
				row[colIdx] = s
			case "v":
				inV = false
			case "t":
				inT = false
			}
		case xml.CharData:
			if inV || (inT && cellType == "inlineStr") {
				val.Write(t)
			}
		}
	}
}

// colIndex turns a cell reference like "BC12" into a 0-based column index.
func colIndex(ref string) int {
	n := 0
	for _, ch := range ref {
		if ch < 'A' || ch > 'Z' {
			break
		}
		n = n*26 + int(ch-'A'+1)
	}
	return n - 1
}
