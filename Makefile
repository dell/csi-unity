NAME:=csi-unity

.PHONY: all
all: go-build

ifneq (on,$(GO111MODULE))
export GO111MODULE := on
endif

.PHONY: go-vendor
go-vendor:
	git config core.hooksPath hooks
	go mod vendor

.PHONY: go-build
go-build: go-vendor
	rm -f core/core_generated.go
	cd core && go generate
	go build .

# Integration tests using Godog. Populate env.sh with the hardware parameters
integration-test:
	( cd test/integration-test; sh run.sh )

# Unit tests using Godog. Populate env.sh with the hardware parameters
unit-test:
	( cd test/unit-test; sh run.sh )

#
# Docker-related tasks
#
# Generates the docker container (but does not push)
docker-build: go-build
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
	rm -rf bin
	rm -f core/core_generated.go
	go clean

#
# Tests-related tasks
.PHONY: integ-test
integ-test: go-build
	go test -v ./test/...
