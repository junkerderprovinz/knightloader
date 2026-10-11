package backup

import (
	"bytes"
	"errors"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
)

func TestASealedArchiveOpensOnlyWithItsKey(t *testing.T) {
	key, _ := appkey.Generate()
	other, _ := appkey.Generate()
	archive := []byte("PK\x03\x04 settings.json router-secret")

	sealed, err := Seal(key, archive)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("router-secret")) || bytes.HasPrefix(sealed, []byte("PK")) {
		t.Fatal("the sealed archive is readable without the key")
	}
	if got, err := Unseal(key, sealed); err != nil || !bytes.Equal(got, archive) {
		t.Fatalf("Unseal with the key = %q, %v", got, err)
	}
	if _, err := Unseal(nil, sealed); !errors.Is(err, ErrNeedsKey) {
		t.Errorf("Unseal without a key = %v, want ErrNeedsKey", err)
	}
	if _, err := Unseal(other, sealed); !errors.Is(err, ErrWrongKey) {
		t.Errorf("Unseal with another key = %v, want ErrWrongKey", err)
	}
	// A backup made before the key restores as it always did.
	if got, err := Unseal(key, archive); err != nil || !bytes.Equal(got, archive) {
		t.Errorf("Unseal of a plain archive = %q, %v", got, err)
	}
}
