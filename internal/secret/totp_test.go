package secret

import (
	"strings"
	"testing"
	"time"
)

// rfcSecret is the shared secret RFC 6238's own test vectors use: the ASCII
// string "12345678901234567890", base32-encoded. Every vector below is copied
// from that appendix rather than produced by this package, which is the whole
// point - a hand-written HMAC that agrees with itself proves nothing, and an
// authenticator app somebody has already installed is the real verifier.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPMatchesTheRFCVectors(t *testing.T) {
	// The appendix prints eight digits; a six-digit code is the last six of the
	// same number, because the truncation is one modulo and the digit count only
	// decides which power of ten.
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, c := range cases {
		got, err := TOTPCode(rfcSecret, time.Unix(c.unix, 0))
		if err != nil {
			t.Fatalf("TOTPCode at %d: %v", c.unix, err)
		}
		if got != c.want {
			t.Errorf("TOTPCode at %d = %q, want %q", c.unix, got, c.want)
		}
	}
}

// TestTOTPAcceptsOneStepOfSkewAndNoMore pins the window. A phone whose clock is
// a few seconds out, and a code typed just as it rolls over, are the ordinary
// cases; two steps is a minute of an attacker's time bought for nothing.
func TestTOTPAcceptsOneStepOfSkewAndNoMore(t *testing.T) {
	now := time.Unix(1111111111, 0)
	for _, d := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		code, err := TOTPCode(rfcSecret, now.Add(d))
		if err != nil {
			t.Fatal(err)
		}
		if !ValidTOTP(rfcSecret, code, now) {
			t.Errorf("a code from %v away was refused; one step either side has to pass", d)
		}
	}
	for _, d := range []time.Duration{-60 * time.Second, 60 * time.Second} {
		code, err := TOTPCode(rfcSecret, now.Add(d))
		if err != nil {
			t.Fatal(err)
		}
		if ValidTOTP(rfcSecret, code, now) {
			t.Errorf("a code from %v away was accepted; the window is one step, not two", d)
		}
	}
}

// TestTOTPToleratesWhatPeopleType: authenticator apps print "123 456", and a
// secret copied out of this app's own field can arrive lower-cased.
func TestTOTPToleratesWhatPeopleType(t *testing.T) {
	now := time.Unix(1234567890, 0)
	if !ValidTOTP(rfcSecret, "005 924", now) {
		t.Error("a code with the space the app prints was refused")
	}
	if !ValidTOTP(strings.ToLower(rfcSecret), "005924", now) {
		t.Error("a lower-cased secret was refused")
	}
	if ValidTOTP(rfcSecret, "", now) {
		t.Error("an empty code was accepted")
	}
	if ValidTOTP(rfcSecret, "00592", now) {
		t.Error("a five-digit code was accepted")
	}
}

// TestTOTPStepDoesNotWrapBeforeTheEpoch. A box whose clock has not been set yet
// is the realistic way to get a negative Unix time, and it is exactly the moment
// a second factor must not start handing out codes from a window no verifier
// will ever reach.
func TestTOTPStepDoesNotWrapBeforeTheEpoch(t *testing.T) {
	early, err := TOTPCode(rfcSecret, time.Unix(-100000, 0))
	if err != nil {
		t.Fatal(err)
	}
	zero, err := TOTPCode(rfcSecret, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if early != zero {
		t.Errorf("a pre-epoch time gave %q, want the step-zero code %q", early, zero)
	}
}

func TestNewTOTPSecretIsUsable(t *testing.T) {
	s, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 32 {
		t.Errorf("secret is %d characters, want the 32 a 20-byte base32 secret takes", len(s))
	}
	now := time.Now()
	code, err := TOTPCode(s, now)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidTOTP(s, code, now) {
		t.Error("a freshly minted secret does not verify its own code")
	}
	other, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if other == s {
		t.Error("two secrets in a row came out identical")
	}
}

// TestTOTPURICarriesWhatTheAppsNeed. The two families of authenticator app
// disagree about where the issuer lives, so it goes in both places.
func TestTOTPURICarriesWhatTheAppsNeed(t *testing.T) {
	uri := TOTPURI("KnightLoader", "this instance", rfcSecret)
	for _, want := range []string{
		"otpauth://totp/",
		"secret=" + rfcSecret,
		"issuer=KnightLoader",
		"algorithm=SHA1",
		"digits=6",
		"period=30",
	} {
		if !strings.Contains(uri, want) {
			t.Errorf("the otpauth URI is missing %q:\n%s", want, uri)
		}
	}
	if !strings.HasPrefix(uri, "otpauth://totp/KnightLoader") {
		t.Errorf("the label does not start with the issuer:\n%s", uri)
	}
}

// ---------------------------------------------------------------------------
// Recovery codes
// ---------------------------------------------------------------------------

func TestRecoveryCodesAreDistinctAndVerifiable(t *testing.T) {
	const key = "0123456789abcdef"
	plain, hashed, err := NewRecoveryCodes(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != RecoveryCodeCount || len(hashed) != RecoveryCodeCount {
		t.Fatalf("got %d plain and %d hashed codes, want %d of each", len(plain), len(hashed), RecoveryCodeCount)
	}
	seen := map[string]bool{}
	for _, c := range plain {
		if seen[c] {
			t.Errorf("%q came out twice", c)
		}
		seen[c] = true
		if strings.Contains(c, "0") || strings.Contains(c, "l") || strings.Contains(c, "1") {
			t.Errorf("%q contains a character people misread off a printed sheet", c)
		}
	}
	for i, c := range plain {
		if got := MatchRecoveryCode(key, c, hashed); got != i {
			t.Errorf("code %d matched index %d", i, got)
		}
	}
	if got := MatchRecoveryCode(key, "not-a-code", hashed); got != -1 {
		t.Errorf("a made-up code matched index %d", got)
	}
	if got := MatchRecoveryCode(key, "", hashed); got != -1 {
		t.Errorf("an empty code matched index %d", got)
	}
}

// TestRecoveryCodeIgnoresWhatOnlyExistsForLegibility. The dash and the case are
// there so somebody can read the code off paper; typing it back the way it looks
// on the sheet, or in capitals, has to work.
func TestRecoveryCodeIgnoresWhatOnlyExistsForLegibility(t *testing.T) {
	const key = "0123456789abcdef"
	plain, hashed, err := NewRecoveryCodes(key)
	if err != nil {
		t.Fatal(err)
	}
	code := plain[3]
	for _, typed := range []string{
		strings.ToUpper(code),
		strings.ReplaceAll(code, "-", " "),
		strings.ReplaceAll(code, "-", ""),
		"  " + code + "  ",
	} {
		if got := MatchRecoveryCode(key, typed, hashed); got != 3 {
			t.Errorf("%q matched index %d, want 3", typed, got)
		}
	}
}

// TestRecoveryCodeIsBoundToTheKey. The stored form is an HMAC under this
// instance's own signing key, so a sheet of codes lifted off one install does
// not open another one that happens to have the same password.
func TestRecoveryCodeIsBoundToTheKey(t *testing.T) {
	plain, hashed, err := NewRecoveryCodes("key-one")
	if err != nil {
		t.Fatal(err)
	}
	if got := MatchRecoveryCode("key-two", plain[0], hashed); got != -1 {
		t.Errorf("a code verified under another instance's key, index %d", got)
	}
}
