package main

import (
	"strings"
	"testing"
)

func TestSharedStringsSkipsRuby(t *testing.T) {
	const xmlDoc = `<?xml version="1.0"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>MJ000001</t></si>
  <si><r><t>実装</t></r><r><t>なし</t></r><rPh sb="0" eb="4"><t>ジッソウ</t></rPh></si>
  <si><t>U+3005</t></si>
</sst>`
	got, err := sharedStrings(strings.NewReader(xmlDoc))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"MJ000001", "実装なし", "U+3005"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSheetToUCSUsesHeaderNames(t *testing.T) {
	strs := []string{"MJ文字図形名", "実装したUCS", "対応するUCS", "MJ000004", "MJ000005", "U+3400"}
	// Column order deliberately differs from the real file: 実装したUCS is
	// column C here and 対応するUCS column B, to prove lookup is by name.
	const sheet = `<?xml version="1.0"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
  <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>2</v></c><c r="C1" t="s"><v>1</v></c></row>
  <row r="2"><c r="A2" t="s"><v>3</v></c><c r="B2" t="inlineStr"><is><t>U+9999</t></is></c><c r="C2" t="s"><v>5</v></c></row>
  <row r="3"><c r="A3" t="s"><v>4</v></c><c r="B3" t="inlineStr"><is><t>U+3401</t></is></c></row>
</sheetData></worksheet>`
	got, err := sheetToUCS(strings.NewReader(sheet), strs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["MJ000004"] != 0x3400 {
		t.Errorf("got %v; want MJ000004→U+3400 only (MJ000005 has no 実装したUCS)", got)
	}
}

func TestSheetToUCSRejectsMissingHeader(t *testing.T) {
	const sheet = `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
  <row r="1"><c r="A1" t="inlineStr"><is><t>foo</t></is></c></row></sheetData></worksheet>`
	if _, err := sheetToUCS(strings.NewReader(sheet), nil); err == nil {
		t.Error("expected error for missing header columns")
	}
}

func TestColIndex(t *testing.T) {
	for ref, want := range map[string]int{"A1": 0, "Z9": 25, "AA1": 26, "BC12": 54} {
		if got := colIndex(ref); got != want {
			t.Errorf("colIndex(%s) = %d, want %d", ref, got, want)
		}
	}
}

func TestNFCFoldsDecomposableCompatIdeographsOnly(t *testing.T) {
	if nfc(0xFA10) != 0x585A { // 塚
		t.Errorf("U+FA10 should NFC to U+585A")
	}
	if nfc(0xFA11) != 0xFA11 { // 﨑 has no decomposition
		t.Errorf("U+FA11 must be left alone")
	}
	if nfc('高') != '高' {
		t.Errorf("ordinary kanji must be unchanged")
	}
}
