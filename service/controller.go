/*
Copyright (c) 2019 Dell EMC Corporation
All Rights Reserved
*/
package service

import (
	"fmt"
	"github.com/dell/gounity/api"
	"github.com/dell/gounity/util"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"strconv"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/gounity"
	"github.com/dell/gounity/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// KeyStoragePool is the key used to get the storagepool name from the
	// volume create parameters map
	csiVolPrefix            = "csi-"
	keyStoragePool          = "storagepool"
	keyThinProvisioned      = "thinProvisioned"
	keyDescription          = "description"
	keyDataReductionEnabled = "isDataReductionEnabled"
	keyTieringPolicy        = "tieringPolicy"
	keyHostIOLimitName      = "hostIOLimitName"
	keyProtocol             = "protocol"
	keyCacheDisabled        = "isCacheDisabled"
	keyNasServer            = "nasServer"
	keyHostIoSize           = "hostIoSize"
)

const (
	FC                   = "FC"
	ISCSI                = "iSCSI"
	NFS                  = "NFS"
	ProtocolNFS          = int(0)
	MAX_ENTRIES_SNAPSHOT = 100
	MAX_ENTRIES_VOLUME   = 100
)

var (
	errUnknownAccessType      = "unknown access type is not Block or Mount"
	errUnknownAccessMode      = "access mode cannot be UNKNOWN"
	errIncompatibleAccessMode = "access mode should be single node reader or single node writer"
	errNoMultiNodeWriter      = "multi-node with writer(s) only supported for block access type"
)

func (s *service) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing CreateVolume with args: %+v", *req)

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume Name cannot be empty"))
	}

	// Validate volume capabilities
	vcs := req.GetVolumeCapabilities()
	if len(vcs) == 0 {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Controller Volume Capability are not provided"))
	}
	if vcs != nil {
		isBlock := accTypeIsBlock(vcs)
		if isBlock {
			return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Block Volume Capability is not supported"))
		}
	}

	supported, reason := valVolumeCaps(vcs, nil)
	if !supported {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume Capabilities are not supported. Reason=["+reason+"]"))
	}

	params := req.GetParameters()
	storagePool, ok := params[keyStoragePool]
	desc := params[keyDescription]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "`%s` is a required parameter", keyStoragePool))
	}

	// AccessibleTopology not currently supported
	accessibility := req.GetAccessibilityRequirements()
	if accessibility != nil {
		log.Errorf("Volume AccessibilityRequirements is not supported")
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume AccessibilityRequirements is not supported"))
	}

	protocol, _ := params[keyProtocol]
	if protocol == "" {
		log.Debugf("Parameter %s is not set [%s]", keyProtocol, params[keyProtocol])
		log.Debugf("Default protocol FC would be used.")
		protocol = "FC"
	}

	if protocol == NFS {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Protocol NFS is not supported. Supported protocols are FC, iSCSI"))
	} else if !(protocol == ISCSI || protocol == FC) {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Invalid value provided in `%s`. Possible values are FC, iSCSI", keyProtocol))
	}

	name := req.GetName()

	if req.GetCapacityRange() == nil {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "RequiredBytes cannot be empty"))
	}
	size := req.GetCapacityRange().RequiredBytes
	if size <= 0 {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "RequiredBytes should be greater then 0"))
	}

	thin, err := strconv.ParseBool(params[keyThinProvisioned])
	if err != nil {
		thin = true
		log.Debugf("Parameter %s is not set [%s]", keyThinProvisioned, params[keyThinProvisioned])
	}

	dataReduction, err := strconv.ParseBool(params[keyDataReductionEnabled])
	if err != nil {
		log.Debugf("Parameter %s is not set [%s]", keyDataReductionEnabled, params[keyDataReductionEnabled])
	}

	tieringPolicy, err := strconv.ParseInt(params[keyTieringPolicy], 0, 32)
	if err != nil {
		tieringPolicy = 0
		log.Debugf("Parameter %s is not set [%s]", keyTieringPolicy, params[keyTieringPolicy])
	}

	hostIOLimitName := strings.TrimSpace(params[keyHostIOLimitName])

	// Creating Volume from a volume content source (only snapshot is supported currently)
	contentSource := req.GetVolumeContentSource()
	if contentSource != nil {
		volumeSource := contentSource.GetVolume()
		if volumeSource != nil {
			return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume clone is not supported"))
		}
		snapshotSource := contentSource.GetSnapshot()
		if snapshotSource != nil {
			snapId := snapshotSource.SnapshotId
			if snapId == "" {
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Source snapshot ID cannot be empty"))
			}
			log.Debugf("Creating the volume from snapshot: %s", snapId)
			snapApi := gounity.NewSnapshot(s.unity)
			snapResp, err := snapApi.FindSnapshotById(ctx, snapId)
			if err != nil {
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Source snapshot not found: %s", snapId))
			}

			volumeApi := gounity.NewVolume(s.unity)
			volId := snapResp.SnapshotContent.StorageResource.Id
			sourceVolResp, err := volumeApi.FindVolumeById(ctx, volId)
			if err != nil {
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Source volume not found: %s", volId))
			}
			// Validate the size is the same.
			if int64(sourceVolResp.VolumeContent.SizeTotal) != size {
				return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Requested size %d is incompatible with source volume size %d",
					size, int64(sourceVolResp.VolumeContent.SizeTotal)))
			}

			// Validate the storagePool is the same.
			if sourceVolResp.VolumeContent.Pool.Id != storagePool {
				return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume storage pool %s is different than the requested storage pool %s",
					sourceVolResp.VolumeContent.Pool.Id, storagePool))
			}

			//Validate the thinProvisioned parameter
			if sourceVolResp.VolumeContent.IsThinEnabled != thin {
				return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume thin provision %s is different than the requested thin provision %s",
					sourceVolResp.VolumeContent.IsThinEnabled, thin))
			}

			//Validate the dataReduction parameter
			if sourceVolResp.VolumeContent.IsDataReductionEnabled != dataReduction {
				return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume data reduction %s is different than the requested data reduction %s",
					sourceVolResp.VolumeContent.IsDataReductionEnabled, dataReduction))
			}

			volResp, _ := volumeApi.FindVolumeByName(ctx, name)
			if volResp != nil {
				//Idempotency Check
				if volResp.VolumeContent.IsThinClone == true && len(volResp.VolumeContent.ParentSnap.Id) > 0 && volResp.VolumeContent.ParentSnap.Id == snapId {
					log.Info("Volume exists in the requested state")
					return utils.GetVolumeResponseFromVolume(volResp, protocol), nil
				}
				return nil, status.Error(codes.AlreadyExists, utils.GetMessageWithRunID(rid, "Volume with same name %s already exists", name))
			}

			if snapResp.SnapshotContent.IsAutoDelete == true {
				err = snapApi.ModifySnapshotAutoDeleteParameter(ctx, snapId)
				if err != nil {
					return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Unable to modify auto-delete parameter for snapshot %s", snapId))
				}
			}

			volResp, err = volumeApi.CreteLunThinClone(ctx, name, snapId, volId)
			if err != nil {
				return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Create volume from snapshot failed with error %v", err))
			}
			volResp, err = volumeApi.FindVolumeByName(ctx, name)
			log.Debugf("Find Volume response: %v Error: %v", volResp, err)
			if volResp != nil {
				return utils.GetVolumeResponseFromVolume(volResp, protocol), nil
			}
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Volume not found after create. %v", err))
		}
	}

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
	volumeApi := gounity.NewVolume(s.unity)

	var hostIOLimit *types.IoLimitPolicy
	var hostIOLimitId string
	if hostIOLimitName != "" {
		hostIOLimit, err = volumeApi.FindHostIOLimitByName(ctx, hostIOLimitName)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "HostIOLimitName %s not found. Error: %v", hostIOLimitName, err))
		}

		hostIOLimitId = hostIOLimit.IoLimitPolicyContent.Id
	}

	//Idempotency check
	vol, _ := volumeApi.FindVolumeByName(ctx, name)
	if vol != nil {
		content := vol.VolumeContent
		if int64(content.SizeTotal) == size {
			log.Info("Volume exists in the requested state with same size")
			return utils.GetVolumeResponseFromVolume(vol, protocol), nil
		} else {
			log.Info("'Volume name' already exists and size is different")
			return nil, status.Error(codes.AlreadyExists, utils.GetMessageWithRunID(rid, "'Volume name' already exists and size is different."))
		}
	}

	log.Debug("Volume does not exist -- proceeding to Create New Volume")
	resp, err := volumeApi.CreateLun(ctx, name, storagePool, desc, uint64(size), int(tieringPolicy), hostIOLimitId, thin, dataReduction)
	log.Debugf("Volume create response:%v Error:%v", resp, err)
	if err != nil {
		return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Create volume error. %v", err))
	}

	resp, err = volumeApi.FindVolumeByName(ctx, name)
	log.Infof("Find Volume response: %v Error: %v", resp, err)
	if resp != nil {
		volumeResp := utils.GetVolumeResponseFromVolume(resp, protocol)
		return volumeResp, nil
	}

	return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Volume/Filesystem not found after create. %v", err))
}

func (s *service) DeleteVolume(
	ctx context.Context,
	req *csi.DeleteVolumeRequest) (
	*csi.DeleteVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing DeleteVolume with args: %+v", *req)

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "VolumeId can't be empty."))
	}
	volumeResp := &csi.DeleteVolumeResponse{}
	var err error

	volumeAPI := gounity.NewVolume(s.unity)
	err = volumeAPI.DeleteVolume(ctx, req.GetVolumeId())
	//Idempotency check
	if err == nil {
		return volumeResp, nil
	} else if err == gounity.NotFoundError {
		log.Info("Volume not found on array")
		return volumeResp, nil
	}
	return nil, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Delete volume error %v", err))
}

func (s *service) ControllerPublishVolume(
	ctx context.Context,
	req *csi.ControllerPublishVolumeRequest) (
	*csi.ControllerPublishVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing ControllerPublishVolume with args: %+v", *req)

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	if readOnly := req.GetReadonly(); readOnly == true {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Readonly must be false, because the supported mode only SINGLE_NODE_WRITER"))
	}

	if req.GetVolumeCapability().GetAccessMode().Mode != csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER {
		log.Debugf("Access mode %s is not supported", req.GetVolumeCapability().GetAccessMode().Mode)
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume AccessMode is supported only by SINGLE_NODE_WRITER"))
	}

	volID := req.GetVolumeId()
	if volID == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume ID is required"))
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Node ID is required"))
	}

	vc := req.GetVolumeCapability()
	if vc == nil {
		log.Errorf("volume capability is required")
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume capability is required"))
	}
	am := vc.GetAccessMode()
	if am == nil {
		log.Error("access mode is required")
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "access mode is required"))
	}

	if am.Mode == csi.VolumeCapability_AccessMode_UNKNOWN {
		log.Errorf(errUnknownAccessMode)
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, errUnknownAccessMode))
	}

	protocol := req.GetVolumeContext()[keyProtocol]
	log.Debugf("Protocol is: %s", protocol)

	hostAPI := gounity.NewHost(s.unity)
	host, err := hostAPI.FindHostByName(ctx, nodeID)
	if err != nil {
		log.Errorf("Find Host Failed %v", err)
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find Host Failed %v", err))
	}
	hostContent := host.HostContent
	hostID := hostContent.ID

	if protocol == FC && len(hostContent.FcInitiators) == 0 {
		log.Debug("Cannot publish volume as protocol in the Storage class is 'FC' but the node has no valid FC initiators")
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Cannot publish volume as protocol in the Storage class is 'FC' but the node has no valid FC initiators"))
	} else if protocol == ISCSI && len(hostContent.IscsiInitiators) == 0 {
		log.Debug("Cannot publish volume as protocol in the Storage class is 'iScsi' but the node has no valid iScsi initiators")
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Cannot publish volume as protocol in the Storage class is 'iScsi' but the node has no valid iScsi initiators"))
	}

	volumeAPI := gounity.NewVolume(s.unity)
	vol, err := volumeAPI.FindVolumeById(ctx, volID)
	if err != nil {
		log.Error("Find Volume Failed ", err)
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find volume Failed %v", err))
	}

	pinfo := make(map[string]string)
	pinfo["lun"] = volID
	pinfo["host"] = nodeID

	//Idempotency check
	content := vol.VolumeContent
	if len(content.HostAccessResponse) > 1 { //If the volume has 2 or more host access
		return nil, status.Error(codes.Aborted, utils.GetMessageWithRunID(rid, "Volume has been published to multiple hosts already."))
	}

	for _, hostaccess := range content.HostAccessResponse {
		hostcontent := hostaccess.HostContent
		hostAccessID := hostcontent.ID
		if hostAccessID == hostID {
			log.Info("Volume has been published to the given host and exists in the required state.")
			return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
		} else {
			return nil, status.Error(codes.Aborted, utils.GetMessageWithRunID(rid, "Volume has been published to a different host already."))
		}
	}

	log.Info("Adding host access to ", hostID, " on volume ", volID)
	err = volumeAPI.ExportVolume(ctx, volID, hostID)
	if err != nil {
		log.Error("Export Volume Failed. ", err)
		return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Export Volume Failed %v", err))
	}

	return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
}

func (s *service) ControllerUnpublishVolume(
	ctx context.Context,
	req *csi.ControllerUnpublishVolumeRequest) (
	*csi.ControllerUnpublishVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing ControllerUnpublishVolume with args: %+v", *req)

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	volID := req.GetVolumeId()
	if volID == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume ID is required"))
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Node ID is required"))
	}

	hostAPI := gounity.NewHost(s.unity)
	host, err := hostAPI.FindHostByName(ctx, nodeID)
	if err != nil {
		log.Error("Find Host Failed ", err)
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find Host Failed %v", err))
	}
	hostContent := host.HostContent
	hostID := hostContent.ID

	volumeAPI := gounity.NewVolume(s.unity)
	vol, err := volumeAPI.FindVolumeById(ctx, volID)
	if err != nil {
		log.Error("Find Volume Failed ", err)
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find Volume Failed %v", err))
	}

	//Idempotency check
	content := vol.VolumeContent
	if len(content.HostAccessResponse) > 0 {
		log.Info("Removing Host access on Volume ", volID)
		err = volumeAPI.UnexportVolume(ctx, volID)
		if err != nil {
			log.Debug("Unexport Volume Failed. ", err)
			return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Unexport Volume Failed. %v", err))
		}
	} else {
		log.Info(fmt.Sprintf("The given Node %s does not have access on the given volume %s. Already in Unpublished state.", hostID, volID))
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (s *service) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing ValidateVolumeCapabilities with args: %+v", *req)

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}
	volID := req.GetVolumeId()
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "VolumeId can't be empty."))
	}

	volumeAPI := gounity.NewVolume(s.unity)
	vol, err := volumeAPI.FindVolumeById(ctx, volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "volume not found error: %v", err))
	}

	vcs := req.GetVolumeCapabilities()
	supported, reason := valVolumeCaps(vcs, vol)
	resp := &csi.ValidateVolumeCapabilitiesResponse{}
	if supported {
		// The optional fields volume_context and parameters are not passed.
		confirmed := &csi.ValidateVolumeCapabilitiesResponse_Confirmed{}
		confirmed.VolumeCapabilities = vcs
		resp.Confirmed = confirmed
		return resp, nil
	} else {
		resp.Message = reason
		return resp, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Unsupported capability"))
	}
}

func (s *service) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing ListVolumes with args: %+v", *req)

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	var (
		startToken int
		err        error
		maxEntries = int(req.MaxEntries)
	)

	//Limiting the number of volumes to 100 to avoid timeout issues
	if maxEntries > MAX_ENTRIES_VOLUME || maxEntries == 0 {
		maxEntries = MAX_ENTRIES_VOLUME
	}

	if req.StartingToken != "" {
		i, err := strconv.ParseInt(req.StartingToken, 10, 32)
		if err != nil {
			return nil, status.Error(codes.Aborted, utils.GetMessageWithRunID(rid, "Unable to parse StartingToken: %v into uint32", req.StartingToken))
		}
		startToken = int(i)
	}

	volumeAPI := gounity.NewVolume(s.unity)
	volumes, nextToken, err := volumeAPI.ListVolumes(ctx, startToken, maxEntries)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Unable to get the volumes: %v", err))
	}
	// Process the source volumes and make CSI Volumes
	entries, err := s.getCSIVolumes(volumes)
	if err != nil {
		return nil, err
	}
	return &csi.ListVolumesResponse{
		Entries:   entries,
		NextToken: strconv.Itoa(nextToken),
	}, nil
}

func (s *service) GetCapacity(
	ctx context.Context,
	req *csi.GetCapacityRequest) (
	*csi.GetCapacityResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing GetCapacity with args: %+v", *req)

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	params := req.GetParameters()
	storagePool, ok := params[keyStoragePool]

	if !ok {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "`%s` is a required parameter", keyStoragePool))
	}

	poolAPI := gounity.NewStoragePool(s.unity)
	var content types.StoragePoolContent

	if pool, err := poolAPI.FindStoragePoolById(ctx, storagePool); pool == nil {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Storage Pool ID `%s` not found. %v", storagePool, err))
	} else {
		content = pool.StoragePoolContent
	}

	getCapacityResponse := &csi.GetCapacityResponse{
		AvailableCapacity: int64(content.FreeCapacity),
	}
	return getCapacityResponse, nil
}

func (s *service) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing CreateSnapshot with args: %+v", *req)
	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	if len(req.SourceVolumeId) == 0 {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Storage Resource ID cannot be empty"))
	}
	var err error
	req.Name, err = util.ValidateResourceName(req.Name, api.MaxResourceNameLength)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "invalid snapshot name [%v]", err))
	}

	snapApi := gounity.NewSnapshot(s.unity)
	//Idempotenc check
	snap, _ := snapApi.FindSnapshotByName(ctx, req.Name)
	if snap != nil {
		if snap.SnapshotContent.StorageResource.Id == req.SourceVolumeId {
			log.Infof("Snapshot already exists with same name %s for same storage resource %s", req.Name, req.SourceVolumeId)
			return utils.GetSnapshotResponseFromSnapshot(snap), nil
		}
		log.Errorf("Snapshot already exists with same name %s for a different storage resource %s", req.Name, snap.SnapshotContent.StorageResource.Id)
		return nil, status.Error(codes.AlreadyExists, utils.GetMessageWithRunID(rid, "Snapshot with same name %s already exists for storage resource %s", req.Name, snap.SnapshotContent.StorageResource.Id))
	}
	newSnapshot, err := snapApi.CreateSnapshot(ctx, req.SourceVolumeId, req.Name, req.Parameters["description"], req.Parameters["retentionDuration"])
	if err != nil {
		return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Create Snapshot error: %v", err))
	}
	newSnapshot, _ = snapApi.FindSnapshotByName(ctx, req.Name)
	if newSnapshot != nil {
		return utils.GetSnapshotResponseFromSnapshot(newSnapshot), nil
	} else {
		return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Find Snapshot error after create. %v", err))
	}
}

func (s *service) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing DeleteSnapshot with args: %+v", *req)
	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}
	if req.SnapshotId == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "snapshot ID is mandatory parameter"))
	}
	snapApi := gounity.NewSnapshot(s.unity)
	//Idempotenc check
	snap, err := snapApi.FindSnapshotById(ctx, req.SnapshotId)
	//snapshot exists, continue deleting the snapshot
	if err != nil {
		log.Info("Snapshot doesn't exists")
	}

	if snap != nil {
		err := snapApi.DeleteSnapshot(ctx, req.SnapshotId)
		if err != nil {
			return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Delete Snapshot error: %v", err))
		}
	}

	delSnapResponse := &csi.DeleteSnapshotResponse{}
	return delSnapResponse, nil
}

func (s *service) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing ListSnapshot with args: %+v", *req)
	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	var (
		startToken     int
		err            error
		maxEntries     = int(req.MaxEntries)
		sourceVolumeId = req.SourceVolumeId
		snapshotId     = req.SnapshotId
	)

	//Limiting the number of snapshots to 100 to avoid timeout issues
	if maxEntries > MAX_ENTRIES_SNAPSHOT || maxEntries == 0 {
		maxEntries = MAX_ENTRIES_SNAPSHOT
	}

	if req.StartingToken != "" {
		i, err := strconv.ParseInt(req.StartingToken, 10, 32)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Unable to parse StartingToken: %v into uint32", req.StartingToken))
		}
		startToken = int(i)
	}
	snapApi := gounity.NewSnapshot(s.unity)
	snaps, nextToken, err := snapApi.ListSnapshots(ctx, startToken, maxEntries, sourceVolumeId, snapshotId)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Unable to get the snapshots: %v", err))
	}

	// Process the source snapshots and make CSI Snapshot
	entries, err := s.getCSISnapshots(snaps)
	if err != nil {
		return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, err.Error()))
	}
	return &csi.ListSnapshotsResponse{
		Entries:   entries,
		NextToken: strconv.Itoa(nextToken),
	}, nil
}

func (s *service) controllerProbe(ctx context.Context) error {
	rid, _ := utils.GetRunidAndLogger(ctx)
	// Check that we have the details needed to login to the Gateway
	if s.opts.Endpoint == "" {
		return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Missing Unity endpoint"))
	}
	if s.opts.User == "" {
		return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "missing Unity user"))
	}
	if s.opts.Password == "" {
		return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Missing Unity password"))
	}

	// Create our Unity API client, if needed
	if s.unity == nil {
		c, err := gounity.NewClientWithArgs(ctx, s.opts.Endpoint, s.opts.Insecure, s.opts.UseCerts)
		if err != nil {
			return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Unable to create Unity client: %s", err.Error()))
		}
		s.unity = c
	}

	if s.unity.GetToken() == "" {
		err := s.unity.Authenticate(ctx, &gounity.ConfigConnect{
			Endpoint: s.opts.Endpoint,
			Username: s.opts.User,
			Password: s.opts.Password,
		})
		if err != nil {
			if e, ok := status.FromError(err); ok {
				if e.Code() == codes.Unauthenticated {
					return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Unable to login to Unity. Error: %s", err.Error()))
				}
			}
			return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Unable to login to Unity. Verify hostname/IP Address of unity. Error: %s", err.Error()))
		}
	}

	return nil
}

// ControllerGetCapabilities implements the default GRPC callout.
// Default supports all capabilities
func (cs *service) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Infof("Executing ControllerGetCapabilities with args: %+v", *req)
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
					},
				},
			},
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
					},
				},
			},
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
					},
				},
			},
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_GET_CAPACITY,
					},
				},
			},
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
					},
				},
			},
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
					},
				},
			},
		},
	}, nil
}

func (s *service) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing ControllerExpandVolume with args: %+v", *req)
	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volumeId is mandatory parameter"))
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
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "required bytes can not be 0 or less"))
	}

	volumeApi := gounity.NewVolume(s.unity)
	//Idempotency check
	volume, err := volumeApi.FindVolumeById(ctx, req.VolumeId)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "unable to find the volume"))
	}

	if volume.VolumeContent.SizeTotal > uint64(capacity) {
		return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "requested new capacity smaller than existing capacity"))
	}

	volumeResp := &csi.ControllerExpandVolumeResponse{
		CapacityBytes: capacity,
	}
	if volume.VolumeContent.SizeTotal == uint64(capacity) {
		log.Infof("New Volume size (%d) is same as existing Volume size. Ignoring expand volume operation.", volume.VolumeContent.SizeTotal)
		volumeResp.NodeExpansionRequired = false
		return volumeResp, nil
	}

	err = volumeApi.ExpandVolume(ctx, req.VolumeId, uint64(capacity))
	if err != nil {
		return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "unable to expand volume. Error %v", err))
	}

	volume, err = volumeApi.FindVolumeById(ctx, req.VolumeId)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "unable to find the volume"))
	}
	volumeResp.CapacityBytes = int64(volume.VolumeContent.SizeTotal)
	volumeResp.NodeExpansionRequired = true
	return volumeResp, err
}

func (s *service) getCSIVolumes(volumes []types.Volume) ([]*csi.ListVolumesResponse_Entry, error) {
	entries := make([]*csi.ListVolumesResponse_Entry, len(volumes))
	for i, vol := range volumes {
		// Make the additional volume attributes
		attributes := map[string]string{
			"Name":          vol.VolumeContent.Name,
			"Type":          strconv.Itoa(vol.VolumeContent.Type),
			"Wwn":           vol.VolumeContent.Wwn,
			"StoragePoolID": vol.VolumeContent.Pool.Id,
		}
		//Create CSI volume
		vi := &csi.Volume{
			VolumeId:      vol.VolumeContent.ResourceId,
			CapacityBytes: int64(vol.VolumeContent.SizeTotal),
			VolumeContext: attributes,
		}

		entries[i] = &csi.ListVolumesResponse_Entry{
			Volume: vi,
		}
	}

	return entries, nil
}

func (s *service) getCSISnapshots(snaps []types.Snapshot) ([]*csi.ListSnapshotsResponse_Entry, error) {
	entries := make([]*csi.ListSnapshotsResponse_Entry, len(snaps))
	for i, snap := range snaps {
		isReady := false
		if snap.SnapshotContent.State == 2 {
			isReady = true
		}
		var timestamp *timestamp.Timestamp
		if !snap.SnapshotContent.CreationTime.IsZero() {
			timestamp, _ = ptypes.TimestampProto(snap.SnapshotContent.CreationTime)
		}

		//Create CSI Snapshot
		vi := &csi.Snapshot{
			SizeBytes:      snap.SnapshotContent.Size,
			SnapshotId:     snap.SnapshotContent.ResourceId,
			SourceVolumeId: snap.SnapshotContent.StorageResource.Id,
			CreationTime:   timestamp,
			ReadyToUse:     isReady,
		}

		entries[i] = &csi.ListSnapshotsResponse_Entry{
			Snapshot: vi,
		}
	}
	return entries, nil
}
