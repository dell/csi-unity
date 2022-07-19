package service

import (
	"context"
	"strings"

	csiext "github.com/dell/dell-csi-extensions/replication"
	"github.com/dell/gounity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *service) CreateRemoteVolume(ctx context.Context, req *csiext.CreateRemoteVolumeRequest) (*csiext.CreateRemoteVolumeResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Debugf("Executing CreateRemoteVolume with args: %+v", *req)
	volID := req.GetVolumeHandle()
	if volID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	volID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, volID, volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}
	//remoteVolumeResp := &csiext.CreateRemoteVolumeResponse{}
	if protocol == NFS {
		fsAPI := gounity.NewFilesystem(unity)
		fileSystems, err := fsAPI.FindFilesystemByID(ctx, volID)
		if err != nil {
			return nil, err
		}
		replAPI := gounity.NewReplicationSession(unity)
		rs, err := replAPI.FindReplicationSessionBySrcResourceID(ctx, fileSystems.FileContent.StorageResource.ID)
		if err != nil {
			return nil, err
		}

		var remoteVolumeID string
		replSession, err := replAPI.FindReplicationSessionById(ctx, rs.ReplicationSessionContent.ReplicationSessionId)
		if err != nil {
			return nil, err
		}
		remoteVolumeID = replSession.ReplicationSessionContent.DstResourceId
		if remoteVolumeID == "" {
			return nil, status.Errorf(codes.Internal, "couldn't find volume id in replication session")
		}
		remoteArrayID := req.Parameters[s.WithRP(keyReplicationRemoteSystem)]
		remoteUnity, err := getUnityClient(ctx, s, remoteArrayID)
		if err != nil {
			return nil, err
		}
		ctx, log = setArrayIDContext(ctx, remoteArrayID)
		if err := s.requireProbe(ctx, remoteArrayID); err != nil {
			return nil, err
		}

		replfsAPI := gounity.NewFilesystem(remoteUnity)
		fsName := strings.Split(req.GetVolumeHandle(), "-")[0] + "-" + strings.Split(req.GetVolumeHandle(), "-")[1]
		remoteFs, err := replfsAPI.FindFilesystemByName(ctx, fsName)
		log.Info("fsName ", fsName, " localArrayID: ", req.Parameters[keyArrayID], " remoteArrayID: ", remoteArrayID)
		vgPrefix := strings.ReplaceAll(fsName, remoteArrayID, req.Parameters[keyArrayID])
		remoteParams := map[string]string{
			"remoteSystem": arrayID,
			"volumeId":     remoteFs.FileContent.ID,
			"protocol":     protocol,
		}

		remoteVolume := getRemoteCSIVolume(vgPrefix+"-"+protocol+"-"+strings.ToLower(remoteArrayID)+"-"+remoteFs.FileContent.ID, int64(fileSystems.FileContent.SizeTotal))
		remoteVolume.VolumeContext = remoteParams
		return &csiext.CreateRemoteVolumeResponse{
			RemoteVolume: remoteVolume,
		}, nil
	} else {
		return nil, status.Error(codes.Unimplemented, "Block replication is not implemented")
	}
}

func (s *service) CreateStorageProtectionGroup(ctx context.Context, req *csiext.CreateStorageProtectionGroupRequest) (*csiext.CreateStorageProtectionGroupResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Debugf("Executing CreateRemoteVolume with args: %+v", *req)
	volID := req.GetVolumeHandle()
	if volID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	volID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, volID, volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	if protocol == NFS {
		fsAPI := gounity.NewFilesystem(unity)

		fileSystems, err := fsAPI.FindFilesystemByID(ctx, volID)
		if err != nil {
			return nil, err
		}
		replAPI := gounity.NewReplicationSession(unity)
		rs, err := replAPI.FindReplicationSessionBySrcResourceID(ctx, fileSystems.FileContent.StorageResource.ID)
		if err != nil {
			return nil, err
		}
		replSession, err := replAPI.FindReplicationSessionById(ctx, rs.ReplicationSessionContent.ReplicationSessionId)
		if err != nil {
			return nil, err
		}
		remoteProtectionGroupId := strings.Split(req.VolumeHandle, "=_=")[0]
		localProtectionGroupId := strings.ReplaceAll(remoteProtectionGroupId, strings.ToUpper(req.Parameters[s.WithRP(keyReplicationRemoteSystem)]), strings.ToUpper(req.Parameters[(keyArrayID)]))
		localParams := map[string]string{
			s.opts.replicationContextPrefix + "systemName":       arrayID,
			s.opts.replicationContextPrefix + "remoteSystemName": replSession.ReplicationSessionContent.RemoteSystem.Name,
			s.opts.replicationContextPrefix + "VolumeGroupName":  fileSystems.FileContent.Name,
		}
		remoteParams := map[string]string{
			s.opts.replicationContextPrefix + "systemName":       replSession.ReplicationSessionContent.RemoteSystem.Name,
			s.opts.replicationContextPrefix + "remoteSystemName": arrayID,
			s.opts.replicationContextPrefix + "VolumeGroupName":  fileSystems.FileContent.Name,
		}
		return &csiext.CreateStorageProtectionGroupResponse{
			LocalProtectionGroupId:          localProtectionGroupId,
			RemoteProtectionGroupId:         remoteProtectionGroupId,
			LocalProtectionGroupAttributes:  localParams,
			RemoteProtectionGroupAttributes: remoteParams,
		}, nil
	} else {
		return nil, status.Error(codes.Unimplemented, "Block replication is not implemented")
	}
}
func (s *service) DeleteStorageProtectionGroup(ctx context.Context, req *csiext.DeleteStorageProtectionGroupRequest) (*csiext.DeleteStorageProtectionGroupResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	localParams := req.GetProtectionGroupAttributes()
	groupID := req.GetProtectionGroupId()
	arrayID, ok := localParams[s.opts.replicationContextPrefix+"systemName"]
	if !ok {
		log.Error("Can't get systemName from PG params")
	}
	remoteArrayID, ok := localParams[s.opts.replicationContextPrefix+"remoteSystemName"]
	if !ok {
		log.Error("Can't get systemName from PG params")
	}

	if err := s.requireProbe(ctx, arrayID); err != nil {
		return nil, err
	}
	localUnity, err := getUnityClient(ctx, s, arrayID)
	if err != nil {
		return nil, err
	}

	fileds := map[string]interface{}{
		"ProtectedStorageGroup": groupID,
	}

	log.WithFields(fileds).Info("Deleting storage protection group")
	fsAPI := gounity.NewFilesystem(localUnity)
	prefix := strings.ReplaceAll(groupID, strings.ToUpper(arrayID), strings.ToUpper(remoteArrayID))
	fsGroup, err := fsAPI.FindFileSystemGroupByPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}

	if len(fsGroup.Filesystems) != 0 {
		return nil, status.Error(codes.Internal, "FS group is not empty")
	}

	return &csiext.DeleteStorageProtectionGroupResponse{}, nil
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
