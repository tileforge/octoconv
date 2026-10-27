package convert

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func referenceLooksLikeHTMLFromPrefix(prefix []byte) bool {
	buf := bytes.TrimPrefix(prefix, []byte{0xEF, 0xBB, 0xBF})
	buf = bytes.TrimLeft(buf, " \t\r\n\f\v")

	lower := bytes.ToLower(buf)
	if bytes.HasPrefix(lower, []byte("<!doctype html")) {
		return true
	}
	if bytes.HasPrefix(lower, []byte("<html")) {
		rest := lower[len("<html"):]
		if len(rest) == 0 {
			return false
		}
		switch rest[0] {
		case '>', '/', ' ', '\t', '\r', '\n':
			return true
		}
	}
	return false
}

func htmlLiteralsIn(t *testing.T, glob string) [][]byte {
	t.Helper()
	files, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no file matches %s", glob)
	}
	var out [][]byte
	for _, f := range files {
		node, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		ast.Inspect(node, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			l := strings.ToLower(s)
			if strings.Contains(l, "<html") || strings.Contains(l, "<!doctype") {
				out = append(out, []byte(s))
			}
			return true
		})
	}
	return out
}

func htmlReferenceCorpus(t *testing.T) [][]byte {
	t.Helper()
	var corpus [][]byte
	corpus = append(corpus, htmlLiteralsIn(t, "*_test.go")...)
	corpus = append(corpus, htmlLiteralsIn(t, "../api/*_test.go")...)
	files, err := filepath.Glob("../e2e/testdata/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		corpus = append(corpus, raw)
	}
	markers := []string{"<!doctype html", "<html"}
	cases := []string{"", "<", "<!", "<h", "<ht", "<htm", "<!doctype", "<!doctype ", "<!doctype htm", "<!doctype html>", "<html>", "<html", "<HTML", "<HtMl", "<!DOCTYPE HTML", "<!DocType Html>", "<!doctype  html>", "<! doctype html>", "<html >", "<html/", "<htmlx>"}
	leads := []string{"", "\xEF\xBB\xBF", " ", "\t", "\r\n", "\f\v", " \t\r\n\f\v", "\xEF\xBB\xBF \n", "\x00", "\x01", " ", " ", "\xEF\xBB\xBF\xEF\xBB\xBF"}
	for _, lead := range leads {
		for _, c := range cases {
			corpus = append(corpus, []byte(lead+c))
			corpus = append(corpus, []byte(lead+c+" more"))
		}
	}
	for b := 0; b < 256; b++ {
		corpus = append(corpus, append([]byte("<html"), byte(b)))
		corpus = append(corpus, append([]byte("<HTML"), byte(b), 'x'))
		corpus = append(corpus, append([]byte("<!doctype htm"), byte(b)))
	}
	for _, m := range markers {
		for i := 0; i <= len(m); i++ {
			corpus = append(corpus, []byte(m[:i]))
			corpus = append(corpus, []byte(strings.ToUpper(m[:i])))
		}
	}
	runes := []string{"K", "İ", "Ⱥ", "ⱥ", "É", "Α", "ẞ", "\U0001F600", "�"}
	for _, r := range runes {
		corpus = append(corpus, []byte(r+"<html>"))
		corpus = append(corpus, []byte("<"+r+"tml>"))
		corpus = append(corpus, []byte("<html"+r))
		corpus = append(corpus, []byte("<!doctype "+r+"tml>"))
		corpus = append(corpus, []byte("<!doctype html"+r))
		corpus = append(corpus, []byte(strings.Repeat(r, 20)+"<html>"))
		corpus = append(corpus, []byte("<html>"+strings.Repeat(r, 20)))
		corpus = append(corpus, []byte(strings.Repeat(r, 4)))
	}
	invalid := []string{"\xff", "\xfe", "\xc0", "\xe2\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\x80\x80\x80"}
	for _, x := range invalid {
		corpus = append(corpus, []byte(x+"<html>"))
		corpus = append(corpus, []byte("<"+x+"html>"))
		corpus = append(corpus, []byte("<html"+x))
		corpus = append(corpus, []byte("<!doctype"+x+" html>"))
		corpus = append(corpus, []byte(strings.Repeat(x, 30)))
	}
	corpus = append(corpus, nil, []byte{})
	corpus = append(corpus, bytes.Repeat([]byte("Ⱥ"), SniffWindowBytes/2))
	corpus = append(corpus, []byte("<html>"+strings.Repeat("Ⱥ", 4096)))
	corpus = append(corpus, []byte(strings.Repeat("Ⱥ", 7)+"<html>"))

	pieces := []string{"<", "!", "doctype", "DOCTYPE", " ", "html", "HTML", "h", "t", "m", "l", "H", "T", "M", "L", ">", "/", "\t", "\r", "\n", "\f", "\v", "\xEF\xBB\xBF", "K", "İ", "Ⱥ", "ⱥ", "é", "\U0001F600", "\xff", "\xc3", "x", "\x00"}
	rng := rand.New(rand.NewPCG(78, 18))
	for range 5000 {
		var b strings.Builder
		n := 1 + rng.IntN(12)
		for range n {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		corpus = append(corpus, []byte(b.String()))
	}
	return corpus
}

func TestLooksLikeHTMLMatchesTheReference(t *testing.T) {
	corpus := htmlReferenceCorpus(t)
	var trues, invalidUTF8, nonASCII int
	var divergent []string
	for _, in := range corpus {
		want := referenceLooksLikeHTMLFromPrefix(in)
		got := LooksLikeHTMLFromPrefix(in)
		if want {
			trues++
		}
		if !utf8.Valid(in) {
			invalidUTF8++
		}
		if bytes.ContainsFunc(in, func(r rune) bool { return r >= utf8.RuneSelf }) {
			nonASCII++
		}
		if got != want {
			divergent = append(divergent, strconv.Quote(string(in[:min(len(in), 80)])))
			if len(divergent) <= 20 {
				t.Errorf("LooksLikeHTMLFromPrefix(%q) = %v, the 805d49da reference says %v", in[:min(len(in), 80)], got, want)
			}
		}
	}
	t.Logf("%d inputs (%d true on the reference, %d invalid UTF-8, %d with non-ASCII bytes); divergences from the 805d49da reference: %d %v", len(corpus), trues, invalidUTF8, nonASCII, len(divergent), divergent)
	if len(corpus) < 5000 {
		t.Fatalf("the corpus has %d inputs, want at least 5000", len(corpus))
	}
}
