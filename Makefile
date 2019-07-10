BINARY_NAME=mqhammer
BUILD_DIR ?= build
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
DOCKER_IMAGE=rollulus/mq-hammer

# get version info from git's tags
GIT_COMMIT := $(shell git rev-parse HEAD)
GIT_TAG := $(shell git describe --tags --dirty --always 2>/dev/null)
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null)
DOCKER_TAG := $(shell echo ${GIT_TAG} | tr -d 'v' | sed 's/dirty/SNAPSHOT/g' )

# inject version info into version vars
LD_RELEASE_FLAGS += -X github.com/rollulus/mq-hammer/pkg/hammer.GitCommit=${GIT_COMMIT}
LD_RELEASE_FLAGS += -X github.com/rollulus/mq-hammer/pkg/hammer.GitTag=${GIT_TAG}
LD_RELEASE_FLAGS += -X github.com/rollulus/mq-hammer/pkg/hammer.SemVer=${VERSION}

.PHONY: build

all: test build
dockerbin:
		GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LD_RELEASE_FLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) -v
build:
		$(GOBUILD) -ldflags "$(LD_RELEASE_FLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) -v
test:
		$(GOTEST) -v ./...
clean:
	rm -rf $(BUILD_DIR)

bootstrap:
	dep version || go get -u github.com/golang/dep/cmd/dep
	dep ensure

docker: dockerbin
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
docker_push: docker
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

github_release:
	goreleaser -v || go get github.com/goreleaser/goreleaser
	rm -rf ./build/goreleaser-dist
	./scripts/goreleaser.yaml.sh "$(LD_RELEASE_FLAGS)" >./build/gorel.yaml
	goreleaser --config ./build/gorel.yaml
