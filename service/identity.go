/*
 Copyright © 2019-2026 Dell Inc. or its subsidiaries. All Rights Reserved.

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

import (
	"context"
	"strings"

	"github.com/dell/csi-unity/service/logging"
	"github.com/container-storage-interface/spec/lib/go/csi"
)

func (s *service) Probe(
	ctx context.Context,
	req *csi.ProbeRequest) (
	*csi.ProbeResponse, error,
) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Infof("Executing Probe with args: %+v", logging.LogRequestFields(req))
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
	_ context.Context,
	_ *csi.GetPluginInfoRequest) (
	*csi.GetPluginInfoResponse, error,
) {
	Manifest["semver"] = ManifestSemver

	return &csi.GetPluginInfoResponse{
		Name:          Name,
		VendorVersion: ManifestSemver,
		Manifest:      Manifest,
	}, nil
}

func (s *service) GetPluginCapabilities(
	ctx context.Context,
	req *csi.GetPluginCapabilitiesRequest) (
	*csi.GetPluginCapabilitiesResponse, error,
) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Infof("Executing GetPluginCapabilities with args: %+v", logging.LogRequestFields(req))
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
