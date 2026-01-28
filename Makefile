# Copyright © 2026 Dell Inc. or its subsidiaries. All Rights Reserved.
#
# Dell Technologies, Dell and other trademarks are trademarks of Dell Inc.
# or its subsidiaries. Other trademarks may be trademarks of their respective 
# owners.

include images.mk

.PHONY: all
all: build

# This will be overridden during image build.
IMAGE_VERSION ?= 0.0.0
LDFLAGS = "-X main.ManifestSemver=$(IMAGE_VERSION)"

UNIT_TESTED_PACKAGES := \
	github.com/dell/csi-unity \
	github.com/dell/csi-unity/k8sutils \
	github.com/dell/csi-unity/provider \
	github.com/dell/csi-unity/service \
	github.com/dell/csi-unity/service/csiutils \
	github.com/dell/csi-unity/service/logging

build:
	git config core.hooksPath hooks
	cd core && go generate
	CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build -ldflags $(LDFLAGS) -mod=vendor .

unit-test:
	go clean -cache
	@for pkg in $(UNIT_TESTED_PACKAGES); do \
  		echo "****** go test -v -short -race -count=1 -cover -coverprofile cover.out $$pkg ******"; \
		go test -v -short -race -count=1 -cover -coverprofile cover.out $$pkg; \
	done

# Integration tests using Godog. Populate env.sh with the hardware parameters
integration-test:
	( cd test/integration-test; sh run.sh )

# BDD tests using Godog. Populate env.sh with the hardware parameters
bdd-test:
	( cd test/bdd-test; sh run.sh )

.PHONY: clean
clean:
	rm -rf core/core_generated.go vendor csm-temp-repo csm-common.mk
	go clean

.PHONY: integ-test
integ-test: build
	go test -v ./test/...

check:
	sh scripts/check.sh
