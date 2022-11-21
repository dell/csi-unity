/*
 Copyright © 2019 Dell Inc. or its subsidiaries. All Rights Reserved.

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

package service

const (
	// EnvEndpoint is the name of the environment variable used to set the
	// HTTP endpoint of the Unity XT Gateway
	EnvEndpoint = "X_CSI_UNITY_ENDPOINT"

	// EnvNodeName is the name of the enviroment variable used to set the
	// hostname where the node service is running
	EnvNodeName = "X_CSI_UNITY_NODENAME"

	// EnvAutoProbe is the name of the environment variable used to specify
	// that the controller service should automatically probe itself if it
	// receives incoming requests before having been probed, in direct
	// violation of the CSI spec
	EnvAutoProbe = "X_CSI_UNITY_AUTOPROBE"

	//EnvPvtMountDir is required to Node Unstage volume where the volume has been mounted
	//as a global mount via CSI-Unity v1.0 or v1.1
	EnvPvtMountDir = "X_CSI_PRIVATE_MOUNT_DIR"

	//EnvEphemeralStagingPath - Ephemeral staging path
	EnvEphemeralStagingPath = "X_CSI_EPHEMERAL_STAGING_PATH"

	// EnvISCSIChroot is the path to which the driver will chroot before
	// running any iscsi commands. This value should only be set when instructed
	// by technical support.
	EnvISCSIChroot = "X_CSI_ISCSI_CHROOT"

	// EnvKubeConfigPath indicates kubernetes configuration that has to be used by CSI Driver
	EnvKubeConfigPath = "KUBECONFIG"

	//SyncNodeInfoTimeInterval - Time interval to add node info to array. Default 60 minutes.
	//X_CSI_UNITY_SYNC_NODEINFO_INTERVAL has been deprecated and will be removes in a future release
	SyncNodeInfoTimeInterval = "X_CSI_UNITY_SYNC_NODEINFO_INTERVAL"

	// EnvAllowRWOMultiPodAccess - Environment variable to configure sharing of a single volume across multiple pods within the same node
	// Multi-node access is still not allowed for ReadWriteOnce Mount volumes.
	// Enabling this option techincally violates the CSI 1.3 spec in the NodePublishVolume stating the required error returns.
	//X_CSI_UNITY_ALLOW_MULTI_POD_ACCESS has been deprecated and will be removes in a future release
	EnvAllowRWOMultiPodAccess = "X_CSI_UNITY_ALLOW_MULTI_POD_ACCESS"

	// EnvIsVolumeHealthMonitorEnabled - Environment variable to enable/disable health monitor of CSI volumes
	EnvIsVolumeHealthMonitorEnabled = "X_CSI_HEALTH_MONITOR_ENABLED"
)
