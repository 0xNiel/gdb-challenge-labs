package flag

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEncodeDecode_RoundTrip(t *testing.T) {
	t.Parallel()
	f := Derive("s", "tier1-01-off-by-one")
	mix := SeedMix("s", "tier1-01-off-by-one")
	for _, key := range []uint32{0, 1, 345, mix, 0xffffffff} {
		blob := Encode(f, key, mix)
		if got := Decode(blob, key, mix); got != f {
			t.Errorf("key %d: decoded %q", key, got)
		}
		if got := Decode(blob, key+1, mix); got == f || strings.Contains(got, "LAB{") {
			t.Errorf("key %d+1 decoded %q", key, got)
		}
		if bytes.Contains(blob[:], []byte("LAB{")) || bytes.Contains(blob[:], []byte(f[4:28])) {
			t.Errorf("key %d: the blob contains the flag", key)
		}
	}
}

// The generated header compiled by a real C compiler: the right key prints the flag, wrong
// keys print something else, and the binary holds neither "LAB{" nor the flag body. Uses
// the host's cc; the build image does the same with musl in scripts/challenge-build.sh.
func TestHeader_CompilesAndDecodes(t *testing.T) {
	t.Parallel()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler on this host")
	}
	const slug, secret, key = "tier1-01-off-by-one", "test-secret-do-not-use", 345
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "flag_blob.h"), []byte(Header(slug, secret, key)), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `#include <stdio.h>
#include <stdlib.h>
#include "flag_blob.h"
int main(int argc, char **argv) {
	char out[30];
	flag_decode((unsigned)strtoul(argv[1], 0, 0), out);
	fwrite(out, 1, 29, stdout);
	return 0;
}
`
	if err := os.WriteFile(filepath.Join(dir, "t.c"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "t")
	if out, err := exec.Command(cc, "-O0", "-o", bin, filepath.Join(dir, "t.c")).CombinedOutput(); err != nil {
		t.Fatalf("cc: %v\n%s", err, out)
	}
	want := Derive(secret, slug)
	run := func(k uint64) string {
		out, err := exec.Command(bin, strconv.FormatUint(k, 10)).Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	if got := run(key); got != want {
		t.Fatalf("right key printed %q, want %s", got, want)
	}
	for _, k := range []uint64{0, key - 1, key + 1, 1 << 31} {
		if got := run(k); got == want || strings.Contains(got, "LAB{") {
			t.Errorf("key %d printed %q", k, got)
		}
	}
	b, _ := os.ReadFile(bin)
	if bytes.Contains(b, []byte("LAB{")) || bytes.Contains(b, []byte(want[4:28])) {
		t.Error("the compiled binary contains the flag")
	}
}
