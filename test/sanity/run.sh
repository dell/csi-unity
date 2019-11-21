#!/bin/sh
rm -rf /tmp/csi-mount
csi-sanity --ginkgo.v --test.short \
	--csi.endpoint=$PWD/unix_sock \
	--csi.secrets=secrets.yaml \
	--ginkgo.skip "GetCapacity|create a volume with already existing name and different capacity"