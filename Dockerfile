ARG GOIMAGE
ARG BASEIMAGE
ARG GOPROXY

# Stage to build the driver
FROM $GOIMAGE as builder
RUN mkdir -p /go/src
COPY ./ /go/src/csi-unity

WORKDIR /go/src/csi-unity
RUN mkdir -p bin
RUN go generate
RUN GOOS=linux CGO_ENABLED=0 GOARCH=amd64 go build -ldflags '-extldflags "-static"' -o bin/csi-unity
# Print the version
RUN go run core/semver/semver.go -f mk


# Dockerfile to build Unity CSI Driver
# Fetching the base ubi micro image with the require packges committed using buildah
FROM $BASEIMAGE as driver

COPY --from=builder /go/src/csi-unity/bin/csi-unity /
COPY scripts/run.sh /
RUN chmod 777 /run.sh
ENTRYPOINT ["/run.sh"]

# final stage
FROM driver as final

LABEL vendor="Dell Technologies" \
      maintainer="Dell Technologies" \
      name="csi-unity" \
      summary="CSI Driver for Dell Unity XT" \
      description="CSI Driver for provisioning persistent storage from Dell Unity XT" \
      release="1.14.0" \
      version="2.14.0" \
      license="Apache-2.0"
COPY licenses /licenses
