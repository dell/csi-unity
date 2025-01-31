# Includes the following generated file to get semantic version information
include semver.mk
ifdef NOTES
	RELNOTE="-$(NOTES)"
else
	RELNOTE=
endif

# local build, use user and timestamp it
NAME:=csi-unity
DOCKER_IMAGE_NAME ?= ${NAME}-${USER}
VERSION:=$(shell  date +%Y%m%d%H%M%S)
BIN_DIR:=bin
BIN_NAME:=${NAME}
DOCKER_REPO ?= dellemc
DOCKER_NAMESPACE ?= csi-unity
DOCKER_IMAGE_TAG ?= ${VERSION}

.PHONY: docker-build
docker-build:
	echo ${VERSION} ${GITLAB_CI} ${CI_COMMIT_TAG} ${CI_COMMIT_SHA}
	rm -f core/core_generated.go
	cd core && go generate
	go run core/semver/semver.go -f mk >semver.mk
	mkdir -p ${BIN_DIR}
	GOOS=linux CGO_ENABLED=0 GOARCH=amd64 go build -ldflags '-extldflags "-static"' -o ${BIN_DIR}/${BIN_NAME}
	docker build --pull -t ${DOCKER_IMAGE_NAME}:${DOCKER_IMAGE_TAG} --build-arg GOPROXY=$(GOPROXY) --build-arg BASEIMAGE=$(CSM_BASEIMAGE) --build-arg GOIMAGE=$(DEFAULT_GOIMAGE) .
	docker tag ${DOCKER_IMAGE_NAME}:${DOCKER_IMAGE_TAG} ${DOCKER_REPO}/${DOCKER_NAMESPACE}/${DOCKER_IMAGE_NAME}:${DOCKER_IMAGE_TAG}

.PHONY: docker-push
docker-push: docker-build
	docker push ${DOCKER_REPO}/${DOCKER_NAMESPACE}/${DOCKER_IMAGE_NAME}:${DOCKER_IMAGE_TAG}

version:
	@echo "MAJOR $(MAJOR) MINOR $(MINOR) PATCH $(PATCH) BUILD ${BUILD} TYPE ${TYPE} RELNOTE $(RELNOTE) SEMVER $(SEMVER)"
	@echo "Target Version: $(VERSION)"

