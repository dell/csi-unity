#!/bin/bash
# This script will build an image for the Unity CSI Driver
# Before running this script, make sure that you have podman installed on your system
# If you are going to push the image to an image repo, make sure that you are logged in
# sh build.sh: build the image
# sh build.sh -p: build and push the image

function git_version {
   local gitdesc=$(git describe --long)
   local version="${gitdesc%%-*}"
   MAJOR_VERSION=$(echo $version | cut -d. -f1)
   MINOR_VERSION=$(echo $version | cut -d. -f2)
   PATCH_NUMBER=$(echo $version | cut -d. -f3)
   BUILD_NUMBER_FROM_GIT=$(sed -e 's#.*-\(\)#\1#' <<< "${gitdesc%-*}")
   echo MAJOR_VERSION=$MAJOR_VERSION MINOR_VERSION=$MINOR_VERSION PATCH_NUMBER=$PATCH_NUMBER BUILD_NUMBER_FROM_GIT=$BUILD_NUMBER_FROM_GIT
   echo Target Version=$VERSION
}

function build_image {
   echo $BUILDCMD build -t ${IMAGE_NAME}:${IMAGE_TAG} .
   (cd .. && $BUILDCMD build -t ${IMAGE_NAME}:${IMAGE_TAG} --build-arg GOPROXY=$GOPROXY -f csi-unity/Dockerfile.podman . --format=docker)
   echo $BUILDCMD tag ${IMAGE_NAME}:${IMAGE_TAG} ${IMAGE_REPO}/${IMAGE_REPO_NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}
   $BUILDCMD tag ${IMAGE_NAME}:${IMAGE_TAG} ${IMAGE_REPO}/${IMAGE_REPO_NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}
}

function push_image {
   echo $BUILDCMD push ${IMAGE_REPO}/${IMAGE_REPO_NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}
   $BUILDCMD push ${IMAGE_REPO}/${IMAGE_REPO_NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}
}

NAME=csi-unity
IMAGE_NAME=${NAME}-${USER}
VERSION=$(date +%Y%m%d%H%M%S)
BIN_DIR=bin
BIN_NAME=${NAME}
IMAGE_REPO=amaas-eos-mw1.cec.lab.emc.com:5028
IMAGE_REPO_NAMESPACE=csi-unity
IMAGE_TAG=${VERSION}

# Read options
while getopts 'ph' flag; do
  case "${flag}" in
    p) PUSH_IMAGE='true' ;;
    h) git_version
       exit 0 ;;
    *) git_version
       exit 0 ;;
  esac
done

BUILDCMD="podman"
DOCKEROPT="--format=docker"
set -e

command -v podman
if [ $? -eq 0 ]; then
    echo "Using podman for building image"
else
    echo "podman must be installed for building UBI based image"
    exit 1
fi

# Build the image
build_image

if [ "$PUSH_IMAGE" = true ]; then
    push_image
fi

exit 0