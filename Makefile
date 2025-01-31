NAME:=csi-unity

.PHONY: all
all: go-build

ifneq (on,$(GO111MODULE))
export GO111MODULE := on
endif

IMAGE_NAME=csi-unity
IMAGE_REGISTRY=dellemc
IMAGE_TAG=$(shell date +%Y%m%d%H%M%S)

.PHONY: go-vendor
go-vendor:
	go mod vendor

.PHONY: go-build
go-build: clean
	git config core.hooksPath hooks
	rm -f core/core_generated.go
	cd core && go generate
	go build .

# Only unit testing utils for now. More work to do but need to start somewhere.
unit-test:
	( cd service/utils; go clean -cache; go test -v -coverprofile=c.out ./... )

# Integration tests using Godog. Populate env.sh with the hardware parameters
integration-test:
	( cd test/integration-test; sh run.sh )

# BDD tests using Godog. Populate env.sh with the hardware parameters
bdd-test:
	( cd test/bdd-test; sh run.sh )

.PHONY: download-csm-common
download-csm-common:
	curl -O -L https://raw.githubusercontent.com/dell/csm/main/config/csm-common.mk

#
# Docker-related tasks
#
# Generates the docker container (but does not push)
podman-build: download-csm-common go-build
	$(eval include csm-common.mk)
	podman build --pull -t $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) --build-arg GOIMAGE=$(DEFAULT_GOIMAGE) --build-arg BASEIMAGE=$(CSM_BASEIMAGE) --build-arg GOPROXY=$(GOPROXY) . --format=docker

podman-build-no-cache: download-csm-common go-build
	$(eval include csm-common.mk)
	podman build --pull --no-cache -t $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) --build-arg GOIMAGE=$(DEFAULT_GOIMAGE) --build-arg BASEIMAGE=$(CSM_BASEIMAGE) --build-arg GOPROXY=$(GOPROXY) . --format=docker

podman-push:
	podman push $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

#
# Docker-related tasks
#
# Generates the docker container (but does not push)
docker-build: download-csm-common go-build
	cd core && go generate
	go run core/semver/semver.go -f mk >semver.mk
	make -f docker.mk docker-build

docker-push:
	make -f docker.mk docker-push

version:
	go generate
	go run core/semver/semver.go -f mk >semver.mk
	make -f docker.mk version

.PHONY: clean
clean:
	rm -f core/core_generated.go
	go clean

#
# Tests-related tasks
.PHONY: integ-test
integ-test: go-build
	go test -v ./test/...

check:
	sh scripts/check.sh
