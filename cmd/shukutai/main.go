// Command shukutai folds kanji variants from the command line.
//
//	shukutai key 髙橋 渡邉 齋藤      # one folded key per argument
//	shukutai eq 渡邊 渡辺            # exit 0 if equal, 1 if not
//	shukutai why 邉                  # show the candidates and the chain
//
// With no arguments after the verb, lines are read from stdin.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/doxuta/shukutai"
)

func main() {
	analogy := flag.Bool("analogy", false, "also follow 読み・字形による類推 links")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: shukutai [-analogy] key|eq|why [text...]")
		flag.PrintDefaults()
	}
	flag.Parse()
	basis := shukutai.Default
	if *analogy {
		basis = shukutai.All
	}
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	verb, rest := args[0], args[1:]
	if len(rest) == 0 && verb != "eq" {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			rest = append(rest, sc.Text())
		}
	}
	switch verb {
	case "key":
		for _, s := range rest {
			fmt.Println(shukutai.Key(s, basis))
		}
	case "eq":
		if len(rest) != 2 {
			fmt.Fprintln(os.Stderr, "eq needs exactly two arguments")
			os.Exit(2)
		}
		if !shukutai.Equal(rest[0], rest[1], basis) {
			fmt.Printf("%s != %s (%s vs %s)\n", rest[0], rest[1], shukutai.Key(rest[0], basis), shukutai.Key(rest[1], basis))
			os.Exit(1)
		}
		fmt.Printf("%s == %s (%s)\n", rest[0], rest[1], shukutai.Key(rest[0], basis))
	case "why":
		for _, s := range rest {
			for _, r := range s {
				why(r, basis)
			}
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func why(r rune, b shukutai.Basis) {
	final, ok := shukutai.Fold(r, b)
	switch {
	case !ok:
		fmt.Printf("%c U+%04X: unresolved (branches disagree or cycle)\n", r, r)
	case final == r:
		fmt.Printf("%c U+%04X: fixed point\n", r, r)
	default:
		fmt.Printf("%c U+%04X → %c U+%04X\n", r, r, final, final)
	}
	for _, c := range shukutai.Candidates(r) {
		mark := " "
		if c.Basis&b == 0 {
			mark = "-" // present in the data but excluded by the chosen basis
		}
		fmt.Printf("  %s %c U+%04X  [%s]\n", mark, c.To, c.To, c.Basis)
	}
}
