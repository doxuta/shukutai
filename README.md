# shukutai

[![ci](https://github.com/doxuta/shukutai/actions/workflows/ci.yml/badge.svg)](https://github.com/doxuta/shukutai/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/doxuta/shukutai.svg)](https://pkg.go.dev/github.com/doxuta/shukutai)

Fold Japanese kanji variants (異体字) to one canonical form, so that
`髙橋` and `高橋`, `渡邉` and `渡辺`, `山﨑` and `山崎` compare equal. Pure Go;
the library imports only the standard library (`golang.org/x/text` appears in
`go.mod` for the table generator and the tests). One 368 KB table built from
IPA's **MJ縮退マップ** (MJ Shrink Map) rather than a hand-written list.

縮退 (*shukutai*) is the map's own word for the operation: reducing a glyph
to the nearest character that JIS X 0213 can represent.

## Why

Japanese names carry variant glyphs that Unicode treats as distinct
characters. `String.normalize`, `norm.NFKC`, `ICU` — none of them fold
`髙` to `高`, because they are not compatibility forms of each other; they are
two code points with two identities. Measured over this table's 21,599
shrink-map source characters, **NFKC changes zero of them**. Of the seven
variants you meet most often in a name column — 﨑 髙 邉 邊 德 濵 栁 — NFKC
folds none, and this package folds all seven.

The usual fix is a hand-maintained old→new list. The maintainers of
[`geolonia/normalize-japanese-addresses`](https://github.com/geolonia/normalize-japanese-addresses/issues/199)
have had an open issue since 2023 asking to replace exactly such a list with
something comprehensive. Japan's Character Information Technology Promotion
Council already publishes the comprehensive version: the MJ縮退マップ links
~59,000 glyphs from the family-register and resident-register character sets
to JIS X 0213 characters, and records *why* for each link — a JIS
unification rule, a Ministry of Justice notice, a dictionary, or shape
analogy. This package puts that table behind three functions.

## Install

```
go install github.com/doxuta/shukutai/cmd/shukutai@latest
```

## Try it

```
$ shukutai key 髙橋 渡邉 齋藤 山﨑 塚本
高橋
渡辺
斎藤
山崎
塚本
```

The last line looks unchanged; it is not. The input `塚` is the CJK
Compatibility Ideograph U+FA10 and the output is U+585A.

```
$ shukutai eq 渡邊 渡辺
渡邊 == 渡辺 (渡辺)

$ shukutai eq 斎藤 斉藤
斎藤 != 斉藤 (斎藤 vs 斉藤)      # exit status 1: 斎 and 斉 are different characters
```

`why` shows the evidence:

```
$ shukutai why 邉 髙 高
邉 U+9089 → 辺 U+8FBA
    辺 U+8FBA  [koseki]
    邊 U+908A  [koseki]
髙 U+9AD9 → 高 U+9AD8
    高 U+9AD8  [jis|koseki]
高 U+9AD8: fixed point
```

`邉` has two recorded targets. `邊` itself folds to `辺`, so both branches
end in the same place and the fold is accepted. When branches end in
different places, `shukutai` leaves the character alone rather than guess
(see Limitations).

## Library

```go
import "github.com/doxuta/shukutai"

shukutai.Key("髙橋", shukutai.Default)          // "高橋"
shukutai.Equal("渡邉", "渡辺", shukutai.Default) // true
shukutai.Fold('﨑', shukutai.Default)          // '崎', true
shukutai.Fold('㐄', shukutai.Default)          // '㐄', false — 井 or 牛, no evidence to choose
shukutai.Candidates('邉')                       // [{辺 koseki} {邊 koseki}]
```

`Basis` is a bit set naming the evidence categories you are willing to
follow. `Default` is every category except `Analogy` (読み・字形による類推,
the map's weakest tier, 413 glyphs). Pass `shukutai.Koseki|shukutai.JIS` to
accept only family-register notices and JIS unification, or `All` to include
analogy.

`Key` drops variation selectors (U+FE00–FE0F, U+E0100–E01EF) and does nothing
else to the input. Run `norm.NFKC` first if you also want width and kana
differences to collapse.

## What is in the table

Built by `internal/gen` from two files, both CC BY-SA 2.1 JP:

- [MJ縮退マップ Ver.1.2.0](https://moji.or.jp/mojikiban/map/) (2018-01-26) — the links, keyed by MJ glyph name
- [MJ文字情報一覧表 Ver.006.02](https://moji.or.jp/mojikiban/mjlist/) (2024-01) — glyph name → code point

```
go run ./internal/gen -shrink MJShrinkMap.1.2.0.json -mji mji.00602.xlsx -o table.tsv
```

| | |
|---|---|
| glyphs in the shrink map | 58,862 |
| glyphs with at least one link | 35,851 |
| of those, skipped because they have no plain code point (IVS-only) | 5,456 |
| self-links dropped (glyph already representable) | 10,235 |
| **pairs in `table.tsv`** | **28,515** |
| distinct source code points | 22,601 |
| fold to a unique fixed point under `Default` | 19,523 (86.4%) |
| left unresolved (branches disagree) | 3,078 (13.6%) |
| longest chain | 4 hops |
| cycles (strongly connected components / glyphs) | 6 / 13 |
| distinct fixed points reached | 6,863 |
| extra pairs from Unicode (compatibility ideograph → canonical) | 1,002 |

Each line of `table.tsv` is `from<TAB>to<TAB>basis`, hex code points and a
decimal bit set. `grep '^9AD9' table.tsv` tells you what the map says about
`髙`. Every number above is pinned by a test — `TestReadmeClaims` for all of
them bar the cycle row, which `TestCyclesResolveDeterministically` pins; a
data update is supposed to break them.

## Limitations

- **No ranking.** 告示582号 carries an explicit 第1順位/第2順位 and the
  family-register notices carry a hop count; both could break ties. This
  package does not use them, so 3,078 source characters stay unresolved
  (`㐄` → 井 or 牛): 2,107 in the BMP and 971 outside it. Using the rank is
  the obvious upgrade; IPA's own guidance is that context should decide,
  which is why it is not done blindly here. The same missing rank is what
  forces the arbitrary half of the cycle tie-break below.
- **The map is not a DAG, and one cycle is broken arbitrarily.** The links go
  both ways for six glyph families (13 glyphs, 7 elementary cycles), so the
  fold has to elect a canonical form. It takes the glyph the strongest
  evidence inside the cycle points at — that settles 靎/靏/鶴 on `鶴`, because
  the links to `鶴` are family-register notices and the links back are
  dictionary-tier — and breaks a remaining tie on the lowest code point. The
  other five are symmetric family-register pairs (`址`↔`阯`, `雕`↔`鵰`,
  `輀`↔`轜`, `羐`↔`羑`, `㿉`↔`㿗`) where that second rule decides and the
  evidence does not. Nothing outside a cycle is affected, and no link out of
  a cycle member is followed, so `鶴` → `靍` U+974D is dropped and `靍` stays
  its own key. `TestCyclesResolveDeterministically` pins the whole set.
- **Variation sequences are stripped, not distinguished.** `葛` + U+E0100
  and `葛` + U+E0101 are different MJ glyphs with possibly different links;
  this package treats both as plain `葛`. The 5,456 glyphs that exist only
  as an IVS are not in the table at all. For a matching key this is usually
  what you want; for glyph-accurate work it is not.
- **Targets are JIS X 0213, not 新字体.** The map answers "what can this be
  written as in JIS", so pairs where both sides are already in JIS with no
  recorded link — 啞/唖, 鷗/鴎 — are not folded. This is not an
  old-orthography → new-orthography converter.
- **The map is from 2018** (Ver.1.2.0, against MJ一覧表 Ver.005.02); the code
  point table is from 2024 (Ver.006.02). Glyphs added between those versions
  have no links.
- **Default excludes `Analogy`.** Measured, it changes nothing: the number of
  unresolved characters is 3,078 under `Default` and under `All`. It is kept
  as an explicit opt-in because the map itself files it as the weakest tier.

## Prior art

- [tomatomerde/itaiji-normalize](https://github.com/tomatomerde/itaiji-normalize)
  (TypeScript, 2026) — builds from the same two files and goes further:
  IVS-aware, rank- and hop-based tie-breaking, per-hop Unicode normalisation.
  Its `NOTES.md` documents the pitfalls of this data (keying on 対応するUCS
  instead of 実装したUCS creates 歯↔齒 cycles; ruby runs inside xlsx shared
  strings; compatibility-ideograph targets breaking idempotence). This
  package was designed after reading it and deliberately does less.
- [camelmasa/itaiji](https://github.com/camelmasa/itaiji) (Ruby) and its
  ports — the hand-written ~1,400-pair list most projects use.
- [mandel59/mj2jisx0213](https://github.com/mandel59/mj2jisx0213) — an
  earlier SQL-based reduction over the same map.
- [yassun/mojikiban-go](https://github.com/yassun/mojikiban-go) — a Go client
  for the former `mojikiban.ipa.go.jp` search API, not a table.

## Licence

Code: MIT (`LICENSE`). `table.tsv`: CC BY-SA 2.1 JP, derived from IPA's
MJ縮退マップ and MJ文字情報一覧表 (`LICENSE-DATA`). Attribute IPA if you
redistribute the table.

Built with an AI-agent workflow (Claude Code) with the author reviewing
design, data provenance and every test; the numbers above were measured, not
quoted.
