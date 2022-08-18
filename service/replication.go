package service

import (
	"context"
	csiext "github.com/dell/dell-csi-extensions/replication"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *service) CreateRemoteVolume(ctx context.Context, req *csiext.CreateRemoteVolumeRequest) (*csiext.CreateRemoteVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Not implemented")
}

func (s *service) CreateStorageProtectionGroup(ctx context.Context, req *csiext.CreateStorageProtectionGroupRequest) (*csiext.CreateStorageProtectionGroupResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Not implemented")
}

func (s *service) DeleteStorageProtectionGroup(ctx context.Context, req *csiext.DeleteStorageProtectionGroupRequest) (*csiext.DeleteStorageProtectionGroupResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Not implemented")
}

func (s *service) ExecuteAction(ctx context.Context, req *csiext.ExecuteActionRequest) (*csiext.ExecuteActionResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Not implemented")
}

func (s *service) GetStorageProtectionGroupStatus(ctx context.Context, req *csiext.GetStorageProtectionGroupStatusRequest) (*csiext.GetStorageProtectionGroupStatusResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Not implemented")
}

// WithRP appends Replication Prefix to provided string
func (s *service) WithRP(key string) string {
	return s.opts.replicationPrefix + "/" + key
}

func getRemoteCSIVolume(volumeID string, size int64) *csiext.Volume {
	volume := &csiext.Volume{
		CapacityBytes: size,
		VolumeId:      volumeID,
		VolumeContext: nil,
	}
	return volume
}
