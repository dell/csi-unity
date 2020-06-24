package service

const (
	// EnvEndpoint is the name of the environment variable used to set the
	// HTTP endpoint of the Unity Gateway
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

	// EnvISCSIChroot is the path to which the driver will chroot before
	// running any iscsi commands. This value should only be set when instructed
	// by technical support.
	EnvISCSIChroot = "X_CSI_ISCSI_CHROOT"

	//Time interval to add node info to array. Default 60 minutes.
	SyncNodeInfoTimeInterval = "X_CSI_UNITY_SYNC_NODEINFO_INTERVAL"
)
