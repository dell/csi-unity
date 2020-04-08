package service

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sync"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/gofsutil"
	"github.com/dell/gounity"
	gounityapi "github.com/dell/gounity/api"
	"github.com/dell/gounity/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"context"
	"strings"
	"time"
)

var (
	devDiskByIDPrefix           = "/dev/disk/by-id/wwn-0x"
	targetMountRecheckSleepTime = 3 * time.Second
	maxBlockDevicesPerWWN       = 16
	lipSleepTime                = 5 * time.Second
	removeDeviceSleepTime       = 100 * time.Millisecond
	nodePublishSleepTime        = 1 * time.Second
	deviceDeletionTimeout       = 50 * time.Millisecond
	deviceDeletionPoll          = 5 * time.Millisecond
	multipathSleepTime          = 5 * time.Second
	multipathMutex              sync.Mutex
	deviceDeleteMutex           sync.Mutex
	lunzMutex                   sync.Mutex
)

func (s *service) NodeStageVolume(
	ctx context.Context,
	req *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing NodeStageVolume with args: %+v", *req)
	// Probe the node if required and make sure startup called
	err := s.nodeProbe(ctx)
	if err != nil {
		log.Error("nodeProbe failed with error :" + err.Error())
		return nil, err
	}

	privTgt := req.GetStagingTargetPath()
	if privTgt == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Target Path is required"))
	}

	protocol := req.GetVolumeContext()[keyProtocol]
	log.Debugf("Protocol is: %s", protocol)

	// Get the VolumeID and parse it
	volId := req.GetVolumeId()
	if volId == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "VolumeId can't be empty."))
	}

	volumeApi := gounity.NewVolume(s.unity)
	volume, err := volumeApi.FindVolumeById(ctx, volId)
	if err != nil {
		// If the volume isn't found, we cannot stage it
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Volume not found.%v", err))
	}

	if protocol == ISCSI {

		log.Debug("Rescanning Iscsi node")
		err = s.iscsiClient.PerformRescan()
		if err != nil {
			log.Error("RescanSCSIHost error: " + err.Error())
		}
	}

	symlinkPath, devPath, err := s.nodeScanForDeviceSymlinkPath(ctx, volume.VolumeContent.Wwn, protocol)

	// scan for the symlink and device path
	time.Sleep(nodePublishSleepTime)
	deviceWWN := utils.GetWwnFromVolumeContentWwn(volume.VolumeContent.Wwn)
	_, newDevPath, err := gofsutil.WWNToDevicePathX(ctx, deviceWWN)

	if err == nil && newDevPath != "" && newDevPath != devPath {
		log.Debugf("devPath updated from %s to %s", devPath, newDevPath)
		devPath = newDevPath
	} else if err != nil {
		log.Debugf("Error during stage volume: %v", err)
	}

	log.Debugf("Node Stage completed successfully - SymlinkPath is %s, DevPath is %s", symlinkPath, devPath)
	return &csi.NodeStageVolumeResponse{}, nil
}

func (s *service) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing NodeUnstageVolume with args: %+v", *req)
	// Probe the node if required and make sure startup called
	err := s.nodeProbe(ctx)
	if err != nil {
		log.Error("nodeProbe failed with error :" + err.Error())
		return nil, err
	}

	stageTgt := req.GetStagingTargetPath()
	if stageTgt == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "A Staging Target argument is required"))
	}

	err = gofsutil.Unmount(ctx, stageTgt)
	if err != nil {
		log.Errorf("NodeUnstageVolume error unmount stageTgt: %s ", err.Error())
	}

	// Get the VolumeID and parse it
	id := req.GetVolumeId()

	volumeApi := gounity.NewVolume(s.unity)
	volume, err := volumeApi.FindVolumeById(ctx, id)
	if err != nil {
		// If the volume isn't found, k8s will retry NodeUnstage forever so...
		// Make it stop...
		if err == gounity.FilesystemNotFoundError {
			return &csi.NodeUnstageVolumeResponse{}, nil
		}
		return nil, err
	}

	err = s.nodeDeleteBlockDevices(ctx, volume.VolumeContent.Wwn, stageTgt)
	if err != nil {
		log.Errorf("Delete Block devices error: %v", err)
		return nil, err
	}

	// Remove the mount private directory if present, and the directory
	err = removeWithRetry(ctx, stageTgt)
	if err != nil {
		log.Infof("Error removing stageTgt: %v", err)
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (s *service) NodePublishVolume(
	ctx context.Context,
	req *csi.NodePublishVolumeRequest) (
	*csi.NodePublishVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing NodePublishVolume with args: %+v", *req)

	if err := s.requireProbe(ctx); err != nil {
		//Temporary fix for bug in kubernetes - Return error once issue is fixed on k8s
		log.Info("Probe has not been invoked. Hence invoking Probe before Node publish volume")
		err = s.nodeProbe(ctx)
		if err != nil {
			return nil, err
		}
	}

	protocol := req.GetVolumeContext()[keyProtocol]
	log.Debugf("Protocol is: %s", protocol)

	if req.GetReadonly() == true {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "readonly must be false, because the supported mode only SINGLE_NODE_WRITER"))
	}

	// Get the VolumeID and validate against the volume
	volID := req.GetVolumeId()
	log.Printf("NodePublishVolume id: %s", volID)

	if volID == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume ID is required"))
	}

	volumeApi := gounity.NewVolume(s.unity)
	volume, err := volumeApi.FindVolumeById(ctx, volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "volume with ID '%s' not found", volID))
	}

	//Check if the volume is given access to the node
	err = s.checkVolumeMapping(ctx, volume)
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

	symlinkPath, _, err := s.nodeScanForDeviceSymlinkPath(ctx, volume.VolumeContent.Wwn, protocol)
	if err != nil || symlinkPath == "" {
		log.Debugf("Error during Node scan for device symlink path: %v", err)
		if err := checkAndRemoveLunz(ctx); err != nil {
			log.Debugf("Error during removal of Lunz device: %v", err)
			return nil, err
		}
		symlinkPath, _, err = s.nodeScanForDeviceSymlinkPath(ctx, volume.VolumeContent.Wwn, protocol)
		if err != nil {
			log.Debugf("Error during Node scan for device symlink path %v", err)
			return nil, err
		}
	}

	if err := publishVolume(ctx, req, s.opts.PvtMountDir, symlinkPath); err != nil {
		log.Debugf("Error during Publish: %v", err)
		return nil, err
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

// checkAndRemoveLunz checks for LUNZ devices by scanning the entries in /proc/scsi/scsi,

// identifying the model, vendor, host, channel and id of each entry, and then if an model entry is found named LUNZ with vendor

// DGC, then call a SCSI "remove-single-device" command is sent to the associated device.
func checkAndRemoveLunz(ctx context.Context) error {
	lunzMutex.Lock()
	defer lunzMutex.Unlock()
	ctx, log, rid := GetRunidLog(ctx)
	arg0 := "cat"
	arg1 := "/proc/scsi/scsi"

	log.Debugf("Obtained current ctx %v and rid %s", ctx, rid)

	cmd := exec.Command(arg0, arg1)
	stdout, err := cmd.Output()

	if err != nil {
		log.Errorf("Error during command execution: %v", err)
		return err
	}

	var modelString = regexp.MustCompile(`Model:\s+(\w.*?)\s*Rev:`)
	modelResult := modelString.FindAllStringSubmatch(string(stdout), -1)

	var vendorString = regexp.MustCompile(`Vendor:\s+(\w.*?)\s*Model:`)
	vendorResult := vendorString.FindAllStringSubmatch(string(stdout), -1)

	var hostString = regexp.MustCompile(`Host:\s+scsi(\w.*?)\s*Channel:`)
	hostResult := hostString.FindAllStringSubmatch(string(stdout), -1)

	var idString = regexp.MustCompile(`Id:\s+(\w.*?)\s*Lun:`)
	idResult := idString.FindAllStringSubmatch(string(stdout), -1)

	var channelString = regexp.MustCompile(`Channel:\s+(\w.*?)\s*Id:`)
	channelResult := channelString.FindAllStringSubmatch(string(stdout), -1)

	resultID := []string{}
	for i := 0; i < len(idResult); i++ {
		resultID = append(resultID, idResult[i][1])
	}

	resultChannel := []string{}
	for i := 0; i < len(channelResult); i++ {
		resultChannel = append(resultChannel, channelResult[i][1])
	}

	resultModel := []string{}
	for i := 0; i < len(modelResult); i++ {
		resultModel = append(resultModel, modelResult[i][1])
	}

	resultVendor := []string{}
	for i := 0; i < len(vendorResult); i++ {
		resultVendor = append(resultVendor, vendorResult[i][1])
	}

	resultHost := []string{}
	for i := 0; i < len(hostResult); i++ {
		resultHost = append(resultHost, hostResult[i][1])
	}

	for index, element := range resultModel {
		if element == "LUNZ" && resultVendor[index] == "DGC" {
			// We invoke the scsi remove-single-device command
			// only when the Vendor is DGC and LUN model is LUNZ
			filePath := "/proc/scsi/scsi"

			file, err := os.OpenFile(filePath, os.O_WRONLY, os.ModeDevice)
			if err != nil {
				log.Errorf("Error opening file %v", err)
				continue
			}
			if file != nil {
				command := fmt.Sprintf("scsi remove-single-device %s %s %s %d", resultHost[index],
					resultChannel[index], resultID[index], 0)
				log.Infof("Attempting to remove LUNZ with command %s", command)

				_, err = file.WriteString(command)
				if err != nil {
					fmt.Printf("error while writing...%v", err)
					file.Close()
					continue
				}
				file.Close()
			}
			log.Infof("LUNZ removal successful..")
		}
	}

	return nil
}

// Node Unpublish Volume - Unmounts the volume from the target path and from private directory
// Required - Volume ID and Target path
func (s *service) NodeUnpublishVolume(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest) (
	*csi.NodeUnpublishVolumeResponse, error) {

	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing NodeUnpublishVolume with args: %+v", *req)

	if err := s.requireProbe(ctx); err != nil {
		//Temporary fix for bug in kubernetes - Return error once issue is fixed on k8s
		log.Info("Probe has not been invoked. Hence invoking Probe before Node unpublish volume")
		err = s.nodeProbe(ctx)
		if err != nil {
			return nil, err
		}
	}

	id := req.GetVolumeId()
	volumeApi := gounity.NewVolume(s.unity)
	volume, err := volumeApi.FindVolumeById(ctx, id)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "volume with ID '%s' not found", id))
	}
	// Get the target path
	target := req.GetTargetPath()
	if target == "" {
		log.Error("Target path required")
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "target path required"))
	}

	log.Debug("NodeUnpublishVolume Target Path:", target)

	// Look through the mount table for the target path.
	var targetMount gofsutil.Info
	if targetMount, err = s.getTargetMount(ctx, target); err != nil {
		log.Error("Get target mount error. Error:", err)
		return nil, err
	}
	log.Debugf("Target Mount: %s", targetMount)

	if targetMount.Device == "" {
		// This should not happen normally. idempotent requests should be rare.
		// If we incorrectly exit here, conflicting devices will be left
		log.Debugf("No target mount found. waiting %v to re-verify no target %s mount", targetMountRecheckSleepTime, target)
		time.Sleep(targetMountRecheckSleepTime)

		targetMount, err = s.getTargetMount(ctx, target)

		//Make sure volume is not mounted elsewhere
		devMnts := make([]gofsutil.Info, 0)

		deviceWWN := strings.ReplaceAll(volume.VolumeContent.Wwn, ":", "")
		deviceWWN = strings.ToLower(deviceWWN)

		symlinkPath, _, err := gofsutil.WWNToDevicePathX(ctx, deviceWWN)
		if err != nil {
			log.Debugf("Disk path not found. Error: %v", err)
		}

		sysDevice, err := GetDevice(ctx, symlinkPath)

		if sysDevice != nil {
			devMnts, _ = getDevMounts(ctx, sysDevice)
		}

		if (err != nil || targetMount.Device == "") && len(devMnts) == 0 {
			log.Debugf("Still no mount entry for target, so assuming this is an idempotent call: %s", target)
			return &csi.NodeUnpublishVolumeResponse{}, nil
		}

		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Volume %s has been mounted outside the provided target path %s", id, target))
	}

	devicePath := targetMount.Device
	if devicePath == "devtmpfs" || devicePath == "" {
		devicePath = targetMount.Source
	}
	log.Debugf("TargetMount: %s", targetMount)

	err = s.checkVolumeMapping(ctx, volume)
	if err != nil {
		// Idempotent scenario - return ok if not published
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	privTgt := getPrivateMountPoint(s.opts.PvtMountDir, id)
	log.Debug("PrivateMountPoint:", privTgt)

	var lastUnmounted bool
	if lastUnmounted, err = unpublishVolume(ctx, req, s.opts.PvtMountDir, devicePath); err != nil {
		log.Errorf("UnpublishVolume error: %v", err)
		return nil, err
	}

	if lastUnmounted {
		err = s.nodeDeleteBlockDevices(ctx, volume.VolumeContent.Wwn, privTgt)
		if err != nil {
			log.Errorf("Delete Block devices error: %v", err)
			return nil, err
		}
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	// It is unusual that we have not removed the last mount (i.e. lastUnmounted == false)
	// Recheck to make sure the target is unmounted.
	log.Info("Not the last mount - rechecking target mount is gone")
	if targetMount, err = s.getTargetMount(ctx, target); err != nil {
		return nil, err
	}
	if targetMount.Device != "" {
		log.Error("Target mount still present... returning failure")
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Target Mount still present"))
	}
	// Get the device mounts
	dev, err := GetDevice(ctx, devicePath)
	if err != nil {
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, err.Error()))
	}
	log.Info("Rechecking dev mounts")
	mnts, err := getDevMounts(ctx, dev)
	if err != nil {
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, err.Error()))
	}
	if len(mnts) > 0 {
		log.Printf("Device mounts still present: %#v", mnts)
	} else {
		if err := s.nodeDeleteBlockDevices(ctx, volume.VolumeContent.Wwn, privTgt); err != nil {
			log.Errorf("Delete Block devices error: %v", err)
			return nil, err
		}
	}
	removeWithRetry(ctx, target)
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *service) NodeGetInfo(
	ctx context.Context,
	req *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Infof("Executing NodeGetInfo with args: %+v", *req)

	if err := s.requireProbe(ctx); err != nil {
		log.Info("AutoProbe has not been called. Executing manual probe")
		err = s.nodeProbe(ctx)
		if err != nil {
			return nil, err
		}
	}

	//Get Host Name
	if s.opts.NodeName == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "'Node Name' has not been configured. Set environment variable X_CSI_UNITY_NODENAME"))
	}

	//Get FC Initiator WWNs
	wwns, errFc := utils.GetFCInitiators(ctx)
	if errFc != nil {
		log.Info("'FC Initiators' cannot be retrieved.")
	}

	//Get iSCSI Initiator IQN
	iqns, errIscsi := s.iscsiClient.GetInitiators("")
	if errIscsi != nil {
		log.Info("'iSCSI Initiators' cannot be retrieved.")
	}

	if errFc != nil && errIscsi != nil {
		log.Errorf("Node %s does not have FC or iSCSI initiators", s.opts.NodeName)
		return nil, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Node %s does not have FC or iSCSI initiators", s.opts.NodeName))
	}

	hostApi := gounity.NewHost(s.unity)

	//Find Host on the Array
	host, err := hostApi.FindHostByName(ctx, s.opts.NodeName)
	if err != nil {
		if err == gounity.HostNotFoundError {

			//Create Host
			hostApi := gounity.NewHost(s.unity)
			host, err := hostApi.CreateHost(ctx, s.opts.NodeName)
			if err != nil {
				return nil, err
			}
			hostContent := host.HostContent
			log.Debugf("New Host Id: %s", hostContent.ID)

			//Create Host Ip Port
			_, err = hostApi.CreateHostIpPort(ctx, hostContent.ID, s.opts.LongNodeName)
			if err != nil {
				return nil, err
			}

			if len(wwns) > 0 {
				//Create Host FC Initiators
				log.Infof("FC Initiators found: %s", wwns)
				for _, wwn := range wwns {
					log.Infof("Adding wwn Initiator: %s to host: %s ", hostContent.ID, wwn)
					_, err = hostApi.CreateHostInitiator(ctx, hostContent.ID, wwn, gounityapi.FCInitiatorType)
					if err != nil {
						log.Errorf("Adding wwn initiator error: %v", err)
						return nil, err
					}
				}
			}
			if len(iqns) > 0 {
				//Create Host iSCSI Initiators
				log.Infof("iSCSI Initiators found: %s", iqns)
				for _, iqn := range iqns {
					log.Infof("Adding iSCSI Initiator: %s to host: %s ", hostContent.ID, iqn)
					_, err = hostApi.CreateHostInitiator(ctx, hostContent.ID, iqn, gounityapi.ISCSCIInitiatorType)
					if err != nil {
						log.Errorf("Adding iSCSI initiator error: %v", err)
						return nil, err
					}
				}
			}
		} else {
			return nil, err
		}
	} else {
		log.Infof("Host %s exists on the array", s.opts.NodeName)
		hostContent := host.HostContent
		arrayHostWwns, err := s.getArrayHostInitiators(ctx, host)
		if err != nil {
			log.Error(fmt.Sprintf("Error while finding initiators for host %s on the array:", hostContent.ID), err)
			return nil, err
		}

		//Check if all elements of wwns is present inside arrayHostWwns
		if utils.ArrayContainsAll(append(wwns, iqns...), arrayHostWwns) && len(append(wwns, iqns...)) == len(arrayHostWwns) {
			log.Info("Node initiators are synchronized with the Host Wwns on the array")
		} else {

			//Find Initiators on the array host that are not present on the node
			extraWwns := utils.FindAdditionalWwns(append(wwns, iqns...), arrayHostWwns)
			if len(extraWwns) > 0 {
				return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Host has got foreign Initiators. Host initiators on the array require correction before proceeding further."))
			}

			//Modify host operation
			for _, wwn := range wwns {
				log.Infof("Adding wwn Initiator: %s to host: %s ", hostContent.ID, wwn)
				_, err = hostApi.CreateHostInitiator(ctx, hostContent.ID, wwn, gounityapi.FCInitiatorType)
				if err != nil {
					log.Errorf("Adding wwn initiator error: %v", err)
					return nil, err
				}
			}
			for _, iqn := range iqns {
				log.Infof("Adding iSCSI Initiator: %s to host: %s ", hostContent.ID, iqn)
				_, err = hostApi.CreateHostInitiator(ctx, hostContent.ID, iqn, gounityapi.ISCSCIInitiatorType)
				if err != nil {
					log.Errorf("Adding iSCSI initiator error: %v", err)
					return nil, err
				}
			}
		}

		//Check Ip of the host with Host IP Port
		findHostIpPort := false
		for _, ipPort := range hostContent.IpPorts {
			hostIpPort, err := hostApi.FindHostIpPortById(ctx, ipPort.Id)
			if err != nil {
				continue
			}
			if hostIpPort != nil && hostIpPort.HostIpContent.Address == s.opts.LongNodeName {
				findHostIpPort = true
				break
			}
		}

		if findHostIpPort == false {
			//Create Host Ip Port
			_, err = hostApi.CreateHostIpPort(ctx, hostContent.ID, s.opts.LongNodeName)
			if err != nil {
				return nil, err
			}
		}
	}

	if len(iqns) > 0 {

		s.copyMultipathConfigFile(ctx, s.opts.Chroot)

		ipInterfaceAPI := gounity.NewIpInterface(s.unity)
		ipInterfaces, err := ipInterfaceAPI.ListIscsiIPInterfaces(ctx)
		if err != nil {
			log.Errorf("Error retrieving iScsi Interface IPs from the array: %v", err)
			return nil, err
		}

		interfaceIps := utils.GetIPsFromInferfaces(ctx, ipInterfaces)

		//Always discover and login during driver start up
		s.iScsiDiscoverAndLogin(ctx, interfaceIps)
	}

	//hostName would be the Node ID
	return &csi.NodeGetInfoResponse{
		NodeId: s.opts.NodeName,
	}, nil
}

func (s *service) NodeGetCapabilities(
	ctx context.Context,
	req *csi.NodeGetCapabilitiesRequest) (
	*csi.NodeGetCapabilitiesResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Infof("Executing NodeGetCapabilities with args: %+v", *req)

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
	rid, log := utils.GetRunidAndLogger(ctx)
	log.Info("Executing nodeProbe")
	// Check that we have the details needed to login to the Gateway
	if s.opts.Endpoint == "" {
		return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "missing Unity endpoint"))
	}
	if s.opts.User == "" {
		return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "missing Unity user"))
	}
	if s.opts.Password == "" {
		return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "missing Unity password"))
	}

	// Create our Unity API client, if needed
	if s.unity == nil {
		c, err := gounity.NewClientWithArgs(ctx, s.opts.Endpoint, s.opts.Insecure, s.opts.UseCerts)
		if err != nil {
			return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "unable to create Unity client: %s", err.Error()))
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
			return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "unable to login to Unity: %s", err.Error()))
		}
	}

	return nil
}

// Check if the volume is published to the node
func (s *service) checkVolumeMapping(ctx context.Context, volume *types.Volume) error {
	rid, log := utils.GetRunidAndLogger(ctx)
	//Get Host Name
	hostName := s.opts.NodeName

	hostAPI := gounity.NewHost(s.unity)
	host, err := hostAPI.FindHostByName(ctx, hostName)
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
	return status.Error(codes.Aborted, utils.GetMessageWithRunID(rid, "Volume %s has not been published to this node %s.", volName, hostName))
}

func (s *service) getTargetMount(ctx context.Context, target string) (gofsutil.Info, error) {
	rid, log := utils.GetRunidAndLogger(ctx)
	var targetMount gofsutil.Info
	mounts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		log.Error("could not reliably determine existing mount status")
		return targetMount, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "could not reliably determine existing mount status"))
	}
	for _, mount := range mounts {
		if mount.Path == target {
			targetMount = mount
			log.Debugf("matching targetMount %s target %s", target, mount.Path)
			break
		}
	}
	return targetMount, nil
}

// Given a volume WWN, delete all the associated block devices (including multipath) on that node.

// nodeScanForDeviceSymlinkPath scans SCSI devices to get the device symlink and device paths.
// devinceWWN is the volume's WWN field
func (s *service) nodeScanForDeviceSymlinkPath(ctx context.Context, wwn, protocol string) (string, string, error) {
	rid, log := utils.GetRunidAndLogger(ctx)
	var err error
	deviceWWN := utils.GetWwnFromVolumeContentWwn(wwn)

	ipInterfaceAPI := gounity.NewIpInterface(s.unity)
	ipInterfaces, err := ipInterfaceAPI.ListIscsiIPInterfaces(ctx)
	if err != nil {
		log.Errorf("Error retrieving iScsi Interface IPs from the array: %v", err)
		return "", "", err
	}
	interfaceIps := utils.GetIPsFromInferfaces(ctx, ipInterfaces)

	arrayTargets := make([]string, 0)
	if protocol == ISCSI {
		//add iScsi targets to targets or empty for FC
		arrayTargets = s.iScsiDiscoverAndfetchTargets(ctx, interfaceIps)
	}

	// Determine if the device is visible, if not perform a simple rescan.
	symlinkPath, devicePath, err := gofsutil.WWNToDevicePathX(ctx, deviceWWN)
	for retry := 0; devicePath == "" && retry < 3; retry++ {
		if err == nil && symlinkPath != "" && devicePath != "" {
			break
		}

		err = gofsutil.RescanSCSIHost(ctx, arrayTargets, deviceWWN)
		if err != nil {
			log.Debugf("Error during RescanScsiHost: %v", err)
		}
		time.Sleep(nodePublishSleepTime)
		symlinkPath, devicePath, err = gofsutil.WWNToDevicePathX(ctx, deviceWWN)
	}

	// Rescan FC adapters
	if err != nil && protocol == FC {
		log.Error("No Fibrechannel DevicePath found after multiple rescans... last resort is issue_lip and retry")
		maxRetries := 3
		for retry := 0; err != nil && retry < maxRetries; retry++ {
			err := gofsutil.IssueLIPToAllFCHosts(ctx)
			if err != nil {
				log.Error(err.Error())
			}
			// Look for the device again
			time.Sleep(lipSleepTime)
			symlinkPath, devicePath, err = gofsutil.WWNToDevicePathX(ctx, deviceWWN)
			if err == nil && symlinkPath != "" && devicePath != "" {
				break
			}
		}
	}

	//Re discover and login for iScsi
	if err != nil && protocol == ISCSI {
		log.Error("No iSCSI DevicePath found after multiple rescans... last resort is iscsi DiscoverTargets and PerformRescan")

		maxRetries := 2
		for retry := 0; err != nil && retry < maxRetries; retry++ {
			// Try running the iscsi discovery process again.
			s.iScsiDiscoverAndLogin(ctx, interfaceIps)
			time.Sleep(nodePublishSleepTime)

			err = s.iscsiClient.PerformRescan()
			if err != nil {
				log.Error("RescanSCSIHost error: " + err.Error())
			}
			// Look for the device again
			time.Sleep(nodePublishSleepTime)
			symlinkPath, devicePath, err = gofsutil.WWNToDevicePathX(ctx, deviceWWN)
			if err == nil && symlinkPath != "" && devicePath != "" {
				break
			}
		}
	}

	if err != nil {
		log.Error("Unable to find device after multiple discovery attempts: " + err.Error())
		return "", "", status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Unable to find device after multiple discovery attempts: "+err.Error()))
	}
	return symlinkPath, devicePath, nil
}

func (s *service) getArrayHostInitiators(ctx context.Context, host *types.Host) ([]string, error) {
	var hostInitiatorWwns []string
	hostContent := host.HostContent
	hostAPI := gounity.NewHost(s.unity)
	hostInitiators := append(hostContent.FcInitiators, hostContent.IscsiInitiators...)
	for _, initiator := range hostInitiators {
		initiatorID := initiator.Id
		hostInitiator, err := hostAPI.FindHostInitiatorById(ctx, initiatorID)
		if err != nil {
			return nil, err
		}
		hostInitiatorWwns = append(hostInitiatorWwns, strings.ToLower(hostInitiator.HostInitiatorContent.InitiatorId))
	}
	return hostInitiatorWwns, nil
}

func (s *service) iScsiDiscoverAndLogin(ctx context.Context, interfaceIps []string) {
	ctx, log, _ := GetRunidLog(ctx)

	validIPs := s.getValidInterfaceIps(ctx, interfaceIps)

	log.Debug("Valid IPs: ", validIPs)

	for _, ip := range validIPs {
		// passing true to login to target after discovery
		log.Debug("Begin discover and login to: ", ip)

		targets, err := s.iscsiClient.DiscoverTargets(ip, false)
		if err != nil {
			log.Debugf("Error executing iscsiadm discovery: %v", err)
			continue
		}

		for _, tgt := range targets {
			ipSlice := strings.Split(tgt.Portal, ":")
			if utils.ArrayContains(validIPs, ipSlice[0]) {
				err = s.iscsiClient.PerformLogin(tgt)
				if err != nil {
					log.Debugf("Error logging in to target %s : %v", tgt.Target, err)
				} else {
					log.Debugf("Login successful to target %s", tgt.Target)
				}
			}
		}
	}
	log.Debug("Completed discovery and rescan of all IP Interfaces")
}

func (s *service) iScsiDiscoverAndfetchTargets(ctx context.Context, interfaceIps []string) []string {
	log := utils.GetRunidLogger(ctx)
	targetIqns := make([]string, 0)
	validIPs := s.getValidInterfaceIps(ctx, interfaceIps)

	for _, ip := range validIPs {
		log.Debug("Begin discover on IP: ", ip)
		targets, err := s.iscsiClient.DiscoverTargets(ip, false)
		if err != nil {
			log.Debugf("Error executing iscsiadm discovery: %v", err)
			continue
		}

		for _, tgt := range targets {
			targetIqns = append(targetIqns, tgt.Target)
		}
	}
	return targetIqns
}

func (s *service) getValidInterfaceIps(ctx context.Context, interfaceIps []string) []string {
	ctx, log, _ := GetRunidLog(ctx)
	validIPs := make([]string, 0)

	for _, ip := range interfaceIps {
		if utils.IPReachable(ctx, ip, IScsiPort, TcpDialTimeout) {
			validIPs = append(validIPs, ip)
		} else {
			log.Debugf("Skipping IP : %s", ip)
		}
	}
	return validIPs
}

// copyMultipathConfig file copies the /etc/multipath.conf file from the nodeRoot chdir path to
// /etc/multipath.conf if testRoot is "". testRoot can be set for testing to copy somehwere else,
// but it should be empty ( "" ) for normal operation. nodeRoot is normally iscsiChroot env. variable.
func (s *service) copyMultipathConfigFile(ctx context.Context, nodeRoot string) error {
	log := utils.GetRunidLogger(ctx)
	var srcFile *os.File
	var dstFile *os.File
	var err error
	// Copy the multipath.conf file from /noderoot/etc/multipath.conf (EnvISCSIChroot)to /etc/multipath.conf if present
	srcFile, err = os.Open(nodeRoot + "/etc/multipath.conf")
	if err == nil {
		dstFile, err = os.Create("/etc/multipath.conf")
		if err != nil {
			log.Error("Could not open /etc/multipath.conf for writing")
		} else {
			written, _ := io.Copy(dstFile, srcFile)
			log.Debugf("copied %d bytes to /etc/multipath.conf", written)
			dstFile.Close()
		}
		srcFile.Close()
	}
	return err
}
