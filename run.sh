#!/usr/bin/env bash
# run.sh — single entry point for gdb Challenge Labs.
#
# Anything that needs containerd or runsc runs inside the Lima dev VM. On macOS this
# script re-executes those subcommands in the VM automatically; on Linux it runs them
# directly. The Makefile is a thin alias layer over this file.
#
# Usage: ./run.sh <command> [flags]     (./run.sh help for the list)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VM_NAME="${LABS_VM:-labs}"
LIMA_TEMPLATE="$ROOT/deploy/lima/labs-dev.yaml"
LIMA_RENDERED="$ROOT/.lima/labs-dev.rendered.yaml"
LABD_DIR="$ROOT/labd"
WEB_DIR="$ROOT/web"
METRICS_DIR="$ROOT/docs/metrics"
LABD_CONFIG="${LABD_CONFIG:-$LABD_DIR/labd.dev.yaml}"

say()  { printf '==> %s\n' "$*" >&2; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }
is_linux() { [[ "$(uname -s)" == "Linux" ]]; }
is_darwin() { [[ "$(uname -s)" == "Darwin" ]]; }

usage() {
  cat <<'EOF'
gdb Challenge Labs — ./run.sh <command> [flags]

  help                         this text
  doctor [--strict]            check every dependency for THIS host (macOS or Linux, arm64 or x86-64),
                               print what is ok, what is missing and how to install it; --strict exits 1
  check                        alias for `doctor --strict` (used by gates and CI)

  vm up [--arch x86_64]        macOS: create/start the Lima dev VM and provision it (idempotent)
                               Linux: provision this machine directly (containerd, runsc, Postgres, Go, uv)
  vm verify                    run a container under runsc in namespace `labs`, check cgroup v2
  vm ssh [-- cmd...]           macOS: shell into the VM at the repo root. Linux: run locally
  vm status | vm down | vm delete   (macOS only; no-ops on Linux)

  build [--race]               build labd and labd-perf into labd/bin/ (runs in the VM on macOS)
  test [--go] [--web] [--integration] [--e2e] [--all] [-v]
                               default --all = --go --web (host). --integration/--e2e run in the VM
  lint | fmt                   gofmt/vet/staticcheck, ruff, shellcheck | apply formatters

  labd [--config PATH] [args]  run labd in the VM (default config labd/labd.dev.yaml)
  web [--port 8000]            run the Django dev server on the host
  dev                          labd in the VM (background) + Django on the host
  db up|down|shell|migrate|reset
                               Postgres in the VM; `migrate` runs labd migrate then Django migrate

  images labbase|perf|build|all
  images challenge <dir> [--push]
  perf --scenario P0..P9 [--n N] [--hold 20m] [--ramp 5] [--runtime runsc|runc] [--out DIR]
  gate --phase N               exit test for phase N (scripts/gate.sh)
  deploy                       production deploy on the VPS (Phase 8)

Environment: LABS_VM (VM name, default labs), LIMA_ARCH (aarch64|x86_64), LABD_CONFIG.
EOF
}

# ---------------------------------------------------------------- VM plumbing

vm_exists()  { have limactl && limactl list -q 2>/dev/null | grep -qx "$VM_NAME"; }
vm_running() { have limactl && limactl list --json 2>/dev/null | grep -q "\"name\":\"$VM_NAME\".*\"status\":\"Running\""; }

# Run a command on Linux: directly if we are on Linux, else inside the VM at the repo root.
run_linux() {
  if is_linux; then
    "$@"
  else
    vm_running || die "VM '$VM_NAME' is not running; ./run.sh vm up"
    # The repo is shared with the Mac; keep the VM's Python venv out of web/.venv so the two
    # platforms never overwrite each other's binaries.
    limactl shell --workdir "$ROOT" "$VM_NAME" -- env UV_PROJECT_ENVIRONMENT=/var/tmp/labs-web-venv "$@"
  fi
}

render_lima_template() {
  mkdir -p "$(dirname "$LIMA_RENDERED")"
  sed "s|__REPO_ROOT__|$ROOT|g" "$LIMA_TEMPLATE" > "$LIMA_RENDERED"
}

cmd_vm() {
  local sub="${1:-}"; shift || true
  is_darwin || is_linux || die "unsupported host"
  # On a Linux host there is no VM: the machine itself is the lab host (ADR 0001).
  if is_linux; then
    case "$sub" in
      up)     say "Linux host ($(uname -m)): provisioning this machine directly, no VM"
              sudo bash "$ROOT/deploy/scripts/provision.sh" --role dev; return ;;
      verify) bash "$ROOT/deploy/scripts/verify-runtime.sh"; return ;;
      ssh)    [[ "${1:-}" == "--" ]] && shift; "${@:-bash}"; return ;;
      status) say "native Linux host $(hostname) $(uname -m); nothing to report"; return ;;
      down|delete) say "native Linux host; nothing to $sub"; return ;;
      *)      die "vm: up|verify|ssh|status|down|delete" ;;
    esac
  fi
  case "$sub" in
    up)
      local arch="${LIMA_ARCH:-}"
      while [[ $# -gt 0 ]]; do case "$1" in --arch) arch="$2"; shift 2;; *) die "vm up: unknown flag $1";; esac; done
      have limactl || die "limactl not found (brew install lima)"
      render_lima_template
      if vm_exists; then
        say "starting existing VM $VM_NAME"; limactl start "$VM_NAME"
      else
        local flags=(--name "$VM_NAME" --tty=false)
        if [[ -n "$arch" ]]; then
          flags+=(--arch "$arch")
          [[ "$arch" == "x86_64" ]] && is_darwin && flags+=(--vm-type qemu)
        fi
        say "creating VM $VM_NAME ${arch:+(arch $arch)}"
        limactl start "${flags[@]}" "$LIMA_RENDERED"
      fi
      say "provisioning (idempotent)"
      limactl shell --workdir "$ROOT" "$VM_NAME" -- sudo bash deploy/scripts/provision.sh --role dev
      ;;
    verify)  run_linux bash deploy/scripts/verify-runtime.sh ;;
    ssh)     [[ "${1:-}" == "--" ]] && shift; vm_running || die "VM not running"
             limactl shell --workdir "$ROOT" "$VM_NAME" -- "${@:-bash}" ;;
    status)  if vm_exists; then limactl list "$VM_NAME"; else say "VM '$VM_NAME' not created yet — ./run.sh vm up"; fi ;;
    down)    if vm_exists; then limactl stop "$VM_NAME"; else say "VM '$VM_NAME' does not exist"; fi ;;
    delete)  if vm_exists; then limactl delete --force "$VM_NAME"; else say "VM '$VM_NAME' does not exist"; fi ;;
    *)       die "vm: up|verify|ssh|status|down|delete" ;;
  esac
}

# ---------------------------------------------------------------- doctor (dependency report)

# Minimum versions. Keep in sync with docs/CONVENTIONS.md and deploy/scripts/provision.sh.
MIN_GO=1.25
MIN_CONTAINERD=2.0
MIN_GIT=2.30
MIN_BASH=4.0
MIN_MACOS=13.0

# ver_ge A B — true when dotted version A >= B (compares up to three numeric fields).
ver_ge() {
  awk -v a="$1" -v b="$2" 'BEGIN{split(a,x,".");split(b,y,".");
    for(i=1;i<=3;i++){xa=x[i]+0;yb=y[i]+0;if(xa>yb)exit 0;if(xa<yb)exit 1}exit 0}'
}

pkg_mgr() {
  if have brew; then echo brew; elif have apt-get; then echo apt; elif have dnf; then echo dnf
  elif have pacman; then echo pacman; else echo none; fi
}

# tool_version NAME — best-effort dotted version, empty if unknown.
tool_version() {
  case "$1" in
    go)         go version 2>/dev/null | awk '{print $3}' | sed 's/^go//' ;;
    containerd) containerd --version 2>/dev/null | awk '{print $3}' | sed 's/^v//' ;;
    ctr)        ctr --version 2>/dev/null | awk '{print $3}' | sed 's/^v//' ;;
    runsc)      runsc --version 2>/dev/null | head -n1 | awk '{print $NF}' ;;
    limactl)    limactl --version 2>/dev/null | awk '{print $3}' ;;
    uv)         uv --version 2>/dev/null | awk '{print $2}' ;;
    git)        git --version 2>/dev/null | awk '{print $3}' ;;
    docker)     docker --version 2>/dev/null | awk '{print $3}' | tr -d , ;;
    bash)       echo "${BASH_VERSION%%(*}" ;;
    make)       make --version 2>/dev/null | head -n1 | awk '{print $NF}' ;;
    psql)       psql --version 2>/dev/null | awk '{print $3}' ;;
    *)          "$1" --version 2>/dev/null | head -n1 | grep -oE '[0-9]+(\.[0-9]+)+' | head -n1 ;;
  esac
}

# install_hint NAME — one-line install suggestion for this host.
install_hint() {
  local pm; pm="$(pkg_mgr)"
  case "$1" in
    go)         case "$pm" in brew) echo "brew install go";; *) echo "./run.sh vm up (provision.sh installs Go) or https://go.dev/dl";; esac ;;
    uv)         echo "curl -LsSf https://astral.sh/uv/install.sh | sh" ;;
    git)        case "$pm" in brew) echo "brew install git";; apt) echo "sudo apt-get install -y git";; dnf) echo "sudo dnf install -y git";; pacman) echo "sudo pacman -S git";; *) echo "install git";; esac ;;
    make)       case "$pm" in brew) echo "xcode-select --install";; apt) echo "sudo apt-get install -y make";; dnf) echo "sudo dnf install -y make";; pacman) echo "sudo pacman -S make";; *) echo "install make";; esac ;;
    jq|shellcheck|curl)
                case "$pm" in brew) echo "brew install $1";; apt) echo "sudo apt-get install -y $1";; dnf) echo "sudo dnf install -y $1";; pacman) echo "sudo pacman -S $1";; *) echo "install $1";; esac ;;
    bash)       echo "brew install bash   (macOS ships bash 3.2; run.sh works on it but 4+ is recommended)" ;;
    limactl)    echo "brew install lima" ;;
    docker)     if is_darwin; then echo "Docker Desktop or: brew install colima docker docker-buildx && colima start"; else echo "https://docs.docker.com/engine/install/ (or nerdctl); only needed to BUILD images"; fi ;;
    buildx)     echo "docker buildx is part of Docker Desktop; on Linux: sudo apt-get install -y docker-buildx-plugin" ;;
    containerd|ctr|runsc|containerd-shim-runsc-v1)
                echo "./run.sh vm up   (runs deploy/scripts/provision.sh --role dev on this Linux host)" ;;
    psql)       case "$pm" in brew) echo "brew install libpq && brew link --force libpq";; apt) echo "sudo apt-get install -y postgresql-client";; *) echo "install the postgresql client";; esac ;;
    binfmt)     echo "docker run --privileged --rm tonistiigi/binfmt --install all   (cross-arch image builds)" ;;
    *)          echo "install $1" ;;
  esac
}

cmd_doctor() {
  local strict=0
  while [[ $# -gt 0 ]]; do case "$1" in --strict) strict=1;; *) die "doctor: unknown flag $1";; esac; shift; done

  local needed="" optional_missing="" warnings=""
  hdr() { printf '==> %s\n' "$*"; }
  row() { printf '  %-8s %-26s %s\n' "$1" "$2" "$3"; }
  # tool NAME REQUIRED(1/0) [MINVER] [PURPOSE]
  tool() {
    local name="$1" required="$2" min="${3:-}" purpose="${4:-}" v
    if have "$name"; then
      v="$(tool_version "$name")"
      if [[ -n "$min" && -n "$v" ]] && ! ver_ge "$v" "$min"; then
        row "OLD" "$name $v" "need >= $min — $(install_hint "$name")"
        if [[ $required == 1 ]]; then needed+="  $name >= $min: $(install_hint "$name")"$'\n'
        else optional_missing+="  $name >= $min: $(install_hint "$name")"$'\n'; fi
      else
        row "ok" "$name ${v:-}" "$purpose"
      fi
    elif [[ $required == 1 ]]; then
      row "MISSING" "$name" "$purpose — $(install_hint "$name")"
      needed+="  $name: $(install_hint "$name")"$'\n'
    else
      row "--" "$name" "optional: $purpose — $(install_hint "$name")"
      optional_missing+="  $name: $(install_hint "$name")"$'\n'
    fi
  }
  fact() { row "info" "$1" "$2"; }
  warn_row() { row "WARN" "$1" "$2"; warnings+="  $1: $2"$'\n'; }

  local os arch; os="$(uname -s)"; arch="$(uname -m)"
  hdr "host"
  fact "os / arch" "$os $arch ($(uname -r))"
  if is_linux; then
    fact "cpu / memory" "$(nproc) vCPU, $(awk '/MemTotal/{printf "%.0f GB", $2/1048576}' /proc/meminfo)"
    # osr KEY — a field from /etc/os-release, unquoted (parsed, not sourced).
    osr() { sed -n "s/^$1=//p" /etc/os-release 2>/dev/null | tr -d '"' | head -n1; }
    local distro did dver dlike
    distro="$(osr PRETTY_NAME)"; did="$(osr ID)"; dver="$(osr VERSION_ID)"; dlike="$did $(osr ID_LIKE)"
    if [[ "$did" == ubuntu && "$dver" == 24.04 ]]; then
      fact "distro" "$distro (reference)"
    elif [[ " $dlike " == *" ubuntu "* || " $dlike " == *" debian "* ]]; then
      fact "distro" "${distro:-unknown} — supported; vm up adds upstream repos for anything missing (e.g. Postgres 16)"
    else
      warn_row "distro" "${distro:-unknown} is not Debian/Ubuntu-based; provision.sh (./run.sh vm up) needs apt"
    fi
    grep -qi microsoft /proc/version 2>/dev/null && warn_row "WSL detected" "gVisor under WSL2 is untested here; needs cgroup v2 (kernelCommandLine=cgroup_no_v1=all in .wslconfig)"
    if [[ "$(stat -fc %T /sys/fs/cgroup 2>/dev/null)" == "cgroup2fs" ]]; then fact "cgroup" "v2 (cgroup2fs)"
    else warn_row "cgroup" "not cgroup v2 — labs need it (boot with systemd.unified_cgroup_hierarchy=1)"; fi
    if [[ -e /dev/kvm ]]; then fact "/dev/kvm" "present (runsc --platform=kvm can be benchmarked)"; else fact "/dev/kvm" "absent (systrap platform only)"; fi
  elif is_darwin; then
    local macv; macv="$(sw_vers -productVersion 2>/dev/null || echo 0)"
    fact "cpu / memory" "$(sysctl -n hw.ncpu) cores, $(( $(sysctl -n hw.memsize) / 1073741824 )) GB"
    if ver_ge "$macv" "$MIN_MACOS"; then fact "macOS" "$macv (vz virtualisation available for Lima)"
    else warn_row "macOS $macv" "Lima vz needs macOS >= $MIN_MACOS; qemu fallback is slow"; fi
    fact "note" "labs need Linux: on macOS everything container-related runs in the Lima VM (./run.sh vm up)"
  else
    warn_row "unsupported OS" "$os — use Linux or macOS"
  fi

  hdr "core toolchain (required everywhere)"
  tool bash 0 "$MIN_BASH" "shell for run.sh"
  tool git 1 "$MIN_GIT" "version control"
  tool make 1 "" "Makefile aliases"
  tool curl 1 "" "downloads in provision.sh"
  tool go 1 "$MIN_GO" "labd, labd-perf"
  tool uv 1 "" "Python 3.13 + Django (uv installs Python itself)"
  tool jq 0 "" "JSON in scripts and gates"
  tool shellcheck 0 "" "lint for shell scripts (./run.sh lint)"

  hdr "container runtime"
  if is_darwin; then
    tool limactl 1 "" "Linux VM with containerd + runsc"
    if have limactl; then
      if vm_running; then fact "lima vm '$VM_NAME'" "running — dependencies inside it are checked by ./run.sh vm verify"
      elif vm_exists; then fact "lima vm '$VM_NAME'" "exists, stopped — ./run.sh vm up"
      else fact "lima vm '$VM_NAME'" "not created yet — ./run.sh vm up"; fi
    fi
  else
    tool containerd 1 "$MIN_CONTAINERD" "runs lab containers"
    tool ctr 1 "" "containerd CLI used by scripts and gates"
    tool runsc 1 "" "gVisor sandbox runtime"
    tool containerd-shim-runsc-v1 1 "" "containerd ↔ runsc shim"
    if [[ -S /run/containerd/containerd.sock ]]; then
      if [[ -w /run/containerd/containerd.sock ]]; then fact "containerd socket" "writable by $(id -un)"
      else fact "containerd socket" "present; needs sudo or membership of the socket's group (provision.sh sets this up)"; fi
    else
      [[ "$needed" == *containerd* ]] || warn_row "containerd socket" "/run/containerd/containerd.sock missing — is containerd running? (sudo systemctl start containerd)"
    fi
    if have runsc; then
      local rtoml=/etc/containerd/runsc.toml
      if [[ -f "$rtoml" ]]; then fact "runsc.toml" "$(grep -E '^\s*platform' "$rtoml" 2>/dev/null | head -n1 | tr -s ' ' || echo present)"
      else warn_row "runsc.toml" "$rtoml missing — ./run.sh vm up writes it"; fi
    fi
  fi

  hdr "image building (needed to build labbase, perf and challenge images)"
  tool docker 0 "" "docker buildx multi-arch builds"
  if have docker; then
    if docker buildx version >/dev/null 2>&1; then fact "docker buildx" "$(docker buildx version 2>/dev/null | awk '{print $2}')"
    else row "--" "docker buildx" "optional: $(install_hint buildx)"; optional_missing+="  docker buildx: $(install_hint buildx)"$'\n'; fi
    if is_linux; then
      local other; [[ "$arch" == "x86_64" ]] && other=qemu-aarch64 || other=qemu-x86_64
      if [[ -e /proc/sys/fs/binfmt_misc/$other ]]; then fact "binfmt ($other)" "registered — can build images for the other architecture"
      else row "--" "binfmt ($other)" "optional: $(install_hint binfmt)"; optional_missing+="  binfmt: $(install_hint binfmt)"$'\n'; fi
    fi
  fi

  hdr "database"
  tool psql 0 "" "Postgres CLIENT on this host, for poking at the DB by hand"
  if is_linux; then
    # The SERVER is not a doctor item: ./run.sh vm up installs it (Postgres 16 on every host).
    local pgv; pgv="$(dpkg-query -W -f='${Version}' postgresql-16 2>/dev/null || true)"
    if [[ -n "$pgv" ]]; then
      if have pg_isready && pg_isready -q 2>/dev/null; then fact "postgres server" "16 ($pgv), accepting connections"
      else fact "postgres server" "16 ($pgv), not running (./run.sh db up)"; fi
    else
      fact "postgres server" "not installed yet — ./run.sh vm up installs Postgres 16 (not an apt step for you)"
    fi
  else
    fact "postgres server" "runs inside the Lima VM; ./run.sh vm up installs it"
  fi

  hdr "project state"
  present() { if [[ -f "$1" ]]; then fact "$2" "present"; else fact "$2" "absent — $3"; fi; }
  present "$LABD_DIR/go.mod"        "labd/go.mod"        "Phase 0, task 0.2"
  present "$WEB_DIR/pyproject.toml" "web/pyproject.toml" "Phase 0, task 0.3"
  present "$ROOT/.env"              ".env"               "cp deploy/env.example .env when a phase needs secrets"

  echo
  if [[ -n "$needed" ]]; then
    printf 'STILL NEEDED (required):\n%s' "$needed"
  else
    printf 'All required dependencies for this host are present.\n'
  fi
  if [[ -n "$optional_missing" ]]; then printf '\nOPTIONAL (install when the phase you work on needs it):\n%s' "$optional_missing"; fi
  if [[ -n "$warnings" ]]; then printf '\nWARNINGS:\n%s' "$warnings"; fi
  printf '\nNext: read CLAUDE.md, then docs/STATUS.md. Onboarding: docs/ONBOARDING.md\n'

  if [[ $strict -eq 1 && -n "$needed" ]]; then return 1; fi
  return 0
}

cmd_check() { cmd_doctor --strict; }

# ---------------------------------------------------------------- build / test / lint

cmd_build() {
  local race=""
  while [[ $# -gt 0 ]]; do case "$1" in --race) race="-race"; shift;; *) die "build: unknown flag $1";; esac; done
  [[ -f "$LABD_DIR/go.mod" ]] || die "labd/go.mod missing — Phase 0, task 0.2"
  say "building labd and labd-perf"
  # shellcheck disable=SC2086
  run_linux bash -c "cd '$LABD_DIR' && mkdir -p bin && go build $race -o bin/ ./cmd/..."
}

cmd_test() {
  local go=0 web=0 integ=0 e2e=0 verbose=""
  [[ $# -eq 0 ]] && { go=1; web=1; }
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --go) go=1;; --web) web=1;; --integration) integ=1;; --e2e) e2e=1;;
      --all) go=1; web=1;; -v) verbose="-v";;
      *) die "test: unknown flag $1";;
    esac; shift
  done
  local rc=0
  if [[ $go -eq 1 ]]; then
    if [[ -f "$LABD_DIR/go.mod" ]]; then
      say "go test"; (cd "$LABD_DIR" && go vet ./... && go test $verbose -race ./...) || rc=1
    else warn "labd/go.mod missing — skipping Go tests (Phase 0, task 0.2)"; rc=1; fi
  fi
  if [[ $web -eq 1 ]]; then
    if [[ -f "$WEB_DIR/pyproject.toml" ]]; then
      say "pytest"; (cd "$WEB_DIR" && uv run pytest -q ${verbose:+-v}) || rc=1
    else warn "web/pyproject.toml missing — skipping Django tests (Phase 0, task 0.3)"; rc=1; fi
  fi
  if [[ $integ -eq 1 ]]; then
    say "go integration tests (Linux)"
    run_linux bash -c "cd '$LABD_DIR' && go test $verbose -tags integration -count=1 ./integration/... ./internal/..." || rc=1
  fi
  if [[ $e2e -eq 1 ]]; then
    say "end-to-end tests (Linux)"
    run_linux bash -c "cd '$WEB_DIR' && uv run pytest -q ${verbose:+-v} tests/e2e" || rc=1
  fi
  return $rc
}

cmd_lint() {
  local rc=0
  if [[ -f "$LABD_DIR/go.mod" ]]; then
    say "gofmt / go vet"
    (cd "$LABD_DIR" && test -z "$(gofmt -l . | tee /dev/stderr)" && go vet ./...) || rc=1
    # Advisory: a staticcheck built with an older Go cannot analyse a newer stdlib and fails
    # for reasons unrelated to our code.
    if have staticcheck; then
      (cd "$LABD_DIR" && staticcheck ./...) \
        || warn "staticcheck failed; if the error mentions a Go version, update it: go install honnef.co/go/tools/cmd/staticcheck@latest"
    fi
  fi
  if [[ -f "$WEB_DIR/pyproject.toml" ]]; then
    say "ruff"; (cd "$WEB_DIR" && uv run ruff check . && uv run ruff format --check .) || rc=1
  fi
  if have shellcheck; then
    say "shellcheck"
    # shellcheck disable=SC2046
    shellcheck "$ROOT/run.sh" $(find "$ROOT/scripts" "$ROOT/deploy/scripts" "$ROOT/images" "$ROOT/labd/perf" -name '*.sh' 2>/dev/null) || rc=1
  else warn "shellcheck not installed"; fi
  return $rc
}

cmd_fmt() {
  [[ -f "$LABD_DIR/go.mod" ]] && (cd "$LABD_DIR" && gofmt -w .)
  [[ -f "$WEB_DIR/pyproject.toml" ]] && (cd "$WEB_DIR" && uv run ruff format . && uv run ruff check --fix .)
  true
}

# ---------------------------------------------------------------- run services

cmd_labd() {
  local cfg="$LABD_CONFIG"
  if [[ "${1:-}" == "--config" ]]; then cfg="$2"; shift 2; fi
  [[ -x "$LABD_DIR/bin/labd" ]] || cmd_build
  say "labd --config $cfg $*"
  run_linux "$LABD_DIR/bin/labd" --config "$cfg" "$@"
}

cmd_web() {
  local port=8000
  while [[ $# -gt 0 ]]; do case "$1" in --port) port="$2"; shift 2;; *) die "web: unknown flag $1";; esac; done
  [[ -f "$WEB_DIR/manage.py" ]] || die "web/manage.py missing — Phase 0, task 0.3"
  say "django runserver on :$port"
  (cd "$WEB_DIR" && DJANGO_SETTINGS_MODULE=config.settings.dev uv run python manage.py runserver "127.0.0.1:$port")
}

cmd_dev() {
  say "starting labd in the background"
  cmd_labd "$@" &
  local labd_pid=$!
  trap 'kill "$labd_pid" 2>/dev/null || true' EXIT INT TERM
  sleep 1
  cmd_web
}

cmd_db() {
  local sub="${1:-}"; shift || true
  case "$sub" in
    up)      run_linux sudo systemctl start postgresql ;;
    down)    run_linux sudo systemctl stop postgresql ;;
    shell)   run_linux psql "${DATABASE_URL:-postgres://labd@localhost/labs}" "$@" ;;
    migrate)
      [[ -x "$LABD_DIR/bin/labd" ]] || cmd_build
      run_linux "$LABD_DIR/bin/labd" --config "$LABD_CONFIG" migrate
      [[ -f "$WEB_DIR/manage.py" ]] && (cd "$WEB_DIR" && uv run python manage.py migrate) ;;
    reset)
      say "dropping and recreating database labs (dev only)"
      run_linux sudo -u postgres psql -c 'DROP DATABASE IF EXISTS labs;' -c 'CREATE DATABASE labs OWNER labd;'
      cmd_db migrate ;;
    *) die "db: up|down|shell|migrate|reset" ;;
  esac
}

# ---------------------------------------------------------------- images / perf / gate / deploy

cmd_images() {
  local what="${1:-}"; shift || true
  case "$what" in
    labbase|perf|build)
      [[ -x "$ROOT/images/$what/build.sh" ]] || die "images/$what/build.sh missing — see the phase document"
      bash "$ROOT/images/$what/build.sh" "$@" ;;
    challenge)
      local dir="${1:?images challenge <dir>}"; shift
      [[ -x "$ROOT/scripts/challenge-build.sh" ]] || die "scripts/challenge-build.sh missing — Phase 5"
      bash "$ROOT/scripts/challenge-build.sh" "$dir" "$@" ;;
    all) cmd_images build; cmd_images labbase; cmd_images perf ;;
    *) die "images: labbase|perf|build|all|challenge <dir>" ;;
  esac
}

cmd_perf() {
  local scenario="" n=10 hold="20m" ramp=5 runtime="runsc" out="$METRICS_DIR"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --scenario) scenario="$2"; shift 2;; --n) n="$2"; shift 2;; --hold) hold="$2"; shift 2;;
      --ramp) ramp="$2"; shift 2;; --runtime) runtime="$2"; shift 2;; --out) out="$2"; shift 2;;
      *) die "perf: unknown flag $1";;
    esac
  done
  [[ -n "$scenario" ]] || die "perf: --scenario P0..P9 is required"
  case "$scenario" in
    P0) [[ -x "$LABD_DIR/perf/p0/p0.sh" ]] || die "labd/perf/p0/p0.sh missing — Phase 1"
        run_linux bash "$LABD_DIR/perf/p0/p0.sh" --runtime "$runtime" --out "$out" ;;
    P1|P2|P3|P4|P5|P6|P7|P8|P9)
        [[ -x "$LABD_DIR/bin/labd-perf" ]] || cmd_build
        run_linux "$LABD_DIR/bin/labd-perf" run --scenario "$scenario" --n "$n" --hold "$hold" \
          --ramp "$ramp" --runtime "$runtime" --out "$out" ;;
    *) die "perf: unknown scenario $scenario" ;;
  esac
}

cmd_gate() {
  local phase=""
  while [[ $# -gt 0 ]]; do case "$1" in --phase) phase="$2"; shift 2;; *) die "gate: unknown flag $1";; esac; done
  [[ "$phase" =~ ^[0-8]$ ]] || die "gate: --phase 0..8 is required"
  bash "$ROOT/scripts/gate.sh" "$phase"
}

cmd_deploy() {
  [[ -x "$ROOT/scripts/deploy.sh" ]] || die "scripts/deploy.sh missing — Phase 8"
  bash "$ROOT/scripts/deploy.sh" "$@"
}

# ---------------------------------------------------------------- dispatch

main() {
  local cmd="${1:-help}"; shift || true
  case "$cmd" in
    help|-h|--help) usage ;;
    doctor)  cmd_doctor "$@" ;;
    check)   cmd_check "$@" ;;
    vm)      cmd_vm "$@" ;;
    build)   cmd_build "$@" ;;
    test)    cmd_test "$@" ;;
    lint)    cmd_lint "$@" ;;
    fmt)     cmd_fmt "$@" ;;
    labd)    cmd_labd "$@" ;;
    web)     cmd_web "$@" ;;
    dev)     cmd_dev "$@" ;;
    db)      cmd_db "$@" ;;
    images)  cmd_images "$@" ;;
    perf)    cmd_perf "$@" ;;
    gate)    cmd_gate "$@" ;;
    deploy)  cmd_deploy "$@" ;;
    *) usage; die "unknown command: $cmd" ;;
  esac
}

main "$@"
