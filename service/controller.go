/*
Copyright (c) 2019 Dell EMC Corporation
All Rights Reserved
*/
package service

import (
	"fmt"
	"github.com/dell/gounity/util"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"strconv"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/gounity"
	types "github.com/dell/gounity/payloads"
	"github.com/golang/glog"
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
)

var (
	errUnknownAccessType      = "unknown access type is not Block or Mount"
	errUnknownAccessMode      = "access mode cannot be UNKNOWN"
	errIncompatibleAccessMode = "access mode should be single node reader or single node writer"
	errNoMultiNodeWriter      = "multi-node with writer(s) only supported for block access type"
)

func (s *service) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	log.Info("Executing CreateVolume")
	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	if err := validateVolumeCreateParam(req); err != nil {
		return nil, err
	}

	// Validate volume capabilities
	vcs := req.GetVolumeCapabilities()
	if len(vcs) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Controller Volume Capability are not provided")
	}
	if vcs != nil {
		isBlock := accTypeIsBlock(vcs)
		if isBlock {
			return nil, status.Error(codes.InvalidArgument, "Block Volume Capability is not supported")
		}
	}

	supported, reason := valVolumeCaps(vcs, nil)
	if !supported {
		return nil, status.Error(codes.InvalidArgument, "Volume Capabilities are not supported. Reason=["+reason+"]")
	}

	params := req.GetParameters()
	storagePool, ok := params[keyStoragePool]
	desc := params[keyDescription]

	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "`%s` is a required parameter", keyStoragePool)
	}

	// Volume content source is not supported until Create volume from Snapshot is implemented
	contentSource := req.GetVolumeContentSource()
	if contentSource != nil {
		log.Errorf("VolumeContentSource - Create volume from Snapshot is not supported %v", contentSource)
		return nil, status.Error(codes.Unimplemented, "VolumeContentSource - Create volume from Snapshot is not supported")
	}

	var thin = true
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

	volumeApi := gounity.NewVolume(s.unity)
	hostIOLimitName := strings.TrimSpace(params[keyHostIOLimitName])
	log.Debug("hostIOLimitName: ", hostIOLimitName)
	var hostIOLimit *types.IoLimitPolicy
	var hostIOLimitId string
	if hostIOLimitName != "" {
		hostIOLimit, err = volumeApi.FindHostIOLimitByName(hostIOLimitName)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("HostIOLimitName %s not found. Error: %v", hostIOLimitName, err))
		}

		hostIOLimitId = hostIOLimit.IoLimitPolicyContent.Id
	}

	name := req.GetName()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "'name' cannot be empty")
	}

	//Add prefix csi- if the volume name doesn't have a prefix given by the side car container
	if !strings.Contains(name, "-") {
		name = csiVolPrefix + name
	}

	if req.GetCapacityRange() == nil {
		return nil, status.Error(codes.InvalidArgument, "RequiredBytes can not be empty")
	}
	size := req.GetCapacityRange().RequiredBytes
	if size <= 0 {
		return nil, status.Error(codes.InvalidArgument, "RequiredBytes should be more then 0")
	}

	//Idempotency check
	vol, _ := volumeApi.FindVolumeByName(name)
	if vol != nil {
		content := vol.VolumeContent
		if int64(content.SizeTotal) == size {
			log.Info("Volume exists in the requested state with same size")
			volumeResp := utils.GetVolumeResponseFromVolume(vol)
			return volumeResp, nil
		} else {
			log.Info("'Volume name' already exists and size is different")
			return nil, status.Error(codes.AlreadyExists, "'Volume name' already exists and size is different.")
		}
	}

	log.Info("Volume does not exist -- proceeding to Create New Volume")
	resp, err := volumeApi.CreateLun(name, storagePool, desc, uint64(size), int(tieringPolicy), hostIOLimitId, thin, dataReduction)
	log.Infof("Volume create response:%v Error:%v", resp, err)
	if err != nil {
		return nil, status.Error(codes.Unknown, "Create volume error."+err.Error())
	}

	resp, err = volumeApi.FindVolumeByName(name)
	log.Infof("Find Volume response: %v Error: %v", resp, err)
	if resp != nil {
		volumeResp := utils.GetVolumeResponseFromVolume(resp)
		return volumeResp, nil
	}
	return nil, status.Error(codes.NotFound, fmt.Sprintf("Volume not found after create. %v", err))
}

func (s *service) DeleteVolume(
	ctx context.Context,
	req *csi.DeleteVolumeRequest) (
	*csi.DeleteVolumeResponse, error) {
	log.Info("Executing DeleteVolume")

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId can't be empty.")
	}
	volumeResp := &csi.DeleteVolumeResponse{}

	volumeApi := gounity.NewVolume(s.unity)

	err := volumeApi.DeleteVolume(req.GetVolumeId())
	//Idempotency check
	if err == nil {
		return volumeResp, nil
	} else if err == gounity.NotFoundError {
		log.Info("Volume not found on array")
		return volumeResp, nil
	}
	return nil, status.Error(codes.Unknown, fmt.Sprintf("Delete volume error %v", err))
}

func (s *service) ControllerPublishVolume(
	ctx context.Context,
	req *csi.ControllerPublishVolumeRequest) (
	*csi.ControllerPublishVolumeResponse, error) {
	log.Info("Executing ControllerPublishVolume")

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	if readOnly := req.GetReadonly(); readOnly == true {
		return nil, status.Error(codes.InvalidArgument, "Readonly must be false, because the supported mode only SINGLE_NODE_WRITER")
	}

	if req.GetVolumeCapability().GetAccessMode().Mode != csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER {
		log.Debug(fmt.Sprintf("Access mode %s is not supported", req.GetVolumeCapability().GetAccessMode().Mode))
		return nil, status.Error(codes.InvalidArgument, "volume AccessMode is supported only by SINGLE_NODE_WRITER")
	}

	volID := req.GetVolumeId()
	if volID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Node ID is required")
	}

	vc := req.GetVolumeCapability()
	if vc == nil {
		log.Error("volume capability is required")
		return nil, status.Error(codes.InvalidArgument, "volume capability is required")
	}
	am := vc.GetAccessMode()
	if am == nil {
		log.Error("access mode is required")
		return nil, status.Error(codes.InvalidArgument, "access mode is required")
	}

	if am.Mode == csi.VolumeCapability_AccessMode_UNKNOWN {
		log.Error(errUnknownAccessMode)
		return nil, status.Error(codes.InvalidArgument, errUnknownAccessMode)
	}

	hostAPI := gounity.NewHost(s.unity)
	host, err := hostAPI.FindHostByName(nodeID)
	if err != nil {
		log.Error("Find Host Failed ", err)
		return nil, status.Error(codes.NotFound, fmt.Sprintf("Find Host Failed %v", err))
	}
	hostContent := host.HostContent
	hostID := hostContent.ID

	volumeAPI := gounity.NewVolume(s.unity)
	vol, err := volumeAPI.FindVolumeById(volID)
	if err != nil {
		log.Error("Find Volume Failed ", err)
		return nil, status.Error(codes.NotFound, fmt.Sprintf("Find volume Failed %v", err))
	}

	pinfo := make(map[string]string)
	pinfo["lun"] = volID
	pinfo["host"] = nodeID

	//Idempotency check
	content := vol.VolumeContent
	if len(content.HostAccessResponse) > 1 { //If the volume has 2 or more host access
		return nil, status.Error(codes.Aborted, "Volume has been published to multiple hosts already.")
	}

	for _, hostaccess := range content.HostAccessResponse {
		hostcontent := hostaccess.HostContent
		hostAccessID := hostcontent.ID
		if hostAccessID == hostID {
			log.Info("Volume has been published to the given host and exists in the required state.")
			return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
		} else {
			return nil, status.Error(codes.Aborted,
				"Volume has been published to a different host already.")
		}
	}

	log.Info("Adding host access to ", hostID, " on volume ", volID)
	err = volumeAPI.ExportVolume(volID, hostID)
	if err != nil {
		log.Error("Export Volume Failed. ", err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("Export Volume Failed %v", err))
	}

	return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
}

func (s *service) ControllerUnpublishVolume(
	ctx context.Context,
	req *csi.ControllerUnpublishVolumeRequest) (
	*csi.ControllerUnpublishVolumeResponse, error) {
	log.Info("Executing ControllerUnpublishVolume")

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	volID := req.GetVolumeId()
	if volID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"Volume ID is required")
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"Node ID is required")
	}

	hostAPI := gounity.NewHost(s.unity)
	host, err := hostAPI.FindHostByName(nodeID)
	if err != nil {
		log.Error("Find Host Failed ", err)
		return nil, status.Error(codes.NotFound, fmt.Sprintf("Find Host Failed %v", err))
	}
	hostContent := host.HostContent
	hostID := hostContent.ID

	volumeAPI := gounity.NewVolume(s.unity)
	vol, err := volumeAPI.FindVolumeById(volID)
	if err != nil {
		log.Error("Find Volume Failed ", err)
		return nil, status.Error(codes.NotFound, fmt.Sprintf("Find Volume Failed %v", err))
	}

	//Idempotency check
	content := vol.VolumeContent
	if len(content.HostAccessResponse) > 0 {
		log.Info("Removing Host access on Volume ", volID)
		err = volumeAPI.UnexportVolume(volID)
		if err != nil {
			log.Debug("Unexport Volume Failed. ", err)
			return nil, status.Error(codes.Unknown, fmt.Sprintf("Unexport Volume Failed. %v", err))
		}
	} else {
		log.Info(fmt.Sprintf("The given Node %s does not have access on the given volume %s. Already in Unpublished state.", hostID, volID))
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *service) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	log.Info("Executing ValidateVolumeCapabilities")

	if err := cs.requireProbe(ctx); err != nil {
		return nil, err
	}
	volID := req.GetVolumeId()
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId can't be empty.")
	}

	volumeAPI := gounity.NewVolume(cs.unity)
	vol, err := volumeAPI.FindVolumeById(volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("volume not found error: %v", err))
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
		return resp, status.Error(codes.Unknown, fmt.Sprintf("Unsupported capability"))
	}
}

func valVolumeCaps(vcs []*csi.VolumeCapability, vol *types.Volume) (bool, string) {
	var (
		supported = true
		isBlock   = accTypeIsBlock(vcs)
		reason    string
	)
	// Check that all access types are valid
	if !checkValidAccessTypes(vcs) {
		return false, errUnknownAccessType
	}

	for _, vc := range vcs {
		am := vc.GetAccessMode()
		if am == nil {
			continue
		}

		switch am.Mode {
		case csi.VolumeCapability_AccessMode_UNKNOWN:
			supported = false
			reason = errUnknownAccessMode
			break
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
			break
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			if !isBlock {
				supported = false
				reason = errNoMultiNodeWriter
			}

			supported = false
			reason = fmt.Sprintf("%s %s received:[%s]", reason, errIncompatibleAccessMode, vc.AccessMode)
			break
		default:
			// This is to guard against new access modes not understood
			supported = false
			reason = fmt.Sprintf("%s %s", reason, errUnknownAccessMode)
		}
	}

	return supported, reason
}

func accTypeIsBlock(vcs []*csi.VolumeCapability) bool {
	for _, vc := range vcs {
		if at := vc.GetBlock(); at != nil {
			return true
		}
	}
	return false
}

func checkValidAccessTypes(vcs []*csi.VolumeCapability) bool {
	for _, vc := range vcs {
		if vc == nil {
			continue
		}
		atblock := vc.GetBlock()
		if atblock != nil {
			continue
		}
		atmount := vc.GetMount()
		if atmount != nil {
			continue
		}
		// Unknown access type, we should reject it.
		return false
	}
	return true
}

func (cs *service) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	log.Info("Executing ListVolumes")

	if err := cs.requireProbe(ctx); err != nil {
		return nil, err
	}

	var (
		startToken int
		err        error
		maxEntries = int(req.MaxEntries)
	)

	//Limiting the number of volumes to 100 to avoid timeout issues
	if maxEntries > 100 || maxEntries == 0 {
		maxEntries = 100
	}

	if req.StartingToken != "" {
		i, err := strconv.ParseInt(req.StartingToken, 10, 32)
		if err != nil {
			return nil, status.Errorf(
				codes.Aborted,
				"Unable to parse StartingToken: %v into uint32",
				req.StartingToken)
		}
		startToken = int(i)
	}

	volumeAPI := gounity.NewVolume(cs.unity)
	volumes, nextToken, err := volumeAPI.ListVolumes(startToken, maxEntries)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Unable to get the volumes: %v", err)
	}
	// Process the source volumes and make CSI Volumes
	entries, err := cs.getCSIVolumes(volumes)
	if err != nil {
		return nil, err
	}
	return &csi.ListVolumesResponse{
		Entries:   entries,
		NextToken: strconv.Itoa(nextToken),
	}, nil
}

func (cs *service) GetCapacity(
	ctx context.Context,
	req *csi.GetCapacityRequest) (
	*csi.GetCapacityResponse, error) {
	if err := cs.requireProbe(ctx); err != nil {
		return nil, err
	}
	log.Info("Executing GetCapacity")

	params := req.GetParameters()
	storagePool, ok := params[keyStoragePool]

	if !ok {
		return nil, status.Errorf(codes.InvalidArgument,
			"`%s` is a required parameter", keyStoragePool)
	}

	poolAPI := gounity.NewStoragePool(cs.unity)
	var content types.StoragePoolContent

	if pool, err := poolAPI.FindStoragePoolById(storagePool); pool == nil {
		return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("Storage Pool ID `%s` not found. %v", storagePool, err))
	} else {
		content = pool.StoragePoolContent
	}

	getCapacityResponse := &csi.GetCapacityResponse{
		AvailableCapacity: int64(content.FreeCapacity),
	}
	return getCapacityResponse, nil
}

func (cs *service) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	if err := cs.requireProbe(ctx); err != nil {
		return nil, err
	}
	log.Info("Executing CreateSnapshot")

	if len(req.SourceVolumeId) == 0 {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Storage Resource ID cannot be empty"))
	}
	var err error
	req.Name, err = util.ValidateResourceName(req.Name)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid snapshot name [%v]", err))
	}

	snapApi := gounity.NewSnapshot(cs.unity)
	//Idempotenc check
	snap, _ := snapApi.FindSnapshotByName(req.Name)
	if snap != nil {
		log.Info("snapshot name already exists")
		return utils.GetSnapshotResponseFromSnapshot(snap), nil
	}
	newSnapshot, err := snapApi.CreateSnapshot(req.SourceVolumeId, req.Name, req.Parameters["description"], req.Parameters["retentionDuration"], req.Parameters["isReadOnly"])
	if err != nil {
		return nil, status.Error(codes.Unknown, fmt.Sprintf("Create Snapshot error: %v", err))
	}
	newSnapshot, _ = snapApi.FindSnapshotByName(req.Name)
	if newSnapshot != nil {
		return utils.GetSnapshotResponseFromSnapshot(newSnapshot), nil
	} else {
		return nil, status.Error(codes.Unknown, fmt.Sprintf("Find Snapshot error after create. %v", err))
	}
}

func (cs *service) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	log.Info("Executing DeleteSnapshot")
	if err := cs.requireProbe(ctx); err != nil {
		return nil, err
	}
	if req.SnapshotId == "" {
		return nil, status.Error(codes.InvalidArgument, "snapshot ID is mandatory parameter")
	}
	snapApi := gounity.NewSnapshot(cs.unity)
	//Idempotenc check
	snap, err := snapApi.FindSnapshotById(req.SnapshotId)
	//snapshot exists, continue deleting the snapshot
	if err != nil {
		log.Info("Snapshot doesn't exists")
	}

	if snap != nil {
		err := snapApi.DeleteSnapshot(req.SnapshotId)
		if err != nil {
			return nil, status.Error(codes.Unknown, fmt.Sprintf("Delete Snapshot error: %v", err))
		}
	}

	delSnapResponse := &csi.DeleteSnapshotResponse{}
	return delSnapResponse, nil
}

func (cs *service) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	log.Info("Executing ListSnapshot")
	if err := cs.requireProbe(ctx); err != nil {
		return nil, err
	}

	var (
		startToken     int
		err            error
		maxEntries     = int(req.MaxEntries)
		sourceVolumeId = req.SourceVolumeId
		snapshotId     = req.SnapshotId
	)

	if req.StartingToken != "" {
		i, err := strconv.ParseInt(req.StartingToken, 10, 32)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "Unable to parse StartingToken: %v into uint32", req.StartingToken)
		}
		startToken = int(i)
	}
	snapApi := gounity.NewSnapshot(cs.unity)
	snaps, nextToken, err := snapApi.ListSnapshots(startToken, maxEntries, sourceVolumeId, snapshotId)
	if err != nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("Unable to get the snapshots: %v", err))
	}

	// Process the source snapshots and make CSI Snapshot
	entries, err := cs.getCSISnapshots(snaps)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &csi.ListSnapshotsResponse{
		Entries:   entries,
		NextToken: strconv.Itoa(nextToken),
	}, nil
}

func (s *service) controllerProbe(ctx context.Context) error {
	log.Info("Executing Controller Probe")
	// Check that we have the details needed to login to the Gateway
	if s.opts.Endpoint == "" {
		return status.Error(codes.FailedPrecondition, "Missing Unity endpoint")
	}
	if s.opts.User == "" {
		return status.Error(codes.FailedPrecondition,
			"missing Unity user")
	}
	if s.opts.Password == "" {
		return status.Error(codes.FailedPrecondition, "Missing Unity password")
	}

	// Create our Unity API client, if needed
	if s.unity == nil {
		c, err := gounity.NewClientWithArgs(s.opts.Endpoint, s.opts.Insecure, s.opts.UseCerts)
		if err != nil {
			return status.Errorf(codes.FailedPrecondition, "Unable to create Unity client: %s", err.Error())
		}
		s.unity = c
	}

	if s.unity.GetToken() == "" {
		err := s.unity.Authenticate(&gounity.ConfigConnect{
			Endpoint: s.opts.Endpoint,
			Username: s.opts.User,
			Password: s.opts.Password,
		})
		if err != nil {
			if e, ok := status.FromError(err); ok {
				if e.Code() == codes.Unauthenticated {
					return status.Errorf(codes.FailedPrecondition, "Unable to login to Unity. Error: %s", err.Error())
				}
			}
			return status.Errorf(codes.FailedPrecondition, "Unable to login to Unity. Verify hostname/IP Address of unity. Error: %s", err.Error())
		}
	}

	return nil
}

// ControllerGetCapabilities implements the default GRPC callout.
// Default supports all capabilities
func (cs *service) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	glog.Infof("Executing ControllerGetCapabilities")

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
	log.Info("Executing ControllerExpandVolume")
	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	if req.VolumeId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "volumeId is mandatory parameter")
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
		return nil, status.Errorf(codes.InvalidArgument, "required bytes can not be 0 or less")
	}

	volumeApi := gounity.NewVolume(s.unity)
	//Idempotency check
	volume, err := volumeApi.FindVolumeById(req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "unable to find the volume")
	}

	if volume.VolumeContent.SizeTotal > uint64(capacity) {
		return nil, status.Errorf(codes.Unknown, fmt.Sprintf("requested new capacity smaller than existing capacity"))
	}

	volumeResp := &csi.ControllerExpandVolumeResponse{
		CapacityBytes: capacity,
	}
	if volume.VolumeContent.SizeTotal == uint64(capacity) {
		log.Infof("New Volume size (%d) is same as existing Volume size. Ignoring expand volume operation.", volume.VolumeContent.SizeTotal)
		volumeResp.NodeExpansionRequired = false
		return volumeResp, nil
	}

	err = volumeApi.ExpandVolume(req.VolumeId, uint64(capacity))
	if err != nil {
		return nil, status.Errorf(codes.Unknown, fmt.Sprintf("unable to expand volume. Error %v", err))
	}

	volume, err = volumeApi.FindVolumeById(req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "unable to find the volume")
	}
	volumeResp.CapacityBytes = int64(volume.VolumeContent.SizeTotal)
	volumeResp.NodeExpansionRequired = true
	return volumeResp, err
}

func (s *service) requireProbe(ctx context.Context) error {
	if s.unity == nil {
		if !s.opts.AutoProbe {
			return status.Error(codes.FailedPrecondition,
				"Controller Service has not been probed")
		}
		log.Debug("probing controller service automatically")
		if err := s.controllerProbe(ctx); err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"failed to probe/init plugin: %s", err.Error())
		}
	}
	return nil
}

func (cs *service) getCSIVolumes(volumes []types.Volume) ([]*csi.ListVolumesResponse_Entry, error) {
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

func (cs *service) getCSISnapshots(snaps []types.Snapshot) ([]*csi.ListSnapshotsResponse_Entry, error) {
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
			SourceVolumeId: snap.SnapshotContent.Lun.Id,
			CreationTime:   timestamp,
			ReadyToUse:     isReady,
		}

		entries[i] = &csi.ListSnapshotsResponse_Entry{
			Snapshot: vi,
		}
	}
	return entries, nil
}
