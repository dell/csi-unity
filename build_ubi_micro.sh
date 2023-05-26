#!/bin/bash
microcontainer=$(buildah from $1)
micromount=$(buildah mount $microcontainer)
dnf install --installroot $micromount --releasever=8 --nodocs --setopt install_weak_deps=false --setopt=reposdir=/etc/yum.repos.d/ e2fsprogs which xfsprogs device-mapper-multipath util-linux -y
dnf clean all --installroot $micromount
buildah umount $microcontainer
buildah commit $microcontainer csiunity-ubimicro