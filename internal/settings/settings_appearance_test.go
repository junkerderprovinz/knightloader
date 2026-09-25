package settings

import "testing"

func TestAStoredShapeKeepsItsShape(t *testing.T) {
	for _, shape := range []string{ShapeRound, ShapeSoft, ShapeSquare, ShapeLeaf} {
		if got := loadFrom(t, `{"shape":"`+shape+`"}`).Shape; got != shape {
			t.Errorf("a stored %q loads as %q", shape, got)
		}
	}
}

func TestAnInstallWithNoStoredShapeStartsOnSoft(t *testing.T) {
	if got := loadFrom(t, `{}`).Shape; got != ShapeSoft {
		t.Errorf("a settings.json without a shape loads as %q, want soft", got)
	}
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Shape; got != ShapeSoft {
		t.Errorf("an install with no settings.json draws %q, want soft", got)
	}
}

func TestAShapeNoPickerOffersFallsBackToSoft(t *testing.T) {
	for _, doc := range []string{`{"shape":"oval"}`, `{"shape":""}`} {
		if got := loadFrom(t, doc).Shape; got != ShapeSoft {
			t.Errorf("%s loads as %q, want soft", doc, got)
		}
	}
}
