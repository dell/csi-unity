/*
Copyright (c) 2019 Dell EMC Corporation
All Rights Reserved
*/
package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dell/gounity/api"
	"github.com/dell/gounity/util"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/gounity"
	"github.com/dell/gounity/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	keyArrayId              = "arrayId"
	keyProtocol             = "protocol"
	keyNasServer            = "nasServer"
	keyHostIoSize           = "hostIoSize"
)

const (
	FC                       = "FC"
	ISCSI                    = "iSCSI"
	NFS                      = "NFS"
	ProtocolUnknown          = "Unknown"
	ProtocolNFS              = int(0)
	MAX_ENTRIES_SNAPSHOT     = 100
	MAX_ENTRIES_VOLUME       = 100
	NFSShareLocalPath        = "/"
	NFSShareNamePrefix       = "csishare-"
	AdditionalFilesystemSize = 1.5 * 1024 * 1024 * 1024
)

var (
	errUnknownAccessType      = "unknown access type is not Block or Mount"
	errUnknownAccessMode      = "access mode cannot be UNKNOWN"
	errIncompatibleAccessMode = "access mode should be single node reader or single node writer"
	errNoMultiNodeWriter      = "multi-node with writer(s) only supported for block access type"
)

type resourceType string

const volumeType resourceType = "volume"
const snapshotType resourceType = "snapshot"

func (s *service) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing CreateVolume with args: %+v", *req)
	params := req.GetParameters()
	arrayId := strings.ToLower(strings.TrimSpace(params[keyArrayId]))
	if arrayId == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "ArrayId cannot be empty"))
	}
	ctx, log = setArrayIdContext(ctx, arrayId)

	if err := s.requireProbe(ctx, arrayId); err != nil {
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

	protocol, _ := params[keyProtocol]
	if protocol == "" {
		log.Debugf("Parameter %s is not set [%s]. Default protocol is set to FC.", keyProtocol, params[keyProtocol])
		protocol = FC
	}

	//We dont have protocol from volume context ID and hence considering protocol from storage class as the
	//primary protocol
	protocol, err := s.validateAndGetProtocol(ctx, protocol, "")
	if err != nil {
		return nil, err
	}

	supported, reason := valVolumeCaps(vcs, protocol)
	if !supported {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume Capabilities are not supported. Reason=["+reason+"]"))
	}
	storagePool, ok := params[keyStoragePool]
	desc := params[keyDescription]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "`%s` is a required parameter", keyStoragePool))
	}

	// AccessibleTopology not currently supported
	accessibility := req.GetAccessibilityRequirements()
	if accessibility != nil {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume AccessibilityRequirements is not supported"))
	}

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
		log.Debugf("Parameter %s is set to [%t]", keyThinProvisioned, thin)
	}

	dataReduction, err := strconv.ParseBool(params[keyDataReductionEnabled])
	if err != nil {
		log.Debugf("Parameter %s is set to [%t]", keyDataReductionEnabled, dataReduction)
	}

	tieringPolicy, err := strconv.ParseInt(params[keyTieringPolicy], 0, 64)
	if err != nil {
		tieringPolicy = 0
		log.Debugf("Parameter %s is set to [%d]", keyTieringPolicy, tieringPolicy)
	}

	hostIOLimitName := strings.TrimSpace(params[keyHostIOLimitName])

	hostIoSize, err := strconv.ParseInt(params[keyHostIoSize], 0, 64)
	if err != nil {
		hostIoSize = 8192
	}

	unity, err := s.getUnityClient(ctx, arrayId)
	if err != nil {
		return nil, err
	}
	volName := req.GetName()
	// Creating Volume from a volume content source (only snapshot is supported currently)
	contentSource := req.GetVolumeContentSource()
	if contentSource != nil {
		volumeSource := contentSource.GetVolume()

		if volumeSource != nil {
			sourceVolId := volumeSource.VolumeId
			sourceVolId, _, sourceArrayId, _, err := s.validateAndGetResourceDetails(ctx, volumeSource.VolumeId, volumeType)
			if err != nil {
				return nil, err
			}
			if sourceVolId == "" {
				return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume ID cannot be empty"))
			}

			log.Debugf("Cloning Volume: %s", sourceVolId)
			if arrayId != sourceArrayId {
				return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume array id: %s is different than required volume array id: %s", sourceArrayId, arrayId))
			}
			if protocol == NFS {

				snapApi := gounity.NewSnapshot(unity)
				fileAPI := gounity.NewFilesystem(unity)
				filesystem, err := fileAPI.FindFilesystemById(ctx, sourceVolId)
				isSnapshot := false
				var snapResp *types.Snapshot
				var snapErr error
				if err != nil {
					//Filesystem not found - Check if PVC exists as a snapshot [Cloned volume in case of NFS]
					snapResp, snapErr = snapApi.FindSnapshotById(ctx, sourceVolId)
					if snapErr != nil {
						return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find source filesystem: %s Failed. Error: %v ", sourceVolId, err))
					}
					isSnapshot = true
					filesystem, err = s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.Id, arrayId)
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
						return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Requested size %d should be same as source filesystem size %d", size, snapSize))
					}
					//Idempotency check
					snapResp, err := snapApi.FindSnapshotByName(ctx, volName)
					if snapResp == nil {
						//Create Volume from Snapshot(Copy snapshot on array)
						snapResp, err = s.createFilesystemFromSnapshot(ctx, sourceVolId, volName, arrayId)
						if err != nil {
							return nil, err
						}
					} else if snapResp.SnapshotContent.Size != int64(size+AdditionalFilesystemSize) {
						return nil, status.Error(codes.AlreadyExists, utils.GetMessageWithRunID(rid, "Snapshot with same name %s already exists in different size.", volName))
					}
					snapResp.SnapshotContent.Size -= AdditionalFilesystemSize
					csiVolResp := utils.GetVolumeResponseFromSnapshot(snapResp, arrayId, protocol)
					csiVolResp.Volume.ContentSource = req.GetVolumeContentSource()
					return csiVolResp, nil
				} else {
					fsSize := int64(filesystem.FileContent.SizeTotal - AdditionalFilesystemSize)
					if size != fsSize {
						return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Requested size %d should be same as source volume size %d",
							size, fsSize))
					}

					snap, err := s.createIdempotentSnapshot(ctx, req.Name, sourceVolId, desc, "", protocol, arrayId, true)
					if err != nil {
						return nil, err
					}
					csiVolResp := utils.GetVolumeResponseFromSnapshot(snap, arrayId, protocol)
					csiVolResp.Volume.ContentSource = req.GetVolumeContentSource()
					return csiVolResp, nil
				}
			} else {

				volumeApi := gounity.NewVolume(unity)
				sourceVolResp, err := volumeApi.FindVolumeById(ctx, sourceVolId)
				if err != nil {
					return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Source volume not found: %s. Error: %v", sourceVolId, err))
				}
				// Validate the size parameter
				if int64(sourceVolResp.VolumeContent.SizeTotal) != size {
					return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Requested size %d should be same as source volume size %d",
						size, int64(sourceVolResp.VolumeContent.SizeTotal)))
				}
				// Validate the storagePool parameter
				if sourceVolResp.VolumeContent.Pool.Id != storagePool {
					return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume storage pool %s is different than the requested storage pool %s",
						sourceVolResp.VolumeContent.Pool.Id, storagePool))
				}
				//Validate the thinProvisioned parameter
				if sourceVolResp.VolumeContent.IsThinEnabled != thin {
					return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume thin provision %v is different than the requested thin provision %v",
						sourceVolResp.VolumeContent.IsThinEnabled, thin))
				}
				//Validate the dataReduction parameter
				if sourceVolResp.VolumeContent.IsDataReductionEnabled != dataReduction {
					return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume data reduction %v is different than the requested data reduction %v",
						sourceVolResp.VolumeContent.IsDataReductionEnabled, dataReduction))
				}
				//Validate the tieringPolicy parameter
				if int64(sourceVolResp.VolumeContent.TieringPolicy) != tieringPolicy {
					return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume tiering policy %v is different than the requested tiering policy %v",
						sourceVolResp.VolumeContent.TieringPolicy, tieringPolicy))
				}

				volResp, _ := volumeApi.FindVolumeByName(ctx, volName)
				if volResp != nil {
					//Idempotency Check
					if volResp.VolumeContent.IsThinClone && len(volResp.VolumeContent.ParentVolume.Id) > 0 && volResp.VolumeContent.ParentVolume.Id == sourceVolId &&
						volResp.VolumeContent.SizeTotal == sourceVolResp.VolumeContent.SizeTotal {
						log.Infof("Volume %s exists in the requested state as a clone of volume %s", volName, sourceVolResp.VolumeContent.Name)
						csiVolResp := utils.GetVolumeResponseFromVolume(volResp, arrayId, protocol)
						csiVolResp.Volume.ContentSource = req.GetVolumeContentSource()
						return csiVolResp, nil
					}
					return nil, status.Error(codes.AlreadyExists, utils.GetMessageWithRunID(rid, "Volume with same name %s already exists", volName))
				}

				//Perform volume cloning
				volResp, err = volumeApi.CreateCloneFromVolume(ctx, volName, sourceVolId)
				if err != nil {
					if err == gounity.CreateSnapshotFailedError {
						return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Unable to Create Snapshot for Volume Cloning for source volume: %s", sourceVolId))
					} else if err == gounity.CloningFailedError {
						return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Volume cloning for source volume: %s failed.", sourceVolId))
					}
				}

				volResp, err = volumeApi.FindVolumeByName(ctx, volName)
				if volResp != nil {
					csiVolResp := utils.GetVolumeResponseFromVolume(volResp, arrayId, protocol)
					csiVolResp.Volume.ContentSource = req.GetVolumeContentSource()
					return csiVolResp, nil
				}
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Volume not found after create. %v", err))
			}
		}
		snapshotSource := contentSource.GetSnapshot()
		if snapshotSource != nil {
			snapId := snapshotSource.SnapshotId
			if snapId == "" {
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Source snapshot ID cannot be empty"))
			}
			snapId, _, sourceArrayId, _, err := s.validateAndGetResourceDetails(ctx, snapId, snapshotType)
			if err != nil {
				return nil, err
			}

			log.Debugf("Creating the volume from snapshot: %s", snapId)
			if arrayId != sourceArrayId {
				return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source snapshot array id: %s is different than required volume array id: %s", sourceArrayId, arrayId))
			}
			snapApi := gounity.NewSnapshot(unity)
			snapResp, err := snapApi.FindSnapshotById(ctx, snapId)
			if err != nil {
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Source snapshot not found: %s", snapId))
			}

			if protocol == NFS {

				sourceFilesystemResp, err := s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.Id, arrayId)
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
					return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Requested size %d should be same as source snapshot size %d", size, snapSize))
				}

				snapshotApi := gounity.NewSnapshot(unity)
				snapResp, err := snapshotApi.FindSnapshotByName(ctx, volName)
				if snapResp != nil {
					//Idempotency check
					if snapResp.SnapshotContent.ParentSnap.Id == snapId && snapResp.SnapshotContent.AccessType == int(gounity.ProtocolAccessType) {
						log.Infof("Filesystem %s exists in the requested state as a volume from snapshot(snapshot on array) %s", volName, snapId)
						snapResp.SnapshotContent.Size -= AdditionalFilesystemSize
						csiVolResp := utils.GetVolumeResponseFromSnapshot(snapResp, arrayId, protocol)
						csiVolResp.Volume.ContentSource = req.GetVolumeContentSource()
						return csiVolResp, nil
					}
					return nil, status.Error(codes.AlreadyExists, utils.GetMessageWithRunID(rid, "Filesystem with same name %s already exists", volName))
				}

				//Create Volume from Snapshot(Copy snapshot on array)
				snapResp, err = s.createFilesystemFromSnapshot(ctx, snapId, volName, arrayId)
				if err != nil {
					return nil, err
				}

				if snapResp != nil {
					snapResp.SnapshotContent.Size -= AdditionalFilesystemSize
					csiVolResp := utils.GetVolumeResponseFromSnapshot(snapResp, arrayId, protocol)
					csiVolResp.Volume.ContentSource = req.GetVolumeContentSource()
					return csiVolResp, nil
				}
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Filesystem: %s not found after create. Error: %v", volName, err))
			} else {
				volId := snapResp.SnapshotContent.StorageResource.Id
				volumeApi := gounity.NewVolume(unity)
				sourceVolResp, err := volumeApi.FindVolumeById(ctx, volId)
				if err != nil {
					return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Source volume not found: %s", volId))
				}

				// Validate the size parameter
				if snapResp.SnapshotContent.Size != size {
					return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Requested size %d should be same as source snapshot size %d", size, snapResp.SnapshotContent.Size))
				}

				// Validate the storagePool is the same.
				if sourceVolResp.VolumeContent.Pool.Id != storagePool {
					return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume storage pool %s is different than the requested storage pool %s",
						sourceVolResp.VolumeContent.Pool.Id, storagePool))
				}

				//Validate the thinProvisioned parameter
				if sourceVolResp.VolumeContent.IsThinEnabled != thin {
					return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume thin provision %v is different than the requested thin provision %v",
						sourceVolResp.VolumeContent.IsThinEnabled, thin))
				}

				//Validate the dataReduction parameter
				if sourceVolResp.VolumeContent.IsDataReductionEnabled != dataReduction {
					return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume data reduction %v is different than the requested data reduction %v",
						sourceVolResp.VolumeContent.IsDataReductionEnabled, dataReduction))
				}

				//Validate the tieringPolicy parameter
				if int64(sourceVolResp.VolumeContent.TieringPolicy) != tieringPolicy {
					return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume tiering policy %v is different than the requested tiering policy %v",
						sourceVolResp.VolumeContent.TieringPolicy, tieringPolicy))
				}

				volResp, _ := volumeApi.FindVolumeByName(ctx, volName)
				if volResp != nil {
					//Idempotency Check
					if volResp.VolumeContent.IsThinClone == true && len(volResp.VolumeContent.ParentSnap.Id) > 0 && volResp.VolumeContent.ParentSnap.Id == snapId {
						log.Info("Volume exists in the requested state")
						csiVolResp := utils.GetVolumeResponseFromVolume(volResp, arrayId, protocol)
						csiVolResp.Volume.ContentSource = req.GetVolumeContentSource()
						return csiVolResp, nil
					}
					return nil, status.Error(codes.AlreadyExists, utils.GetMessageWithRunID(rid, "Volume with same name %s already exists", volName))
				}

				if snapResp.SnapshotContent.IsAutoDelete == true {
					err = snapApi.ModifySnapshotAutoDeleteParameter(ctx, snapId)
					if err != nil {
						return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Unable to modify auto-delete parameter for snapshot %s", snapId))
					}
				}

				volResp, err = volumeApi.CreteLunThinClone(ctx, volName, snapId, volId)
				if err != nil {
					return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Create volume from snapshot failed with error %v", err))
				}
				volResp, err = volumeApi.FindVolumeByName(ctx, volName)
				if err != nil {
					log.Debugf("Find Volume response: %v Error: %v", volResp, err)
				}

				if volResp != nil {
					csiVolResp := utils.GetVolumeResponseFromVolume(volResp, arrayId, protocol)
					csiVolResp.Volume.ContentSource = req.GetVolumeContentSource()
					return csiVolResp, nil
				}
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Volume not found after create. %v", err))
			}
		}
	}

	if protocol == NFS {

		nasServer, ok := params[keyNasServer]
		if !ok {
			return nil, status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "`%s` is a required parameter", keyNasServer))
		}

		//Add AdditionalFilesystemSize in size as Unity use this much size for metadata in filesystem
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

		//Idempotency check
		fileApi := gounity.NewFilesystem(unity)
		filesystem, _ := fileApi.FindFilesystemByName(ctx, volName)
		if filesystem != nil {
			content := filesystem.FileContent
			if int64(content.SizeTotal) == size && content.NASServer.Id == nasServer && content.Pool.Id == storagePool {
				log.Info("Filesystem exists in the requested state with same size, NAS server and storage pool")
				filesystem.FileContent.SizeTotal -= AdditionalFilesystemSize
				return utils.GetVolumeResponseFromFilesystem(filesystem, arrayId, protocol), nil
			} else {
				log.Info("'Filesystem name' already exists and size/NAS server/storage pool is different")
				return nil, status.Error(codes.AlreadyExists, utils.GetMessageWithRunID(rid, "'Filesystem name' already exists and size/NAS server/storage pool is different."))
			}
		}

		log.Debug("Filesystem does not exist, proceeding to create new filesystem")
		//Hardcoded ProtocolNFS to 0 in order to support only NFS
		resp, err := fileApi.CreateFilesystem(ctx, volName, storagePool, desc, nasServer, uint64(size), int(tieringPolicy), int(hostIoSize), ProtocolNFS, thin, dataReduction)
		//Add method to create filesystem
		if err != nil {
			log.Debugf("Filesystem create response:%v Error:%v", resp, err)
		}

		if err != nil {
			return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Create Filesystem %s failed with error: %v", volName, err))
		}

		resp, err = fileApi.FindFilesystemByName(ctx, volName)
		if err != nil {
			log.Debugf("Find Filesystem response: %v Error: %v", resp, err)
		}

		if resp != nil {
			resp.FileContent.SizeTotal -= AdditionalFilesystemSize
			filesystemResp := utils.GetVolumeResponseFromFilesystem(resp, arrayId, protocol)
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
		volumeApi := gounity.NewVolume(unity)

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
		vol, _ := volumeApi.FindVolumeByName(ctx, volName)
		if vol != nil {
			content := vol.VolumeContent
			if int64(content.SizeTotal) == size {
				log.Info("Volume exists in the requested state with same size")
				return utils.GetVolumeResponseFromVolume(vol, arrayId, protocol), nil
			} else {
				log.Info("'Volume name' already exists and size is different")
				return nil, status.Error(codes.AlreadyExists, utils.GetMessageWithRunID(rid, "'Volume name' already exists and size is different."))
			}
		}

		log.Debug("Volume does not exist, proceeding to create new volume")
		resp, err := volumeApi.CreateLun(ctx, volName, storagePool, desc, uint64(size), int(tieringPolicy), hostIOLimitId, thin, dataReduction)
		if err != nil {
			return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Create Volume %s failed with error: %v", volName, err))
		}

		resp, err = volumeApi.FindVolumeByName(ctx, volName)
		if resp != nil {
			volumeResp := utils.GetVolumeResponseFromVolume(resp, arrayId, protocol)
			log.Debugf("CreateVolume successful for volid: [%s]", volumeResp.Volume.VolumeId)
			return volumeResp, nil
		}
	}

	return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Volume/Filesystem not found after create. %v", err))
}

func (s *service) DeleteVolume(
	ctx context.Context,
	req *csi.DeleteVolumeRequest) (
	*csi.DeleteVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing DeleteVolume with args: %+v", *req)
	volId, protocol, arrayId, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIdContext(ctx, arrayId)
	if err := s.requireProbe(ctx, arrayId); err != nil {
		return nil, err
	}
	deleteVolumeResp := &csi.DeleteVolumeResponse{}
	var snapErr error
	//Not validating protocol here to support deletion of pvcs from v1.0
	if protocol != NFS {
		//Check stale snapshots used for volume cloning and delete if exist
		snapshotApi := gounity.NewSnapshot(unity)
		snapsResp, _, snapshotErr := snapshotApi.ListSnapshots(ctx, 0, 0, volId, "")
		if snapshotErr != nil {
			return nil, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "List snapshots for volume %s failed with error: %v", volId, snapshotErr))
		}
		totalSnaps := len(snapsResp)
		for _, snapResp := range snapsResp {
			snapshotName := snapResp.SnapshotContent.Name
			if strings.Contains(snapshotName, gounity.SnapForClone) {
				reqDeleteSnapshot := new(csi.DeleteSnapshotRequest)
				reqDeleteSnapshot.SnapshotId = snapResp.SnapshotContent.ResourceId
				_, snapshotErr = s.DeleteSnapshot(ctx, reqDeleteSnapshot)
				if snapshotErr != nil {
					return nil, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Volume %s can not be deleted as it has associated snapshots.", volId))
				}
				totalSnaps -= 1
			}
		}
		if totalSnaps > 0 {
			return nil, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Volume %s can not be deleted as it has associated snapshots.", volId))
		}
		//Delete the block volume
		volumeAPI := gounity.NewVolume(unity)
		err = volumeAPI.DeleteVolume(ctx, volId)
	} else {
		fileAPI := gounity.NewFilesystem(unity)
		var filesystemResp *types.Filesystem
		filesystemResp, err = fileAPI.FindFilesystemById(ctx, volId)
		if err == nil {
			if len(filesystemResp.FileContent.NFSShare) > 0 || len(filesystemResp.FileContent.CIFSShare) > 0 {
				return nil, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Filesystem %s can not be deleted as it has associated NFS or SMB shares.", volId))
			}
			err = fileAPI.DeleteFilesystem(ctx, volId)
		} else {
			//Do not reuse err as it is used for idempotency check
			snapshotApi := gounity.NewSnapshot(unity)
			snapResp, fsSnapErr := snapshotApi.FindSnapshotById(ctx, volId)
			snapErr = fsSnapErr
			if fsSnapErr == nil {
				//Validate if snapshot has any nfs shares
				sourceVolId, err := fileAPI.GetFilesystemIdFromResId(ctx, snapResp.SnapshotContent.StorageResource.Id)
				if err != nil {
					return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Source storage resource: %s filesystem Id not found. Error: %v", snapResp.SnapshotContent.StorageResource.Id, err))
				}
				filesystemResp, err = fileAPI.FindFilesystemById(ctx, sourceVolId)
				if err != nil {
					return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find source filesystem: %s failed with error: %v", sourceVolId, err))
				}
				for _, nfsShare := range filesystemResp.FileContent.NFSShare {
					if nfsShare.ParentSnap.Id == volId {
						return nil, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Snapshot %s can not be deleted as it has associated NFS or SMB shares.", volId))
					}
				}
				err = snapshotApi.DeleteSnapshot(ctx, volId)
			}
		}
	}

	//Idempotency check
	if err == nil {
		log.Debugf("DeleteVolume successful for volid: [%s]", req.VolumeId)
		return deleteVolumeResp, nil
	} else if err == gounity.FilesystemNotFoundError || err == gounity.VolumeNotFoundError || snapErr == gounity.SnapshotNotFoundError {
		log.Debug("Volume not found on array")
		log.Debugf("DeleteVolume successful for volid: [%s]", req.VolumeId)
		return deleteVolumeResp, nil
	}
	return nil, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Delete Volume %s failed with error: %v", volId, err))
}

func (s *service) ControllerPublishVolume(
	ctx context.Context,
	req *csi.ControllerPublishVolumeRequest) (
	*csi.ControllerPublishVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing ControllerPublishVolume with args: %+v", *req)

	if readOnly := req.GetReadonly(); readOnly == true {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Readonly must be false, because the supported mode only SINGLE_NODE_WRITER"))
	}

	volID, protocol, arrayId, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIdContext(ctx, arrayId)
	if err := s.requireProbe(ctx, arrayId); err != nil {
		return nil, err
	}

	vc := req.GetVolumeCapability()
	if vc == nil {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume capability is required"))
	}
	am := vc.GetAccessMode()
	if am == nil {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "access mode is required"))
	}

	protocol, err = s.validateAndGetProtocol(ctx, protocol, req.GetVolumeContext()[keyProtocol])
	if err != nil {
		return nil, err
	}

	supportedAM, _ := valVolumeCaps([]*csi.VolumeCapability{vc}, protocol)
	if !supportedAM {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Access mode %s is not supported", req.GetVolumeCapability().GetAccessMode().Mode))
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Node ID is required"))
	}

	hostAPI := gounity.NewHost(unity)
	host, err := hostAPI.FindHostByName(ctx, nodeID)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find Host Failed %v", err))
	}
	hostContent := host.HostContent
	hostID := hostContent.ID

	pinfo := make(map[string]string)
	pinfo["volumeContextId"] = req.GetVolumeId()
	pinfo["arrayId"] = arrayId
	pinfo["host"] = nodeID

	if protocol == FC || protocol == ISCSI {
		pinfo["lun"] = volID
		if protocol == FC && len(hostContent.FcInitiators) == 0 {
			return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Cannot publish volume as protocol in the Storage class is 'FC' but the node has no valid FC initiators"))
		} else if protocol == ISCSI && len(hostContent.IscsiInitiators) == 0 {
			return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Cannot publish volume as protocol in the Storage class is 'iScsi' but the node has no valid iScsi initiators"))
		}

		volumeAPI := gounity.NewVolume(unity)
		vol, err := volumeAPI.FindVolumeById(ctx, volID)
		if err != nil {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find volume Failed %v", err))
		}

		//Idempotency check
		content := vol.VolumeContent
		if len(content.HostAccessResponse) > 1 { //If the volume has 2 or more host access
			return nil, status.Error(codes.Aborted, utils.GetMessageWithRunID(rid, "Volume has been published to multiple hosts already."))
		}

		for _, hostaccess := range content.HostAccessResponse {
			hostcontent := hostaccess.HostContent
			hostAccessID := hostcontent.ID
			if hostAccessID == hostID {
				log.Debug("Volume has been published to the given host and exists in the required state.")
				return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
			} else {
				return nil, status.Error(codes.Aborted, utils.GetMessageWithRunID(rid, "Volume has been published to a different host already."))
			}
		}

		log.Debug("Adding host access to ", hostID, " on volume ", volID)
		err = volumeAPI.ExportVolume(ctx, volID, hostID)
		if err != nil {
			return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Export Volume Failed %v", err))
		}
		log.Debugf("ControllerPublishVolume successful for volid: [%s]", req.GetVolumeId())
		return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
	} else {
		pinfo["filesystem"] = volID
		fileAPI := gounity.NewFilesystem(unity)
		isSnapshot := false
		filesystemResp, err := fileAPI.FindFilesystemById(ctx, volID)
		var snapResp *types.Snapshot
		if err != nil {
			snapshotApi := gounity.NewSnapshot(unity)
			snapResp, err = snapshotApi.FindSnapshotById(ctx, volID)
			if err != nil {
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find filesystem: %s failed with error: %v", volID, err))
			}
			isSnapshot = true

			filesystemResp, err = s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.Id, arrayId)
			if err != nil {
				return nil, err
			}
		}
		//Create NFS Share if not already present on array
		nfsShareName := NFSShareNamePrefix + filesystemResp.FileContent.Name
		if isSnapshot {
			nfsShareName = NFSShareNamePrefix + snapResp.SnapshotContent.Name
		}
		nfsShareExist := false
		var nfsShareID string
		for _, nfsShare := range filesystemResp.FileContent.NFSShare {
			if isSnapshot {
				if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.Id == volID {
					nfsShareExist = true
					nfsShareName = nfsShare.Name
					nfsShareID = nfsShare.Id
				}
			} else {
				if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.Id == "" {
					nfsShareExist = true
					nfsShareName = nfsShare.Name
					nfsShareID = nfsShare.Id
				}
			}
		}
		if !nfsShareExist {
			if isSnapshot {
				nfsShareResp, err := fileAPI.CreateNFSShareFromSnapshot(ctx, nfsShareName, NFSShareLocalPath, volID, gounity.NoneDefaultAccess)
				if err != nil {
					return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Create NFS Share failed. Error: %v", err))
				}
				nfsShareID = nfsShareResp.NFSShareContent.Id
			} else {
				filesystemResp, err = fileAPI.CreateNFSShare(ctx, nfsShareName, NFSShareLocalPath, volID, gounity.NoneDefaultAccess)
				if err != nil {
					return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Create NFS Share failed. Error: %v", err))
				}
			}
			for _, nfsShare := range filesystemResp.FileContent.NFSShare {
				if nfsShare.Name == nfsShareName {
					nfsShareID = nfsShare.Id
				}
			}
		}

		//Allocate host access to NFS Share with appropriate access mode
		nfsShareResp, err := fileAPI.FindNFSShareById(ctx, nfsShareID)
		if err != nil {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find NFS Share: %s failed. Error: %v", nfsShareID, err))
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
						otherHostsWithAccess -= 1
					}
				}
			}
		}
		if foundIncompatible {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Host: %s has access on NFS Share: %s with incompatible access mode.", nodeID, nfsShareID))
		}
		if am.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER && otherHostsWithAccess > 0 {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Other hosts have access on NFS Share: %s", nfsShareID))
		}
		//Idempotent case
		if foundIdempotent {
			log.Info("Host has access to the given host and exists in the required state.")
			return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
		}
		if am.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
			readHostIDList = append(readHostIDList, hostID)
			if isSnapshot {
				err = fileAPI.ModifyNFSShareCreatedFromSnapshotHostAccess(ctx, nfsShareID, readHostIDList, gounity.ReadOnlyRootAccessType)
			} else {
				err = fileAPI.ModifyNFSShareHostAccess(ctx, volID, nfsShareID, readHostIDList, gounity.ReadOnlyRootAccessType)
			}
		} else {
			readWriteHostIDList = append(readWriteHostIDList, hostID)
			if isSnapshot {
				err = fileAPI.ModifyNFSShareCreatedFromSnapshotHostAccess(ctx, nfsShareID, readWriteHostIDList, gounity.ReadWriteRootAccessType)
			} else {
				err = fileAPI.ModifyNFSShareHostAccess(ctx, volID, nfsShareID, readWriteHostIDList, gounity.ReadWriteRootAccessType)
			}
		}
		if err != nil {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Allocating host %s access to NFS Share failed. Error: %v", nodeID, err))
		}
		log.Debugf("NFS Share: %s is accessible to host: %s with access mode: %s", nfsShareID, nodeID, am.Mode)
		log.Debugf("ControllerPublishVolume successful for volid: [%s]", req.GetVolumeId())
		return &csi.ControllerPublishVolumeResponse{PublishContext: pinfo}, nil
	}
}

func (s *service) ControllerUnpublishVolume(
	ctx context.Context,
	req *csi.ControllerUnpublishVolumeRequest) (
	*csi.ControllerUnpublishVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing ControllerUnpublishVolume with args: %+v", *req)

	volID, protocol, arrayId, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIdContext(ctx, arrayId)
	if err := s.requireProbe(ctx, arrayId); err != nil {
		return nil, err
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Node ID is required"))
	}

	hostAPI := gounity.NewHost(unity)
	host, err := hostAPI.FindHostByName(ctx, nodeID)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find Host Failed %v", err))
	}
	hostContent := host.HostContent
	hostID := hostContent.ID

	if protocol != NFS {
		volumeAPI := gounity.NewVolume(unity)
		vol, err := volumeAPI.FindVolumeById(ctx, volID)
		if err != nil {
			// If the volume isn't found, k8s will retry Controller Unpublish forever so...
			// There is no way back if volume isn't found and so considering this scenario idempotent
			if err == gounity.VolumeNotFoundError {
				log.Debugf("Volume %s not found on the array %s during Controller Unpublish. Hence considering the call to be idempotent", volID, arrayId)
				return &csi.ControllerUnpublishVolumeResponse{}, nil
			}
			return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "%v", err))
		}

		//Idempotency check
		content := vol.VolumeContent
		if len(content.HostAccessResponse) > 0 {
			log.Debug("Removing Host access on Volume ", volID)
			err = volumeAPI.UnexportVolume(ctx, volID)
			if err != nil {
				return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Unexport Volume Failed. %v", err))
			}
		} else {
			log.Info(fmt.Sprintf("The given Node %s does not have access on the given volume %s. Already in Unpublished state.", hostID, volID))
		}
		log.Debugf("ControllerUnpublishVolume successful for volid: [%s]", req.GetVolumeId())
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	} else {
		fileAPI := gounity.NewFilesystem(unity)
		isSnapshot := false
		filesystem, err := fileAPI.FindFilesystemById(ctx, volID)
		var snapResp *types.Snapshot
		if err != nil {
			snapshotApi := gounity.NewSnapshot(unity)
			snapResp, err = snapshotApi.FindSnapshotById(ctx, volID)
			if err != nil {
				// If the filesysten isn't found, k8s will retry Controller Unpublish forever so...
				// There is no way back if filesystem isn't found and so considering this scenario idempotent
				if err == gounity.FilesystemNotFoundError || err == gounity.SnapshotNotFoundError {
					log.Debugf("Filesystem %s not found on the array %s during Controller Unpublish. Hence considering the call to be idempotent", volID, arrayId)
					return &csi.ControllerUnpublishVolumeResponse{}, nil
				}
				return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Find filesystem %s failed with error: %v", volID, err))
			}
			isSnapshot = true
			filesystem, err = s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.Id, arrayId)
			if err != nil {
				return nil, err
			}
		}
		//Remove host access from NFS Share
		nfsShareName := NFSShareNamePrefix + filesystem.FileContent.Name
		if isSnapshot {
			nfsShareName = NFSShareNamePrefix + snapResp.SnapshotContent.Name
		}
		shareExists := false
		deleteShare := true
		var nfsShareID string
		for _, nfsShare := range filesystem.FileContent.NFSShare {
			if isSnapshot {
				if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.Id == volID {
					shareExists = true
					if nfsShare.Name != nfsShareName {
						//This means that share was created manually on array, hence don't delete via driver
						deleteShare = false
						nfsShareName = nfsShare.Name
					}
					nfsShareID = nfsShare.Id
				}
			} else {
				if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.Id == "" {
					shareExists = true
					if nfsShare.Name != nfsShareName {
						//This means that share was created manually on array, hence don't delete via driver
						deleteShare = false
						nfsShareName = nfsShare.Name
					}
					nfsShareID = nfsShare.Id
				}
			}
		}
		if !shareExists {
			log.Infof("NFS Share: %s not found on array.", nfsShareName)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}

		nfsShareResp, err := fileAPI.FindNFSShareById(ctx, nfsShareID)
		if err != nil {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find NFS Share: %s failed. Error: %v", nfsShareID, err))
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
					otherHostsWithAccess -= 1
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
					otherHostsWithAccess -= 1
				} else {
					readWriteHostIDList = append(readWriteHostIDList, host.ID)
				}
			}
		}
		if foundIncompatible {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Cannot remove host access. Host: %s has access on NFS Share: %s with incompatible access mode.", nodeID, nfsShareID))
		}
		if foundReadOnly {
			if isSnapshot {
				err = fileAPI.ModifyNFSShareCreatedFromSnapshotHostAccess(ctx, nfsShareID, readHostIDList, gounity.ReadOnlyRootAccessType)
			} else {
				err = fileAPI.ModifyNFSShareHostAccess(ctx, volID, nfsShareID, readHostIDList, gounity.ReadOnlyRootAccessType)
			}
		} else if foundReadWrite {
			if isSnapshot {
				err = fileAPI.ModifyNFSShareCreatedFromSnapshotHostAccess(ctx, nfsShareID, readWriteHostIDList, gounity.ReadWriteRootAccessType)
			} else {
				err = fileAPI.ModifyNFSShareHostAccess(ctx, volID, nfsShareID, readWriteHostIDList, gounity.ReadWriteRootAccessType)
			}
		} else {
			//Idempotent case
			log.Infof("Host: %s has no access on NFS Share: %s", nodeID, nfsShareID)
		}
		if err != nil {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Removing host %s access to NFS Share failed. Error: %v", nodeID, err))
		}
		log.Debugf("Host: %s access is removed from NFS Share: %s", nodeID, nfsShareID)

		//Delete NFS Share
		if deleteShare {
			if otherHostsWithAccess > 0 {
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "NFS Share: %s can not be deleted as other hosts have access on it.", nfsShareID))
			}
			if isSnapshot {
				err = fileAPI.DeleteNFSShareCreatedFromSnapshot(ctx, nfsShareID)
			} else {
				err = fileAPI.DeleteNFSShare(ctx, filesystem.FileContent.Id, nfsShareID)
			}
			if err != nil {
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Delete NFS Share: %s Failed with error: %v", nfsShareID, err))
			}
			log.Debugf("NFS Share: %s deleted successfully.", nfsShareID)
		}
		log.Debugf("ControllerUnpublishVolume successful for volid: [%s]", req.GetVolumeId())
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}
}

func (s *service) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing ValidateVolumeCapabilities with args: %+v", *req)

	volID, _, arrayId, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIdContext(ctx, arrayId)
	if err := s.requireProbe(ctx, arrayId); err != nil {
		return nil, err
	}

	volumeAPI := gounity.NewVolume(unity)
	_, err = volumeAPI.FindVolumeById(ctx, volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Volume not found. Error: %v", err))
	}

	params := req.GetParameters()
	protocol, _ := params[keyProtocol]
	if protocol == "" {
		log.Errorf("Protocol is required to validate capabilities")
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Protocol is required to validate capabilities"))
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
	} else {
		resp.Message = reason
		return resp, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Unsupported capability"))
	}
}

func (s *service) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Not implemented")
}

func (s *service) GetCapacity(
	ctx context.Context,
	req *csi.GetCapacityRequest) (
	*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Not implemented")
}

func (s *service) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing CreateSnapshot with args: %+v", *req)

	if len(req.SourceVolumeId) == 0 {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Storage Resource ID cannot be empty"))
	}
	var err error
	req.Name, err = util.ValidateResourceName(req.Name, api.MaxResourceNameLength)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "invalid snapshot name [%v]", err))
	}

	//Source volume is for volume clone or snapshot clone
	volId, protocol, arrayId, _, err := s.validateAndGetResourceDetails(ctx, req.SourceVolumeId, volumeType)
	if err != nil {
		return nil, err
	}

	ctx, log = setArrayIdContext(ctx, arrayId)
	if err := s.requireProbe(ctx, arrayId); err != nil {
		return nil, err
	}

	//Idempotency check
	snap, err := s.createIdempotentSnapshot(ctx, req.Name, volId, req.Parameters["description"], req.Parameters["retentionDuration"], protocol, arrayId, false)
	if err != nil {
		return nil, err
	}
	return utils.GetSnapshotResponseFromSnapshot(snap, protocol, arrayId), nil
}

func (s *service) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing DeleteSnapshot with args: %+v", *req)

	snapId, _, arrayId, unity, err := s.validateAndGetResourceDetails(ctx, req.SnapshotId, snapshotType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIdContext(ctx, arrayId)
	if err := s.requireProbe(ctx, arrayId); err != nil {
		return nil, err
	}

	snapApi := gounity.NewSnapshot(unity)
	//Idempotency check
	snap, err := snapApi.FindSnapshotById(ctx, snapId)
	//snapshot exists, continue deleting the snapshot
	if err != nil {
		log.Info("Snapshot doesn't exists")
	}

	if snap != nil {
		err := snapApi.DeleteSnapshot(ctx, snapId)
		if err != nil {
			return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Delete Snapshot error: %v", err))
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
	snapId, protocol, arrayId, unity, err := s.validateAndGetResourceDetails(ctx, req.SnapshotId, snapshotType)
	if err != nil {
		return nil, err
	}

	ctx, log = setArrayIdContext(ctx, arrayId)
	if err := s.requireProbe(ctx, arrayId); err != nil {
		return nil, err
	}

	snapApi := gounity.NewSnapshot(unity)

	//Limiting the number of snapshots to 100 to avoid timeout issues
	if maxEntries > MAX_ENTRIES_SNAPSHOT || maxEntries == 0 {
		maxEntries = MAX_ENTRIES_SNAPSHOT
	}

	if req.StartingToken != "" {
		i, err := strconv.ParseInt(req.StartingToken, 10, 64)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Unable to parse StartingToken: %v into uint32", req.StartingToken))
		}
		startToken = int(i)
	}

	snaps, nextToken, err := snapApi.ListSnapshots(ctx, startToken, maxEntries, "", snapId)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Unable to get the snapshots: %v", err))
	}

	// Process the source snapshots and make CSI Snapshot
	entries, err := s.getCSISnapshots(snaps, req.SourceVolumeId, protocol, arrayId)
	if err != nil {
		return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, err.Error()))
	}
	log.Debugf("ListSnapshot successful for snapid: [%s]", req.SnapshotId)
	return &csi.ListSnapshotsResponse{
		Entries:   entries,
		NextToken: strconv.Itoa(nextToken),
	}, nil
}

func (s *service) controllerProbe(ctx context.Context, arrayId string) error {
	return s.probe(ctx, "Controller", arrayId)
}

// ControllerGetCapabilities implements the default GRPC callout.
// Default supports all capabilities
func (cs *service) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Debugf("Executing ControllerGetCapabilities with args: %+v", *req)
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
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
					},
				},
			},
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (s *service) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing ControllerExpandVolume with args: %+v", *req)

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volumeId is mandatory parameter"))
	}

	volId, protocol, arrayId, unity, err := s.validateAndGetResourceDetails(ctx, req.VolumeId, volumeType)
	if err != nil {
		return nil, err
	}

	ctx, log = setArrayIdContext(ctx, arrayId)
	if err := s.requireProbe(ctx, arrayId); err != nil {
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
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Required bytes can not be 0 or less"))
	}

	expandVolumeResp := &csi.ControllerExpandVolumeResponse{
		CapacityBytes: capacity,
	}

	if protocol == NFS {
		//Adding Additional size used for metadata
		capacity += AdditionalFilesystemSize
		filesystemApi := gounity.NewFilesystem(unity)

		filesystem, err := filesystemApi.FindFilesystemById(ctx, volId)
		if err != nil {
			snapshotApi := gounity.NewSnapshot(unity)
			_, err = snapshotApi.FindSnapshotById(ctx, volId)
			if err != nil {
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find filesystem %s failed with error: %v", volId, err))
			}
			return nil, status.Error(codes.Unimplemented, utils.GetMessageWithRunID(rid, "Expand Volume not supported for cloned filesystems(snapshot on array)"))
		}

		//Idempotency check
		if filesystem.FileContent.SizeTotal >= uint64(capacity) {
			log.Infof("New Filesystem size (%d) is same as existing Filesystem size. Ignoring expand volume operation.", filesystem.FileContent.SizeTotal)
			expandVolumeResp := &csi.ControllerExpandVolumeResponse{
				CapacityBytes: 0,
			}
			expandVolumeResp.NodeExpansionRequired = false
			return expandVolumeResp, nil
		}

		err = filesystemApi.ExpandFilesystem(ctx, volId, uint64(capacity))
		if err != nil {
			return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Expand filesystem failed with error: %v", err))
		}

		filesystem, err = filesystemApi.FindFilesystemById(ctx, volId)
		if err != nil {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find filesystem failed with error: %v", err))
		}
		expandVolumeResp.CapacityBytes = int64(filesystem.FileContent.SizeTotal) - AdditionalFilesystemSize
		expandVolumeResp.NodeExpansionRequired = false
		return expandVolumeResp, err
	} else {
		volumeApi := gounity.NewVolume(unity)
		//Idempotency check
		volume, err := volumeApi.FindVolumeById(ctx, volId)
		if err != nil {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find volume failed with error: %v", err))
		}

		nodeExpansionRequired := false
		content := volume.VolumeContent
		if len(content.HostAccessResponse) >= 1 { //If the volume has 1 or more host access  then set nodeExpansionRequired as true
			nodeExpansionRequired = true
		}

		if volume.VolumeContent.SizeTotal >= uint64(capacity) {
			log.Infof("New Volume size (%d) is same as existing Volume size. Ignoring expand volume operation.", volume.VolumeContent.SizeTotal)
			expandVolumeResp := &csi.ControllerExpandVolumeResponse{
				CapacityBytes: 0,
			}
			expandVolumeResp.NodeExpansionRequired = nodeExpansionRequired
			return expandVolumeResp, nil
		}

		err = volumeApi.ExpandVolume(ctx, volId, uint64(capacity))
		if err != nil {
			return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Expand volume failed with error: %v", err))
		}

		volume, err = volumeApi.FindVolumeById(ctx, volId)
		if err != nil {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find volume failed with error: %v", err))
		}
		expandVolumeResp.CapacityBytes = int64(volume.VolumeContent.SizeTotal)
		expandVolumeResp.NodeExpansionRequired = nodeExpansionRequired
		return expandVolumeResp, err
	}
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

func (s *service) getCSISnapshots(snaps []types.Snapshot, volId, protocol, arrayId string) ([]*csi.ListSnapshotsResponse_Entry, error) {
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

		snapId := fmt.Sprintf("%s-%s-%s-%s", snap.SnapshotContent.Name, protocol, arrayId, snap.SnapshotContent.ResourceId)

		size := snap.SnapshotContent.Size
		if protocol == NFS {
			size -= AdditionalFilesystemSize
		}
		//Create CSI Snapshot
		vi := &csi.Snapshot{
			SizeBytes:      size,
			SnapshotId:     snapId,
			SourceVolumeId: volId,
			CreationTime:   timestamp,
			ReadyToUse:     isReady,
		}

		entries[i] = &csi.ListSnapshotsResponse_Entry{
			Snapshot: vi,
		}
	}
	return entries, nil
}

func (s *service) validateAndGetProtocol(ctx context.Context, protocol, scProtocol string) (string, error) {
	ctx, log, rid := GetRunidLog(ctx)
	if protocol == ProtocolUnknown || protocol == "" {
		protocol = scProtocol
		log.Debug("Protocol is not set. Considering protocol value from the storageclass")
	}

	if protocol != FC && protocol != ISCSI && protocol != NFS {
		return "", status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Invalid value provided for Protocol: %s", protocol))
	}
	return protocol, nil
}

func (s *service) getFilesystemByResourceID(ctx context.Context, resourceID, arrayID string) (*types.Filesystem, error) {
	ctx, _, rid := GetRunidLog(ctx)
	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, err
	}
	filesystemAPI := gounity.NewFilesystem(unity)

	filesystemID, err := filesystemAPI.GetFilesystemIdFromResId(ctx, resourceID)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Storage resource: %s filesystem Id not found. Error: %v", resourceID, err))
	}
	sourceFilesystemResp, err := filesystemAPI.FindFilesystemById(ctx, filesystemID)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Filesystem: %s not found. Error: %v", filesystemID, err))
	}
	return sourceFilesystemResp, nil
}

//Create Volume from Snapshot(Copy snapshot on array)
func (s *service) createFilesystemFromSnapshot(ctx context.Context, snapID, volumeName, arrayID string) (*types.Snapshot, error) {
	ctx, _, rid := GetRunidLog(ctx)
	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, err
	}
	snapshotAPI := gounity.NewSnapshot(unity)

	snapResp, err := snapshotAPI.CopySnapshot(ctx, snapID, volumeName)
	if err != nil {
		return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Create Filesystem from snapshot failed with error. Error: %v", err))
	}

	snapResp, err = snapshotAPI.FindSnapshotByName(ctx, volumeName)
	if err != nil {
		return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Create Filesystem from snapshot failed with error. Error: %v", err))
	}

	return snapResp, nil
}

func (s *service) createIdempotentSnapshot(ctx context.Context, snapshotName, sourceVolID, description, retentionDuration, protocol, arrayID string, isClone bool) (*types.Snapshot, error) {
	ctx, log, rid := GetRunidLog(ctx)
	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, err
	}
	snapshotAPI := gounity.NewSnapshot(unity)

	isSnapshot := false
	var snapResp *types.Snapshot
	var filesystemResp *types.Filesystem
	if protocol == NFS {
		fileAPI := gounity.NewFilesystem(unity)
		filesystemResp, err = fileAPI.FindFilesystemById(ctx, sourceVolID)
		if err != nil {
			snapshotApi := gounity.NewSnapshot(unity)
			snapResp, err = snapshotApi.FindSnapshotById(ctx, sourceVolID)
			if err != nil {
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find source filesystem: %s failed with error: %v", sourceVolID, err))
			}
			isSnapshot = true
			filesystemResp, err = s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.Id, arrayID)
			if err != nil {
				return nil, err
			}
		}
	}

	if protocol == NFS && !isSnapshot {
		sourceVolID = filesystemResp.FileContent.StorageResource.Id
	}

	snap, _ := snapshotAPI.FindSnapshotByName(ctx, snapshotName)
	if snap != nil {
		if snap.SnapshotContent.StorageResource.Id == sourceVolID || (isSnapshot && snap.SnapshotContent.StorageResource.Id == filesystemResp.FileContent.StorageResource.Id) {
			//Subtract AdditionalFilesystemSize for Filesystem snapshots
			if protocol == NFS {
				snap.SnapshotContent.Size -= AdditionalFilesystemSize
			}
			log.Infof("Snapshot already exists with same name %s for same storage resource %s", snapshotName, sourceVolID)
			return snap, nil
		}
		return nil, status.Error(codes.AlreadyExists, utils.GetMessageWithRunID(rid, "Snapshot with same name %s already exists for storage resource %s", snapshotName, snap.SnapshotContent.StorageResource.Id))
	}

	var newSnapshot *types.Snapshot
	if isSnapshot {
		newSnapshot, err = snapshotAPI.CopySnapshot(ctx, sourceVolID, snapshotName)
		if err != nil {
			return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Create Snapshot error: %v", err))
		}
		err = snapshotAPI.ModifySnapshot(ctx, newSnapshot.SnapshotContent.ResourceId, description, retentionDuration)
		if err != nil {
			log.Infof("Unable to modify description and retention duration in created snapshot %s. Error: %s", newSnapshot.SnapshotContent.ResourceId, err)
		}
	} else {
		if isClone {
			newSnapshot, err = snapshotAPI.CreateSnapshotWithFsAccesType(ctx, sourceVolID, snapshotName, description, retentionDuration, gounity.ProtocolAccessType)
		} else {
			newSnapshot, err = snapshotAPI.CreateSnapshot(ctx, sourceVolID, snapshotName, description, retentionDuration)
		}
		if err != nil {
			return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Create Snapshot error: %v", err))
		}
	}

	newSnapshot, _ = snapshotAPI.FindSnapshotByName(ctx, snapshotName)
	if newSnapshot != nil {
		//Subtract AdditionalFilesystemSize for Filesystem snapshots{
		if protocol == NFS {
			newSnapshot.SnapshotContent.Size -= AdditionalFilesystemSize
		}
		return newSnapshot, nil
	}
	return nil, status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Find Snapshot error after create. %v", err))
}
