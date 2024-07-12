# Dockerfile to build Unity CSI Driver
ARG BASEIMAGE

FROM $BASEIMAGE

# validate some cli utilities are found
RUN which mkfs.ext4
RUN which mkfs.xfs

COPY "bin/csi-unity" /
COPY "scripts/run.sh" /

RUN chmod 777 /run.sh

ENTRYPOINT ["/run.sh"]