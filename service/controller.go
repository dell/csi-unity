/*
 *
 * Copyright © 2021-2024 Dell Inc. or its subsidiaries. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

/*
 Copyright © 2019-2025 Dell Inc. or its subsidiaries. All Rights Reserved.

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
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dell/gounity/api"
	"github.com/dell/gounity/gounityutil"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/service/csiutils"
	"github.com/dell/gounity"
	types "github.com/dell/gounity/apitypes"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	// KeyStoragePool is the key used to get the storagePool name from the
	// volume create parameters map
	keyStoragePool          = "storagePool"
	keyThinProvisioned      = "thinProvisioned"
	keyDescription          = "description"
	keyDataReductionEnabled = "isDataReductionEnabled"
	keyTieringPolicy        = "tieringPolicy"
	keyHostIOLimitName      = "hostIOLimitName"
	keyArrayID              = "arrayId"
	keyProtocol             = "protocol"
	keyNasServer            = "nasServer"
	keyHostIoSize           = "hostIoSize"
)

// Constants used across module
const (
	FC                       = "FC"
	ISCSI                    = "iSCSI"
	NFS                      = "NFS"
	ProtocolUnknown          = "Unknown"
	ProtocolNFS              = int(0)
	MaxEntriesSnapshot       = 100
	MaxEntriesVolume         = 100
	NFSShareLocalPath        = "/"
	NFSShareNamePrefix       = "csishare-"
	AdditionalFilesystemSize = 1.5 * 1024 * 1024 * 1024
)

var (
	errUnknownAccessType      = "unknown access type is not Block or Mount"
	errUnknownAccessMode      = "access mode cannot be UNKNOWN"
	errIncompatibleAccessMode = "access mode should be single node reader or single node writer"
	errNoMultiNodeWriter      = "Multi-node with writer(s) only supported for block access type"
	errNoMultiNodeReader      = "Multi-node Reader access mode is only supported for block access type"
	errBlockReadOnly          = "Read Only Many access mode not supported for Block Volume"
	errBlockNFS               = "Block Volume Capability is not supported for NFS"
)

// CRParams - defines placeholder for all create volume parameters
type CRParams struct {
	VolumeName      string
	Protocol        string
	StoragePool     string
	Desciption      string
	HostIOLimitName string
	Thin            bool
	DataReduction   bool
	Size            int64
	TieringPolicy   int64
	HostIoSize      int64
}

type resourceType string

const (
	volumeType   resourceType = "volume"
	snapshotType resourceType = "snapshot"
)

func (s *service) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing CreateVolume with args: %+v", *req)
	params := req.GetParameters()
	arrayID := strings.ToLower(strings.TrimSpace(params[keyArrayID]))
	if arrayID == "" {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "ArrayId cannot be empty"))
	}
	ctx, log = setArrayIDContext(ctx, arrayID)

	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, err
	}

	protocol, storagePool, size, tieringPolicy, hostIoSize, thin, dataReduction, err := ValidateCreateVolumeRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	volName := req.GetName()
	accessibility := req.GetAccessibilityRequirements()
	preferredAccessibility := accessibility.GetPreferred()

	log.Infof("PREFERRED-->%+v", preferredAccessibility)

	desc := params[keyDescription]
	hostIOLimitName := strings.TrimSpace(params[keyHostIOLimitName])

	crParams := CRParams{
		VolumeName:      volName,
		Protocol:        protocol,
		StoragePool:     storagePool,
		Desciption:      desc,
		HostIOLimitName: hostIOLimitName,
		Thin:            thin,
		DataReduction:   dataReduction,
		Size:            size,
		TieringPolicy:   tieringPolicy,
		HostIoSize:      hostIoSize,
	}

	// Creating Volume from a volume content source
	contentSource := req.GetVolumeContentSource()
	if contentSource != nil {

		volumeSource := contentSource.GetVolume()
		if volumeSource != nil {

			sourceVolID := volumeSource.VolumeId
			log.Debugf("Cloning Volume: %s", sourceVolID)
			resp, err := s.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, unity, preferredAccessibility)
			return resp, err
		}

		snapshotSource := contentSource.GetSnapshot()
		if snapshotSource != nil {

			snapshotID := snapshotSource.SnapshotId
			log.Debugf("Create Volume from Snapshot: %s", snapshotID)

			resp, err := s.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, unity, preferredAccessibility)
			return resp, err
		}
	}

	// Create Fresh Volume
	if protocol == NFS {

		nasServer, ok := params[keyNasServer]
		if !ok {
			return nil, status.Errorf(codes.InvalidArgument, "%s", csiutils.GetMessageWithRunID(rid, "`%s` is a required parameter", keyNasServer))
		}

		// Add AdditionalFilesystemSize in size as Unity XT use this much size for metadata in filesystem
		size += AdditionalFilesystemSize

		// log all parameters used in Create File System call
		fields := map[string]interface{}{
			"storagePool":   storagePool,
			"Accessibility": accessibility,
			"contentSource": contentSource,
			"thin":          thin,
			"dataReduction": dataReduction,
			"tieringPolicy": tieringPolicy,
			"protocol":      protocol,
			"nasServer":     nasServer,
			"hostIoSize":    hostIoSize,
		}
		log.WithFields(fields).Infof("Executing Create File System with following fields")

		// Idempotency check
		filesystem, _ := unity.FindFilesystemByName(ctx, volName)
		if filesystem != nil {
			content := filesystem.FileContent
			if int64(content.SizeTotal) /* #nosec G115 -- This is a false positive */ == size && content.NASServer.ID == nasServer && content.Pool.ID == storagePool {
				log.Info("Filesystem exists in the requested state with same size, NAS server and storage pool")
				filesystem.FileContent.SizeTotal -= AdditionalFilesystemSize
				return csiutils.GetVolumeResponseFromFilesystem(filesystem, arrayID, protocol, preferredAccessibility), nil
			}
			log.Info("'Filesystem name' already exists and size/NAS server/storage pool is different")
			return nil, status.Error(codes.AlreadyExists, csiutils.GetMessageWithRunID(rid, "'Filesystem name' already exists and size/NAS server/storage pool is different."))

		}

		log.Debug("Filesystem does not exist, proceeding to create new filesystem")
		// Hardcoded ProtocolNFS to 0 in order to support only NFS
		resp, err := unity.CreateFilesystem(ctx, volName, storagePool, desc, nasServer, uint64(size), int(tieringPolicy), int(hostIoSize), ProtocolNFS, thin, dataReduction) // #nosec G115 - This is a false positive
		// Add method to create filesystem
		if err != nil {
			log.Debugf("Filesystem create response:%v Error:%v", resp, err)
			return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Create Filesystem %s failed with error: %v", volName, err))
		}

		resp, err = unity.FindFilesystemByName(ctx, volName)
		if err != nil {
			log.Debugf("Find Filesystem response: %v Error: %v", resp, err)
		}

		if resp != nil {
			resp.FileContent.SizeTotal -= AdditionalFilesystemSize
			filesystemResp := csiutils.GetVolumeResponseFromFilesystem(resp, arrayID, protocol, preferredAccessibility)
			return filesystemResp, nil
		}
	} else {
		// log all parameters used in CreateVolume call
		fields := map[string]interface{}{
			"storagePool":     storagePool,
			"Accessibility":   accessibility,
			"contentSource":   contentSource,
			"thin":            thin,
			"dataReduction":   dataReduction,
			"tieringPolicy":   tieringPolicy,
			"protocol":        protocol,
			"hostIOLimitName": hostIOLimitName,
		}
		log.WithFields(fields).Infof("Executing CreateVolume with following fields")

		var hostIOLimit *types.IoLimitPolicy
		var hostIOLimitID string
		if hostIOLimitName != "" {
			hostIOLimit, err = unity.FindHostIOLimitByName(ctx, hostIOLimitName)
			if err != nil {
				return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "HostIOLimitName %s not found. Error: %v", hostIOLimitName, err))
			}

			hostIOLimitID = hostIOLimit.IoLimitPolicyContent.ID
		}

		// Idempotency check
		vol, _ := unity.FindVolumeByName(ctx, volName)
		if vol != nil {
			content := vol.VolumeContent
			if int64(content.SizeTotal) /* #nosec G115 -- This is a false positive */ == size {
				log.Info("Volume exists in the requested state with same size")
				return csiutils.GetVolumeResponseFromVolume(vol, arrayID, protocol, preferredAccessibility), nil
			}
			log.Info("'Volume name' already exists and size is different")
			return nil, status.Error(codes.AlreadyExists, csiutils.GetMessageWithRunID(rid, "'Volume name' already exists and size is different."))
		}

		log.Debug("Volume does not exist, proceeding to create new volume")
		resp, err := unity.CreateLun(ctx, volName, storagePool, desc, uint64(size), int(tieringPolicy), hostIOLimitID, thin, dataReduction) // #nosec G115 - This is a false positive
		if err != nil {
			return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Create Volume %s failed with error: %v", volName, err))
		}

		resp, err = unity.FindVolumeByName(ctx, volName)
		if resp != nil {
			volumeResp := csiutils.GetVolumeResponseFromVolume(resp, arrayID, protocol, preferredAccessibility)
			log.Debugf("CreateVolume successful for volid: [%s]", volumeResp.Volume.VolumeId)
			return volumeResp, nil
		}
	}

	return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Volume/Filesystem not found after create. %v", err))
}

func (s *service) DeleteVolume(
	ctx context.Context,
	req *csi.DeleteVolumeRequest) (
	*csi.DeleteVolumeResponse, error,
) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing DeleteVolume with args: %+v", *req)
	var snapErr error
	volID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}
	deleteVolumeResp := &csi.DeleteVolumeResponse{}
	// Not validating protocol here to support deletion of pvcs from v1.0
	if protocol != NFS {

		// Delete logic for FC and iSCSI volumes
		var throwErr error
		err, throwErr = s.deleteBlockVolume(ctx, volID, unity)
		if throwErr != nil {
			return nil, throwErr
		}

	} else {

		// Delete logic for Filesystem
		var throwErr error
		err, snapErr, throwErr = s.deleteFilesystem(ctx, volID, unity)
		if throwErr != nil {
			return nil, throwErr
		}
	}

	// Idempotency check
	if err == nil {
		log.Debugf("DeleteVolume successful for volid: [%s]", req.VolumeId)
		return deleteVolumeResp, nil
	} else if err == gounity.ErrorFilesystemNotFound || err == gounity.ErrorVolumeNotFound || snapErr == gounity.ErrorSnapshotNotFound {
		log.Debug("Volume not found on array")
		log.Debugf("DeleteVolume successful for volid: [%s]", req.VolumeId)
		return deleteVolumeResp, nil
	}
	return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Delete Volume %s failed with error: %v", volID, err))
}

func (s *service) ControllerPublishVolume(
	ctx context.Context,
	req *csi.ControllerPublishVolumeRequest) (
	*csi.ControllerPublishVolumeResponse, error,
) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Debugf("Executing ControllerPublishVolume with args: %+v", *req)

	volID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	protocol, nodeID, err := ValidateControllerPublishRequest(ctx, req, protocol)
	if err != nil {
		return nil, err
	}

	hostNames := strings.Split(nodeID, ",")
	host, err := s.getHostID(ctx, arrayID, hostNames[0], hostNames[1])
	if err != nil {
		return nil, err
	}
	hostContent := host.HostContent
	hostID := hostContent.ID

	pinfo := make(map[string]string)
	pinfo["volumeContextId"] = req.GetVolumeId()
	pinfo["arrayId"] = arrayID
	pinfo["host"] = nodeID

	vc := req.GetVolumeCapability()
	am := vc.GetAccessMode()

	if protocol == FC || protocol == ISCSI {
		resp, err := s.exportVolume(ctx, protocol, volID, hostID, nodeID, arrayID, unity, pinfo, host, vc)
		return resp, err
	}

	// Export for NFS
	resp, err := s.exportFilesystem(ctx, volID, hostID, nodeID, arrayID, unity, pinfo, am)
	return resp, err
}

func (s *service) ControllerUnpublishVolume(
	ctx context.Context,
	req *csi.ControllerUnpublishVolumeRequest) (
	*csi.ControllerUnpublishVolumeResponse, error,
) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing ControllerUnpublishVolume with args: %+v", *req)

	volID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "Node ID is required"))
	}

	hostNames := strings.Split(nodeID, ",")
	host, err := s.getHostID(ctx, arrayID, hostNames[0], hostNames[1])
	if err != nil {
		return nil, err
	}
	hostContent := host.HostContent
	hostID := hostContent.ID

	if protocol != NFS {

		vol, err := unity.FindVolumeByID(ctx, volID)
		if err != nil {
			// If the volume isn't found, k8s will retry Controller Unpublish forever so...
			// There is no way back if volume isn't found and so considering this scenario idempotent
			if err == gounity.ErrorVolumeNotFound {
				log.Debugf("Volume %s not found on the array %s during Controller Unpublish. Hence considering the call to be idempotent", volID, arrayID)
				return &csi.ControllerUnpublishVolumeResponse{}, nil
			}
			return nil, status.Error(codes.Internal, csiutils.GetMessageWithRunID(rid, "%v", err))
		}

		// Idempotency check
		content := vol.VolumeContent
		if len(content.HostAccessResponse) > 0 {

			hostIDList := make([]string, 0)

			// Check if the volume is published to any other node and retain it - RWX raw block
			for _, hostaccess := range content.HostAccessResponse {
				hostcontent := hostaccess.HostContent
				hostAccessID := hostcontent.ID
				if hostAccessID != hostID {
					hostIDList = append(hostIDList, hostAccessID)
				}
			}

			log.Debug("Removing Host access on Volume ", volID)
			log.Debug("List of host access that will be retained on the volume: ", hostIDList)
			err = unity.ModifyVolumeExport(ctx, volID, hostIDList)
			if err != nil {
				return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Unexport Volume Failed. %v", err))
			}
		} else {
			log.Info(fmt.Sprintf("The given Node %s does not have access on the given volume %s. Already in Unpublished state.", hostID, volID))
		}
		log.Debugf("ControllerUnpublishVolume successful for volid: [%s]", req.GetVolumeId())
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// Unexport for NFS
	err = s.unexportFilesystem(ctx, volID, hostID, nodeID, req.GetVolumeId(), arrayID, unity)
	if err != nil {
		return nil, err
	}
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (s *service) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing ValidateVolumeCapabilities with args: %+v", *req)

	volID, _, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	_, err = unity.FindVolumeByID(ctx, volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Volume not found. Error: %v", err))
	}

	params := req.GetParameters()
	protocol, _ := params[keyProtocol]
	if protocol == "" {
		log.Errorf("Protocol is required to validate capabilities")
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Protocol is required to validate capabilities"))
	}

	vcs := req.GetVolumeCapabilities()
	supported, reason := valVolumeCaps(vcs, protocol)
	resp := &csi.ValidateVolumeCapabilitiesResponse{}
	if supported {
		// The optional fields volume_context and parameters are not passed.
		confirmed := &csi.ValidateVolumeCapabilitiesResponse_Confirmed{}
		confirmed.VolumeCapabilities = vcs
		resp.Confirmed = confirmed
		return resp, nil
	}
	resp.Message = reason
	return resp, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Unsupported capability"))
}

func (s *service) ListVolumes(_ context.Context, _ *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Not implemented")
}

func (s *service) GetCapacity(
	ctx context.Context,
	req *csi.GetCapacityRequest) (
	*csi.GetCapacityResponse, error,
) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing GetCapacity with args: %+v", *req)

	params := req.GetParameters()

	// Get arrayId from params
	arrayID := strings.ToLower(strings.TrimSpace(params[keyArrayID]))

	if arrayID == "" {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "ArrayId cannot be empty"))
	}
	ctx, log = setArrayIDContext(ctx, arrayID)

	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, err
	}

	capacity, err := unity.GetCapacity(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	log.Infof("Available capacity from the Array: %d", capacity.Entries[0].Content.SizeFree)

	maxVolSize, err := s.getMaximumVolumeSize(ctx, arrayID)
	if err != nil {
		return &csi.GetCapacityResponse{
			AvailableCapacity: int64(capacity.Entries[0].Content.SizeFree),
		}, nil
	}

	return &csi.GetCapacityResponse{
		AvailableCapacity: int64(capacity.Entries[0].Content.SizeFree),
		MaximumVolumeSize: wrapperspb.Int64(maxVolSize),
	}, nil
}

func (s *service) getMaximumVolumeSize(ctx context.Context, arrayID string) (int64, error) {
	ctx, log, _ := GetRunidLog(ctx)
	unity, err := s.getUnityClient(ctx, arrayID)
	maxVolumeSize, err := unity.GetMaxVolumeSize(ctx, "Limit_MaxLUNSize")
	if err != nil {
		log.Debugf("GetMaxVolumeSize returning: %v for Array having arrayId %s", err, arrayID)
		return 0, err
	}
	return int64(maxVolumeSize.MaxVolumSizeContent.Limit), nil
}

func (s *service) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing CreateSnapshot with args: %+v", *req)

	if len(req.SourceVolumeId) == 0 {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "Storage Resource ID cannot be empty"))
	}
	var err error
	req.Name, err = gounityutil.ValidateResourceName(req.Name, api.MaxResourceNameLength)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "invalid snapshot name [%v]", err))
	}

	// Source volume is for volume clone or snapshot clone
	volID, protocol, arrayID, _, err := s.validateAndGetResourceDetails(ctx, req.SourceVolumeId, volumeType)
	if err != nil {
		return nil, err
	}

	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	// Idempotency check
	snap, err := s.createIdempotentSnapshot(ctx, req.Name, volID, req.Parameters["description"], req.Parameters["retentionDuration"], protocol, arrayID, false)
	if err != nil {
		return nil, err
	}
	return csiutils.GetSnapshotResponseFromSnapshot(snap, protocol, arrayID), nil
}

func (s *service) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing DeleteSnapshot with args: %+v", *req)

	snapID, _, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.SnapshotId, snapshotType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	// Idempotency check
	snap, err := unity.FindSnapshotByID(ctx, snapID)
	// snapshot exists, continue deleting the snapshot
	if err != nil {
		log.Info("Snapshot doesn't exists")
	}

	if snap != nil {
		err := unity.DeleteSnapshot(ctx, snapID)
		if err != nil {
			return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Delete Snapshot error: %v", err))
		}
	}

	delSnapResponse := &csi.DeleteSnapshotResponse{}
	log.Debugf("Delete snapshot successful [%s]", req.SnapshotId)
	return delSnapResponse, nil
}

func (s *service) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing ListSnapshot with args: %+v", *req)

	var (
		startToken int
		err        error
		maxEntries = int(req.MaxEntries)
	)
	snapID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.SnapshotId, snapshotType)
	if err != nil {
		return nil, err
	}

	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	// Limiting the number of snapshots to 100 to avoid timeout issues
	if maxEntries > MaxEntriesSnapshot || maxEntries == 0 {
		maxEntries = MaxEntriesSnapshot
	}

	if req.StartingToken != "" {
		i, err := strconv.ParseInt(req.StartingToken, 10, 64)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "Unable to parse StartingToken: %v into uint32", req.StartingToken))
		}
		startToken = int(i)
	}

	snaps, nextToken, err := unity.ListSnapshots(ctx, startToken, maxEntries, "", snapID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Unable to get the snapshots: %v", err))
	}

	// Process the source snapshots and make CSI Snapshot
	entries, err := s.getCSISnapshots(snaps, req.SourceVolumeId, protocol, arrayID)
	if err != nil {
		return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "%s", err.Error()))
	}
	log.Debugf("ListSnapshot successful for snapid: [%s]", req.SnapshotId)
	return &csi.ListSnapshotsResponse{
		Entries:   entries,
		NextToken: strconv.Itoa(nextToken),
	}, nil
}

func (s *service) controllerProbe(ctx context.Context, arrayID string) error {
	return s.probe(ctx, "Controller", arrayID)
}

// ControllerGetCapabilities implements the default GRPC callout.
// Default supports all capabilities
func (s *service) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Debugf("Executing ControllerGetCapabilities with args: %+v", *req)
	capabilities := []*csi.ControllerServiceCapability{
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_GET_CAPACITY,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_PUBLISH_READONLY,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
				},
			},
		},
	}
	volumeHealthMonitorCapabilities := []*csi.ControllerServiceCapability{
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_GET_VOLUME,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES_PUBLISHED_NODES,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_VOLUME_CONDITION,
				},
			},
		},
	}
	if s.opts.IsVolumeHealthMonitorEnabled {
		capabilities = append(capabilities, volumeHealthMonitorCapabilities...)
	}
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: capabilities,
	}, nil
}

func (s *service) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing ControllerExpandVolume with args: %+v", *req)

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "volumeId is mandatory parameter"))
	}

	volID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.VolumeId, volumeType)
	if err != nil {
		return nil, err
	}

	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	var capacity int64
	if cr := req.CapacityRange; cr != nil {
		if rb := cr.RequiredBytes; rb > 0 {
			capacity = rb
		}
		if lb := cr.LimitBytes; lb > 0 {
			capacity = lb
		}
	}
	if capacity <= 0 {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "Required bytes can not be 0 or less"))
	}

	expandVolumeResp := &csi.ControllerExpandVolumeResponse{
		CapacityBytes: capacity,
	}

	if protocol == NFS {
		// Adding Additional size used for metadata
		capacity += AdditionalFilesystemSize

		filesystem, err := unity.FindFilesystemByID(ctx, volID)
		if err != nil {
			_, err = unity.FindSnapshotByID(ctx, volID)
			if err != nil {
				return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find filesystem %s failed with error: %v", volID, err))
			}
			return nil, status.Error(codes.Unimplemented, csiutils.GetMessageWithRunID(rid, "Expand Volume not supported for cloned filesystems(snapshot on array)"))
		}

		// Idempotency check
		if filesystem.FileContent.SizeTotal >= uint64(capacity) /* #nosec G115 -- This is a false positive */ {
			log.Infof("New Filesystem size (%d) is lower or same as existing Filesystem size. Ignoring expand volume operation.", filesystem.FileContent.SizeTotal)
			expandVolumeResp.NodeExpansionRequired = false
			return expandVolumeResp, nil
		}

		err = unity.ExpandFilesystem(ctx, volID, uint64(capacity)) // #nosec G115 - This is a false positive
		if err != nil {
			return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Expand filesystem failed with error: %v", err))
		}

		filesystem, err = unity.FindFilesystemByID(ctx, volID)
		if err != nil {
			return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find filesystem failed with error: %v", err))
		}
		expandVolumeResp.CapacityBytes = int64(filesystem.FileContent.SizeTotal) /* #nosec G115 -- This is a false positive */ - AdditionalFilesystemSize
		expandVolumeResp.NodeExpansionRequired = false
		return expandVolumeResp, err
	}
	// Idempotency check
	volume, err := unity.FindVolumeByID(ctx, volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find volume failed with error: %v", err))
	}

	nodeExpansionRequired := false
	content := volume.VolumeContent
	if len(content.HostAccessResponse) >= 1 { // If the volume has 1 or more host access  then set nodeExpansionRequired as true
		nodeExpansionRequired = true
	}

	if volume.VolumeContent.SizeTotal >= uint64(capacity) {
		log.Infof("New Volume size (%d) is same as existing Volume size. Ignoring expand volume operation.", volume.VolumeContent.SizeTotal)
		expandVolumeResp.NodeExpansionRequired = nodeExpansionRequired
		return expandVolumeResp, nil
	}

	err = unity.ExpandVolume(ctx, volID, uint64(capacity))
	if err != nil {
		return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Expand volume failed with error: %v", err))
	}

	volume, err = unity.FindVolumeByID(ctx, volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find volume failed with error: %v", err))
	}
	expandVolumeResp.CapacityBytes = int64(volume.VolumeContent.SizeTotal) /* #nosec G115 -- This is a false positive */
	expandVolumeResp.NodeExpansionRequired = nodeExpansionRequired
	return expandVolumeResp, err
}

func (s *service) getCSIVolumes(volumes []types.Volume) ([]*csi.ListVolumesResponse_Entry, error) {
	entries := make([]*csi.ListVolumesResponse_Entry, len(volumes))
	for i, vol := range volumes {
		// Make the additional volume attributes
		attributes := map[string]string{
			"Name":          vol.VolumeContent.Name,
			"Type":          strconv.Itoa(vol.VolumeContent.Type),
			"Wwn":           vol.VolumeContent.Wwn,
			"StoragePoolID": vol.VolumeContent.Pool.ID,
		}
		// Create CSI volume
		vi := &csi.Volume{
			VolumeId:      vol.VolumeContent.ResourceID,
			CapacityBytes: int64(vol.VolumeContent.SizeTotal), /* #nosec G115 -- This is a false positive */
			VolumeContext: attributes,
		}

		entries[i] = &csi.ListVolumesResponse_Entry{
			Volume: vi,
		}
	}

	return entries, nil
}

func (s *service) getCSISnapshots(snaps []types.Snapshot, volID, protocol, arrayID string) ([]*csi.ListSnapshotsResponse_Entry, error) {
	entries := make([]*csi.ListSnapshotsResponse_Entry, len(snaps))
	for i, snap := range snaps {
		isReady := false
		if snap.SnapshotContent.State == 2 {
			isReady = true
		}
		var timestamp *timestamppb.Timestamp
		if !snap.SnapshotContent.CreationTime.IsZero() {
			timestamp = timestamppb.New(snap.SnapshotContent.CreationTime)
		}

		snapID := fmt.Sprintf("%s-%s-%s-%s", snap.SnapshotContent.Name, protocol, arrayID, snap.SnapshotContent.ResourceID)

		size := snap.SnapshotContent.Size
		if protocol == NFS {
			size -= AdditionalFilesystemSize
		}
		// Create CSI Snapshot
		vi := &csi.Snapshot{
			SizeBytes:      size,
			SnapshotId:     snapID,
			SourceVolumeId: volID,
			CreationTime:   timestamp,
			ReadyToUse:     isReady,
		}

		entries[i] = &csi.ListSnapshotsResponse_Entry{
			Snapshot: vi,
		}
	}
	return entries, nil
}

// @TODO: Check if arrayID can be changed to unity client
func (s *service) getFilesystemByResourceID(ctx context.Context, resourceID, arrayID string) (*types.Filesystem, error) {
	ctx, _, rid := GetRunidLog(ctx)
	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, err
	}

	filesystemID, err := unity.GetFilesystemIDFromResID(ctx, resourceID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Storage resource: %s filesystem Id not found. Error: %v", resourceID, err))
	}
	sourceFilesystemResp, err := unity.FindFilesystemByID(ctx, filesystemID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Filesystem: %s not found. Error: %v", filesystemID, err))
	}
	return sourceFilesystemResp, nil
}

// Create Volume from Snapshot(Copy snapshot on array)
func (s *service) createFilesystemFromSnapshot(ctx context.Context, snapID, volumeName, arrayID string) (*types.Snapshot, error) {
	ctx, _, rid := GetRunidLog(ctx)
	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, err
	}

	snapResp, err := unity.CopySnapshot(ctx, snapID, volumeName)
	if err != nil {
		return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Create Filesystem from snapshot failed with error. Error: %v", err))
	}

	snapResp, err = unity.FindSnapshotByName(ctx, volumeName)
	if err != nil {
		return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Create Filesystem from snapshot failed with error. Error: %v", err))
	}

	return snapResp, nil
}

func (s *service) createIdempotentSnapshot(ctx context.Context, snapshotName, sourceVolID, description, retentionDuration, protocol, arrayID string, isClone bool) (*types.Snapshot, error) {
	ctx, log, rid := GetRunidLog(ctx)
	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, err
	}

	isSnapshot := false
	var snapResp *types.Snapshot
	var filesystemResp *types.Filesystem
	if protocol == NFS {
		filesystemResp, err = unity.FindFilesystemByID(ctx, sourceVolID)
		if err != nil {
			snapResp, err = unity.FindSnapshotByID(ctx, sourceVolID)
			if err != nil {
				return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find source filesystem: %s failed with error: %v", sourceVolID, err))
			}
			isSnapshot = true
			filesystemResp, err = s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.ID, arrayID)
			if err != nil {
				return nil, err
			}
		}
	}

	if protocol == NFS && !isSnapshot {
		sourceVolID = filesystemResp.FileContent.StorageResource.ID
	}

	snap, _ := unity.FindSnapshotByName(ctx, snapshotName)
	if snap != nil {
		if snap.SnapshotContent.StorageResource.ID == sourceVolID || (isSnapshot && snap.SnapshotContent.StorageResource.ID == filesystemResp.FileContent.StorageResource.ID) {
			// Subtract AdditionalFilesystemSize for Filesystem snapshots
			if protocol == NFS {
				snap.SnapshotContent.Size -= AdditionalFilesystemSize
			}
			log.Infof("Snapshot already exists with same name %s for same storage resource %s", snapshotName, sourceVolID)
			return snap, nil
		}
		return nil, status.Error(codes.AlreadyExists, csiutils.GetMessageWithRunID(rid, "Snapshot with same name %s already exists for storage resource %s", snapshotName, snap.SnapshotContent.StorageResource.ID))
	}

	var newSnapshot *types.Snapshot
	if isSnapshot {
		newSnapshot, err = unity.CopySnapshot(ctx, sourceVolID, snapshotName)
		if err != nil {
			return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Create Snapshot error: %v", err))
		}
		err = unity.ModifySnapshot(ctx, newSnapshot.SnapshotContent.ResourceID, description, retentionDuration)
		if err != nil {
			log.Infof("Unable to modify description and retention duration in created snapshot %s. Error: %s", newSnapshot.SnapshotContent.ResourceID, err)
		}
	} else {
		if isClone {
			newSnapshot, err = unity.CreateSnapshotWithFsAccesType(ctx, sourceVolID, snapshotName, description, retentionDuration, gounity.ProtocolAccessType)
		} else {
			newSnapshot, err = unity.CreateSnapshot(ctx, sourceVolID, snapshotName, description, retentionDuration)
		}
		if err != nil {
			return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Create Snapshot error: %v", err))
		}
	}

	newSnapshot, _ = unity.FindSnapshotByName(ctx, snapshotName)
	if newSnapshot != nil {
		// Subtract AdditionalFilesystemSize for Filesystem snapshots{
		if protocol == NFS {
			newSnapshot.SnapshotContent.Size -= AdditionalFilesystemSize
		}
		return newSnapshot, nil
	}
	return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Find Snapshot error after create. %v", err))
}

func (s *service) getHostID(ctx context.Context, arrayID, shortHostname, longHostname string) (*types.Host, error) {
	ctx, _, rid := GetRunidLog(ctx)
	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Unable to get Unity client."))
	}

	host, err := unity.FindHostByName(ctx, shortHostname)
	if err != nil {
		if err != gounity.ErrorHostNotFound {
			return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find Host Failed %v", err))
		}
	}
	if host != nil {
		for _, hostIPPort := range host.HostContent.IPPorts {
			if hostIPPort.Address == longHostname {
				return host, nil
			}
		}
	}

	host, err = unity.FindHostByName(ctx, longHostname)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find Host Failed %v", err))
	}
	for _, hostIPPort := range host.HostContent.IPPorts {
		if hostIPPort.Address == longHostname {
			return host, nil
		}
	}
	return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find Host Id Failed."))
}

// createVolumeClone - Method to create a volume clone with idempotency for all protocols
func (s *service) createVolumeClone(ctx context.Context, crParams *CRParams, sourceVolID, arrayID string, contentSource *csi.VolumeContentSource, unity gounity.UnityClient, preferredAccessibility []*csi.Topology) (*csi.CreateVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	if sourceVolID == "" {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "Source volume ID cannot be empty"))
	}

	sourceVolID, _, sourceArrayID, _, err := s.validateAndGetResourceDetails(ctx, sourceVolID, volumeType)
	if err != nil {
		return nil, err
	}

	if arrayID != sourceArrayID {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "Source volume array id: %s is different than required volume array id: %s", sourceArrayID, arrayID))
	}

	volName := crParams.VolumeName
	protocol := crParams.Protocol
	storagePool := crParams.StoragePool
	desc := crParams.Desciption
	thin := crParams.Thin
	dataReduction := crParams.DataReduction
	size := crParams.Size
	tieringPolicy := crParams.TieringPolicy
	hostIoSize := crParams.HostIoSize

	if protocol == NFS {

		filesystem, err := unity.FindFilesystemByID(ctx, sourceVolID)
		isSnapshot := false
		var snapResp *types.Snapshot
		var snapErr error
		if err != nil {
			// Filesystem not found - Check if PVC exists as a snapshot [Cloned volume in case of NFS]
			snapResp, snapErr = unity.FindSnapshotByID(ctx, sourceVolID)
			if snapErr != nil {
				log.Debugf("Tried to check if PVC exists as a snapshot: %v", snapErr)
				return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find source filesystem: %s Failed. Error: %v ", sourceVolID, err))
			}
			isSnapshot = true
			filesystem, err = s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.ID, arrayID)
			if err != nil {
				return nil, err
			}
		}

		err = validateCreateFsFromSnapshot(ctx, filesystem, storagePool, tieringPolicy, hostIoSize, thin, dataReduction)
		if err != nil {
			return nil, err
		}

		if isSnapshot {
			// Validate the size parameter
			snapSize := int64(snapResp.SnapshotContent.Size - AdditionalFilesystemSize)
			if snapSize != size {
				return nil, status.Errorf(codes.InvalidArgument, "%s", csiutils.GetMessageWithRunID(rid, "Requested size %d should be same as source filesystem size %d", size, snapSize))
			}
			// Idempotency check
			snapResp, err := unity.FindSnapshotByName(ctx, volName)
			if snapResp == nil {
				// Create Volume from Snapshot(Copy snapshot on array)
				snapResp, err = s.createFilesystemFromSnapshot(ctx, sourceVolID, volName, arrayID)
				if err != nil {
					return nil, err
				}
			} else if snapResp.SnapshotContent.Size != int64(size+AdditionalFilesystemSize) {
				return nil, status.Error(codes.AlreadyExists, csiutils.GetMessageWithRunID(rid, "Snapshot with same name %s already exists in different size.", volName))
			}
			snapResp.SnapshotContent.Size -= AdditionalFilesystemSize
			csiVolResp := csiutils.GetVolumeResponseFromSnapshot(snapResp, arrayID, protocol, preferredAccessibility)
			csiVolResp.Volume.ContentSource = contentSource
			return csiVolResp, nil
		}
		fsSize := int64(filesystem.FileContent.SizeTotal - AdditionalFilesystemSize) /* #nosec G115 -- This is a false positive */
		if size != fsSize {
			return nil, status.Errorf(codes.InvalidArgument, "%s", csiutils.GetMessageWithRunID(rid, "Requested size %d should be same as source volume size %d",
				size, fsSize))
		}

		snap, err := s.createIdempotentSnapshot(ctx, volName, sourceVolID, desc, "", protocol, arrayID, true)
		if err != nil {
			return nil, err
		}
		csiVolResp := csiutils.GetVolumeResponseFromSnapshot(snap, arrayID, protocol, preferredAccessibility)
		csiVolResp.Volume.ContentSource = contentSource
		return csiVolResp, nil
	}

	// If protocol is FC or iSCSI
	sourceVolResp, err := unity.FindVolumeByID(ctx, sourceVolID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Source volume not found: %s. Error: %v", sourceVolID, err))
	}

	err = validateCreateVolumeFromSource(ctx, sourceVolResp, storagePool, tieringPolicy, size, thin, dataReduction, false)
	if err != nil {
		return nil, err
	}

	volResp, _ := unity.FindVolumeByName(ctx, volName)
	if volResp != nil {
		// Idempotency Check
		if volResp.VolumeContent.IsThinClone && len(volResp.VolumeContent.ParentVolume.ID) > 0 && volResp.VolumeContent.ParentVolume.ID == sourceVolID &&
			volResp.VolumeContent.SizeTotal == sourceVolResp.VolumeContent.SizeTotal {
			log.Infof("Volume %s exists in the requested state as a clone of volume %s", volName, sourceVolResp.VolumeContent.Name)
			csiVolResp := csiutils.GetVolumeResponseFromVolume(volResp, arrayID, protocol, preferredAccessibility)
			csiVolResp.Volume.ContentSource = contentSource
			return csiVolResp, nil
		}
		return nil, status.Error(codes.AlreadyExists, csiutils.GetMessageWithRunID(rid, "Volume with same name %s already exists", volName))
	}

	// Perform volume cloning
	volResp, err = unity.CreateCloneFromVolume(ctx, volName, sourceVolID)
	if err != nil {
		if err == gounity.ErrorCreateSnapshotFailed {
			return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Unable to Create Snapshot for Volume Cloning for source volume: %s", sourceVolID))
		} else if err == gounity.ErrorCloningFailed {
			return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Volume cloning for source volume: %s failed.", sourceVolID))
		}
	}

	volResp, err = unity.FindVolumeByName(ctx, volName)
	if volResp != nil {
		csiVolResp := csiutils.GetVolumeResponseFromVolume(volResp, arrayID, protocol, preferredAccessibility)
		csiVolResp.Volume.ContentSource = contentSource
		return csiVolResp, nil
	}
	return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Volume not found after create. %v", err))
}

// createVolumeFromSnap - Method to create a volume from snapshot with idempotency for all protocols
func (s *service) createVolumeFromSnap(ctx context.Context, crParams *CRParams, snapshotID, arrayID string, contentSource *csi.VolumeContentSource, unity gounity.UnityClient, preferredAccessibility []*csi.Topology) (*csi.CreateVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	if snapshotID == "" {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Source snapshot ID cannot be empty"))
	}

	snapshotID, _, sourceArrayID, _, err := s.validateAndGetResourceDetails(ctx, snapshotID, snapshotType)
	if err != nil {
		return nil, err
	}

	if arrayID != sourceArrayID {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "Source snapshot array id: %s is different than required volume array id: %s", sourceArrayID, arrayID))
	}

	volName := crParams.VolumeName
	protocol := crParams.Protocol
	storagePool := crParams.StoragePool
	thin := crParams.Thin
	dataReduction := crParams.DataReduction
	size := crParams.Size
	tieringPolicy := crParams.TieringPolicy
	hostIoSize := crParams.HostIoSize

	snapResp, err := unity.FindSnapshotByID(ctx, snapshotID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Source snapshot not found: %s", snapshotID))
	}

	if protocol == NFS {

		sourceFilesystemResp, err := s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.ID, arrayID)
		if err != nil {
			return nil, err
		}

		err = validateCreateFsFromSnapshot(ctx, sourceFilesystemResp, storagePool, tieringPolicy, hostIoSize, thin, dataReduction)
		if err != nil {
			return nil, err
		}
		// Validate the size parameter
		snapSize := int64(snapResp.SnapshotContent.Size - AdditionalFilesystemSize)
		if snapSize != size {
			return nil, status.Errorf(codes.InvalidArgument, "%s", csiutils.GetMessageWithRunID(rid, "Requested size %d should be same as source snapshot size %d", size, snapSize))
		}

		snapResp, err := unity.FindSnapshotByName(ctx, volName)
		if snapResp != nil {
			// Idempotency check
			if snapResp.SnapshotContent.ParentSnap.ID == snapshotID && snapResp.SnapshotContent.AccessType == int(gounity.ProtocolAccessType) {
				log.Infof("Filesystem %s exists in the requested state as a volume from snapshot(snapshot on array) %s", volName, snapshotID)
				snapResp.SnapshotContent.Size -= AdditionalFilesystemSize
				csiVolResp := csiutils.GetVolumeResponseFromSnapshot(snapResp, arrayID, protocol, preferredAccessibility)
				csiVolResp.Volume.ContentSource = contentSource
				return csiVolResp, nil
			}
			return nil, status.Error(codes.AlreadyExists, csiutils.GetMessageWithRunID(rid, "Filesystem with same name %s already exists", volName))
		}

		// Create Volume from Snapshot(Copy snapshot on array)
		snapResp, err = s.createFilesystemFromSnapshot(ctx, snapshotID, volName, arrayID)
		if err != nil {
			return nil, err
		}

		if snapResp != nil {
			snapResp.SnapshotContent.Size -= AdditionalFilesystemSize
			csiVolResp := csiutils.GetVolumeResponseFromSnapshot(snapResp, arrayID, protocol, preferredAccessibility)
			csiVolResp.Volume.ContentSource = contentSource
			return csiVolResp, nil
		}
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Filesystem: %s not found after create. Error: %v", volName, err))
	}

	// If protocol is FC or iSCSI
	volID := snapResp.SnapshotContent.StorageResource.ID
	sourceVolResp, err := unity.FindVolumeByID(ctx, volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Source volume not found: %s", volID))
	}

	err = validateCreateVolumeFromSource(ctx, sourceVolResp, storagePool, tieringPolicy, size, thin, dataReduction, true)
	if err != nil {
		return nil, err
	}

	// Validate the size parameter
	if snapResp.SnapshotContent.Size != size {
		return nil, status.Errorf(codes.InvalidArgument, "%s", csiutils.GetMessageWithRunID(rid, "Requested size %d should be same as source snapshot size %d", size, snapResp.SnapshotContent.Size))
	}

	volResp, _ := unity.FindVolumeByName(ctx, volName)
	if volResp != nil {
		// Idempotency Check
		if volResp.VolumeContent.IsThinClone == true && len(volResp.VolumeContent.ParentSnap.ID) > 0 && volResp.VolumeContent.ParentSnap.ID == snapshotID {
			log.Info("Volume exists in the requested state")
			csiVolResp := csiutils.GetVolumeResponseFromVolume(volResp, arrayID, protocol, preferredAccessibility)
			csiVolResp.Volume.ContentSource = contentSource
			return csiVolResp, nil
		}
		return nil, status.Error(codes.AlreadyExists, csiutils.GetMessageWithRunID(rid, "Volume with same name %s already exists", volName))
	}

	if snapResp.SnapshotContent.IsAutoDelete == true {
		err = unity.ModifySnapshotAutoDeleteParameter(ctx, snapshotID)
		if err != nil {
			return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Unable to modify auto-delete parameter for snapshot %s", snapshotID))
		}
	}

	volResp, err = unity.CreteLunThinClone(ctx, volName, snapshotID, volID)
	if err != nil {
		return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Create volume from snapshot failed with error %v", err))
	}
	volResp, err = unity.FindVolumeByName(ctx, volName)
	if err != nil {
		log.Debugf("Find Volume response: %v Error: %v", volResp, err)
	}

	if volResp != nil {
		csiVolResp := csiutils.GetVolumeResponseFromVolume(volResp, arrayID, protocol, preferredAccessibility)
		csiVolResp.Volume.ContentSource = contentSource
		return csiVolResp, nil
	}
	return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Volume not found after create. %v", err))
}

// deleteFilesystem - Method to handle delete filesystem logic
func (s *service) deleteFilesystem(ctx context.Context, volID string, unity gounity.UnityClient) (error, error, error) {
	ctx, _, rid := GetRunidLog(ctx)
	var filesystemResp *types.Filesystem
	var snapErr error
	filesystemResp, err := unity.FindFilesystemByID(ctx, volID)
	if err == nil {
		// Validate if filesystem has any NFS or SMB shares or snapshots attached
		if len(filesystemResp.FileContent.NFSShare) > 0 || len(filesystemResp.FileContent.CIFSShare) > 0 {
			return nil, nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Filesystem %s can not be deleted as it has associated NFS or SMB shares.", volID))
		}
		snapsResp, _, snapshotErr := unity.ListSnapshots(ctx, 0, 0, filesystemResp.FileContent.StorageResource.ID, "")
		if snapshotErr != nil {
			return nil, nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "List snapshots for filesystem %s failed with error: %v", volID, snapshotErr))
		}

		for _, snapResp := range snapsResp {
			if snapResp.SnapshotContent.AccessType == int(gounity.CheckpointAccessType) {
				return nil, nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Filesystem %s can not be deleted as it has associated snapshots.", volID))
			}
		}
		err = unity.DeleteFilesystem(ctx, volID)
	} else {
		// Do not reuse err as it is used for idempotency check
		snapResp, fsSnapErr := unity.FindSnapshotByID(ctx, volID)
		snapErr = fsSnapErr
		if fsSnapErr == nil {
			// Validate if snapshot has any NFS or SMB shares
			sourceVolID, err := unity.GetFilesystemIDFromResID(ctx, snapResp.SnapshotContent.StorageResource.ID)
			if err != nil {
				return nil, nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Source storage resource: %s filesystem Id not found. Error: %v", snapResp.SnapshotContent.StorageResource.ID, err))
			}
			filesystemResp, err = unity.FindFilesystemByID(ctx, sourceVolID)
			if err != nil {
				return nil, nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find source filesystem: %s failed with error: %v", sourceVolID, err))
			}
			for _, nfsShare := range filesystemResp.FileContent.NFSShare {
				if nfsShare.ParentSnap.ID == volID {
					return nil, nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Snapshot %s can not be deleted as it has associated NFS or SMB shares.", volID))
				}
			}
			err = unity.DeleteFilesystemAsSnapshot(ctx, volID, filesystemResp)
		}
	}
	return err, snapErr, nil
}

// deleteBlockVolume - Method to handle delete FC and iSCSI volumes
func (s *service) deleteBlockVolume(ctx context.Context, volID string, unity gounity.UnityClient) (error, error) {
	ctx, log, rid := GetRunidLog(ctx)
	// Check stale snapshots used for volume cloning and delete if exist
	snapsResp, _, snapshotErr := unity.ListSnapshots(ctx, 0, 0, volID, "")
	if snapshotErr != nil {
		return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "List snapshots for volume %s failed with error: %v", volID, snapshotErr))
	}
	totalSnaps := len(snapsResp)
	for _, snapResp := range snapsResp {
		snapshotName := snapResp.SnapshotContent.Name
		if strings.Contains(snapshotName, gounity.SnapForClone) {
			reqDeleteSnapshot := new(csi.DeleteSnapshotRequest)
			reqDeleteSnapshot.SnapshotId = snapResp.SnapshotContent.ResourceID
			_, snapshotErr = s.DeleteSnapshot(ctx, reqDeleteSnapshot)
			if snapshotErr != nil {
				return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Volume %s can not be deleted as it has associated snapshots.", volID))
			}
			totalSnaps--
		}
	}
	if totalSnaps > 0 {
		return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Volume %s can not be deleted as it has associated snapshots.", volID))
	}

	// Check if any host still has access to the volume. This can happen if ControllerPublishVolume timed out,
	// and ControllerUnpublishVolume ran before host access was fully established. In that case, Unpublish does nothing.
	// If host access addition in the array completes just before DeleteVolume,
	// the array will fail the delete call unless we remove the host access first.

	vol, err := unity.FindVolumeByID(ctx, volID)
	if err != nil {
		if errors.Is(err, gounity.ErrorVolumeNotFound) {
			return gounity.ErrorVolumeNotFound, nil // already deleted, nothing more to do
		}
		return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Failed to fetch volume %s details from storage array.", volID))
	}

	if len(vol.VolumeContent.HostAccessResponse) > 0 {
		log.Infof("Removing host access for volume %s: %v", volID, vol.VolumeContent.HostAccessResponse)
		err = unity.UnexportVolume(ctx, volID)
		unexpErr, statusErr := checkVolumeUnexportError(err, volID, rid, log)
		if unexpErr != nil || statusErr != nil {
			return unexpErr, statusErr
		}
	}

	// Delete the block volume
	err = unity.DeleteVolume(ctx, volID)
	return checkVolumeDeleteError(err, volID, rid, log)
}

func checkVolumeUnexportError(err error, volID, rid string, log *logrus.Entry) (error, error) {
	if err == nil {
		return nil, nil
	} else if strings.Contains(err.Error(), gounity.NothingToModifyErrorCode) {
		log.Debugf("Host access for volume %s already removed", volID)
		return nil, nil
	} else if strings.Contains(err.Error(), gounity.VolumeNotFoundErrorCode) {
		return gounity.ErrorVolumeNotFound, nil // volume already deleted, nothing more to do
	} else if strings.Contains(err.Error(), "context deadline exceeded") {
		log.Debugf("Remove host access request for volume %s timed out, try again", volID)
		return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Remove host access for volume timed out."))
	} else if strings.Contains(err.Error(), gounity.LUNModifiedErrorCode) {
		log.Debugf("Failed to remove host access for volume %s, LUN modified by another request, try again", volID)
		return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Remove host access for volume failed."))
	}
	log.Errorf("Failed to remove host access for volume %s: %v", volID, err)
	return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Remove host access for volume failed."))
}

func checkVolumeDeleteError(err error, volID, rid string, log *logrus.Entry) (error, error) {
	if err == nil {
		return nil, nil
	} else if strings.Contains(err.Error(), gounity.LUNModifiedErrorCode) {
		log.Debugf("Failed to delete volume %s, LUN modified by another requested, try again", volID)
		return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Delete volume from storage array failed."))
	} else if strings.Contains(err.Error(), gounity.VolumeHostAccessErrorCode) {
		log.Debugf("Failed to delete volume %s, remove host access", volID)
		return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Delete volume from storage array failed, since it still has host access."))
	} else if errors.Is(err, gounity.ErrorVolumeNotFound) || strings.Contains(err.Error(), gounity.VolumeNotFoundErrorCode) {
		return gounity.ErrorVolumeNotFound, nil // already deleted, nothing more to do
	} else if strings.Contains(err.Error(), "context deadline exceeded") {
		log.Debugf("Delete volume %s from array timed out, try again", volID)
		return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Delete volume from storage array timed out."))
	}
	log.Errorf("Failed to delete volume %s from array: %v", volID, err)
	return nil, status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Delete volume from storage array failed."))
}

// exportFilesystem - Method to export filesystem with idempotency
func (s *service) exportFilesystem(ctx context.Context, volID, hostID, nodeID, arrayID string, unity gounity.UnityClient, pinfo map[string]string, am *csi.VolumeCapability_AccessMode) (*csi.ControllerPublishVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	pinfo["filesystem"] = volID
	isSnapshot := false
	filesystemResp, err := unity.FindFilesystemByID(ctx, volID)
	var snapResp *types.Snapshot

	if err != nil {
		snapResp, err = unity.FindSnapshotByID(ctx, volID)
		if err != nil {
			return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find filesystem: %s failed with error: %v", volID, err))
		}
		isSnapshot = true

		filesystemResp, err = s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.ID, arrayID)
		if err != nil {
			return nil, err
		}
	}
	// Create NFS Share if not already present on array
	nfsShareName := NFSShareNamePrefix + filesystemResp.FileContent.Name
	if isSnapshot {
		nfsShareName = NFSShareNamePrefix + snapResp.SnapshotContent.Name
	}
	nfsShareExist := false
	var nfsShareID string
	for _, nfsShare := range filesystemResp.FileContent.NFSShare {
		if isSnapshot {
			if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.ID == volID {
				nfsShareExist = true
				nfsShareName = nfsShare.Name
				nfsShareID = nfsShare.ID
			}
		} else {
			if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.ID == "" {
				nfsShareExist = true
				nfsShareName = nfsShare.Name
				nfsShareID = nfsShare.ID
			}
		}
	}
	if !nfsShareExist {
		if isSnapshot {
			nfsShareResp, err := unity.CreateNFSShareFromSnapshot(ctx, nfsShareName, NFSShareLocalPath, volID, gounity.NoneDefaultAccess)
			if err != nil {
				return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Create NFS Share failed. Error: %v", err))
			}
			nfsShareID = nfsShareResp.NFSShareContent.ID
		} else {
			filesystemResp, err = unity.CreateNFSShare(ctx, nfsShareName, NFSShareLocalPath, volID, gounity.NoneDefaultAccess)
			if err != nil {
				return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Create NFS Share failed. Error: %v", err))
			}
		}
		for _, nfsShare := range filesystemResp.FileContent.NFSShare {
			if nfsShare.Name == nfsShareName {
				nfsShareID = nfsShare.ID
			}
		}
	}

	// Allocate host access to NFS Share with appropriate access mode
	nfsShareResp, err := unity.FindNFSShareByID(ctx, nfsShareID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find NFS Share: %s failed. Error: %v", nfsShareID, err))
	}
	readOnlyHosts := nfsShareResp.NFSShareContent.ReadOnlyHosts
	readWriteHosts := nfsShareResp.NFSShareContent.ReadWriteHosts
	readOnlyRootHosts := nfsShareResp.NFSShareContent.ReadOnlyRootAccessHosts
	readWriteRootHosts := nfsShareResp.NFSShareContent.RootAccessHosts

	foundIncompatible := false
	foundIdempotent := false
	otherHostsWithAccess := len(readOnlyHosts)
	var readHostIDList, readWriteHostIDList []string
	for _, host := range readOnlyHosts {
		if host.ID == hostID {
			foundIncompatible = true
			break
		}
	}
	otherHostsWithAccess += len(readWriteHosts)
	if !foundIncompatible {
		for _, host := range readWriteHosts {
			if host.ID == hostID {
				foundIncompatible = true
				break
			}
		}
	}
	otherHostsWithAccess += len(readOnlyRootHosts)
	if !foundIncompatible {
		for _, host := range readOnlyRootHosts {
			readHostIDList = append(readHostIDList, host.ID)
			if host.ID == hostID {
				if am.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
					foundIdempotent = true
				} else {
					foundIncompatible = true
				}
			}
		}
	}
	otherHostsWithAccess += len(readWriteRootHosts)
	if !foundIncompatible && !foundIdempotent {
		for _, host := range readWriteRootHosts {
			readWriteHostIDList = append(readWriteHostIDList, host.ID)
			if host.ID == hostID {
				if am.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
					foundIncompatible = true
				} else {
					foundIdempotent = true
					otherHostsWithAccess--
				}
			}
		}
	}
	if foundIncompatible {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Host: %s has access on NFS Share: %s with incompatible access mode.", nodeID, nfsShareID))
	}
	if (am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER || am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER || am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER) && otherHostsWithAccess > 0 {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Other hosts have access on NFS Share: %s", nfsShareID))
	}
	// Idempotent case
	if foundIdempotent {
		log.Info("Host has access to the given host and exists in the required state.")
		return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
	}
	if am.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
		readHostIDList = append(readHostIDList, hostID)
		if isSnapshot {
			err = unity.ModifyNFSShareCreatedFromSnapshotHostAccess(ctx, nfsShareID, readHostIDList, gounity.ReadOnlyRootAccessType)
		} else {
			err = unity.ModifyNFSShareHostAccess(ctx, volID, nfsShareID, readHostIDList, gounity.ReadOnlyRootAccessType)
		}
	} else {
		readWriteHostIDList = append(readWriteHostIDList, hostID)
		if isSnapshot {
			err = unity.ModifyNFSShareCreatedFromSnapshotHostAccess(ctx, nfsShareID, readWriteHostIDList, gounity.ReadWriteRootAccessType)
		} else {
			err = unity.ModifyNFSShareHostAccess(ctx, volID, nfsShareID, readWriteHostIDList, gounity.ReadWriteRootAccessType)
		}
	}
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Allocating host %s access to NFS Share failed. Error: %v", nodeID, err))
	}
	log.Debugf("NFS Share: %s is accessible to host: %s with access mode: %s", nfsShareID, nodeID, am.Mode)
	log.Debugf("ControllerPublishVolume successful for volid: [%s]", pinfo["volumeContextId"])
	return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
}

// exportVolume - Method to export volume with idempotency
func (s *service) exportVolume(ctx context.Context, protocol, volID, hostID, _, _ string, unity gounity.UnityClient, pinfo map[string]string, host *types.Host, vc *csi.VolumeCapability) (*csi.ControllerPublishVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	pinfo["lun"] = volID
	am := vc.GetAccessMode()
	hostContent := host.HostContent
	if protocol == FC && len(hostContent.FcInitiators) == 0 {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "Cannot publish volume as protocol in the Storage class is 'FC' but the node has no valid FC initiators"))
	} else if protocol == ISCSI && len(hostContent.IscsiInitiators) == 0 {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "Cannot publish volume as protocol in the Storage class is 'iSCSI' but the node has no valid iSCSI initiators"))
	}

	vol, err := unity.FindVolumeByID(ctx, volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find volume Failed %v", err))
	}

	content := vol.VolumeContent
	hostIDList := make([]string, 0)

	// Idempotency check
	for _, hostaccess := range content.HostAccessResponse {
		hostcontent := hostaccess.HostContent
		hostAccessID := hostcontent.ID
		if hostAccessID == hostID {
			log.Debug("Volume has been published to the given host and exists in the required state.")
			return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
		} else if vc.GetMount() != nil && (am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER || am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER || am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER || am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY) {
			return nil, status.Error(codes.Aborted, csiutils.GetMessageWithRunID(rid, "Volume has been published to a different host already."))
		}
		// Gather list of hosts to which the volume is already published to
		hostIDList = append(hostIDList, hostAccessID)
	}

	// Append the curent hostID as well
	hostIDList = append(hostIDList, hostID)

	log.Debug("Adding host access to ", hostID, " on volume ", volID)
	log.Debug("List of all hosts to which the volume will have access: ", hostIDList)
	err = unity.ModifyVolumeExport(ctx, volID, hostIDList)
	if err != nil {
		return nil, status.Error(codes.Unknown, csiutils.GetMessageWithRunID(rid, "Export Volume Failed %v", err))
	}
	log.Debugf("ControllerPublishVolume successful for volid: [%s]", pinfo["volumeContextId"])
	return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
}

// unexportFilesystem - Method to handle unexport filesystem logic with idempotency
func (s *service) unexportFilesystem(ctx context.Context, volID, hostID, nodeID, volumeContextID, arrayID string, unity gounity.UnityClient) error {
	ctx, log, rid := GetRunidLog(ctx)
	isSnapshot := false
	filesystem, err := unity.FindFilesystemByID(ctx, volID)
	var snapResp *types.Snapshot
	if err != nil {
		snapResp, err = unity.FindSnapshotByID(ctx, volID)
		if err != nil {
			// If the filesystem isn't found, k8s will retry Controller Unpublish forever so...
			// There is no way back if filesystem isn't found and so considering this scenario idempotent
			if err == gounity.ErrorFilesystemNotFound || err == gounity.ErrorSnapshotNotFound {
				log.Debugf("Filesystem %s not found on the array %s during Controller Unpublish. Hence considering the call to be idempotent", volID, arrayID)
				return nil
			}
			return status.Error(codes.Internal, csiutils.GetMessageWithRunID(rid, "Find filesystem %s failed with error: %v", volID, err))
		}
		isSnapshot = true
		filesystem, err = s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.ID, arrayID)
		if err != nil {
			return err
		}
	}
	// Remove host access from NFS Share
	nfsShareName := NFSShareNamePrefix + filesystem.FileContent.Name
	if isSnapshot {
		nfsShareName = NFSShareNamePrefix + snapResp.SnapshotContent.Name
	}
	shareExists := false
	deleteShare := true
	var nfsShareID string
	for _, nfsShare := range filesystem.FileContent.NFSShare {
		if isSnapshot {
			if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.ID == volID {
				shareExists = true
				if nfsShare.Name != nfsShareName {
					// This means that share was created manually on array, hence don't delete via driver
					deleteShare = false
					nfsShareName = nfsShare.Name
				}
				nfsShareID = nfsShare.ID
			}
		} else {
			if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.ID == "" {
				shareExists = true
				if nfsShare.Name != nfsShareName {
					// This means that share was created manually on array, hence don't delete via driver
					deleteShare = false
					nfsShareName = nfsShare.Name
				}
				nfsShareID = nfsShare.ID
			}
		}
	}
	if !shareExists {
		log.Infof("NFS Share: %s not found on array.", nfsShareName)
		return nil
	}

	nfsShareResp, err := unity.FindNFSShareByID(ctx, nfsShareID)
	if err != nil {
		return status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find NFS Share: %s failed. Error: %v", nfsShareID, err))
	}
	readOnlyHosts := nfsShareResp.NFSShareContent.ReadOnlyHosts
	readWriteHosts := nfsShareResp.NFSShareContent.ReadWriteHosts
	readOnlyRootHosts := nfsShareResp.NFSShareContent.ReadOnlyRootAccessHosts
	readWriteRootHosts := nfsShareResp.NFSShareContent.RootAccessHosts

	foundIncompatible := false
	foundReadOnly := false
	foundReadWrite := false
	otherHostsWithAccess := len(readOnlyHosts)
	var readHostIDList, readWriteHostIDList []string
	for _, host := range readOnlyHosts {
		if host.ID == hostID {
			foundIncompatible = true
			break
		}
	}
	otherHostsWithAccess += len(readWriteHosts)
	if !foundIncompatible {
		for _, host := range readWriteHosts {
			if host.ID == hostID {
				foundIncompatible = true
				break
			}
		}
	}
	otherHostsWithAccess += len(readOnlyRootHosts)
	if !foundIncompatible {
		for _, host := range readOnlyRootHosts {
			if host.ID == hostID {
				foundReadOnly = true
				otherHostsWithAccess--
			} else {
				readHostIDList = append(readHostIDList, host.ID)
			}
		}
	}
	otherHostsWithAccess += len(readWriteRootHosts)
	if !foundIncompatible {
		for _, host := range readWriteRootHosts {
			if host.ID == hostID {
				foundReadWrite = true
				otherHostsWithAccess--
			} else {
				readWriteHostIDList = append(readWriteHostIDList, host.ID)
			}
		}
	}
	if foundIncompatible {
		return status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Cannot remove host access. Host: %s has access on NFS Share: %s with incompatible access mode.", nodeID, nfsShareID))
	}
	if foundReadOnly {
		if isSnapshot {
			err = unity.ModifyNFSShareCreatedFromSnapshotHostAccess(ctx, nfsShareID, readHostIDList, gounity.ReadOnlyRootAccessType)
		} else {
			err = unity.ModifyNFSShareHostAccess(ctx, volID, nfsShareID, readHostIDList, gounity.ReadOnlyRootAccessType)
		}
	} else if foundReadWrite {
		if isSnapshot {
			err = unity.ModifyNFSShareCreatedFromSnapshotHostAccess(ctx, nfsShareID, readWriteHostIDList, gounity.ReadWriteRootAccessType)
		} else {
			err = unity.ModifyNFSShareHostAccess(ctx, volID, nfsShareID, readWriteHostIDList, gounity.ReadWriteRootAccessType)
		}
	} else {
		// Idempotent case
		log.Infof("Host: %s has no access on NFS Share: %s", nodeID, nfsShareID)
	}
	if err != nil {
		return status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Removing host %s access to NFS Share failed. Error: %v", nodeID, err))
	}
	log.Debugf("Host: %s access is removed from NFS Share: %s", nodeID, nfsShareID)

	// Delete NFS Share
	if deleteShare {
		if otherHostsWithAccess > 0 {
			log.Infof("NFS Share: %s can not be deleted as other hosts have access on it.", nfsShareID)
		} else {
			if isSnapshot {
				err = unity.DeleteNFSShareCreatedFromSnapshot(ctx, nfsShareID)
			} else {
				err = unity.DeleteNFSShare(ctx, filesystem.FileContent.ID, nfsShareID)
			}
			if err != nil {
				return status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Delete NFS Share: %s Failed with error: %v", nfsShareID, err))
			}
			log.Debugf("NFS Share: %s deleted successfully.", nfsShareID)
		}
	}
	log.Debugf("ControllerUnpublishVolume successful for volid: [%s]", volumeContextID)

	return nil
}

// createMetricsCollection creates a RealTimeMetrics collection with the specified metric paths on an array
func (s *service) createMetricsCollection(ctx context.Context, arrayID string, metricPaths []string, interval int) (*types.MetricQueryCreateResponse, error) {
	ctx, _, rid := GetRunidLog(ctx)
	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Unable to get Unity client."))
	}

	query, err := unity.CreateRealTimeMetricsQuery(ctx, metricPaths, interval)
	if err != nil {
		return nil, err
	}

	return query, nil
}

// getMetricsCollection retrieves MetricsCollection data on an array given the collection 'id'
func (s *service) getMetricsCollection(ctx context.Context, arrayID string, id int) (*types.MetricQueryResult, error) {
	ctx, _, rid := GetRunidLog(ctx)
	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Unable to get Unity client."))
	}

	collection, err := unity.GetMetricsCollection(ctx, id)
	if err != nil {
		return nil, err
	}

	return collection, nil
}

func (s *service) ControllerGetVolume(ctx context.Context,
	req *csi.ControllerGetVolumeRequest,
) (*csi.ControllerGetVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing ControllerGetVolume with args: %+v", *req)

	volID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}

	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}
	var hosts []string
	abnormal := false
	message := ""

	if protocol != NFS {
		vol, err := unity.FindVolumeByID(ctx, volID)
		if err != nil {
			if err != gounity.ErrorVolumeNotFound {
				return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find volume failed with error: %v", err))
			}
			abnormal = true
			message = "Volume not found"
		}

		if !abnormal {
			content := vol.VolumeContent
			if len(content.HostAccessResponse) > 0 {
				for _, hostaccess := range content.HostAccessResponse {
					hostcontent := hostaccess.HostContent
					hosts = append(hosts, hostcontent.ID)
				}
			}

			// check if volume is in ok state
			if content.Health.Value != 5 {
				abnormal = true
				message = "Volume is not in ok state"
			}
			abnormal = false
			message = "Volume is in ok state"
		}
	} else {
		isSnapshot := false
		filesystem, err := unity.FindFilesystemByID(ctx, volID)
		if err != nil {
			var snapResp *types.Snapshot
			snapResp, err = unity.FindSnapshotByID(ctx, volID)
			if err != nil {
				if err != gounity.ErrorSnapshotNotFound {
					return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find filesystem: %s failed with error: %v", volID, err))
				}
				abnormal = true
				message = "Filesystem not found"
			}

			isSnapshot = true
			if !abnormal {
				filesystem, err = s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.ID, arrayID)
				if err != nil {
					return nil, err
				}
			}
		}

		if !abnormal {
			nfsShareID := ""
			for _, nfsShare := range filesystem.FileContent.NFSShare {
				if isSnapshot {
					if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.ID == volID {
						nfsShareID = nfsShare.ID
					}
				} else {
					if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.ID == "" {
						nfsShareID = nfsShare.ID
					}
				}
			}

			if nfsShareID != "" {
				nfsShareResp, err := unity.FindNFSShareByID(ctx, nfsShareID)
				if err != nil {
					return nil, status.Error(codes.NotFound, csiutils.GetMessageWithRunID(rid, "Find NFS Share: %s failed. Error: %v", nfsShareID, err))
				}
				readOnlyHosts := nfsShareResp.NFSShareContent.ReadOnlyHosts
				readWriteHosts := nfsShareResp.NFSShareContent.ReadWriteHosts
				readOnlyRootHosts := nfsShareResp.NFSShareContent.ReadOnlyRootAccessHosts
				readWriteRootHosts := nfsShareResp.NFSShareContent.RootAccessHosts

				for _, host := range readOnlyHosts {
					hosts = append(hosts, host.ID)
				}
				for _, host := range readWriteHosts {
					hosts = append(hosts, host.ID)
				}
				for _, host := range readOnlyRootHosts {
					hosts = append(hosts, host.ID)
				}
				for _, host := range readWriteRootHosts {
					hosts = append(hosts, host.ID)
				}
			}
		}

		// check if filesystem is in ok state
		if !abnormal {
			if filesystem.FileContent.Health.Value != 5 {
				abnormal = true
				message = "Filesystem is not in ok state"
			}
			abnormal = false
			message = "Filesystem is in ok state"
		}
	}

	resp := &csi.ControllerGetVolumeResponse{
		Volume: &csi.Volume{
			VolumeId: req.GetVolumeId(),
		},
		Status: &csi.ControllerGetVolumeResponse_VolumeStatus{
			PublishedNodeIds: hosts,
			VolumeCondition: &csi.VolumeCondition{
				Abnormal: abnormal,
				Message:  message,
			},
		},
	}
	return resp, nil
}
