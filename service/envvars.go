package service

const (
	// EnvEndpoint is the name of the environment variable used to set the
	// HTTP endpoint of the Unity Gateway
	EnvEndpoint = "X_CSI_UNITY_ENDPOINT"

	// EnvUser is the name of the environment variable used to set the
	// username when authenticating to the Unity Gateway
	EnvUser = "X_CSI_UNITY_USER"

	// EnvPassword is the name of the environment variable used to set the
	// user's password when authenticating to the Unity Gateway
	EnvPassword = "X_CSI_UNITY_PASSWORD"

	// EnvInsecure is the name of the enviroment variable used to specify
	// that the Unity Gateway's certificate chain and host name should not
	// be verified
	EnvInsecure = "X_CSI_UNITY_INSECURE"

	// EnvSystemName is the name of the enviroment variable used to set the
	// name of the Unity system to interact with
	EnvSystemName = "X_CSI_UNITY_SYSTEMNAME"

	// EnvNodeName is the name of the enviroment variable used to set the
	// hostname where the node service is running
	EnvNodeName = "X_CSI_UNITY_NODENAME"

	// EnvAutoProbe is the name of the environment variable used to specify
	// that the controller service should automatically probe itself if it
	// receives incoming requests before having been probed, in direct
	// violation of the CSI spec
	EnvAutoProbe = "X_CSI_UNITY_AUTOPROBE"

	//EnvPvtMountDir is required to Node Publish a volume where the volume will be mounted
	//inside this directory as a private mount
	EnvPvtMountDir = "X_CSI_PRIVATE_MOUNT_DIR"
)
