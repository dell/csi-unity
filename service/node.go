package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/gobrick"
	"github.com/dell/gofsutil"
	"github.com/dell/goiscsi"
	"github.com/dell/gounity"
	gounityapi "github.com/dell/gounity/api"
	"github.com/dell/gounity/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	targetMountRecheckSleepTime = 3 * time.Second
	disconnectVolumeRetryTime   = 1 * time.Second
	nodeStartTimeout			= 3 * time.Second
	lunzMutex                   sync.Mutex
	LUNZHLU                     = 0
	nodeMutex                   sync.Mutex
	sysBlock                    = "/sys/block"
	syncNodeInfoChan            chan bool
)

func (s *service) NodeStageVolume(
	ctx context.Context,
	req *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing NodeStageVolume with args: %+v", *req)
	volId, protocol, arrayId, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIdContext(ctx, arrayId)
	// Probe the node if required and make sure startup called
	if err := s.nodeProbe(ctx, arrayId); err != nil {
		return nil, err
	}

	stagingPath := req.GetStagingTargetPath()
	if stagingPath == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "staging target path required"))
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

	log.Debugf("Protocol is: %s", protocol)

	if protocol == NFS {
		//Perform stage mount for NFS
		nfsShare, nfsv3, nfsv4, err := s.getNFSShare(ctx, volId, arrayId)
		if err != nil {
			return nil, err
		}

		err = s.checkFilesystemMapping(ctx, nfsShare, am, arrayId)
		if err != nil {
			return nil, err
		}

		exportPaths := nfsShare.NFSShareContent.ExportPaths
		if len(exportPaths) == 0 {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Export paths not exist on NFS Share: %s", nfsShare.NFSShareContent.Id))
		}

		err = stagePublishNFS(ctx, req, exportPaths, arrayId, nfsv3, nfsv4)
		if err != nil {
			return nil, err
		}
		log.Debugf("Node Stage completed successfully: filesystem: %s is mounted on staging target path: %s", volId, stagingPath)
		return &csi.NodeStageVolumeResponse{}, nil
	} else {
		//Protocol if FC or iSCSI

		err = s.checkVolumeCapability(ctx, req)
		if err != nil {
			return nil, err
		}

		volumeApi := gounity.NewVolume(unity)
		volume, err := volumeApi.FindVolumeById(ctx, volId)
		if err != nil {
			// If the volume isn't found, we cannot stage it
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Volume not found. [%v]", err))
		}

		//Check if the volume is given access to the node
		hlu, err := s.checkVolumeMapping(ctx, volume, arrayId)
		if err != nil {
			return nil, err
		}

		volumeWwn := utils.GetWwnFromVolumeContentWwn(volume.VolumeContent.Wwn)
		publishContextData := publishContextData{
			deviceWWN:        "0x" + volumeWwn,
			volumeLUNAddress: hlu,
		}

		if hlu == LUNZHLU {
			if err := checkAndRemoveLunz(ctx); err != nil {
				return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Error during removal of Lunz device: [%v]", err))
			}
		}

		useFC := false
		if protocol == ISCSI {
			ipInterfaceAPI := gounity.NewIpInterface(unity)
			ipInterfaces, err := ipInterfaceAPI.ListIscsiIPInterfaces(ctx)
			if err != nil {
				return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Error retrieving iScsi Interface IPs from the array: [%v]", err))
			}
			interfaceIps := utils.GetIPsFromInferfaces(ctx, ipInterfaces)
			publishContextData.iscsiTargets = s.iScsiDiscoverFetchTargets(ctx, interfaceIps)
			log.Debugf("Found iscsi Targets: %s", publishContextData.iscsiTargets)

			if s.iscsiConnector == nil {
				s.initISCSIConnector(s.opts.Chroot)
			}
		} else if protocol == FC {
			useFC = true
			var targetWwns []string
			hostName := s.opts.NodeName

			hostAPI := gounity.NewHost(unity)
			host, err := hostAPI.FindHostByName(ctx, hostName)
			if err != nil {
				return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find Host Failed [%v]", err))
			}

			for _, initiator := range host.HostContent.FcInitiators {
				hostInitiator, err := hostAPI.FindHostInitiatorById(ctx, initiator.Id)
				if err != nil {
					return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find Host Initiator Failed [%v]", err))
				}

				for _, initiatorPath := range hostInitiator.HostInitiatorContent.Paths {
					hostInitiatorPath, err := hostAPI.FindHostInitiatorPathById(ctx, initiatorPath.Id)
					if err != nil {
						return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find Host Initiator Path Failed [%v]", err))
					}

					fcPort, err := hostAPI.FindFcPortById(ctx, hostInitiatorPath.HostInitiatorPathContent.FcPortID.Id)
					if err != nil {
						return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find Fc port Failed [%v]", err))
					}

					wwn := utils.GetFcPortWwnFromVolumeContentWwn(fcPort.FcPortContent.Wwn)
					if !utils.ArrayContains(targetWwns, wwn) {
						log.Debug("Found Target wwn: ", wwn)
						targetWwns = append(targetWwns, wwn)
					}
				}
			}
			publishContextData.fcTargets = targetWwns
			log.Debugf("Found FC Targets: %s", publishContextData.iscsiTargets)

			if s.fcConnector == nil {
				s.initFCConnector(s.opts.Chroot)
			}
		}

		log.Debug("Connect context data: ", publishContextData)
		devicePath, err := s.connectDevice(ctx, publishContextData, useFC)
		if err != nil {
			return nil, err
		}

		err = stageVolume(ctx, req, stagingPath, devicePath)
		if err != nil {
			return nil, err
		}

		log.Debugf("Node Stage completed successfully - Device path is %s", devicePath)
		return &csi.NodeStageVolumeResponse{}, nil
	}
}

func (s *service) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing NodeUnstageVolume with args: %+v", *req)

	// Get the VolumeID and parse it
	volId, protocol, arrayId, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIdContext(ctx, arrayId)
	// Probe the node if required and make sure startup called
	if err := s.nodeProbe(ctx, arrayId); err != nil {
		return nil, err
	}

	stageTgt := req.GetStagingTargetPath()
	if stageTgt == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "A Staging Target argument is required"))
	}

	if protocol == NFS {
		nfsShare, _, _, err := s.getNFSShare(ctx, volId, arrayId)
		if err != nil {
			// If the filesysten isn't found, k8s will retry NodeUnstage forever so...
			// There is no way back if filesystem isn't found and so considering this scenario idempotent
			if err == gounity.FilesystemNotFoundError {
				log.Debugf("Filesystem %s not found on the array %s during Node Unstage. Hence considering the call to be idempotent", volId, arrayId)
				return &csi.NodeUnstageVolumeResponse{}, nil
			}
			return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "%v", err))
		}

		exportPaths := nfsShare.NFSShareContent.ExportPaths
		if len(exportPaths) == 0 {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Export paths not exist on NFS Share: %s", nfsShare.NFSShareContent.Id))
		}

		err = unpublishNFS(ctx, stageTgt, arrayId, exportPaths)
		if err != nil {
			return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "%v", err))
		}
		log.Debugf("Node Unstage completed successfully. No mounts on staging target path: %s", req.GetStagingTargetPath())
		return &csi.NodeUnstageVolumeResponse{}, nil
	} else if protocol == ProtocolUnknown {
		//Volume is mounted via CSI-Unity v1.0 or v1.1 and hence different staging target path was used
		stageTgt = path.Join(s.opts.PvtMountDir, volId)

		hostName := s.opts.NodeName

		hostAPI := gounity.NewHost(unity)
		host, err := hostAPI.FindHostByName(ctx, hostName)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("Find Host Failed %v", err))
		}

		if len(host.HostContent.FcInitiators) == 0 {
			//FC gets precedence if host has both initiators - which is not supported by the driver
			protocol = FC
		} else if len(host.HostContent.IscsiInitiators) == 0 {
			protocol = ISCSI
		}
	} else if protocol != FC && protocol != ISCSI {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Invalid Protocol Value %s after parsing volume context ID %s", protocol, req.GetVolumeId()))
	}

	volumeApi := gounity.NewVolume(unity)
	volume, err := volumeApi.FindVolumeById(ctx, volId)
	if err != nil {
		// If the volume isn't found, k8s will retry NodeUnstage forever so...
		// There is no way back if volume isn't found and so considering this scenario idempotent
		if err == gounity.VolumeNotFoundError {
			log.Debugf("Volume %s not found on the array %s during Node Unstage. Hence considering the call to be idempotent", volId, arrayId)
			return &csi.NodeUnstageVolumeResponse{}, nil
		}
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "%v", err))
	}

	volumeWwn := utils.GetWwnFromVolumeContentWwn(volume.VolumeContent.Wwn)
	lastMounted, devicePath, err := unstageVolume(ctx, req, volumeWwn, s.opts.Chroot)
	if err != nil {
		return nil, err
	}

	if !lastMounted {
		// It is unusual that we have not removed the last mount (i.e. lastUnmounted == false)
		// Recheck to make sure the target is unmounted.
		log.Debug("Not the last mount - rechecking target mount is gone")
		targetMount, err := getTargetMount(ctx, stageTgt)
		if err != nil {
			return nil, err
		}
		if targetMount.Device != "" {
			return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Target Mount still present"))
		}

		if devicePath == "" {
			devicePath = targetMount.Source
		}

		// Get the device mounts
		dev, err := GetDevice(ctx, devicePath)
		if err != nil {
			return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, err.Error()))
		}
		log.Debug("Rechecking dev mounts")
		mnts, err := getDevMounts(ctx, dev)
		if err != nil {
			return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, err.Error()))
		}
		if len(mnts) > 0 {
			return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Device mounts still present after unmounting target and staging mounts %#v", mnts))
		}
	}

	err = s.disconnectVolume(ctx, volumeWwn, protocol)
	if err != nil {
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
	log.Debugf("Executing NodePublishVolume with args: %+v", *req)

	volID, protocol, arrayId, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIdContext(ctx, arrayId)
	// Probe the node if required and make sure startup called
	if err := s.requireProbe(ctx, arrayId); err != nil {
		log.Debug("Probe has not been invoked. Hence invoking Probe before Node publish volume")
		err = s.nodeProbe(ctx, arrayId)
		if err != nil {
			return nil, err
		}
	}

	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "target path required"))
	}

	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "staging target path required"))
	}

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume capability required"))
	}

	accMode := volCap.GetAccessMode()
	if accMode == nil {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume access mode required"))
	}

	if protocol == NFS {
		//Perform target mount for NFS
		nfsShare, nfsv3, nfsv4, err := s.getNFSShare(ctx, volID, arrayId)
		if err != nil {
			return nil, err
		}
		exportPaths := nfsShare.NFSShareContent.ExportPaths
		if len(exportPaths) == 0 {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Export paths not exist on NFS Share: %s", nfsShare.NFSShareContent.Id))
		}
		err = publishNFS(ctx, req, exportPaths, arrayId, s.opts.Chroot, nfsv3, nfsv4)
		if err != nil {
			return nil, err
		}
		log.Debugf("Node Publish completed successfully: filesystem: %s is mounted on target path: %s", volID, targetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	volumeApi := gounity.NewVolume(unity)
	volume, err := volumeApi.FindVolumeById(ctx, volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "volume with ID '%s' not found", volID))
	}

	deviceWWN := utils.GetWwnFromVolumeContentWwn(volume.VolumeContent.Wwn)

	symlinkPath, _, err := gofsutil.WWNToDevicePathX(ctx, deviceWWN)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Disk path not found. Error: %v", err))
	}

	if err := publishVolume(ctx, req, targetPath, symlinkPath, s.opts.Chroot); err != nil {
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
		return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Error during command execution: %v", err))
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
				log.Errorf("Attempting to remove LUNZ with command %s", command)

				_, err = file.WriteString(command)
				if err != nil {
					log.Errorf("error while writing...%v", err)
					file.Close()
					continue
				}
				file.Close()
			}
			log.Debugf("LUNZ removal successful..")
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
	log.Debugf("Executing NodeUnpublishVolume with args: %+v", *req)

	volId, protocol, arrayId, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIdContext(ctx, arrayId)
	if err := s.requireProbe(ctx, arrayId); err != nil {
		//Temporary fix for bug in kubernetes - Return error once issue is fixed on k8s
		log.Debug("Probe has not been invoked. Hence invoking Probe before Node unpublish volume")
		err = s.nodeProbe(ctx, arrayId)
		if err != nil {
			return nil, err
		}
	}

	// Get the target path
	target := req.GetTargetPath()
	if target == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "target path required"))
	}

	if protocol == NFS {
		nfsShare, _, _, err := s.getNFSShare(ctx, volId, arrayId)
		if err != nil {
			// If the filesysten isn't found, k8s will retry NodeUnpublish forever so...
			// There is no way back if filesystem isn't found and so considering this scenario idempotent
			if err == gounity.FilesystemNotFoundError {
				log.Debugf("Filesystem %s not found on the array %s during Node Unpublish. Hence considering the call to be idempotent", volId, arrayId)
				return &csi.NodeUnpublishVolumeResponse{}, nil
			}
			return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "%v", err))
		}
		exportPaths := nfsShare.NFSShareContent.ExportPaths
		if len(exportPaths) == 0 {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Export paths not exist on NFS Share: %s", nfsShare.NFSShareContent.Id))
		}

		err = unpublishNFS(ctx, target, arrayId, exportPaths)
		if err != nil {
			return nil, err
		}
		log.Debugf("Node Unpublish completed successfully. No mounts on target path: %s", req.GetTargetPath())
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	volumeApi := gounity.NewVolume(unity)
	_, err = volumeApi.FindVolumeById(ctx, volId)
	if err != nil {
		// If the volume isn't found, k8s will retry NodeUnpublish forever so...
		// There is no way back if volume isn't found and so considering this scenario idempotent
		if err == gounity.VolumeNotFoundError {
			log.Debugf("Volume %s not found on the array %s during Node Unpublish. Hence considering the call to be idempotent", volId, arrayId)
			return &csi.NodeUnpublishVolumeResponse{}, nil
		}
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "%v", err))
	}

	log.Debug("NodeUnpublishVolume Target Path:", target)

	err = unpublishVolume(ctx, req)
	if err != nil {
		return nil, err
	}

	removeWithRetry(ctx, target)
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *service) NodeGetInfo(
	ctx context.Context,
	req *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error) {

	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing NodeGetInfo with args: %+v", *req)

	atleastOneArraySuccess := false
	//Sleep for a while and wait untill iscsi discovery is completed
	time.Sleep(nodeStartTimeout)
	for _, array := range s.getStorageArrayList() {
		if array.IsHostAdded {
			atleastOneArraySuccess = true
		}
	}

	if atleastOneArraySuccess {
		log.Info("NodeGetInfo success")
		//hostName would be the Node ID
		return &csi.NodeGetInfoResponse{
			NodeId: s.opts.NodeName,
		}, nil
	}

	return nil, status.Error(codes.Unavailable, utils.GetMessageWithRunID(rid, "The node [%s] is not added to any of the arrays", s.opts.NodeName))
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
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
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

func (s *service) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing NodeExpandVolume with args: %+v", *req)

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volumeId is mandatory parameter"))
	}

	volID, _, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.VolumeId, volumeType)
	if err != nil {
		return nil, err
	}

	size := req.GetCapacityRange().GetRequiredBytes()

	ctx, log = setArrayIdContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		log.Debug("AutoProbe has not been called. Executing manual probe")
		err = s.nodeProbe(ctx, arrayID)
		if err != nil {
			return nil, err
		}
	}

	// We are getting target path that points to mounted path on "/"
	// This doesn't help us, though we should trace the path received
	volumePath := req.GetVolumePath()
	if volumePath == "" {
		return nil, status.Error(codes.InvalidArgument,
			utils.GetMessageWithRunID(rid, "Volume path required"))
	}

	volumeAPI := gounity.NewVolume(unity)
	volume, err := volumeAPI.FindVolumeById(ctx, volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find volume Failed %v", err))
	}

	volName := volume.VolumeContent.Name

	//Locate and fetch all (multipath/regular) mounted paths using this volume
	devMnt, err := gofsutil.GetMountInfoFromDevice(ctx, volName)
	if err != nil {
		return nil, status.Error(codes.Internal,
			utils.GetMessageWithRunID(rid, "Failed to find mount info for (%s) with error %v", volName, err))
	}

	log.Debugf("Mount info for volume %s: %+v", volName, devMnt)

	// Rescan the device for the volume expanded on the array
	for _, device := range devMnt.DeviceNames {
		log.Debug("Begin rescan for :", device)
		devicePath := sysBlock + "/" + device
		err = gofsutil.DeviceRescan(ctx, devicePath)
		if err != nil {
			return nil, status.Error(codes.Internal,
				utils.GetMessageWithRunID(rid, "Failed to rescan device (%s) with error %v", devicePath, err))
		}
	}
	// Expand the filesystem with the actual expanded volume size.
	if devMnt.MPathName != "" {
		err = gofsutil.ResizeMultipath(ctx, devMnt.MPathName)
		if err != nil {
			return nil, status.Error(codes.Internal,
				utils.GetMessageWithRunID(rid, "Failed to resize filesystem: device  (%s) with error %v", devMnt.MountPoint, err))
		}
	}
	//For a regular device, get the device path (devMnt.DeviceNames[1]) where the filesystem is mounted
	//PublishVolume creates devMnt.DeviceNames[0] but is left unused for regular devices
	var devicePath string
	if len(devMnt.DeviceNames) > 1 {
		devicePath = "/dev/" + devMnt.DeviceNames[1]
	} else if len(devMnt.DeviceNames) == 1 {
		devicePath = "/dev/" + devMnt.DeviceNames[0]
	} else if devicePath == "" {
		return nil, status.Error(codes.Internal,
			utils.GetMessageWithRunID(rid, "Failed to resize filesystem: device name not found for (%s)", devMnt.MountPoint))
	}

	fsType, err := gofsutil.FindFSType(ctx, devMnt.MountPoint)
	if err != nil {
		return nil, status.Error(codes.Internal,
			utils.GetMessageWithRunID(rid, "Failed to fetch filesystem for volume  (%s) with error %v", devMnt.MountPoint, err))
	}

	log.Infof("Found %s filesystem mounted on volume %s", fsType, devMnt.MountPoint)

	//Resize the filesystem
	err = gofsutil.ResizeFS(ctx, devMnt.MountPoint, devicePath, devMnt.MPathName, fsType)
	if err != nil {
		return nil, status.Error(codes.Internal,
			utils.GetMessageWithRunID(rid, "Failed to resize filesystem: mountpoint (%s) device (%s) with error %v",
				devMnt.MountPoint, devicePath, err))
	}

	log.Debug("Node Expand completed successfully")
	return &csi.NodeExpandVolumeResponse{CapacityBytes: size}, nil
}

func (s *service) nodeProbe(ctx context.Context, arrayId string) error {
	return s.probe(ctx, "Node", arrayId)
}

//Get NFS Share from Filesystem
func (s *service) getNFSShare(ctx context.Context, filesystemId, arrayId string) (*types.NFSShare, bool, bool, error) {
	ctx, _, rid := GetRunidLog(ctx)
	ctx, _ = setArrayIdContext(ctx, arrayId)

	unity, err := s.getUnityClient(ctx, arrayId)
	if err != nil {
		return nil, false, false, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Get Unity client for array %s failed. Error: %v ", arrayId, err))
	}

	isSnapshot := false
	fileApi := gounity.NewFilesystem(unity)
	filesystem, err := fileApi.FindFilesystemById(ctx, filesystemId)
	var snapResp *types.Snapshot
	if err != nil {
		snapshotApi := gounity.NewSnapshot(unity)
		snapResp, err = snapshotApi.FindSnapshotById(ctx, filesystemId)
		if err != nil {
			return nil, false, false, err
		}
		isSnapshot = true
		filesystem, err = s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.Id, arrayId)
		if err != nil {
			return nil, false, false, err
		}
	}

	var nfsShareId string

	for _, nfsShare := range filesystem.FileContent.NFSShare {
		if isSnapshot {
			if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.Id == filesystemId {
				nfsShareId = nfsShare.Id
			}
		} else {
			if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.Id == "" {
				nfsShareId = nfsShare.Id
			}
		}
	}

	if nfsShareId == "" {
		return nil, false, false, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "NFS Share for filesystem: %s not found. Error: %v", filesystemId, err))
	}

	nfsShare, err := fileApi.FindNFSShareById(ctx, nfsShareId)
	if err != nil {
		return nil, false, false, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "NFS Share: %s not found. Error: %v", nfsShareId, err))
	}

	nasServer, err := fileApi.FindNASServerById(ctx, filesystem.FileContent.NASServer.Id)
	if err != nil {
		return nil, false, false, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "NAS Server: %s not found. Error: %v", filesystem.FileContent.NASServer.Id, err))
	}

	if !nasServer.NASServerContent.NFSServer.NFSv3Enabled && !nasServer.NASServerContent.NFSServer.NFSv4Enabled {
		return nil, false, false, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Nas Server: %s does not support NFSv3 and NFSv4. At least one of the versions should be supported", nasServer.NASServerContent.Id))
	}

	return nfsShare, nasServer.NASServerContent.NFSServer.NFSv3Enabled, nasServer.NASServerContent.NFSServer.NFSv4Enabled, nil
}

//Check if the Filesystem has access to the node
func (s *service) checkFilesystemMapping(ctx context.Context, nfsShare *types.NFSShare, am *csi.VolumeCapability_AccessMode, arrayId string) error {
	ctx, _, rid := GetRunidLog(ctx)
	ctx, _ = setArrayIdContext(ctx, arrayId)
	unity, err := s.getUnityClient(ctx, arrayId)
	var accessType gounity.AccessType
	if err != nil {
		return err
	}

	hostAPI := gounity.NewHost(unity)
	host, err := hostAPI.FindHostByName(ctx, s.opts.NodeName)
	if err != nil {
		return status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Host: %s not found. Error: %v", s.opts.NodeName, err))
	}
	hostContent := host.HostContent
	hostID := hostContent.ID

	hostHasAccess := false
	if am.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
		accessType = gounity.ReadOnlyRootAccessType
		for _, host := range nfsShare.NFSShareContent.ReadOnlyRootAccessHosts {
			if host.ID == hostID {
				hostHasAccess = true
			}
		}
	} else {
		accessType = gounity.ReadWriteRootAccessType
		for _, host := range nfsShare.NFSShareContent.RootAccessHosts {
			if host.ID == hostID {
				hostHasAccess = true
			}
		}
	}
	if !hostHasAccess {
		return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Host: %s does not have access: %s on NFS Share: %s", s.opts.NodeName, accessType, nfsShare.NFSShareContent.Id))
	}
	return nil
}

// Check if the volume is published to the node
func (s *service) checkVolumeMapping(ctx context.Context, volume *types.Volume, arrayId string) (int, error) {
	rid, log := utils.GetRunidAndLogger(ctx)
	//Get Host Name
	hostName := s.opts.NodeName
	unity, err := s.getUnityClient(ctx, arrayId)
	if err != nil {
		return 0, err
	}

	hostAPI := gounity.NewHost(unity)
	host, err := hostAPI.FindHostByName(ctx, hostName)
	if err != nil {
		return 0, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Find Host Failed [%v]", err))
	}
	hostContent := host.HostContent
	hostID := hostContent.ID

	content := volume.VolumeContent
	volName := content.Name

	for _, hostaccess := range content.HostAccessResponse {
		hostcontent := hostaccess.HostContent
		hostAccessID := hostcontent.ID
		if hostAccessID == hostID {
			log.Debugf(fmt.Sprintf("Volume %s has been published to the current node %s.", volName, hostName))
			return hostaccess.HLU, nil
		}
	}

	return 0, status.Error(codes.Aborted, utils.GetMessageWithRunID(rid, "Volume %s has not been published to this node %s.", volName, hostName))
}

// Check Volume capabilities and access modes
func (s *service) checkVolumeCapability(ctx context.Context, req *csi.NodeStageVolumeRequest) error {
	rid, _ := utils.GetRunidAndLogger(ctx)

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume capability required"))
	}

	accMode := volCap.GetAccessMode()
	if accMode == nil {
		return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume access mode required"))
	}

	if blockVol := volCap.GetBlock(); blockVol != nil {
		// BlockVolume is not supported
		return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Block Volume not supported"))
	}

	mntVol := volCap.GetMount()
	if mntVol != nil {
		ro := false //Since only SINGLE_NODE_WRITER is supported
		if ro {
			return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "read only not supported for Mount Volume"))
		}
	} else {
		return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume capability - Mount param has not been provided"))
	}
	return nil
}

func getTargetMount(ctx context.Context, target string) (gofsutil.Info, error) {
	rid, log := utils.GetRunidAndLogger(ctx)
	var targetMount gofsutil.Info
	mounts, err := gofsutil.GetMounts(ctx)
	if err != nil {
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

func (s *service) getArrayHostInitiators(ctx context.Context, host *types.Host, arrayId string) ([]string, error) {
	var hostInitiatorWwns []string
	hostContent := host.HostContent
	unity, err := s.getUnityClient(ctx, arrayId)
	if err != nil {
		return nil, err
	}
	hostAPI := gounity.NewHost(unity)
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

func (s *service) iScsiDiscoverFetchTargets(ctx context.Context, interfaceIps []string) []goiscsi.ISCSITarget {
	log := utils.GetRunidLogger(ctx)
	iscsiTargets := make([]goiscsi.ISCSITarget, 0)
	validIPs := s.getValidInterfaceIps(ctx, interfaceIps)

	for _, ip := range validIPs {
		log.Debug("Begin discover on IP: ", ip)
		targets, err := s.iscsiClient.DiscoverTargets(ip, false)
		if err != nil {
			log.Debugf("Error executing iscsiadm discovery: %v", err)
			continue
		}

		for _, tgt := range targets {

			ipSlice := strings.Split(tgt.Portal, ":")
			if utils.ArrayContains(validIPs, ipSlice[0]) {
				iscsiTargets = append(iscsiTargets, tgt)
			}
		}
		//All targets are obtained with one valid IP
		break
	}
	return iscsiTargets
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

func (s *service) connectDevice(ctx context.Context, data publishContextData, useFC bool) (string, error) {
	rid, _ := utils.GetRunidAndLogger(ctx)
	var err error
	var device gobrick.Device
	if useFC {
		device, err = s.connectFCDevice(ctx, data.volumeLUNAddress, data)
	} else {
		device, err = s.connectISCSIDevice(ctx, data.volumeLUNAddress, data)
	}

	if err != nil {
		return "", status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Unable to find device after multiple discovery attempts: [%v]", err))
	}
	devicePath := path.Join("/dev/", device.Name)
	return devicePath, nil
}

func (s *service) connectISCSIDevice(ctx context.Context,
	lun int, data publishContextData) (gobrick.Device, error) {
	var targets []gobrick.ISCSITargetInfo
	for _, t := range data.iscsiTargets {
		targets = append(targets, gobrick.ISCSITargetInfo{Target: t.Target, Portal: t.Portal})
	}
	// separate context to prevent 15 seconds cancel from kubernetes
	connectorCtx, cFunc := context.WithTimeout(ctx, time.Second*120)
	defer cFunc()

	return s.iscsiConnector.ConnectVolume(connectorCtx, gobrick.ISCSIVolumeInfo{
		Targets: targets,
		Lun:     lun,
	})
}

func (s *service) connectFCDevice(ctx context.Context,
	lun int, data publishContextData) (gobrick.Device, error) {
	var targets []gobrick.FCTargetInfo
	for _, wwn := range data.fcTargets {
		targets = append(targets, gobrick.FCTargetInfo{WWPN: wwn})
	}
	// separate context to prevent 15 seconds cancel from kubernetes
	connectorCtx, cFunc := context.WithTimeout(ctx, time.Second*120)
	defer cFunc()

	return s.fcConnector.ConnectVolume(connectorCtx, gobrick.FCVolumeInfo{
		Targets: targets,
		Lun:     lun,
	})
}

// disconnectVolume disconnects a volume from a node and will verify it is disonnected
// by no more /dev/disk/by-id entry, retrying if necessary.
func (s *service) disconnectVolume(ctx context.Context, volumeWWN, protocol string) error {
	rid, log := utils.GetRunidAndLogger(ctx)

	if protocol == FC {
		s.initFCConnector(s.opts.Chroot)
	} else if protocol == ISCSI {
		s.initISCSIConnector(s.opts.Chroot)
	}

	for i := 0; i < 3; i++ {
		var deviceName string
		symlinkPath, devicePath, _ := gofsutil.WWNToDevicePathX(ctx, volumeWWN)
		if devicePath == "" {
			if i == 0 {
				log.Infof("NodeUnstage - Couldn't find device path for volume %s", volumeWWN)
			}
			return nil
		}
		devicePathComponents := strings.Split(devicePath, "/")
		deviceName = devicePathComponents[len(devicePathComponents)-1]

		nodeUnstageCtx, cancel := context.WithTimeout(ctx, time.Second*120)

		if protocol == FC {
			s.fcConnector.DisconnectVolumeByDeviceName(nodeUnstageCtx, deviceName)
		} else if protocol == ISCSI {
			s.iscsiConnector.DisconnectVolumeByDeviceName(nodeUnstageCtx, deviceName)
		}

		cancel()
		time.Sleep(disconnectVolumeRetryTime)

		// Check that the /sys/block/DeviceName actually exists
		if _, err := ioutil.ReadDir(sysBlock + deviceName); err != nil {
			// If not, make sure the symlink is removed
			log.Debugf("Removing device %s", symlinkPath)
			os.Remove(symlinkPath)
		}
	}

	// Recheck volume disconnected
	devPath, _ := gofsutil.WWNToDevicePath(ctx, volumeWWN)
	if devPath == "" {
		log.Debugf("Disconnect succesful for colume wwn %s", volumeWWN)
		return nil
	}
	return status.Errorf(codes.Internal, utils.GetMessageWithRunID(rid, "disconnectVolume exceeded retry limit WWN %s devPath %s", volumeWWN, devPath))
}

type publishContextData struct {
	deviceWWN        string
	volumeLUNAddress int
	iscsiTargets     []goiscsi.ISCSITarget
	fcTargets        []string
}

// ISCSITargetInfo represents basic information about iSCSI target
type ISCSITargetInfo struct {
	Portal string
	Target string
}

func (s *service) addNodeInformationIntoArray(ctx context.Context, array *StorageArrayConfig) error {
	ctx, log, rid := GetRunidLog(ctx)
	ctx, log = setArrayIdContext(ctx, array.ArrayId)
	unity := array.UnityClient
	hostApi := gounity.NewHost(unity)

	if err := s.requireProbe(ctx, array.ArrayId); err != nil {
		log.Debug("AutoProbe has not been called. Executing manual probe")
		err = s.nodeProbe(ctx, array.ArrayId)
		if err != nil {
			return err
		}
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
		log.Infof("Node %s does not have FC or iSCSI initiators and can only be used for NFS exports", s.opts.NodeName)
	}

	nodeIps, err := utils.GetHostIP()
	if err != nil {
		return status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Unable to get node IP. Error: %v", err))
	}

	//Find Host on the Array
	host, err := hostApi.FindHostByName(ctx, s.opts.NodeName)
	if err != nil {
		if err == gounity.HostNotFoundError {
			//Create Host
			hostApi := gounity.NewHost(unity)
			host, err := hostApi.CreateHost(ctx, s.opts.NodeName)
			if err != nil {
				return err
			}
			hostContent := host.HostContent
			log.Debugf("New Host Id: %s", hostContent.ID)

			//Create Host Ip Port
			_, err = hostApi.CreateHostIpPort(ctx, hostContent.ID, s.opts.LongNodeName)
			if err != nil {
				return err
			}
			for _, nodeIp := range nodeIps {
				_, err = hostApi.CreateHostIpPort(ctx, hostContent.ID, nodeIp)
				if err != nil {
					return err
				}
			}

			if len(wwns) > 0 {
				//Create Host FC Initiators
				log.Debugf("FC Initiators found: %s", wwns)
				for _, wwn := range wwns {
					log.Debugf("Adding wwn Initiator: %s to host: %s ", hostContent.ID, wwn)
					_, err = hostApi.CreateHostInitiator(ctx, hostContent.ID, wwn, gounityapi.FCInitiatorType)
					if err != nil {
						return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Adding wwn initiator error: %v", err))
					}
				}
			}
			if len(iqns) > 0 {
				//Create Host iSCSI Initiators
				log.Debugf("iSCSI Initiators found: %s", iqns)
				for _, iqn := range iqns {
					log.Debugf("Adding iSCSI Initiator: %s to host: %s ", hostContent.ID, iqn)
					_, err = hostApi.CreateHostInitiator(ctx, hostContent.ID, iqn, gounityapi.ISCSCIInitiatorType)
					if err != nil {
						return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Adding iSCSI initiator error: %v", err))
					}
				}
			}
		} else {
			return err
		}
	} else {
		log.Debugf("Host %s exists on the array", s.opts.NodeName)
		hostContent := host.HostContent
		arrayHostWwns, err := s.getArrayHostInitiators(ctx, host, array.ArrayId)
		if err != nil {
			return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Error while finding initiators for host %s on the array: %s error: %v", hostContent.ID, array, err))
		}

		//Check if all elements of wwns is present inside arrayHostWwns
		if utils.ArrayContainsAll(append(wwns, iqns...), arrayHostWwns) && len(append(wwns, iqns...)) == len(arrayHostWwns) {
			log.Info("Node initiators are synchronized with the Host Wwns on the array")
		} else {
			//Find Initiators on the array host that are not present on the node
			extraWwns := utils.FindAdditionalWwns(append(wwns, iqns...), arrayHostWwns)
			if len(extraWwns) > 0 {
				return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Host has got foreign Initiators. Host initiators on the array require correction before proceeding further."))
			}

			//Modify host operation
			for _, wwn := range wwns {
				log.Debugf("Adding wwn Initiator: %s to host: %s ", hostContent.ID, wwn)
				_, err = hostApi.CreateHostInitiator(ctx, hostContent.ID, wwn, gounityapi.FCInitiatorType)
				if err != nil {
					return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Adding wwn initiator error: %v", err))
				}
			}
			for _, iqn := range iqns {
				log.Debugf("Adding iSCSI Initiator: %s to host: %s ", hostContent.ID, iqn)
				_, err = hostApi.CreateHostInitiator(ctx, hostContent.ID, iqn, gounityapi.ISCSCIInitiatorType)
				if err != nil {
					return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Adding iSCSI initiator error: %v", err))
				}
			}
		}

		//Check Ip of the host with Host IP Port
		findHostNamePort := false
		for _, ipPort := range hostContent.IpPorts {
			hostIpPort, err := hostApi.FindHostIpPortById(ctx, ipPort.Id)
			if err != nil {
				continue
			}
			if hostIpPort != nil && hostIpPort.HostIpContent.Address == s.opts.LongNodeName {
				findHostNamePort = true
				continue
			}
			if hostIpPort != nil {
				for i, nodeIp := range nodeIps {
					if hostIpPort.HostIpContent.Address == nodeIp {
						nodeIps[i] = nodeIps[len(nodeIps)-1]
						nodeIps = nodeIps[:len(nodeIps)-1]
						break
					}
				}
			}
		}

		if findHostNamePort == false {
			//Create Host Ip Port
			_, err = hostApi.CreateHostIpPort(ctx, hostContent.ID, s.opts.LongNodeName)
			if err != nil {
				return err
			}
		}
		for _, nodeIp := range nodeIps {
			_, err = hostApi.CreateHostIpPort(ctx, hostContent.ID, nodeIp)
			if err != nil {
				return err
			}
		}
	}

	if len(iqns) > 0 {
		s.copyMultipathConfigFile(ctx, s.opts.Chroot)
		ipInterfaceAPI := gounity.NewIpInterface(unity)
		ipInterfaces, err := ipInterfaceAPI.ListIscsiIPInterfaces(ctx)
		if err != nil {
			return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Error retrieving iScsi Interface IPs from the array: %v", err))
		}

		interfaceIps := utils.GetIPsFromInferfaces(ctx, ipInterfaces)

		//Always discover and login during driver start up
		s.iScsiDiscoverAndLogin(ctx, interfaceIps)
	}
	array.IsHostAdded = true
	return nil
}

func (s *service) syncNodeInfoRoutine(ctx context.Context) {
	ctx, log := setRunIdContext(ctx, "node-0")
	log.Info("Starting goroutine to add Node information to storage array")
	for {
		select {
		case <-syncNodeInfoChan:
			log.Debug("Config change identified. Adding node info")
			s.syncNodeInfo(ctx)
			ctx, log = incrementLogId(ctx, "node")
		case <-time.After(time.Duration(s.opts.SyncNodeInfoTimeInterval) * time.Minute):
			log.Debug("Invoking adding host information to array")
			var allHostsAdded = true
			s.arrays.Range(func(key, value interface{}) bool {
				array := value.(*StorageArrayConfig)
				if !array.IsHostAdded {
					allHostsAdded = false
					return true
				}
				return true
			})

			if !allHostsAdded {
				log.Debug("Some of the hosts are not added, invoking adding host information to array")
				s.syncNodeInfo(ctx)
				ctx, log = incrementLogId(ctx, "node")
			}
		}
	}
}

//Synchronize node information using addNodeInformationIntoArray
func (s *service) syncNodeInfo(ctx context.Context) {
	nodeMutex.Lock()
	defer nodeMutex.Unlock()
	length := 0
	s.arrays.Range(func(_, _ interface{}) bool {
		length++
		return true
	})

	ctx, log := incrementLogId(ctx, "node")
	log.Debug("Synchronizing Node Info")
	// Add a count of three, one for each goroutine.
	s.arrays.Range(func(key, value interface{}) bool {
		array := value.(*StorageArrayConfig)
		if !array.IsHostAdded {
			go func() {
				ctx, log := incrementLogId(ctx, "node")
				err := s.addNodeInformationIntoArray(ctx, array)
				if err == nil {
					array.IsHostAdded = true
					log.Debugf("Node [%s] Added successfully", array.ArrayId)
				} else {
					log.Debugf("Adding node [%s] failed", array.ArrayId)
				}
			}()
		}
		return true
	})
}
