package service

import (
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/gofsutil"
	"github.com/dell/gounity"
	types "github.com/dell/gounity/payloads"
	"github.com/golang/glog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"

	"context"
	"strings"
	"time"
)

var (
	devDiskByIDPrefix           = "/dev/disk/by-id/wwn-0x"
	targetMountRecheckSleepTime = 30 * time.Second
	maxBlockDevicesPerWWN       = 16
	removeDeviceSleepTime       = 1 * time.Second
	multipathMutex              sync.Mutex
	nodePublishSleepTime        = 1 * time.Second
)

func (s *service) NodeStageVolume(
	ctx context.Context,
	req *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error) {
	log.Info("Executing NodeStageVolume")
	// Probe the node if required and make sure startup called
	err := s.nodeProbe(ctx)
	if err != nil {
		log.Error("nodeProbe failed with error :" + err.Error())
		return nil, err
	}

	privTgt := req.GetStagingTargetPath()
	if privTgt == "" {
		return nil, status.Error(codes.InvalidArgument, "Target Path is required")
	}
	// Get the VolumeID and parse it
	volId := req.GetVolumeId()
	if volId == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId can't be empty.")
	}

	volumeApi := gounity.NewVolume(s.unity)
	volume, err := volumeApi.FindVolumeById(volId)
	if err != nil {
		// If the volume isn't found, we cannot stage it
		return nil, status.Error(codes.NotFound, fmt.Sprintf("Volume not found.%v", err))
	}
	var symlinkPath string
	symlinkPath, _, err = s.nodeScanForDeviceSymlinkPath(volume.VolumeContent.Wwn)
	if err != nil {
		return nil, err
	}
	// scan for the symlink and device path
	symlinkPath, devPath, err := s.wwnToDevicePath(volume.VolumeContent.Wwn)
	if err != nil {
		log.Errorf("Error in stage volume: %v", err)
		return nil, err
	}
	log.WithField("SymlinkPath", symlinkPath).WithField("DevPath", devPath).Info("NodeStageVolume completed")
	return &csi.NodeStageVolumeResponse{}, nil
}

func (s *service) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {
	log.Info("Executing NodeUnstageVolume")
	// Probe the node if required and make sure startup called
	err := s.nodeProbe(ctx)
	if err != nil {
		log.Error("nodeProbe failed with error :" + err.Error())
		return nil, err
	}

	privTgt := req.GetStagingTargetPath()
	if privTgt == "" {
		return nil, status.Error(codes.InvalidArgument, "A Staging Target argument is required")
	}

	// Get the VolumeID and parse it
	id := req.GetVolumeId()

	volumeApi := gounity.NewVolume(s.unity)
	volume, err := volumeApi.FindVolumeById(id)
	if err != nil {
		// If the volume isn't found, k8s will retry NodeUnstage forever so...
		// Make it stop...
		return &csi.NodeUnstageVolumeResponse{}, nil
	}
	err = s.nodeDeleteBlockDevices(volume.VolumeContent.Wwn, privTgt)
	if err != nil {
		return nil, err
	}
	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (s *service) NodePublishVolume(
	ctx context.Context,
	req *csi.NodePublishVolumeRequest) (
	*csi.NodePublishVolumeResponse, error) {
	log.Info("Executing NodePublishVolume")
	if err := s.requireProbe(ctx); err != nil {
		//Temporary fix for bug in kubernetes - Return error once issue is fixed on k8s
		log.Info("Probe has not been invoked. Hence invoking Probe before Node publish volume")
		err = s.nodeProbe(ctx)
		if err != nil {
			return nil, err
		}
	}

	if req.GetReadonly() == true {
		return nil, status.Error(codes.InvalidArgument, "readonly must be false, because the supported mode only SINGLE_NODE_WRITER")
	}

	// Get the VolumeID and validate against the volume
	volID := req.GetVolumeId()
	log.Printf("NodePublishVolume id: %s", volID)

	if volID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	volumeApi := gounity.NewVolume(s.unity)
	volume, err := volumeApi.FindVolumeById(volID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "volume with ID '%s' not found", volID)
	}

	//Check if the volume is given access to the node
	err = checkVolumeMapping(s, volume)
	if err != nil {
		log.Printf("NodePublishVolume returning not published to node: %s", volID)
		return nil, err
	}

	volumeContext := req.GetVolumeContext()
	if volumeContext != nil {
		log.Printf("VolumeContext:")
		for key, value := range volumeContext {
			log.Printf("  [%s]=%s", key, value)
		}
	}

	symlinkPath, _, err := s.wwnToDevicePath(volume.VolumeContent.Wwn)
	if err := publishVolume(req, s.opts.PvtMountDir, symlinkPath); err != nil {
		return nil, err
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

// Node Unpublish Volume - Unmounts the volume from the target path and from private directory
// Required - Volume ID and Target path
func (s *service) NodeUnpublishVolume(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest) (
	*csi.NodeUnpublishVolumeResponse, error) {

	id := req.GetVolumeId()
	log.Printf("Executing NodeUnpublishVolume id: %s", id)

	volumeApi := gounity.NewVolume(s.unity)
	volume, err := volumeApi.FindVolumeById(id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "volume with ID '%s' not found", id)
	}
	// Get the target path
	target := req.GetTargetPath()
	if target == "" {
		log.Error("Target path required")
		return nil, status.Error(codes.InvalidArgument, "target path required")
	}

	log.Debug("NodeUnpublishVolume Target Path:", target)

	// Look through the mount table for the target path.
	var targetMount gofsutil.Info
	if targetMount, err = s.getTargetMount(target); err != nil {
		log.Error("Get target mount error. Error:", err)
		return nil, err
	}
	log.Debugf("Target Mount: %s", targetMount)

	if targetMount.Device == "" {
		// This should not happen normally. idempotent requests should be rare.
		// If we incorrectly exit here, conflicting devices will be left
		log.Debugf("No target mount found. waiting %v to re-verify no target %s mount", targetMountRecheckSleepTime, target)
		time.Sleep(targetMountRecheckSleepTime)
		if targetMount, err = s.getTargetMount(target); err != nil {
			log.Info(fmt.Sprintf("Still no mount entry for target, so assuming this is an idempotent call: %s", target))
			return &csi.NodeUnpublishVolumeResponse{}, nil
		}
	}

	devicePath := targetMount.Device
	if devicePath == "devtmpfs" || devicePath == "" {
		devicePath = targetMount.Source
	}
	log.Debugf("TargetMount: %s", targetMount)

	err = checkVolumeMapping(s, volume)
	if err != nil {
		// Idempotent scenario - return ok if not published
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	// scan for the symlink and device path
	symlinkPath, _, err := s.wwnToDevicePath(volume.VolumeContent.Wwn)
	if err != nil {
		log.Errorf("Error in NodeUnpublishVolume volume:%s Error: %v", id, err)
		return nil, err
	}

	privTgt := getPrivateMountPoint(s.opts.PvtMountDir, id)
	log.Debug("PrivateMountPoint:", privTgt)

	var lastUnmounted bool
	if lastUnmounted, err = unpublishVolume(req, s.opts.PvtMountDir, symlinkPath, devicePath); err != nil {
		log.Errorf("UnpublishVolume error: %v", err)
		return nil, err
	}

	if lastUnmounted {
		err = s.nodeDeleteBlockDevices(volume.VolumeContent.Wwn, privTgt)
		if err != nil {
			return nil, err
		}
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	// It is unusual that we have not removed the last mount (i.e. lastUnmounted == false)
	// Recheck to make sure the target is unmounted.
	log.Info("Not the last mount - rechecking target mount is gone")
	if targetMount, err = s.getTargetMount(target); err != nil {
		return nil, err
	}
	if targetMount.Device != "" {
		log.Error("Target mount still present... returning failure")
		return nil, status.Error(codes.Internal, "Target Mount still present")
	}
	// Get the device mounts
	dev, err := GetDevice(devicePath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	log.Info("Rechecking dev mounts")
	mnts, err := getDevMounts(dev)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if len(mnts) > 0 {
		log.Printf("Device mounts still present: %#v", mnts)
	} else {
		if err := s.nodeDeleteBlockDevices(volume.VolumeContent.Wwn, privTgt); err != nil {
			return nil, err
		}
	}
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *service) NodeGetInfo(
	ctx context.Context,
	req *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error) {

	log.Info("Executing NodeGetInfo. Host name:", s.opts.NodeName)
	if err := s.requireProbe(ctx); err != nil {
		log.Infof("AutoProbe has not been called. Executing manual probe")
		err = s.nodeProbe(ctx)
		if err != nil {
			return nil, err
		}
	}

	//Get Host Name
	hostName := s.opts.NodeName
	if hostName == "" {
		return nil, status.Error(codes.InvalidArgument, "'Node Name' has not been configured. Set environment variable X_CSI_UNITY_NODENAME")
	}

	//Get FC Initiator WWNs
	wwns, err := utils.GetFCInitiators()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("'FC Initiators' cannot be retrieved. %v", err))
	}

	hostApi := gounity.NewHost(s.unity)

	//Find Host on the Array
	host, err := hostApi.FindHostByName(s.opts.NodeName)
	if err != nil {
		if err == gounity.HostNotFoundError {

			//Find IP
			ip, err := utils.GetHostIP()
			if err != nil {
				return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("'Host IP' cannot be retrieved. %v", err))
			}

			//Create Host
			hostApi := gounity.NewHost(s.unity)
			host, err := hostApi.CreateHost(s.opts.NodeName)
			if err != nil {
				return nil, err
			}
			hostContent := host.HostContent
			log.Info("New Host Id: ", hostContent.ID)

			//Create Host Ip Port
			_, err = hostApi.CreateHostIpPort(hostContent.ID, ip)
			if err != nil {
				return nil, err
			}

			//Create Host Initiators
			log.Info("WWNs: ", wwns)
			for _, wwn := range wwns {
				log.Infof("Adding Initiator: %s to host: %s ", hostContent.ID, wwn)
				_, err = hostApi.CreateHostInitiator(hostContent.ID, wwn)
				if err != nil {
					log.Error("Adding initiator error:", err)
					return nil, err
				}
			}
		} else {
			return nil, err
		}
	} else {
		log.Infof("Host %s exists on the array", s.opts.NodeName)
		hostContent := host.HostContent
		arrayHostWwns, err := s.getArrayHostWwns(host)
		if err != nil {
			log.Error(fmt.Sprintf("Error while finding initiators for host %s on the array:", hostContent.ID), err)
			return nil, err
		}

		//Check if all elements of wwns is present inside arrayHostWwns
		if utils.ArrayContainsAll(wwns, arrayHostWwns) && len(wwns) == len(arrayHostWwns) {
			log.Info("Node initiators are synchronized with the Host Wwns on the array")
		} else {

			//Find Initiators on the array host that are not present on the node
			extraWwns := utils.FindAdditionalWwns(wwns, arrayHostWwns)
			if len(extraWwns) > 0 {
				return nil, status.Error(codes.Internal, "Host has got foreign Initiators. Host initiators on the array require correction before proceeding further.")
			}

			//Modify host operation
			for _, wwn := range wwns {
				log.Infof("Check and add Initiator: %s to host: %s ", hostContent.ID, wwn)
				_, err = hostApi.CreateHostInitiator(hostContent.ID, wwn)
				if err != nil {
					log.Error("Error while adding initiator:", err)
					return nil, err
				}
			}
		}
	}

	//hostName would be the Node ID
	return &csi.NodeGetInfoResponse{
		NodeId: hostName,
	}, nil
}

func (s *service) NodeGetCapabilities(
	ctx context.Context,
	req *csi.NodeGetCapabilitiesRequest) (
	*csi.NodeGetCapabilitiesResponse, error) {
	glog.V(5).Infof("Using default NodeGetCapabilities")
	log.Info("Executing NodeGetCapabilities")

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_UNKNOWN,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (s *service) NodeGetVolumeStats(
	ctx context.Context,
	req *csi.NodeGetVolumeStatsRequest) (
	*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "NodeGetVolumeStats not supported")
}

func (s *service) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "NodeExpandVolume not supported")
}

func (s *service) nodeProbe(ctx context.Context) error {
	log.Info("Executing nodeProbe")
	// Check that we have the details needed to login to the Gateway
	if s.opts.Endpoint == "" {
		return status.Error(codes.FailedPrecondition,
			"missing Unity endpoint")
	}
	if s.opts.User == "" {
		return status.Error(codes.FailedPrecondition,
			"missing Unity user")
	}
	if s.opts.Password == "" {
		return status.Error(codes.FailedPrecondition,
			"missing Unity password")
	}

	// Create our Unity API client, if needed
	if s.unity == nil {
		c, err := gounity.NewClientWithArgs(s.opts.Endpoint, s.opts.Insecure, s.opts.UseCerts)
		if err != nil {
			return status.Errorf(codes.FailedPrecondition, "unable to create Unity client: %s", err.Error())
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
			return status.Errorf(codes.FailedPrecondition,
				"unable to login to Unity: %s", err.Error())

		}
	}

	return nil
}

// Check if the volume is published to the node
func checkVolumeMapping(s *service, volume *types.Volume) error {
	//Get Host Name
	hostName := s.opts.NodeName

	hostAPI := gounity.NewHost(s.unity)
	host, err := hostAPI.FindHostByName(hostName)
	if err != nil {
		log.Error("Find Host Failed ", err)
		return err
	}
	hostContent := host.HostContent
	hostID := hostContent.ID

	content := volume.VolumeContent
	volName := content.Name

	for _, hostaccess := range content.HostAccessResponse {
		hostcontent := hostaccess.HostContent
		hostAccessID := hostcontent.ID
		if hostAccessID == hostID {
			log.Info(fmt.Sprintf("Volume %s has been published to the current node %s.", volName, hostName))
			return nil
		}
	}

	log.Info(fmt.Sprintf("Volume %s has not been published to this node %s.", volName, hostName))
	return status.Error(codes.Aborted, fmt.Sprintf("Volume %s has not been published to this node %s.", volName, hostName))
}

func (s *service) getTargetMount(target string) (gofsutil.Info, error) {
	var targetMount gofsutil.Info
	mounts, err := gofsutil.GetMounts(context.Background())
	if err != nil {
		log.Error("could not reliably determine existing mount status")
		return targetMount, status.Error(codes.Internal, "could not reliably determine existing mount status")
	}
	for _, mount := range mounts {
		if mount.Path == target {
			targetMount = mount
			log.Printf(fmt.Sprintf("matching targetMount %s target %s", target, mount.Path))
			break
		}
	}
	return targetMount, nil
}

// Given a volume WWN, delete all the associated block devices (including multipath) on that node.

// nodeScanForDeviceSymlinkPath scans SCSI devices to get the device symlink and device paths.
// devinceWWN is the volume's WWN field
func (s *service) nodeScanForDeviceSymlinkPath(wwn string) (string, string, error) {
	var err error

	wwn = strings.ReplaceAll(wwn, ":", "")
	deviceWWN := strings.ToLower(wwn)

	// Determine if the device is visible, if not perform a simple rescan.
	if err := s.rescanHba(wwn); err != nil {
		//Log and continue to fetch device
		log.Error("Error rescanning FC HBA ", err)
	}

	symlinkPath, devicePath, err := s.wwnToDevicePath(wwn)
	for retry := 0; devicePath == "" && retry < 3; retry++ {
		log.Debug("-------------RescanSCSIHost-------------")
		err = RescanSCSIHost()
		time.Sleep(nodePublishSleepTime)
		symlinkPath, devicePath, err = s.wwnToDevicePath(deviceWWN)
	}
	if err != nil {
		log.Info("No DevicePath found after multiple rescans... last resort is iscsi DiscoverTargets and PerformRescan")
		return "", "", status.Error(codes.NotFound, "Unable to find device after multiple discovery attempts: "+err.Error())
	}

	return symlinkPath, devicePath, nil
}

func (s *service) getArrayHostWwns(host *types.Host) ([]string, error) {

	var hostInitiatorWwns []string
	hostContent := host.HostContent
	hostAPI := gounity.NewHost(s.unity)
	for _, fcInitiator := range hostContent.FcInitiators {
		initiatorID := fcInitiator.Id
		hostInitiator, err := hostAPI.FindHostInitiatorById(initiatorID)
		if err != nil {
			return nil, err
		}
		hostInitiatorWwns = append(hostInitiatorWwns, strings.ToLower(hostInitiator.HostInitiatorContent.InitiatorId))
	}
	return hostInitiatorWwns, nil
}
