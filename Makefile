NAME:=csi-unity

.PHONY: all
all: go-build

ifneq (on,$(GO111MODULE))
export GO111MODULE := on
endif

IMAGE_NAME=csi-unity
IMAGE_REGISTRY=dellemc
DEFAULT_IMAGE_TAG=$(shell date +%Y%m%d%H%M%S)
ifeq ($(IMAGETAG),)
export IMAGETAG="$(DEFAULT_IMAGE_TAG)"
endif

.PHONY: go-vendor
go-vendor:
	go mod vendor

.PHONY: go-build
go-build: clean
	git config core.hooksPath hooks
	rm -f core/core_generated.go
	cd core && go generate
	go build .

# Only unit testing service/csiutils and service/logging for now. More work to do but need to start somewhere.
unit-test:
	( cd service && go clean -cache && go test -v -coverprofile=c.out ./csiutils/... ./logging/... )

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
	podman build --pull -t $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGETAG) --build-arg GOIMAGE=$(DEFAULT_GOIMAGE) --build-arg BASEIMAGE=$(CSM_BASEIMAGE) --build-arg GOPROXY=$(GOPROXY) . --format=docker

podman-build-no-cache: download-csm-common go-build
	$(eval include csm-common.mk)
	podman build --pull --no-cache -t $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGETAG) --build-arg GOIMAGE=$(DEFAULT_GOIMAGE) --build-arg BASEIMAGE=$(CSM_BASEIMAGE) --build-arg GOPROXY=$(GOPROXY) . --format=docker

podman-push:
	podman push $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGETAG)

#
# Docker-related tasks
#
# Generates the docker container (but does not push)
docker-build: download-csm-common
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

.PHONY: actions action-help
actions: ## Run all GitHub Action checks that run on a pull request creation
	@echo "Running all GitHub Action checks for pull request events..."
	@act -l | grep -v ^Stage | grep pull_request | grep -v image_security_scan | awk '{print $$2}' | while read WF; do \
		echo "Running workflow: $${WF}"; \
		act pull_request --no-cache-server --platform ubuntu-latest=ghcr.io/catthehacker/ubuntu:act-latest --job "$${WF}"; \
	done

action-help: ## Echo instructions to run one specific workflow locally
	@echo "GitHub Workflows can be run locally with the following command:"
	@echo "act pull_request --no-cache-server --platform ubuntu-latest=ghcr.io/catthehacker/ubuntu:act-latest --job <jobid>"
	@echo ""
	@echo "Where '<jobid>' is a Job ID returned by the command:"
	@echo "act -l"
	@echo ""
	@echo "NOTE: if act is not installed, it can be downloaded from https://github.com/nektos/act"