package provider

/*
Copyright (c) 2019 Dell Corporation
All Rights Reserved
*/

import (
	"github.com/dell/csi-unity/service"
	"github.com/dell/gocsi"
)

// New returns a new CSI Storage Plug-in Provider.
func New() gocsi.StoragePluginProvider {
	svc := service.New()
	return &gocsi.StoragePlugin{
		Controller:                svc,
		Identity:                  svc,
		Node:                      svc,
		BeforeServe:               svc.BeforeServe,
		RegisterAdditionalServers: svc.RegisterAdditionalServers,

		EnvVars: []string{
			// Enable request validation
			gocsi.EnvVarSpecReqValidation + "=true",

			// Enable serial volume access
			gocsi.EnvVarSerialVolAccess + "=true",

			// Treat the following fields as required:
			//    * ControllerPublishVolumeRequest.NodeId
			//    * GetNodeIDResponse.NodeId
			// gocsi.EnvVarRequireNodeID + "=true",

			// Treat the following fields as required:
			//    * ControllerPublishVolumeResponse.PublishInfo
			//    * NodePublishVolumeRequest.PublishInfo
			// gocsi.EnvVarRequirePubVolInfo + "=false",
		},
	}
}
