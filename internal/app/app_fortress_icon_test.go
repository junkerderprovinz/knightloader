package app

import (
	"bytes"
	"os"
	"testing"
)

func TestFortressIconIsTheMarkTheWebInterfaceShows(t *testing.T) {
	web, err := os.ReadFile("../../web/src/assets/logo.svg")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(logoSVG, web) {
		t.Fatal("internal/app/logo.svg differs from web/src/assets/logo.svg")
	}
	if Icon == "" {
		t.Fatal("the mark does not pass as a status icon")
	}
}
