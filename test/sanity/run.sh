#!/bin/sh

rm -rf /tmp/csi-mount

csi-sanity --ginkgo.v \
        --csi.endpoint=$(pwd)/unix_sock \
        --csi.secrets=secrets.yaml \
        --ginkgo.skip "GetCapacity|ListSnapshots|create a volume with already existing name and different capacity" \
