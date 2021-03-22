# Dockerfile to build Unity CSI Driver
FROM centos:7.6.1810

# dependencies, following by cleaning the cache
RUN yum install -y e2fsprogs xfsprogs which nfs-utils device-mapper-multipath \
    && \
    yum clean all \
    && \
    rm -rf /var/cache/run

# validate some cli utilities are found
RUN which mkfs.ext4
RUN which mkfs.xfs

COPY "bin/csi-unity" /
COPY "scripts/run.sh" /

RUN chmod 777 /run.sh

ENTRYPOINT ["/run.sh"]