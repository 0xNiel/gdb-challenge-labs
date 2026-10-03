# Thin alias layer over ./run.sh. Flags go through variables:
#   make perf SCENARIO=P2 N=100 HOLD=20m RUNTIME=runsc
#   make gate PHASE=2
#   make vm-up ARCH=x86_64
SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

PHASE    ?= 0
SCENARIO ?= P1
N        ?= 10
HOLD     ?= 20m
RAMP     ?= 5
RUNTIME  ?= runsc
PORT     ?= 8000
ARCH     ?=
DIR      ?=

.PHONY: help lab-up lab-down lab-status doctor checkvm-up vm-verify vm-ssh vm-down vm-delete build test test-go test-web test-integration test-e2e \
        lint fmt labd web dev db-up db-shell db-migrate db-reset images-labbase images-perf images-build images-all \
        image-challenge perf gate deploy

help: ## list targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
	@echo; echo "  variables: PHASE SCENARIO N HOLD RAMP RUNTIME PORT ARCH DIR   (see ./run.sh help)"

lab-up: ## check every step and start the app at http://127.0.0.1:8000, or say what to run first
	./run.sh lab up
lab-down: ## stop the app and remove any lab still running
	./run.sh lab down
lab-status: ## what is running
	./run.sh lab status

doctor: ## check dependencies for this host; report ok / missing / how to install
	./run.sh doctor
check: ## same as doctor but exits 1 when something required is missing
	./run.sh check

vm-up: ## macOS: create/start + provision the Lima VM (ARCH=x86_64 for emulation). Linux: provision this host
	./run.sh vm up $(if $(ARCH),--arch $(ARCH),)
vm-verify: ## run a container under runsc in the VM
	./run.sh vm verify
vm-ssh: ## shell into the VM
	./run.sh vm ssh
vm-down: ## stop the VM
	./run.sh vm down
vm-delete: ## delete the VM
	./run.sh vm delete

build: ## build labd and labd-perf
	./run.sh build
test: ## Go + Django unit tests
	./run.sh test --all
test-go: ## Go unit tests
	./run.sh test --go
test-web: ## Django tests
	./run.sh test --web
test-integration: ## Go integration tests (in the VM)
	./run.sh test --integration
test-e2e: ## Playwright end-to-end (in the VM)
	./run.sh test --e2e
lint: ## gofmt, vet, ruff, shellcheck
	./run.sh lint
fmt: ## apply formatters
	./run.sh fmt

labd: ## run labd in the VM
	./run.sh labd
web: ## run Django dev server (PORT)
	./run.sh web --port $(PORT)
dev: ## labd (VM, background) + Django (host)
	./run.sh dev

db-up: ## start Postgres in the VM
	./run.sh db up
db-shell: ## psql
	./run.sh db shell
db-migrate: ## labd migrate + Django migrate
	./run.sh db migrate
db-reset: ## drop and recreate the dev database
	./run.sh db reset

images-labbase: ## build the labbase image
	./run.sh images labbase
images-perf: ## build the perf challenge image
	./run.sh images perf
images-build: ## build the gcc toolchain image
	./run.sh images build
images-all: ## build, labbase, perf
	./run.sh images all
image-challenge: ## build one challenge: DIR=challenges/tier1-c-fundamentals/01-off-by-one
	./run.sh images challenge $(DIR)

perf: ## run a perf scenario: SCENARIO N HOLD RAMP RUNTIME
	./run.sh perf --scenario $(SCENARIO) --n $(N) --hold $(HOLD) --ramp $(RAMP) --runtime $(RUNTIME)

gate: ## run the exit test for PHASE
	./run.sh gate --phase $(PHASE)

deploy: ## production deploy (on the VPS)
	./run.sh deploy
