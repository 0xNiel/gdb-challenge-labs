package term

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func TestToken(t *testing.T) {
	t.Parallel()
	key := []byte("test-key")
	tokens := NewTokens(key)
	good := tokens.Mint("s1", 42, t0)

	if uid, err := tokens.Verify(good, "s1", t0.Add(59*time.Second)); err != nil || uid != 42 {
		t.Fatalf("valid token: %d, %v", uid, err)
	}
	if _, err := tokens.Verify(good, "s1", t0.Add(time.Second)); !errors.Is(err, ErrTokenReused) {
		t.Fatalf("reuse: %v", err)
	}

	for name, c := range map[string]struct {
		token, session string
		at             time.Time
		want           error
	}{
		"expired":       {tokens.Mint("s1", 42, t0), "s1", t0.Add(TokenTTL), ErrTokenExpired},
		"wrong session": {tokens.Mint("s1", 42, t0), "s2", t0, ErrTokenSession},
		"other key":     {Mint([]byte("other"), "s1", 42, t0.Add(TokenTTL)), "s1", t0, ErrTokenSignature},
		"tampered sig": {func() string {
			tok := tokens.Mint("s1", 42, t0)
			return tok[:len(tok)-2] + flip(tok[len(tok)-2:])
		}(), "s1", t0, ErrTokenSignature},
		"tampered payload": {func() string {
			_, sig, _ := strings.Cut(tokens.Mint("s1", 42, t0), ".")
			p, _, _ := strings.Cut(tokens.Mint("s1", 43, t0), ".")
			return p + "." + sig
		}(), "s1", t0, ErrTokenSignature},
		"no dot":    {"abc", "s1", t0, ErrTokenMalformed},
		"bad b64":   {"!!.!!", "s1", t0, ErrTokenMalformed},
		"two parts": {Mint(key, "s1|x", 1, t0) /* 4 fields */, "s1|x", t0, ErrTokenMalformed},
	} {
		if _, err := tokens.Verify(c.token, c.session, c.at); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
}

func TestToken_Sweep(t *testing.T) {
	t.Parallel()
	tokens := NewTokens([]byte("k"))
	for i := range 3 {
		tok := tokens.Mint("s", int64(i), t0.Add(time.Duration(i)*time.Minute))
		if _, err := tokens.Verify(tok, "s", t0.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if n := tokens.Sweep(t0.Add(90 * time.Second)); n != 1 || tokens.usedCount() != 2 {
		t.Fatalf("swept %d, %d left; want 1 swept, 2 left", n, tokens.usedCount())
	}
	if n := tokens.Sweep(t0.Add(time.Hour)); n != 2 || tokens.usedCount() != 0 {
		t.Fatalf("swept %d, %d left", n, tokens.usedCount())
	}
}

func flip(s string) string {
	b := []byte(s)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}
