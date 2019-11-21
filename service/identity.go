package service

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/core"
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"strings"
)

func (s *service) Probe(
	ctx context.Context,
	req *csi.ProbeRequest) (
	*csi.ProbeResponse, error) {
	glog.V(5).Infof("Using probe")
	if !strings.EqualFold(s.mode, "node") {
		if err := s.controllerProbe(ctx); err != nil {
			return nil, err
		}
	}
	if !strings.EqualFold(s.mode, "controller") {
		if err := s.nodeProbe(ctx); err != nil {
			return nil, err
		}
	}
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

	glog.V(5).Infof("Using default capabilities")
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		},
	}, nil
}
