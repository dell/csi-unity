package service

import (
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
			if protocol == "NFS" {
				break
			}
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			if protocol == "NFS" {
				break
			}
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
