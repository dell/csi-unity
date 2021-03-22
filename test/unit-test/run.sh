#!/bin/sh
# This will run coverage analysis using the integration testing.
# The env.sh must point to a valid Unity Array# on this system.

rm -f /root/go/bin/csi.sock
source ../../env.sh
echo $SDC_GUID
go test -v -coverprofile=c.out -timeout 60m -coverpkg=github.com/dell/csi-unity/service *test.go &
wait 
go tool cover -html=c.out