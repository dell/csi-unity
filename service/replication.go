package service

import (
	"context"
	csiext "github.com/dell/dell-csi-extensions/replication"

)

func (s *service) CreateRemoteVolume(ctx context.Context, req *csiext.CreateRemoteVolumeRequest) (*csiext.CreateRemoteVolumeResponse, error) {
	panic ("implement me")
}

func (s *service) CreateStorageProtectionGroup(ctx context.Context, req *csiext.CreateStorageProtectionGroupRequest) (*csiext.CreateStorageProtectionGroupResponse, error) {
	panic ("implement me")
}

func (s *service) DeleteStorageProtectionGroup(ctx context.Context, req *csiext.DeleteStorageProtectionGroupRequest) (*csiext.DeleteStorageProtectionGroupResponse, error) {
	panic("implement me")
}

func (s *service) ExecuteAction(ctx context.Context, req *csiext.ExecuteActionRequest) (*csiext.ExecuteActionResponse, error) {
	panic("implement me")
}

func (s *service) GetStorageProtectionGroupStatus(ctx context.Context, req *csiext.GetStorageProtectionGroupStatusRequest) (*csiext.GetStorageProtectionGroupStatusResponse, error) {
	panic("implement me")
}

func getRemoteCSIVolume(volumeID string, size int64) *csiext.Volume{
	volume := &csiext.Volume{
		CapacityBytes: size,
		VolumeId: volumeID,
		VolumeContext: nil,
	}
	return volume
}