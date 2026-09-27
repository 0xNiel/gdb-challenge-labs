#!/usr/bin/env bash
# Pinned checksums are read by indirect expansion (${!sumvar}), invisible to shellcheck:
# shellcheck disable=SC2034
# provision.sh --role dev|prod — idempotent installer for every lab host (ADR 0001):
# the Lima dev VM, a Linux laptop, and the production VPS. Ubuntu 24.04, x86-64 or arm64.
#
# Installs pinned, checksum-verified releases of containerd 2.x, gVisor (runsc + shim),
# runc (dev only, for the gVisor overhead reference runs), Postgres 16, Go and uv.
# Re-running changes nothing: every step checks first and prints "ok" when already done;
# only real work prints "Installing". Phase 0 gate relies on that.
set -euo pipefail

# ---------------------------------------------------------------- pinned versions
CONTAINERD_VERSION=2.4.1
CONTAINERD_SHA256_amd64=d65eda6a188aac1006848d8060099be88ba9c4bcddbfd9a23a961194710d0dd4
CONTAINERD_SHA256_arm64=67f9b0a81c7140aaf15fe69053e88175270fadd33aeaffc1b9017123c1f55cea

GVISOR_RELEASE=20260921
GVISOR_SHA512_x86_64=7c899979bed334f0987888c41e545cede8e1f7e67a257978c8de244c3867e32bc30d08491e9af36720273c502acd01d8d86c9844ebcbe125790c43fe6bdf563e
GVISOR_SHA512_aarch64=9438f926b8fadee8c0b8c5c2c954cfc957618acd5d9e1d669e442a9366412dd8794e210b9ce8c7a880a49c8ee9f4f040943a11edae00893d2b22ca844c61de93

RUNC_VERSION=1.5.2
RUNC_SHA256_amd64=599f6f94ff8c5057241eff0d54c3c74f95c34935b6457b33fe545defc61e9488
RUNC_SHA256_arm64=d10ecae898361832a059be2089bab92d158aec54661b18ed7346ed79628b46b0

GO_VERSION=1.26.4
GO_SHA256_amd64=1153d3d50e0ac764b447adfe05c2bcf08e889d42a02e0fe0259bd47f6733ad7f
GO_SHA256_arm64=ef758ae7c6cf9267c9c0ef080b8965f453d89ab2d25d9eb22de4405925238768

UV_VERSION=0.12.3
POSTGRES_MAJOR=16

# ---------------------------------------------------------------- setup
ROLE=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --role) ROLE="$2"; shift 2 ;;
    *) echo "usage: provision.sh --role dev|prod" >&2; exit 1 ;;
  esac
done
[[ "$ROLE" == dev || "$ROLE" == prod ]] || { echo "usage: provision.sh --role dev|prod" >&2; exit 1; }
[[ $EUID -eq 0 ]] || { echo "provision.sh must run as root (sudo)" >&2; exit 1; }
[[ "$(uname -s)" == Linux ]] || { echo "provision.sh runs on Linux only" >&2; exit 1; }

case "$(uname -m)" in
  x86_64)  GOARCH=amd64; GVARCH=x86_64 ;;
  aarch64) GOARCH=arm64; GVARCH=aarch64 ;;
  *) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

TARGET_USER="${SUDO_USER:-}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
export DEBIAN_FRONTEND=noninteractive
CHANGED=0

step()      { printf '\n==> %s\n' "$*"; }
ok()        { printf '    ok: %s\n' "$*"; }
installing(){ printf '    Installing %s\n' "$*"; CHANGED=1; }

# fetch URL DEST ALGO SUM — download and verify, abort on mismatch.
fetch() {
  local url="$1" dest="$2" algo="$3" sum="$4"
  curl -fsSL --retry 3 -o "$dest" "$url"
  echo "$sum  $dest" | "${algo}sum" -c --quiet - || { echo "checksum mismatch for $url" >&2; exit 1; }
}

# write_if_changed DEST MODE — write stdin to DEST only if content differs. Returns 0 if written.
write_if_changed() {
  local dest="$1" mode="$2" tmp
  tmp="$WORK/$(basename "$1").new"
  cat > "$tmp"
  if [[ -f "$dest" ]] && cmp -s "$tmp" "$dest"; then return 1; fi
  install -D -m "$mode" "$tmp" "$dest"
  return 0
}

# ---------------------------------------------------------------- 1. base packages
step "base packages"
BASE_PKGS=(curl ca-certificates jq git make shellcheck gdb strace bzip2)
# Dev hosts need a C toolchain for `go test -race` (cgo). Production never gets a compiler.
[[ "$ROLE" == dev ]] && BASE_PKGS+=(gcc libc6-dev)
missing=()
for p in "${BASE_PKGS[@]}"; do dpkg -s "$p" >/dev/null 2>&1 || missing+=("$p"); done
if ((${#missing[@]})); then
  installing "${missing[*]}"
  apt-get update -qq
  apt-get install -y -qq --no-install-recommends "${missing[@]}" >/dev/null
else
  ok "${BASE_PKGS[*]}"
fi

# ---------------------------------------------------------------- 2. containerd
step "containerd $CONTAINERD_VERSION"
if command -v containerd >/dev/null && [[ "$(containerd --version | awk '{print $3}')" == "v$CONTAINERD_VERSION" ]]; then
  ok "containerd v$CONTAINERD_VERSION"
else
  installing "containerd $CONTAINERD_VERSION"
  sumvar="CONTAINERD_SHA256_$GOARCH"
  fetch "https://github.com/containerd/containerd/releases/download/v$CONTAINERD_VERSION/containerd-$CONTAINERD_VERSION-linux-$GOARCH.tar.gz" \
        "$WORK/containerd.tgz" sha256 "${!sumvar}"
  tar -C /usr/local -xzf "$WORK/containerd.tgz"
fi

if ! getent group containerd >/dev/null; then installing "group containerd"; groupadd --system containerd; else ok "group containerd"; fi
CONTAINERD_GID="$(getent group containerd | cut -d: -f3)"

# No CRI: labd talks to containerd directly (spec). Socket group-owned by `containerd` (S10).
if write_if_changed /etc/containerd/config.toml 0644 <<EOF
# Managed by deploy/scripts/provision.sh — do not edit by hand.
version = 3
disabled_plugins = ["io.containerd.grpc.v1.cri"]

[grpc]
  address = "/run/containerd/containerd.sock"
  uid = 0
  gid = $CONTAINERD_GID
EOF
then installing "/etc/containerd/config.toml"; RESTART_CONTAINERD=1; else ok "/etc/containerd/config.toml"; fi

if write_if_changed /etc/systemd/system/containerd.service 0644 <<'EOF'
# Managed by deploy/scripts/provision.sh (upstream unit, binaries in /usr/local/bin).
[Unit]
Description=containerd container runtime
Documentation=https://containerd.io
After=network.target local-fs.target

[Service]
ExecStartPre=-/sbin/modprobe overlay
ExecStart=/usr/local/bin/containerd
Type=notify
Delegate=yes
KillMode=process
Restart=always
RestartSec=5
LimitNPROC=infinity
LimitCORE=infinity
TasksMax=infinity
OOMScoreAdjust=-999

[Install]
WantedBy=multi-user.target
EOF
then installing "containerd.service"; systemctl daemon-reload; RESTART_CONTAINERD=1; else ok "containerd.service"; fi

# ---------------------------------------------------------------- 3. gVisor
step "gVisor release $GVISOR_RELEASE"
GVISOR_STAMP=/usr/local/lib/gvisor/RELEASE
if [[ -x /usr/local/bin/runsc && -x /usr/local/bin/containerd-shim-runsc-v1 && -x /usr/local/bin/gvisor-bin/gvisor_sentry \
      && "$(cat "$GVISOR_STAMP" 2>/dev/null)" == "$GVISOR_RELEASE" ]]; then
  ok "runsc $GVISOR_RELEASE"
else
  # Since 2026 releases, runsc needs its sidecars (gvisor-bin/gvisor_sentry, ...) next to it;
  # install the whole tarball, not just runsc and the shim.
  installing "runsc + containerd-shim-runsc-v1 + gvisor-bin/ ($GVISOR_RELEASE)"
  sumvar="GVISOR_SHA512_$GVARCH"
  fetch "https://storage.googleapis.com/gvisor/releases/release/$GVISOR_RELEASE/$GVARCH/gvisor.tar.bz2" \
        "$WORK/gvisor.tar.bz2" sha512 "${!sumvar}"
  mkdir -p "$WORK/gvisor"
  tar -C "$WORK/gvisor" -xjf "$WORK/gvisor.tar.bz2"
  install -m 0755 "$WORK/gvisor/runsc" "$WORK/gvisor/containerd-shim-runsc-v1" /usr/local/bin/
  rm -rf /usr/local/bin/gvisor-bin
  cp -a "$WORK/gvisor/gvisor-bin" /usr/local/bin/gvisor-bin
  chown -R root:root /usr/local/bin/gvisor-bin
  install -D -m 0644 /dev/null "$GVISOR_STAMP"; echo "$GVISOR_RELEASE" > "$GVISOR_STAMP"
  RESTART_CONTAINERD=1
fi

# runsc options passed by labd via the shim's ConfigPath (Phase 2). systrap: no KVM needed.
if write_if_changed /etc/containerd/runsc.toml 0644 <<'EOF'
# Managed by deploy/scripts/provision.sh. Options for containerd-shim-runsc-v1.
[runsc_config]
  platform = "systrap"
EOF
then installing "/etc/containerd/runsc.toml"; else ok "/etc/containerd/runsc.toml"; fi

# ---------------------------------------------------------------- 4. runc (dev) / no runc (prod)
if [[ "$ROLE" == dev ]]; then
  step "runc $RUNC_VERSION (dev only: gVisor overhead reference)"
  if command -v runc >/dev/null && [[ "$(runc --version | sed -n 1p)" == *" $RUNC_VERSION" ]]; then
    ok "runc $RUNC_VERSION"
  else
    installing "runc $RUNC_VERSION"
    sumvar="RUNC_SHA256_$GOARCH"
    fetch "https://github.com/opencontainers/runc/releases/download/v$RUNC_VERSION/runc.$GOARCH" \
          "$WORK/runc" sha256 "${!sumvar}"
    install -m 0755 "$WORK/runc" /usr/local/sbin/runc
  fi
else
  step "runc absent (prod: runsc is the only runtime, S1)"
  for f in /usr/local/sbin/runc /usr/local/bin/containerd-shim-runc-v2; do
    if [[ -e "$f" ]]; then installing "removal of $f"; rm -f "$f"; else ok "$f absent"; fi
  done
fi

if [[ "${RESTART_CONTAINERD:-0}" == 1 ]]; then
  systemctl enable --now containerd >/dev/null 2>&1 || true
  systemctl restart containerd
fi
if systemctl is-active --quiet containerd; then ok "containerd running"
else installing "containerd start"; systemctl enable --now containerd; fi

# The lab namespace (created lazily by containerd; creating it now makes `ctr -n labs` explicit).
if ctr namespaces ls -q 2>/dev/null | grep -qx labs; then ok "namespace labs"
else installing "namespace labs"; ctr namespaces create labs; fi

# ---------------------------------------------------------------- 5. Postgres
step "Postgres $POSTGRES_MAJOR"
if dpkg -s "postgresql-$POSTGRES_MAJOR" >/dev/null 2>&1; then ok "postgresql-$POSTGRES_MAJOR"
else
  installing "postgresql-$POSTGRES_MAJOR"
  apt-get update -qq
  apt-get install -y -qq --no-install-recommends "postgresql-$POSTGRES_MAJOR" >/dev/null
fi
systemctl is-active --quiet postgresql || { installing "postgresql start"; systemctl enable --now postgresql; }

psql_q() { sudo -u postgres psql -qtAX -c "$1"; }
if [[ "$ROLE" == dev ]]; then
  # Dev passwords equal role names (deploy/env.example). Prod roles are created in Phase 8
  # with generated passwords from scripts/gen-secrets.sh.
  for r in labd web; do
    if [[ "$(psql_q "SELECT 1 FROM pg_roles WHERE rolname='$r'")" == 1 ]]; then ok "role $r"
    else installing "role $r"; psql_q "CREATE ROLE $r LOGIN PASSWORD '$r'"; fi
  done
  if [[ "$(psql_q "SELECT 1 FROM pg_database WHERE datname='labs'")" == 1 ]]; then ok "database labs"
  else
    installing "database labs"
    psql_q "CREATE DATABASE labs OWNER labd"
    sudo -u postgres psql -qtAX -d labs -c "GRANT ALL ON SCHEMA public TO web"
  fi
fi

# ---------------------------------------------------------------- 6. Go
step "Go $GO_VERSION"
if [[ -x /usr/local/go/bin/go && "$(/usr/local/go/bin/go version | awk '{print $3}')" == "go$GO_VERSION" ]]; then
  ok "go $GO_VERSION"
else
  installing "go $GO_VERSION"
  sumvar="GO_SHA256_$GOARCH"
  fetch "https://go.dev/dl/go$GO_VERSION.linux-$GOARCH.tar.gz" "$WORK/go.tgz" sha256 "${!sumvar}"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "$WORK/go.tgz"
fi
for b in go gofmt; do
  if [[ "$(readlink /usr/local/bin/$b 2>/dev/null)" == "/usr/local/go/bin/$b" ]]; then ok "/usr/local/bin/$b"
  else installing "/usr/local/bin/$b link"; ln -sf "/usr/local/go/bin/$b" "/usr/local/bin/$b"; fi
done

# ---------------------------------------------------------------- 7. uv
step "uv $UV_VERSION"
if command -v uv >/dev/null && [[ "$(uv --version | awk '{print $2}')" == "$UV_VERSION" ]]; then
  ok "uv $UV_VERSION"
else
  installing "uv $UV_VERSION into /usr/local/bin"
  curl -fsSL "https://astral.sh/uv/$UV_VERSION/install.sh" | env UV_UNMANAGED_INSTALL=/usr/local/bin sh >/dev/null
fi

# ---------------------------------------------------------------- 8. developer access (dev)
if [[ "$ROLE" == dev && -n "$TARGET_USER" && "$TARGET_USER" != root ]]; then
  step "developer access for $TARGET_USER"
  if id -nG "$TARGET_USER" | tr ' ' '\n' | grep -qx containerd; then ok "$TARGET_USER in group containerd"
  else installing "$TARGET_USER into group containerd (log out and in to take effect)"; usermod -aG containerd "$TARGET_USER"; fi
fi

# ---------------------------------------------------------------- 9. prod-only steps
if [[ "$ROLE" == prod ]]; then
  step "production hardening"
  # Phase 8 owns these (docs/plan/phase-8-production.md, task 8.2). This is the one TODO
  # the plan allows in Phase 0.
  for s in "users web/labd + socket group" "Caddy" "ufw 22/80/443" "fail2ban" "unattended-upgrades" \
           "Postgres prod roles and shared_buffers" "ghcr hosts.toml"; do
    echo "    prod step: TODO Phase 8 — $s"
  done
fi

# ---------------------------------------------------------------- summary
step "versions ($ROLE, $(uname -m))"
printf '    %-12s %s\n' containerd "$(containerd --version | awk '{print $3}')" \
                        runsc      "$(runsc --version | sed -n 1p)" \
                        runc       "$(if command -v runc >/dev/null; then runc --version | sed -n 1p; else echo absent; fi)" \
                        postgres   "$(sudo -u postgres psql -tAX -c 'SHOW server_version' | awk '{print $1}')" \
                        go         "$(go version | awk '{print $3}')" \
                        uv         "$(uv --version | awk '{print $2}')" \
                        cgroup     "$(stat -fc %T /sys/fs/cgroup)"
if [[ $CHANGED -eq 0 ]]; then echo; echo "==> nothing to do: host already provisioned"; fi
