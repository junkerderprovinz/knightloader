package watch

import (
	"strings"
	"testing"
)

func TestBOMAndMultiURLLine(t *testing.T) {
	b := "﻿"
	j, err := Parse("links.txt", strings.NewReader(b+"https://a.example/1\nhttps://a.example/2\n"))
	t.Logf("BOM first line: urls=%v err=%v", j.URLs, err)
	j2, err2 := Parse("links.txt", strings.NewReader("https://a.example/1 https://a.example/2\n"))
	t.Logf("space-separated: urls=%v err=%v", j2.URLs, err2)
	j3, err3 := Parse("only.txt", strings.NewReader(b+"https://a.example/1\n"))
	t.Logf("BOM only line: urls=%v err=%v", j3.URLs, err3)
	j4, err4 := Parse("drop.crawljob", strings.NewReader(b+"text=https://a.example/1\n"))
	t.Logf("BOM crawljob: urls=%v err=%v", j4.URLs, err4)
}
