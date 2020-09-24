# Stage to build the driver
FROM golang:1.13 as builder
RUN mkdir -p /go/src
COPY csi-unity/ /go/src/csi-unity

WORKDIR /go/src/csi-unity
RUN mkdir -p bin
RUN go generate
RUN GOOS=linux CGO_ENABLED=0 GOARCH=amd64 go build -ldflags '-extldflags "-static"' -o bin/csi-unity
# Print the version
RUN go run core/semver/semver.go -f mk

# Dockerfile to build Unity CSI Driver
FROM registry.access.redhat.com/ubi7/ubi-minimal:7.8-328 as driver
# dependencies, following by cleaning the cache
RUN microdnf install -y --enablerepo=rhel-7-server-rpms e2fsprogs xfsprogs nfs-utils device-mapper-multipath \
    && \
    microdnf clean all \
    && \
    rm -rf /var/cache/run
COPY --from=builder /go/src/csi-unity/bin/csi-unity /
COPY csi-unity/scripts/run.sh /
RUN chmod 777 /run.sh
ENTRYPOINT ["/run.sh"]

# Stage to check for critical and high CVE issues via Trivy (https://github.com/aquasecurity/trivy)
# will break image build if CRITICAL issues found
# will print out all HIGH issues found
FROM driver as trivy-ubi7m
RUN microdnf install -y tar

FROM trivy-ubi7m as trivy
RUN curl https://raw.githubusercontent.com/aquasecurity/trivy/master/contrib/install.sh | sh
RUN trivy fs -s CRITICAL --exit-code 1 / && \
    trivy fs -s HIGH / && \
    trivy image --reset && \
    rm ./bin/trivy

# final stage
FROM driver as final

LABEL vendor="Dell Inc." \
      name="csi-unity" \
      summary="CSI Driver for Dell EMC Unity" \
      description="CSI Driver for provisioning persistent storage from Dell EMC Unity" \
      version="1.3.0" \
      license="Apache-2.0"
COPY csi-unity/licenses /licenses