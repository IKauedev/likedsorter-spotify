BINARY  := likedsorter

# No Windows o executável precisa de .exe e o shell do make (cmd) não entende /dev/null.
ifeq ($(OS),Windows_NT)
EXE     := .exe
NULL    := NUL
RUNBIN  := bin\$(BINARY).exe
else
EXE     :=
NULL    := /dev/null
RUNBIN  := bin/$(BINARY)
endif

VERSION ?= $(shell git describe --tags --always --dirty 2>$(NULL) || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test test-norace cover cover-check vet fmt fmt-check lint tidy-check ci run clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY)$(EXE) ./cmd/likedsorter

# Compila e instala no PC (copia para a pasta do usuário e ajusta o PATH).
install: build
	$(RUNBIN) install

# -race exige cgo (gcc/clang). No Windows sem MinGW use test-norace; o CI roda com -race.
test:
	go test -race -count=1 ./...

test-norace:
	go test -count=1 ./...

cover:
	go test -count=1 -coverprofile=coverage.txt ./...
	go tool cover -func=coverage.txt | tail -n 1

# Mínimo de 70% em grouping, planner e cache.
cover-check:
	bash scripts/check-coverage.sh 70

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || { echo "arquivos sem gofmt:"; gofmt -l .; exit 1; }

lint: vet
	golangci-lint run ./...

tidy-check:
	go mod tidy
	@git diff --exit-code -- go.mod go.sum || { echo "go.mod/go.sum desatualizados: rode go mod tidy"; exit 1; }

# Tudo que o CI verifica (menos o build multiplataforma).
ci: fmt-check vet lint test cover-check

run:
	go run ./cmd/likedsorter $(ARGS)

clean:
	rm -rf bin dist coverage.txt
