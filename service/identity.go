package service

import (
	csiext "github.com/dell/dell-csi-extensions/replication"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/core"
	"golang.org/x/net/context"
)

func (s *service) Probe(
	ctx context.Context,
	req *csi.ProbeRequest) (
	*csi.ProbeResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Infof("Executing Probe with args: %+v", *req)
	if strings.EqualFold(s.mode, "controller") {
		if err := s.controllerProbe(ctx, ""); err != nil {
			log.Error("Identity probe failed:", err)
			return nil, err
		}
	}
	if strings.EqualFold(s.mode, "node") {
		if err := s.nodeProbe(ctx, ""); err != nil {
			log.Error("Identity probe failed:", err)
			return nil, err
		}
	}
	log.Info("Identity probe success")
	return &csi.ProbeResponse{}, nil
}

func (s *service) GetPluginInfo(
	ctx context.Context,
	req *csi.GetPluginInfoRequest) (
	*csi.GetPluginInfoResponse, error) {

	return &csi.GetPluginInfoResponse{
		Name:          Name,
		VendorVersion: core.SemVer,
		Manifest:      Manifest,
	}, nil
}

func (s *service) GetPluginCapabilities(
	ctx context.Context,
	req *csi.GetPluginCapabilitiesRequest) (
	*csi.GetPluginCapabilitiesResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Infof("Executing GetPluginCapabilities with args: %+v", *req)
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_VolumeExpansion_{
					VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
						Type: csi.PluginCapability_VolumeExpansion_ONLINE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_VolumeExpansion_{
					VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
						Type: csi.PluginCapability_VolumeExpansion_OFFLINE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
		},
	}, nil
}

func (s *service) GetReplicationCapabilities(ctx context.Context, req *csiext.GetReplicationCapabilityRequest) (*csiext.GetReplicationCapabilityResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Infof("Executing GetReplicationCapabilities with args: %+v", *req)
	return &csiext.GetReplicationCapabilityResponse{
		Capabilities: []*csiext.ReplicationCapability{
			{
				Type: &csiext.ReplicationCapability_Rpc{
					Rpc: &csiext.ReplicationCapability_RPC{
						Type: csiext.ReplicationCapability_RPC_CREATE_REMOTE_VOLUME,
					},
				},
			},
			{
				Type: &csiext.ReplicationCapability_Rpc{
					Rpc: &csiext.ReplicationCapability_RPC{
						Type: csiext.ReplicationCapability_RPC_CREATE_PROTECTION_GROUP,
					},
				},
			},
			{
				Type: &csiext.ReplicationCapability_Rpc{
					Rpc: &csiext.ReplicationCapability_RPC{
						Type: csiext.ReplicationCapability_RPC_DELETE_PROTECTION_GROUP,
					},
				},
			},
			{
				Type: &csiext.ReplicationCapability_Rpc{
					Rpc: &csiext.ReplicationCapability_RPC{
						Type: csiext.ReplicationCapability_RPC_REPLICATION_ACTION_EXECUTION,
					},
				},
			},
			{
				Type: &csiext.ReplicationCapability_Rpc{
					Rpc: &csiext.ReplicationCapability_RPC{
						Type: csiext.ReplicationCapability_RPC_MONITOR_PROTECTION_GROUP,
					},
				},
			},
		},
		Actions: []*csiext.SupportedActions{
			{
				Actions: &csiext.SupportedActions_Type{
					Type: csiext.ActionTypes_FAILOVER_REMOTE,
				},
			},
			{
				Actions: &csiext.SupportedActions_Type{
					Type: csiext.ActionTypes_UNPLANNED_FAILOVER_LOCAL,
				},
			},
			{
				Actions: &csiext.SupportedActions_Type{
					Type: csiext.ActionTypes_REPROTECT_LOCAL,
				},
			},
			{
				Actions: &csiext.SupportedActions_Type{
					Type: csiext.ActionTypes_SUSPEND,
				},
			},
			{
				Actions: &csiext.SupportedActions_Type{
					Type: csiext.ActionTypes_RESUME,
				},
			},
			{
				Actions: &csiext.SupportedActions_Type{
					Type: csiext.ActionTypes_SYNC,
				},
			},
		},
	}, nil
}