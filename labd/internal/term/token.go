// Package term is the terminal gateway: one WebSocket per session bridged to the lab's PTY,
// with token auth, rate limits, resize, TTL frames, reconnect grace and command capture
// (spec "Terminal gateway"). The protocol as implemented is in README.md.
package term

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TokenTTL is how long a WebSocket token is valid (spec: 60 s).
const TokenTTL = 60 * time.Second

// Token errors. The handler maps all of them to 401 without saying which.
var (
	ErrTokenMalformed = errors.New("token malformed")
	ErrTokenSignature = errors.New("token signature invalid")
	ErrTokenExpired   = errors.New("token expired")
	ErrTokenSession   = errors.New("token is for another session")
	ErrTokenReused    = errors.New("token already used")
)

var b64 = base64.RawURLEncoding

// Mint returns base64url(session_id|user_id|exp_unix|nonce) "." base64url(HMAC-SHA256(key,
// payload)). The nonce (8 random bytes, base64url) keeps two tokens minted in the same second
// distinct, so both can be used once (ADR 0012). web mints with the same key (WS_TOKEN_KEY)
// for the browser; labd mints only with dev_mint_tokens (ADR 0015). Both pass the shared
// vectors in challenges/schema/ws_token_vectors.json.
func Mint(key []byte, sessionID string, userID int64, exp time.Time) string {
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	return mintNonce(key, sessionID, userID, exp, nonce[:])
}

// mintNonce is Mint with a given nonce, for the shared vectors (ADR 0015).
func mintNonce(key []byte, sessionID string, userID int64, exp time.Time, nonce []byte) string {
	payload := sessionID + "|" + strconv.FormatInt(userID, 10) + "|" + strconv.FormatInt(exp.Unix(), 10) +
		"|" + b64.EncodeToString(nonce)
	return b64.EncodeToString([]byte(payload)) + "." + b64.EncodeToString(sign(key, []byte(payload)))
}

func sign(key, payload []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(payload)
	return m.Sum(nil)
}

// parse checks the signature and returns the fields. It does not check expiry or use.
func parse(key []byte, token string) (sessionID string, userID int64, exp time.Time, err error) {
	p64, s64, ok := strings.Cut(token, ".")
	if !ok {
		return "", 0, exp, ErrTokenMalformed
	}
	payload, err1 := b64.DecodeString(p64)
	sig, err2 := b64.DecodeString(s64)
	if err1 != nil || err2 != nil {
		return "", 0, exp, ErrTokenMalformed
	}
	if !hmac.Equal(sig, sign(key, payload)) {
		return "", 0, exp, ErrTokenSignature
	}
	f := strings.Split(string(payload), "|")
	if len(f) != 4 || f[3] == "" {
		return "", 0, exp, ErrTokenMalformed
	}
	uid, err1 := strconv.ParseInt(f[1], 10, 64)
	unix, err2 := strconv.ParseInt(f[2], 10, 64)
	if err1 != nil || err2 != nil || f[0] == "" {
		return "", 0, exp, ErrTokenMalformed
	}
	return f[0], uid, time.Unix(unix, 0), nil
}

// Tokens verifies tokens and remembers used ones until they expire (single use, S12).
type Tokens struct {
	key  []byte
	mu   sync.Mutex
	used map[string]time.Time // token -> its expiry
}

// NewTokens returns a verifier for key.
func NewTokens(key []byte) *Tokens { return &Tokens{key: key, used: map[string]time.Time{}} }

// Mint is Mint with this verifier's key; exp is now + TokenTTL.
func (t *Tokens) Mint(sessionID string, userID int64, now time.Time) string {
	return Mint(t.key, sessionID, userID, now.Add(TokenTTL))
}

// Verify checks signature, expiry and session, then consumes the token. It returns the
// token's user id.
func (t *Tokens) Verify(token, sessionID string, now time.Time) (int64, error) {
	sid, uid, exp, err := parse(t.key, token)
	if err != nil {
		return 0, err
	}
	if !now.Before(exp) {
		return 0, ErrTokenExpired
	}
	if sid != sessionID {
		return 0, ErrTokenSession
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, seen := t.used[token]; seen {
		return 0, ErrTokenReused
	}
	t.used[token] = exp
	return uid, nil
}

// Sweep forgets used tokens that have expired (they can no longer verify anyway).
func (t *Tokens) Sweep(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for tok, exp := range t.used {
		if !now.Before(exp) {
			delete(t.used, tok)
			n++
		}
	}
	return n
}

// usedCount is for tests.
func (t *Tokens) usedCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.used)
}
