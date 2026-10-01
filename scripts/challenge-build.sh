#!/usr/bin/env bash
# challenge-build.sh <challenge-dir> [--push] [--secret-env NAME] [--runtime runsc|runc] [--keep-work]
#
# Builds one challenge into a lab image and proves it (Phase 5 plan, spec "Build pipeline").
#   1 lint      manifestlint against challenges/schema/manifest.schema.json and the directory
#   2 flag      flag_blob.h from the deploy secret, the slug and the manifest's key_value
#   3 build     the challenge's build.sh in images/build, twice: hashes must match; static
#               (no INTERP, no DYNAMIC segment; ADR 0010)
#   4 leak      no "LAB{" and no flag body in the binary (strings, and the raw bytes)
#   5 oracle    in the lab image under the sandbox: solve.gdb prints the flag; a plain
#               `gdb -batch -ex run` does not
#   6 addresses &main and &printf identical over 3 runs, stopped at main
#   7 image     FROM labbase + src/, the binary and README.md in /opt/lab; never solve.gdb,
#               solution.md, manifest.yaml or flag_blob.h
#   8 publish   --push: push to ghcr.io/$GHCR_NAMESPACE/lab-<slug> and write the digest into
#               manifest.yaml. Without --push: the image is imported into containerd namespace
#               labs as local/lab-<slug>@sha256:<digest> and recorded in
#               .scratch/local-images.json (not in the manifest: a local digest belongs to one
#               host; scripts/challenges-json.sh --local uses it).
#
# Architecture: the binary and image are built for the lab host (./run.sh vm ssh): amd64 on
# the laptop, the authoritative build (oracle under runsc), arm64 in the Mac's VM for the
# authoring loop (oracle under runc there, QUESTIONS Q13). --push requires amd64: challenge
# images are x86-64 only (ADR 0001, Q11).
#
# The secret comes from $DEPLOY_SECRET (or --secret-env NAME). Without --push and without a
# secret, the dev secret "dev-deploy-secret" is used and said so. Output and work files are
# under .scratch/challenge-build/<slug> (git-ignored) and removed at the end unless
# --keep-work, since the oracle's output contains the flag.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../images/lib.sh disable=SC1091
. "$ROOT/images/lib.sh"

DIR="" PUSH=0 SECRET_ENV=DEPLOY_SECRET RUNTIME="" KEEP=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --push) PUSH=1; shift ;;
    --secret-env) SECRET_ENV="$2"; shift 2 ;;
    --runtime) RUNTIME="$2"; shift 2 ;;
    --keep-work) KEEP=1; shift ;;
    -*) img_die "unknown flag $1" ;;
    *) DIR="$1"; shift ;;
  esac
done
[[ -n "$DIR" && -f "$DIR/manifest.yaml" ]] || img_die "usage: challenge-build.sh <challenge-dir> [--push] [--secret-env NAME] [--runtime runsc|runc] [--keep-work]"
DIR="$(cd "$DIR" && pwd)"
REL="${DIR#"$ROOT"/}"
T0=$SECONDS
step() { printf '\n==> [%s] %s\n' "$REL" "$*" >&2; }
ok() { printf '    ok  %s\n' "$*" >&2; }
fail() { printf '    FAIL  %s\n' "$*" >&2; exit 1; }

step "1 lint"
(cd "$ROOT/labd" && go build -o bin/manifestlint ./cmd/manifestlint && go build -o bin/flagblob ./cmd/flagblob)
lint_args=(--schema "$ROOT/challenges/schema/manifest.schema.json" --allow-local)
"$ROOT/labd/bin/manifestlint" "${lint_args[@]}" "$DIR/manifest.yaml" >/dev/null || fail "manifest (above)"
M="$("$ROOT/labd/bin/manifestlint" "${lint_args[@]}" --json "$DIR/manifest.yaml")"
SLUG="$(jq -r .slug <<<"$M")" ENTRY="$(jq -r .entry <<<"$M")" FLAGS="$(jq -r .flags <<<"$M")" KEY="$(jq -r .key_value <<<"$M")"
for f in src/main.c build.sh solve.gdb README.md lesson.md solution.md; do
  [[ -f "$DIR/$f" ]] || fail "missing $f"
done
ok "$SLUG (entry $ENTRY, key $KEY)"

step "2 flag"
if [[ -z "${!SECRET_ENV:-}" ]]; then
  ((PUSH == 0)) || fail "--push needs \$$SECRET_ENV (the production deploy secret)"
  export "$SECRET_ENV=dev-deploy-secret"
  img_say "no \$$SECRET_ENV: using the dev secret; these images are for development only"
fi
WORK="$ROOT/.scratch/challenge-build/$SLUG"
rm -rf "$WORK"; mkdir -p "$WORK/gen" "$WORK/out1" "$WORK/out2" "$WORK/ctx"
cleanup() { ((KEEP)) || rm -rf "$WORK"; }
trap cleanup EXIT
"$ROOT/labd/bin/flagblob" --slug "$SLUG" --key "$KEY" --secret-env "$SECRET_ENV" --out "$WORK/gen/flag_blob.h"
FLAG="$("$ROOT/labd/bin/flagblob" --slug "$SLUG" --secret-env "$SECRET_ENV" --print-flag)"
BODY="${FLAG:4:24}"
ok "flag_blob.h written"

step "3 build"
docker_init
# ctr and specrun need root on the lab host; authenticate once, visibly, before any captured
# output (a prompt inside $(...) would be swallowed; see STATUS 2026-09-27).
"$RUN" vm ssh -- sudo -n true 2>/dev/null || { img_say "the lab host needs sudo for ctr and specrun"; "$RUN" vm ssh -- sudo -v; }
PLATFORM="$(lab_platform)"
ARCH="${PLATFORM#linux/}"
((PUSH == 0)) || [[ "$ARCH" == amd64 ]] || fail "--push builds x86-64 images; this lab host is $ARCH (build on the laptop)"
[[ -n "$RUNTIME" ]] || { [[ "$ARCH" == amd64 ]] && RUNTIME=runsc || RUNTIME=runc; }
"${DOCKER[@]}" image inspect gdblabs/build:dev >/dev/null 2>&1 || bash "$ROOT/images/build/build.sh"
"${DOCKER[@]}" image inspect gdblabs/labbase:dev >/dev/null 2>&1 || bash "$ROOT/images/labbase/build.sh"
CFLAGS="$FLAGS -static -no-pie -fno-pie"
AS_ME=(--user "$(id -u):$(id -g)")
compile() { # OUTDIR
  "${DOCKER[@]}" run --rm --platform "$PLATFORM" "${AS_ME[@]}" -e SOURCE_DATE_EPOCH=0 \
    -e CFLAGS="$CFLAGS" -e ENTRY="$ENTRY" -v "$DIR:/opt/lab:ro" -v "$WORK/gen:/gen:ro" -v "$1:/out" \
    -w /opt/lab gdblabs/build:dev sh /opt/lab/build.sh >&2 || fail "build.sh failed (above)"
  [[ -f "$1/$ENTRY" ]] || fail "build.sh did not write /out/$ENTRY"
}
compile "$WORK/out1"
compile "$WORK/out2"
h1="$(shasum -a 256 "$WORK/out1/$ENTRY" | awk '{print $1}')"
h2="$(shasum -a 256 "$WORK/out2/$ENTRY" | awk '{print $1}')"
[[ "$h1" == "$h2" ]] || fail "not reproducible: $h1 != $h2"
ok "reproducible ($ARCH): sha256 $h1"
in_build() { "${DOCKER[@]}" run --rm --platform "$PLATFORM" "${AS_ME[@]}" -v "$WORK/out1:/out:ro" gdblabs/build:dev "$@"; }
if in_build readelf -lW "/out/$ENTRY" | grep -qE '^\s*(INTERP|DYNAMIC)\b'; then
  fail "not static: an INTERP or DYNAMIC segment (ADR 0010: link with -static -no-pie -fno-pie)"
fi
ok "static, no loader"
BIN="$WORK/out1/$ENTRY"

step "4 leak"
n="$(in_build sh -c "strings -n 4 /out/$ENTRY | grep -c 'LAB{' || true")"
[[ "$n" == 0 ]] || fail "strings finds LAB{ $n times in the binary"
if grep -qaF "$BODY" "$BIN" || in_build sh -c "strings -n 4 /out/$ENTRY" | grep -qF "${BODY:0:8}"; then
  fail "the flag body is in the binary"
fi
ok "no LAB{ and no flag body"

step "7 image"   # before 5 and 6: the oracle runs in the lab image itself
TAG="local/lab-$SLUG:$(git -C "$ROOT" rev-parse --short HEAD)"
cp -R "$DIR/src" "$WORK/ctx/src"
cp "$DIR/README.md" "$WORK/ctx/README.md"
cp "$BIN" "$WORK/ctx/$ENTRY"
cat > "$WORK/ctx/Dockerfile" <<EOF
FROM gdblabs/labbase:dev
COPY --chown=0:0 src/ /opt/lab/src/
COPY --chown=0:0 README.md /opt/lab/README.md
COPY --chown=0:0 $ENTRY /opt/lab/$ENTRY
WORKDIR /opt/lab
USER 1000:1000
EOF
"${DOCKER[@]}" build -q --platform "$PLATFORM" -t "$TAG" "$WORK/ctx" >/dev/null || fail "docker build"
# Nothing private may be in it: list the files the challenge added.
# (labbase keeps ls but not find.)
added="$("${DOCKER[@]}" run --rm --platform "$PLATFORM" --entrypoint /bin/sh "$TAG" -c 'ls -1AR /opt/lab' | grep -v -e ':$' -e '^$' | sort | tr '\n' ' ')"
for bad in solve.gdb solution.md manifest.yaml flag_blob.h lesson.md build.sh; do
  [[ "$added" != *"$bad"* ]] || fail "$bad is in the image"
done
LAYER_KB=$(( ( $("${DOCKER[@]}" image inspect -f '{{.Size}}' "$TAG") - $("${DOCKER[@]}" image inspect -f '{{.Size}}' gdblabs/labbase:dev) ) / 1024 ))
"${DOCKER[@]}" save "$TAG" > "$WORK/image.tar"
"$RUN" vm ssh -- sudo ctr -n labs images import --platform "$PLATFORM" --local "$WORK/image.tar" >/dev/null 2>&1 \
  || "$RUN" vm ssh -- sudo ctr -n labs images import --platform "$PLATFORM" "$WORK/image.tar" >/dev/null
DIGEST="$("$RUN" vm ssh -- sudo ctr -n labs images ls "name==docker.io/$TAG" | awk 'NR==2{print $3}' | tr -d '\r')"
[[ "$DIGEST" == sha256:* ]] || fail "imported image has no digest"
REF="local/lab-$SLUG@$DIGEST"
"$RUN" vm ssh -- sudo ctr -n labs images tag --force "docker.io/$TAG" "$REF" >/dev/null
ok "$REF (files: $added; $LAYER_KB KiB over labbase)"

# specrun in the lab host: the lab's sandbox spec, a terminal (ADR 0007), as uid 1000.
"$RUN" vm ssh -- bash -c "cd labd && go build -o bin/specrun ./cmd/specrun" >/dev/null
in_lab() { # TIMEOUT SCRIPT_FILE — runs the gdb script in the lab under $RUNTIME; PTY output on stdout
  "$RUN" vm ssh -- bash -c "printf '%s\n\x04' \"\$(cat '$2')\" | sudo labd/bin/specrun --stdin --runtime $RUNTIME --timeout $1 \
    --spec labd/sandbox/sandbox-base.json --image '$REF' -- sh -c 'cat > /tmp/s.gdb && exec gdb -q -batch -x /tmp/s.gdb /opt/lab/$ENTRY' 2>&1" \
    | tr -d '\r' | grep -v '^specrun-'
}

step "5 oracle ($RUNTIME)"
cp "$DIR/solve.gdb" "$WORK/solve.gdb"
t=$SECONDS
out="$(in_lab 180s "$WORK/solve.gdb")" || true
ORACLE_S=$((SECONDS - t))
printf '%s\n' "$out" > "$WORK/solve.out"
grep -qF "$FLAG" <<<"$out" || { tail -n 25 <<<"$out" | sed 's/^/        /' >&2; fail "solve.gdb did not print the flag"; }
ok "solve.gdb prints the flag (${ORACLE_S}s)"
printf 'run\n' > "$WORK/plain.gdb"
out="$(in_lab 60s "$WORK/plain.gdb")" || true
grep -qF "$FLAG" <<<"$out" && fail "a plain run prints the flag: the bug does not guard it"
grep -qF "$BODY" <<<"$out" && fail "a plain run prints the flag body"
ok "a plain run does not print the flag"

step "6 addresses"
printf 'break main\nrun\nprint &main\nprint &printf\nkill\n' > "$WORK/addr.gdb"
addrs=()
for i in 1 2 3; do
  a="$(in_lab 60s "$WORK/addr.gdb" | grep -oE '0x[0-9a-f]+ <(main|printf)>' | tr '\n' ' ')"
  [[ "$a" == *"<main>"*"<printf>"* ]] || fail "run $i: could not read &main and &printf: '$a'"
  addrs+=("$a")
done
[[ "${addrs[0]}" == "${addrs[1]}" && "${addrs[1]}" == "${addrs[2]}" ]] || fail "addresses move between runs: ${addrs[*]}"
ok "identical over 3 runs: ${addrs[0]}"

step "8 publish"
if ((PUSH)); then
  [[ -n "${GHCR_NAMESPACE:-}" ]] || fail "--push needs \$GHCR_NAMESPACE (QUESTIONS Q2) and a docker login to ghcr.io"
  remote="ghcr.io/$GHCR_NAMESPACE/lab-$SLUG"
  "${DOCKER[@]}" tag "$TAG" "$remote:${TAG##*:}"
  "${DOCKER[@]}" push -q "$remote:${TAG##*:}" >/dev/null || fail "docker push $remote"
  pushed="$("${DOCKER[@]}" image inspect -f '{{range .RepoDigests}}{{println .}}{{end}}' "$remote:${TAG##*:}" | grep "^$remote@" | head -n1)"
  [[ -n "$pushed" ]] || fail "no registry digest for $remote"
  sed -i.bak "s|^image:.*|image: $pushed|" "$DIR/manifest.yaml" && rm -f "$DIR/manifest.yaml.bak"
  "$ROOT/labd/bin/manifestlint" --schema "$ROOT/challenges/schema/manifest.schema.json" "$DIR/manifest.yaml" >/dev/null || fail "manifest after writing the digest"
  ok "pushed $pushed; manifest.yaml updated (commit it, then scripts/challenges-json.sh)"
else
  LOCAL="$ROOT/.scratch/local-images.json"
  [[ -s "$LOCAL" ]] || echo '{}' > "$LOCAL"
  jq --arg s "$SLUG" --arg r "$REF" --arg a "$ARCH" '.[$s] = {image: $r, arch: $a}' "$LOCAL" > "$LOCAL.tmp" && mv "$LOCAL.tmp" "$LOCAL"
  ok "dev image $REF recorded in .scratch/local-images.json"
fi

METRICS="$ROOT/.scratch/challenge-metrics.jsonl"
jq -cn --arg slug "$SLUG" --arg arch "$ARCH" --arg rt "$RUNTIME" --arg host "$(hostname -s)" --arg date "$(date -u +%FT%TZ)" \
  --argjson layer "$LAYER_KB" --argjson build "$((SECONDS - T0))" --argjson oracle "$ORACLE_S" --arg sha "$h1" \
  '{slug:$slug, arch:$arch, runtime:$rt, host:$host, date:$date, layer_kib:$layer, build_s:$build, oracle_s:$oracle, sha256:$sha}' >> "$METRICS"
printf '\n==> [%s] PASSED in %ds: %s\n' "$REL" "$((SECONDS - T0))" "$REF" >&2
