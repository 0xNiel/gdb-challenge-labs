// Package flag derives a challenge's flag (ADR 0005, invariant S15): flags are never stored,
// only computed from the deploy secret and the challenge slug.
package flag

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
)

// Len is the length of a flag: "LAB{" + 24 + "}".
const Len = 4 + 24 + 1

// Derive returns LAB{ + the first 24 characters of the unpadded RFC 4648 base32 of
// HMAC-SHA256(key = secret, msg = slug) + }. Both are used as UTF-8 bytes, unnormalised.
func Derive(secret, slug string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(slug))
	body := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(m.Sum(nil))
	return "LAB{" + body[:24] + "}"
}

// SeedMix is flagblob's FLAG_SEED_MIX: a 32-bit value derived from the secret and the slug,
// so a rebuild produces the same binary (the plan's reproducible-hash check) while the value
// stays unknowable without the secret. ADR 0005 first said "random per build".
func SeedMix(secret, slug string) uint32 {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("flagblob-seed|" + slug))
	s := m.Sum(nil)
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
}
