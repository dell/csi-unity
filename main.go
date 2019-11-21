package main

import (
	"context"

	"github.com/rexray/gocsi"

	"github.com/dell/csi-unity/provider"
	"github.com/dell/csi-unity/service"
)

// main is ignored when this package is built as a go plug-in.
func main() {
	gocsi.Run(
		context.Background(),
		service.Name,
		"CSI Plugin for Dell EMC Unity Array.",
		usage,
		provider.New())
}

const usage = `    X_CSI_UNITY_ENDPOINT
        Specifies the HTTP endpoint for Unity. This parameter is
        required when running the Controller service.

        The default value is empty.

    X_CSI_UNITY_USER
        Specifies the user name when authenticating to Unity.

        The default value is admin.

    X_CSI_UNITY_PASSWORD
        Specifies the password of the user defined by X_CSI_UNITY_USER to use
        during authenticatiion to Unity. This parameter is required
        when running the Controller service.

        The default value is empty.

    X_CSI_UNITY_INSECURE
        Specifies that the Unity's hostname and certificate chain
	should not be verified.

        The default value is false.

    X_CSI_UNITY_NODENAME
        Specifies the name of the node where the Node plugin is running.

        The default value is empty.

    X_CSI_UNITY_NODENAME_PREFIX
        Specifies prefix to the the name of the node where the Node plugin is running.

        The default value is empty and this is optional.

    X_CSI_PRIVATE_MOUNT_DIR
        Specifies the private directory to which a PVC will be mounted before 
		binding the mount to the target directory.

        The default value is an empty list.
`
