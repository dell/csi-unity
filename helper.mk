# Copyright © 2026 Dell Inc. or its subsidiaries. All Rights Reserved.
#
# Dell Technologies, Dell and other trademarks are trademarks of Dell Inc.
# or its subsidiaries. Other trademarks may be trademarks of their respective 
# owners.

download-csm-common:
	git clone --depth 1 git@eos2git.cec.lab.emc.com:CSM/csm.git temp-repo
	cp temp-repo/config/csm-common.mk .
	rm -rf temp-repo

vendor:
	GOPRIVATE=eos2git.cec.lab.emc.com go mod vendor
