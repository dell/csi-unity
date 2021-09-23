package constants

/*
 Copyright (c) 2019 Dell Inc, or its subsidiaries.
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at
      http://www.apache.org/licenses/LICENSE-2.0
 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

const (

	// ParamCSILogLevel csi driver log level
	ParamCSILogLevel = "CSI_LOG_LEVEL"

	// ParamAllowRWOMultiPodAccess to enable multi pod access for RWO volume
	ParamAllowRWOMultiPodAccess = "ALLOW_RWO_MULTIPOD_ACCESS"

	// ParamMaxUnityVolumesPerNode defines maximum number of unity volumes per node
	ParamMaxUnityVolumesPerNode = "MAX_UNITY_VOLUMES_PER_NODE"

	// ParamSyncNodeInfoTimeInterval defines time interval to sync node info
	ParamSyncNodeInfoTimeInterval = "SYNC_NODE_INFO_TIME_INTERVAL"
)
