#!/bin/sh
#  Copyright © 2019 Dell Inc. or its subsidiaries. All Rights Reserved.
# 
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#       http://www.apache.org/licenses/LICENSE-2.0
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#
# This will run coverage analysis using the integration testing.
# The env.sh must point to a valid Unisphere deployment and the iscsi packages must be installed
# on this system. This will make real calls to  Unisphere

rm -f unix_sock
. ../../env.sh
echo ENDPOINT $CSI_ENDPOINT
echo "Starting the csi-unity driver. You should wait until the node setup is complete before running tests."
../../csi-unity --driver-name=$DRIVER_NAME --driver-config=$DRIVER_CONFIG --driver-secret=$DRIVER_SECRET
