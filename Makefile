BINARY_NAME=mqhammer
BUILD_DIR ?= build
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test

# get version info from git's tags
GIT_COMMIT := $(shell git rev-parse HEAD)
GIT_TAG := $(shell git describe --tags --dirty --always 2>/dev/null)
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null)

# inject version info into version vars
LD_RELEASE_FLAGS += -X main.GitCommit=${GIT_COMMIT}
LD_RELEASE_FLAGS += -X main.GitTag=${GIT_TAG}
LD_RELEASE_FLAGS += -X main.SemVer=${VERSION}

.PHONY: build

all: test build
build:
		$(GOBUILD) -ldflags "$(LD_RELEASE_FLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) -v
test:
		$(GOTEST) -v ./...
clean:
	rm -rf $(BUILD_DIR)

bootstrap:
	dep version || go get -u github.com/golang/dep/cmd/dep
	dep ensure -vendor-only

github_release:
	curl -sL https://git.io/goreleaser | GIT_TAG=${GIT_TAG} bash
