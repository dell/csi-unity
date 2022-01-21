package service

import (
	"fmt"
	"strconv"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/gounity/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// validateVolumeCreateParam - validate volume create params
func validateVolumeCreateParam(req *csi.CreateVolumeRequest) error {
	if req.GetName() == "" {
		return status.Error(codes.InvalidArgument, "Volume Name cannot be empty")
	}

	return nil
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

func accTypeIsBlock(vcs []*csi.VolumeCapability) bool {
	for _, vc := range vcs {
		if at := vc.GetBlock(); at != nil {
			return true
		}
	}
	return false
}

func accTypeBlock(vc *csi.VolumeCapability) bool {
	if at := vc.GetBlock(); at != nil {
		return true
	}
	return false
}

func valVolumeCaps(vcs []*csi.VolumeCapability, protocol string) (bool, string) {
	var (
		supported = true
		isBlock   = accTypeIsBlock(vcs)
		reason    string
	)
	// Check that all access types are valid
	if !checkValidAccessTypes(vcs) {
		return false, errUnknownAccessType
	}

	if isBlock && protocol == NFS {
		return false, errBlockNFS
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
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER:
			break
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER:
			break
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
			break
		case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
			if isBlock {
				supported = false
				reason = errBlockReadOnly
			} else if protocol == FC || protocol == ISCSI {
				supported = false
				reason = errNoMultiNodeReader
			} //else NFS case
			break
		case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			if protocol == NFS || isBlock {
				break
			}

			supported = false
			reason = fmt.Sprintf("%s %s received:[%s]", reason, errNoMultiNodeWriter, vc.AccessMode)
			break
		default:
			// This is to guard against new access modes not understood
			supported = false
			reason = fmt.Sprintf("%s %s", reason, errUnknownAccessMode)
		}
	}

	return supported, reason
}

//validateCreateFsFromSnapshot - Validates idempotency of an existing snapshot created from a filesystem
func validateCreateFsFromSnapshot(ctx context.Context, sourceFilesystemResp *types.Filesystem, storagePool string, tieringPolicy, hostIoSize int64, thin, dataReduction bool) error {

	rid, _ := utils.GetRunidAndLogger(ctx)

	// Validate the storagePool parameter
	if sourceFilesystemResp.FileContent.Pool.ID != storagePool {
		return status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source filesystem storage pool %s is different than the requested storage pool %s",
			sourceFilesystemResp.FileContent.Pool.ID, storagePool))
	}

	//Validate the thinProvisioned parameter
	if sourceFilesystemResp.FileContent.IsThinEnabled != thin {
		return status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source filesystem thin provision %v is different than the requested thin provision %v",
			sourceFilesystemResp.FileContent.IsThinEnabled, thin))
	}

	//Validate the dataReduction parameter
	if sourceFilesystemResp.FileContent.IsDataReductionEnabled != dataReduction {
		return status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source filesystem data reduction %v is different than the requested data reduction %v",
			sourceFilesystemResp.FileContent.IsDataReductionEnabled, dataReduction))
	}

	//Validate the tieringPolicy parameter
	if int64(sourceFilesystemResp.FileContent.TieringPolicy) != tieringPolicy {
		return status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source filesystem tiering policy %v is different than the requested tiering policy %v",
			sourceFilesystemResp.FileContent.TieringPolicy, tieringPolicy))
	}

	//Validate the hostIOSize parameter
	if sourceFilesystemResp.FileContent.HostIOSize != hostIoSize {
		return status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source filesystem host IO size %v is different than the requested host IO size %v",
			sourceFilesystemResp.FileContent.HostIOSize, hostIoSize))
	}

	return nil
}

//validateCreateVolumeFromSource - Validates idempotency of an existing volume created from a volume
func validateCreateVolumeFromSource(ctx context.Context, sourceVolResp *types.Volume, storagePool string, tieringPolicy, size int64, thin, dataReduction, skipSizeValidation bool) error {

	rid, _ := utils.GetRunidAndLogger(ctx)

	// Validate the storagePool parameter
	if sourceVolResp.VolumeContent.Pool.ID != storagePool {
		return status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume storage pool %s is different than the requested storage pool %s",
			sourceVolResp.VolumeContent.Pool.ID, storagePool))
	}
	//Validate the tieringPolicy parameter
	if int64(sourceVolResp.VolumeContent.TieringPolicy) != tieringPolicy {
		return status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume tiering policy %v is different than the requested tiering policy %v",
			sourceVolResp.VolumeContent.TieringPolicy, tieringPolicy))
	}
	//Validate the thinProvisioned parameter
	if sourceVolResp.VolumeContent.IsThinEnabled != thin {
		return status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume thin provision %v is different than the requested thin provision %v",
			sourceVolResp.VolumeContent.IsThinEnabled, thin))
	}
	//Validate the dataReduction parameter
	if sourceVolResp.VolumeContent.IsDataReductionEnabled != dataReduction {
		return status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Source volume data reduction %v is different than the requested data reduction %v",
			sourceVolResp.VolumeContent.IsDataReductionEnabled, dataReduction))
	}

	if skipSizeValidation {
		return nil
	}

	// Validate the size parameter
	if int64(sourceVolResp.VolumeContent.SizeTotal) != size {
		return status.Errorf(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Requested size %d should be same as source volume size %d",
			size, int64(sourceVolResp.VolumeContent.SizeTotal)))
	}

	return nil
}

//ValidateCreateVolumeRequest - Validates all mandatory parameters in create volume request
func ValidateCreateVolumeRequest(ctx context.Context, req *csi.CreateVolumeRequest) (protocol, storagePool string, size, tieringPolicy, hostIoSize int64, thin, dataReduction bool, err error) {

	ctx, log, rid := GetRunidLog(ctx)

	if req.GetName() == "" {
		return "", "", 0, 0, 0, false, false, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume Name cannot be empty"))
	}

	params := req.GetParameters()

	protocol, _ = params[keyProtocol]
	if protocol == "" {
		log.Debugf("Parameter %s is not set [%s]. Default protocol is set to FC.", keyProtocol, params[keyProtocol])
		protocol = FC
	}

	//We dont have protocol from volume context ID and hence considering protocol from storage class as the
	//primary protocol
	protocol, err = ValidateAndGetProtocol(ctx, protocol, "")
	if err != nil {
		return "", "", 0, 0, 0, false, false, err
	}

	storagePool, ok := params[keyStoragePool]
	if !ok {
		return "", "", 0, 0, 0, false, false, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "`%s` is a required parameter", keyStoragePool))
	}

	if req.GetCapacityRange() == nil {
		return "", "", 0, 0, 0, false, false, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "RequiredBytes cannot be empty"))
	}

	size = req.GetCapacityRange().RequiredBytes
	if size <= 0 {
		return "", "", 0, 0, 0, false, false, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "RequiredBytes should be greater then 0"))
	}

	tieringPolicy, err = strconv.ParseInt(params[keyTieringPolicy], 0, 64)
	if err != nil {
		tieringPolicy = 0
		log.Debugf("Parameter %s is set to [%d]", keyTieringPolicy, tieringPolicy)
		err = nil
	}

	hostIoSize, err = strconv.ParseInt(params[keyHostIoSize], 0, 64)
	if err != nil && protocol == NFS {
		log.Debugf("Error parsing host IO size %s and so setting to default value 8192", keyHostIoSize)
		hostIoSize = 8192
		err = nil
	}

	thin, err = strconv.ParseBool(params[keyThinProvisioned])
	if err != nil {
		thin = true
		log.Debugf("Parameter %s is set to [%t]", keyThinProvisioned, thin)
		err = nil
	}

	dataReduction, err = strconv.ParseBool(params[keyDataReductionEnabled])
	if err != nil {
		log.Debugf("Parameter %s is set to [%t]", keyDataReductionEnabled, dataReduction)
		err = nil
	}

	// Check and log topology requirements
	accessibility := req.GetAccessibilityRequirements()
	if accessibility != nil {
		log.Debugf("Volume AccessibilityRequirements: %v", accessibility)
	}

	// Validate volume capabilities
	vcs := req.GetVolumeCapabilities()
	if len(vcs) == 0 {
		return "", "", 0, 0, 0, false, false, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Controller Volume Capability are not provided"))
	}

	supported, reason := valVolumeCaps(vcs, protocol)
	if !supported {
		return "", "", 0, 0, 0, false, false, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume Capabilities are not supported. Reason=["+reason+"]"))
	}

	return
}

//ValidateControllerPublishRequest - method to validate Controller publish volume request
func ValidateControllerPublishRequest(ctx context.Context, req *csi.ControllerPublishVolumeRequest, contextProtocol string) (protocol, nodeID string, err error) {

	ctx, _, rid := GetRunidLog(ctx)

	vc := req.GetVolumeCapability()
	if vc == nil {
		return "", "", status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume capability is required"))
	}
	am := vc.GetAccessMode()
	if am == nil {
		return "", "", status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "access mode is required"))
	}

	protocol, err = ValidateAndGetProtocol(ctx, contextProtocol, req.GetVolumeContext()[keyProtocol])
	if err != nil {
		return "", "", err
	}

	supportedAM, _ := valVolumeCaps([]*csi.VolumeCapability{vc}, protocol)
	if !supportedAM {
		return "", "", status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Access mode %s is not supported", req.GetVolumeCapability().GetAccessMode().Mode))
	}

	nodeID = req.GetNodeId()
	if nodeID == "" {
		return "", "", status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Node ID is required"))
	}

	if protocol == FC || protocol == ISCSI {
		if readOnly := req.GetReadonly(); readOnly == true {
			return "", "", status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Readonly must be false, because it is the supported value for FC/iSCSI"))
		}
	}

	return
}

// ValidateAndGetProtocol - validate and get protocol
func ValidateAndGetProtocol(ctx context.Context, protocol, scProtocol string) (string, error) {
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

//SingleAccessMode returns true if only a single access is allowed SINGLE_NODE_WRITER or SINGLE_NODE_READER_ONLY
func SingleAccessMode(accMode *csi.VolumeCapability_AccessMode) bool {
	switch accMode.GetMode() {
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
		return true
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
		return true
	}
	return false
}
