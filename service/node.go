package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

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
	"k8s.io/kubernetes/pkg/volume/util/fs"
)

// Variables that can be used across module
var (
	targetMountRecheckSleepTime = 3 * time.Second
	disconnectVolumeRetryTime   = 1 * time.Second
	nodeStartTimeout            = 3 * time.Second
	lunzMutex                   sync.Mutex
	LUNZHLU                     = 0
	nodeMutex                   sync.Mutex
	sysBlock                    = "/sys/block"
	syncNodeInfoChan            chan bool
	connectedSystemID           = make([]string, 0)
	VolumeNameLengthConstraint  = 63
)

const (
	componentOkMessage          = "ALRT_COMPONENT_OK"
	maxUnityVolumesPerNodeLabel = "max-unity-volumes-per-node"
	ubuntuNodeRoot              = "/noderoot"
	devtmpfs                    = "devtmpfs"
)

func (s *service) NodeStageVolume(
	ctx context.Context,
	req *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing NodeStageVolume with args: %+v", *req)
	volID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIDContext(ctx, arrayID)
	// Probe the node if required and make sure startup called
	if err := s.nodeProbe(ctx, arrayID); err != nil {
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

	isBlock := accTypeBlock(vc)

	protocol, err = ValidateAndGetProtocol(ctx, protocol, req.GetVolumeContext()[keyProtocol])
	if err != nil {
		return nil, err
	}

	log.Debugf("Protocol is: %s", protocol)

	if protocol == NFS {
		//Perform stage mount for NFS
		nfsShare, nfsv3, nfsv4, err := s.getNFSShare(ctx, volID, arrayID)
		if err != nil {
			return nil, err
		}

		err = s.checkFilesystemMapping(ctx, nfsShare, am, arrayID)
		if err != nil {
			return nil, err
		}

		exportPaths := nfsShare.NFSShareContent.ExportPaths
		if len(exportPaths) == 0 {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Export paths not exist on NFS Share: %s", nfsShare.NFSShareContent.Id))
		}

		err = stagePublishNFS(ctx, req, exportPaths, arrayID, nfsv3, nfsv4)
		if err != nil {
			return nil, err
		}
		log.Debugf("Node Stage completed successfully: filesystem: %s is mounted on staging target path: %s", volID, stagingPath)
		return &csi.NodeStageVolumeResponse{}, nil
	}
	//Protocol if FC or iSCSI

	volumeAPI := gounity.NewVolume(unity)
	volume, err := volumeAPI.FindVolumeById(ctx, volID)
	if err != nil {
		// If the volume isn't found, we cannot stage it
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Volume not found. [%v]", err))
	}

	//Check if the volume is given access to the node
	hlu, err := s.checkVolumeMapping(ctx, volume, arrayID)
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

		hostAPI := gounity.NewHost(unity)
		host, err := s.getHostID(ctx, arrayID, s.opts.NodeName, s.opts.LongNodeName)
		if err != nil {
			return nil, err
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

	//Skip staging for Block devices
	if !isBlock {
		err = stageVolume(ctx, req, stagingPath, devicePath)
		if err != nil {
			return nil, err
		}
	}

	log.Debugf("Node Stage completed successfully - Device path is %s", devicePath)
	return &csi.NodeStageVolumeResponse{}, nil
}

func (s *service) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing NodeUnstageVolume with args: %+v", *req)

	// Get the VolumeID and parse it
	volID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIDContext(ctx, arrayID)
	// Probe the node if required and make sure startup called
	if err := s.nodeProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	stageTgt := req.GetStagingTargetPath()
	if stageTgt == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "A Staging Target argument is required"))
	}

	if protocol == NFS {
		nfsShare, _, _, err := s.getNFSShare(ctx, volID, arrayID)
		if err != nil {
			// If the filesysten isn't found, k8s will retry NodeUnstage forever so...
			// There is no way back if filesystem isn't found and so considering this scenario idempotent
			if err == gounity.FilesystemNotFoundError {
				log.Debugf("Filesystem %s not found on the array %s during Node Unstage. Hence considering the call to be idempotent", volID, arrayID)
				return &csi.NodeUnstageVolumeResponse{}, nil
			}
			return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "%v", err))
		}

		exportPaths := nfsShare.NFSShareContent.ExportPaths
		if len(exportPaths) == 0 {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Export paths not exist on NFS Share: %s", nfsShare.NFSShareContent.Id))
		}

		err = unpublishNFS(ctx, stageTgt, arrayID, exportPaths)
		if err != nil {
			return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "%v", err))
		}
		log.Debugf("Node Unstage completed successfully. No mounts on staging target path: %s", req.GetStagingTargetPath())
		return &csi.NodeUnstageVolumeResponse{}, nil
	} else if protocol == ProtocolUnknown {
		//Volume is mounted via CSI-Unity v1.0 or v1.1 and hence different staging target path was used
		stageTgt = path.Join(s.opts.PvtMountDir, volID)

		host, err := s.getHostID(ctx, arrayID, s.opts.NodeName, s.opts.LongNodeName)
		if err != nil {
			return nil, err
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

	volumeAPI := gounity.NewVolume(unity)
	volume, err := volumeAPI.FindVolumeById(ctx, volID)
	if err != nil {
		// If the volume isn't found, k8s will retry NodeUnstage forever so...
		// There is no way back if volume isn't found and so considering this scenario idempotent
		if err == gounity.VolumeNotFoundError {
			log.Debugf("Volume %s not found on the array %s during Node Unstage. Hence considering the call to be idempotent", volID, arrayID)
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

	var ephemeralVolume bool
	ephemeral, ok := req.VolumeContext["csi.storage.k8s.io/ephemeral"]
	if ok {
		ephemeralVolume = strings.ToLower(ephemeral) == "true"
	}

	if ephemeralVolume {
		return s.ephemeralNodePublishVolume(ctx, req)
	}

	volID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIDContext(ctx, arrayID)
	// Probe the node if required and make sure startup called
	if err := s.requireProbe(ctx, arrayID); err != nil {
		log.Debug("Probe has not been invoked. Hence invoking Probe before Node publish volume")
		err = s.nodeProbe(ctx, arrayID)
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
	if accMode.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER {
		s.opts.AllowRWOMultiPodAccess = false
	} else if accMode.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER {
		s.opts.AllowRWOMultiPodAccess = true
	}

	if protocol == NFS {
		//Perform target mount for NFS
		nfsShare, nfsv3, nfsv4, err := s.getNFSShare(ctx, volID, arrayID)
		if err != nil {
			return nil, err
		}
		exportPaths := nfsShare.NFSShareContent.ExportPaths
		if len(exportPaths) == 0 {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Export paths not exist on NFS Share: %s", nfsShare.NFSShareContent.Id))
		}
		err = publishNFS(ctx, req, exportPaths, arrayID, s.opts.Chroot, nfsv3, nfsv4, s.opts.AllowRWOMultiPodAccess)
		if err != nil {
			return nil, err
		}
		log.Debugf("Node Publish completed successfully: filesystem: %s is mounted on target path: %s", volID, targetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	//Protocol FC or iSCSI

	isBlock := accTypeBlock(volCap)

	if isBlock && req.GetReadonly() == true {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "readonly not supported for Block"))
	}

	volumeAPI := gounity.NewVolume(unity)
	volume, err := volumeAPI.FindVolumeById(ctx, volID)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "volume with ID '%s' not found", volID))
	}

	deviceWWN := utils.GetWwnFromVolumeContentWwn(volume.VolumeContent.Wwn)

	symlinkPath, _, err := gofsutil.WWNToDevicePathX(ctx, deviceWWN)
	if err != nil {
		return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Disk path not found. Error: %v", err))
	}

	if err := publishVolume(ctx, req, targetPath, symlinkPath, s.opts.Chroot, s.opts.AllowRWOMultiPodAccess); err != nil {
		return nil, err
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *service) ephemeralNodePublishVolume(
	ctx context.Context,
	req *csi.NodePublishVolumeRequest) (
	*csi.NodePublishVolumeResponse, error) {
	ctx, log, rid := GetRunidLog(ctx)

	//Create Ephemeral Volume
	volName := req.VolumeId
	if len(volName) > VolumeNameLengthConstraint {
		volName = volName[0 : VolumeNameLengthConstraint-1]
	}
	size, err := utils.ParseSize(req.VolumeContext["size"])
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Unable to parse size. Error: %v", err))
	}
	createVolResp, err := s.CreateVolume(ctx, &csi.CreateVolumeRequest{
		Name: volName,
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: size,
		},
		VolumeCapabilities: []*csi.VolumeCapability{req.VolumeCapability},
		Parameters:         req.VolumeContext,
		Secrets:            req.Secrets,
	})
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Create Ephemeral Volume %s Failed with error: %v", volName, err))
	}
	log.Debugf("Ephemeral Volume %s created successfully", volName)

	//Create NodeUnpublishRequest for rollback scenario
	nodeUnpublishRequest := &csi.NodeUnpublishVolumeRequest{
		VolumeId:   createVolResp.Volume.VolumeId,
		TargetPath: req.TargetPath,
	}

	//ControllerPublishVolume to current node
	controllerPublishResp, err := s.ControllerPublishVolume(ctx, &csi.ControllerPublishVolumeRequest{
		VolumeId:         createVolResp.Volume.VolumeId,
		NodeId:           s.opts.NodeName + "," + s.opts.LongNodeName,
		VolumeCapability: req.VolumeCapability,
		Readonly:         req.Readonly,
		Secrets:          req.Secrets,
		VolumeContext:    createVolResp.Volume.VolumeContext,
	})
	if err != nil {
		//Call Ephemeral Node Unpublish for recovery
		_, _ = s.ephemeralNodeUnpublish(ctx, nodeUnpublishRequest, req.VolumeId)
		return nil, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Ephemeral Controller Publish Volume failed with error: %v", err))
	}
	log.Debug("Ephemeral Controller Publish successful")

	stagingMountPath := path.Join(s.opts.EnvEphemeralStagingTargetPath, req.VolumeId)

	//Node Stage for Ephemeral Volume
	_, err = s.NodeStageVolume(ctx, &csi.NodeStageVolumeRequest{
		VolumeId:          createVolResp.Volume.VolumeId,
		PublishContext:    controllerPublishResp.PublishContext,
		StagingTargetPath: path.Join(stagingMountPath, "globalmount"),
		VolumeCapability:  req.VolumeCapability,
		Secrets:           req.Secrets,
		VolumeContext:     createVolResp.Volume.VolumeContext,
	})
	if err != nil {
		//Call Ephemeral Node Unpublish for recovery
		_, _ = s.ephemeralNodeUnpublish(ctx, nodeUnpublishRequest, req.VolumeId)
		return nil, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Ephemeral Node Stage Volume failed with error: %v", err))
	}
	log.Debug("Ephemeral Node Stage Successful")

	//Node Publish for Ephemeral Volume
	_, err = s.NodePublishVolume(ctx, &csi.NodePublishVolumeRequest{
		VolumeId:          createVolResp.Volume.VolumeId,
		PublishContext:    controllerPublishResp.PublishContext,
		StagingTargetPath: path.Join(stagingMountPath, "globalmount"),
		TargetPath:        req.TargetPath,
		VolumeCapability:  req.VolumeCapability,
		Readonly:          req.Readonly,
		Secrets:           req.Secrets,
		VolumeContext:     createVolResp.Volume.VolumeContext,
	})
	if err != nil {
		//Call Ephemeral Node Unpublish for recovery
		_, _ = s.ephemeralNodeUnpublish(ctx, nodeUnpublishRequest, req.VolumeId)
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Ephemeral Node Publish Volume failed with error: %v", err))
	}
	log.Debug("Ephemeral Node Publish Successful")

	f, err := os.Create(path.Join(stagingMountPath, "id"))
	if err != nil {
		//Call Ephemeral Node Unpublish for recovery
		_, _ = s.ephemeralNodeUnpublish(ctx, nodeUnpublishRequest, req.VolumeId)
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Creation of file failed with error: %v", err))
	}
	defer f.Close()
	_, err2 := f.WriteString(createVolResp.Volume.VolumeId)
	if err2 != nil {
		//Call Ephemeral Node Unpublish for recovery
		_, _ = s.ephemeralNodeUnpublish(ctx, nodeUnpublishRequest, req.VolumeId)
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Save Volume Id in file failed with error: %v", err))
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

	var modelString = regexp.MustCompile(`Model:\s+(\w*.*?)\s*Rev:`)
	modelResult := modelString.FindAllStringSubmatch(string(stdout), -1)

	var vendorString = regexp.MustCompile(`Vendor:\s+(\w*.*?)\s*Model:`)
	vendorResult := vendorString.FindAllStringSubmatch(string(stdout), -1)

	var hostString = regexp.MustCompile(`Host:\s+scsi(\w*.*?)\s*Channel:`)
	hostResult := hostString.FindAllStringSubmatch(string(stdout), -1)

	var idString = regexp.MustCompile(`Id:\s+(\w*.*?)\s*Lun:`)
	idResult := idString.FindAllStringSubmatch(string(stdout), -1)

	var channelString = regexp.MustCompile(`Channel:\s+(\w*.*?)\s*Id:`)
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
				log.Warnf("Error opening file %v", err)
				continue
			}
			if file != nil {
				command := fmt.Sprintf("scsi remove-single-device %s %s %s %d", resultHost[index],
					resultChannel[index], resultID[index], 0)
				log.Debugf("Attempting to remove LUNZ with command %s", command)

				_, err = file.WriteString(command)
				if err != nil {
					log.Warnf("error while writing...%v", err)
					err = file.Close()
					if err != nil {
						log.Infof("Error closing file: %v", err)
					}
					continue
				}
				err = file.Close()
				if err != nil {
					log.Infof("Error closing file: %v", err)
				}
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

	var isEphemeralVolume bool
	volName := req.VolumeId
	file := s.opts.EnvEphemeralStagingTargetPath + req.VolumeId + "/id"
	if _, err := os.Stat(file); err == nil {
		isEphemeralVolume = true
		dat, err := ioutil.ReadFile(filepath.Clean(file))
		if err != nil {
			return nil, errors.New("Unable to get volume id for ephemeral volume")
		}
		req.VolumeId = string(dat)
	}

	if isEphemeralVolume {
		return s.ephemeralNodeUnpublish(ctx, req, volName)
	}

	volID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.GetVolumeId(), volumeType)
	if err != nil {
		return nil, err
	}
	ctx, log = setArrayIDContext(ctx, arrayID)

	// Probe node if required
	if err := s.nodeProbe(ctx, arrayID); err != nil {
		return nil, err
	}

	// Get the target path
	target := req.GetTargetPath()
	if target == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "target path required"))
	}

	if protocol == NFS {
		nfsShare, _, _, err := s.getNFSShare(ctx, volID, arrayID)
		if err != nil {
			// If the filesysten isn't found, k8s will retry NodeUnpublish forever so...
			// There is no way back if filesystem isn't found and so considering this scenario idempotent
			if err == gounity.FilesystemNotFoundError {
				log.Debugf("Filesystem %s not found on the array %s during Node Unpublish. Hence considering the call to be idempotent", volID, arrayID)
				return &csi.NodeUnpublishVolumeResponse{}, nil
			}
			return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "%v", err))
		}
		exportPaths := nfsShare.NFSShareContent.ExportPaths
		if len(exportPaths) == 0 {
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Export paths not exist on NFS Share: %s", nfsShare.NFSShareContent.Id))
		}

		err = unpublishNFS(ctx, target, arrayID, exportPaths)
		if err != nil {
			return nil, err
		}
		log.Debugf("Node Unpublish completed successfully. No mounts on target path: %s", req.GetTargetPath())
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	volumeAPI := gounity.NewVolume(unity)
	_, err = volumeAPI.FindVolumeById(ctx, volID)
	if err != nil {
		// If the volume isn't found, k8s will retry NodeUnpublish forever so...
		// There is no way back if volume isn't found and so considering this scenario idempotent
		if err == gounity.VolumeNotFoundError {
			log.Debugf("Volume %s not found on the array %s during Node Unpublish. Hence considering the call to be idempotent", volID, arrayID)
			return &csi.NodeUnpublishVolumeResponse{}, nil
		}
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "%v", err))
	}

	log.Debug("NodeUnpublishVolume Target Path:", target)

	err = unpublishVolume(ctx, req)
	if err != nil {
		return nil, err
	}

	err = removeWithRetry(ctx, target)
	if err != nil {
		log.Infof("Error removing target: %v", err)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *service) ephemeralNodeUnpublish(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest, volName string) (
	*csi.NodeUnpublishVolumeResponse, error) {
	ctx, _, rid := GetRunidLog(ctx)

	//Node Unpublish for Ephemeral Volume
	_, err := s.NodeUnpublishVolume(ctx, &csi.NodeUnpublishVolumeRequest{
		VolumeId:   req.VolumeId,
		TargetPath: req.TargetPath,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Node Unpublish for ephemeral volume failed with error: %v", err))
	}

	//Node Unstage for Ephemeral Volume
	_, err = s.NodeUnstageVolume(ctx, &csi.NodeUnstageVolumeRequest{
		VolumeId:          req.VolumeId,
		StagingTargetPath: path.Join(s.opts.EnvEphemeralStagingTargetPath, volName, "globalmount"),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Node Unstage for ephemeral volume failed with error: %v", err))
	}

	//Controller Unpublish for Ephemeral Volume
	_, err = s.ControllerUnpublishVolume(ctx, &csi.ControllerUnpublishVolumeRequest{
		VolumeId: req.VolumeId,
		NodeId:   s.opts.NodeName + "," + s.opts.LongNodeName,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Controller Unpublish for ephemeral volume failed with error: %v", err))
	}

	//Delete Volume for Ephemeral Volume
	_, err = s.DeleteVolume(ctx, &csi.DeleteVolumeRequest{
		VolumeId: req.VolumeId,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Delete Volume for ephemeral volume failed with error: %v", err))
	}

	err = os.RemoveAll(s.opts.EnvEphemeralStagingTargetPath + volName + "/id")
	if err != nil {
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Unable to clean id file"))
	}
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
	arraysList := s.getStorageArrayList()

	for _, array := range arraysList {
		if array.IsHostAdded {
			atleastOneArraySuccess = true
		}
	}

	if atleastOneArraySuccess {
		s.validateProtocols(ctx, arraysList)
		topology := getTopology()
		// If topology keys are empty then this node is not capable of either iSCSI/FC but can still provision NFS volumes by default
		log.Debugf("Topology Keys--->%+v", topology)

		// Check for node label 'max-unity-volumes-per-node'. If present set 'MaxVolumesPerNode' to this value.
		// If node label is not present, set 'MaxVolumesPerNode' to default value i.e., 0
		var maxUnityVolumesPerNode int64
		labels, err := s.GetNodeLabels(ctx)
		if err != nil {
			return nil, err
		}

		if val, ok := labels[maxUnityVolumesPerNodeLabel]; ok {
			maxUnityVolumesPerNode, err = strconv.ParseInt(val, 10, 64)
			if err != nil {
				return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "invalid value '%s' specified for 'max-unity-volumes-per-node' node label", val))
			}
		} else {
			// As per the csi spec the plugin MUST NOT set negative values to
			// 'MaxVolumesPerNode' in the NodeGetInfoResponse response
			if s.opts.MaxVolumesPerNode < 0 {
				return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "maxUnityVolumesPerNode MUST NOT be set to negative value"))
			}
			maxUnityVolumesPerNode = s.opts.MaxVolumesPerNode
		}

		log.Info("NodeGetInfo success")
		return &csi.NodeGetInfoResponse{
			NodeId: s.opts.NodeName + "," + s.opts.LongNodeName,
			AccessibleTopology: &csi.Topology{
				Segments: topology,
			},
			MaxVolumesPerNode: maxUnityVolumesPerNode,
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
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_VOLUME_CONDITION,
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

	ctx, log, rid := GetRunidLog(ctx)
	log.Debugf("Executing NodeGetVolumeStats with args: %+v", *req)

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volumeId is mandatory parameter"))
	}

	volumeID, protocol, arrayID, unity, err := s.validateAndGetResourceDetails(ctx, req.VolumeId, volumeType)
	if err != nil {
		return nil, err
	}

	ctx, log = setArrayIDContext(ctx, arrayID)
	if err := s.requireProbe(ctx, arrayID); err != nil {
		log.Debug("AutoProbe has not been called. Executing manual probe")
		err = s.nodeProbe(ctx, arrayID)
		if err != nil {
			return nil, err
		}
	}

	volumePath := req.GetVolumePath()
	if volumePath == "" {
		return nil, status.Error(codes.InvalidArgument,
			utils.GetMessageWithRunID(rid, "Volume path required"))
	}

	if protocol == NFS {
		fileAPI := gounity.NewFilesystem(unity)
		_, err := fileAPI.FindFilesystemById(ctx, volumeID)
		if err != nil {
			if err == gounity.FilesystemNotFoundError {
				resp := &csi.NodeGetVolumeStatsResponse{
					VolumeCondition: &csi.VolumeCondition{
						Abnormal: true,
						Message:  "filesystem not found",
					},
				}
				return resp, nil
			}
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "FileSystem not found. [%v]", err))
		}
	} else {
		volumeAPI := gounity.NewVolume(unity)
		_, err := volumeAPI.FindVolumeById(ctx, volumeID)
		if err != nil {
			if err == gounity.VolumeNotFoundError {
				resp := &csi.NodeGetVolumeStatsResponse{
					VolumeCondition: &csi.VolumeCondition{
						Abnormal: true,
						Message:  "volume not found",
					},
				}
				return resp, nil
			}
			return nil, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Volume not found. [%v]", err))
		}
	}

	targetMount, err := getTargetMount(ctx, volumePath)
	if err != nil || targetMount.Device == "" {
		resp := &csi.NodeGetVolumeStatsResponse{
			VolumeCondition: &csi.VolumeCondition{
				Abnormal: true,
				Message:  "volume path not mounted",
			},
		}
		return resp, nil
	}

	// check if volume path is accessible
	_, err = ioutil.ReadDir(volumePath)
	if err != nil {
		resp := &csi.NodeGetVolumeStatsResponse{
			VolumeCondition: &csi.VolumeCondition{
				Abnormal: true,
				Message:  "volume path not accessible",
			},
		}
		return resp, nil
	}

	// get volume metrics for mounted volume path
	availableBytes, totalBytes, usedBytes, totalInodes, freeInodes, usedInodes, err := fs.Info(volumePath)
	if err != nil {
		return nil, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "failed to get metrics for volume with error: %v", err))
	}

	resp := &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: availableBytes,
				Total:     totalBytes,
				Used:      usedBytes,
				Unit:      csi.VolumeUsage_BYTES,
			},
			{
				Available: freeInodes,
				Total:     totalInodes,
				Used:      usedInodes,
				Unit:      csi.VolumeUsage_INODES,
			},
		},
		VolumeCondition: &csi.VolumeCondition{
			Abnormal: false,
			Message:  "",
		},
	}
	return resp, nil
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

	ctx, log = setArrayIDContext(ctx, arrayID)
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
		//No mounts found - Could be raw block device
		volWwn := utils.GetWwnFromVolumeContentWwn(volume.VolumeContent.Wwn)
		deviceNames, _ := gofsutil.GetSysBlockDevicesForVolumeWWN(context.Background(), volWwn)
		if len(deviceNames) > 0 {
			for _, deviceName := range deviceNames {
				devicePath := sysBlock + "/" + deviceName
				log.Infof("Rescanning raw block device %s to expand size", deviceName)
				err = gofsutil.DeviceRescan(context.Background(), devicePath)
				if err != nil {
					log.Errorf("Failed to rescan device (%s) with error (%s)", devicePath, err.Error())
					return nil, status.Error(codes.Internal, err.Error())
				}
			}

			mpathName, err := getMpathDevFromWwn(ctx, volWwn)
			if err != nil {
				return nil, err
			}

			// Resize the corresponding multipath device
			if mpathName != "" {
				err = gofsutil.ResizeMultipath(ctx, mpathName)
				if err != nil {
					return nil, status.Error(codes.Internal,
						utils.GetMessageWithRunID(rid, "Failed to resize multipath device  (%s) with error %v", mpathName, err))
				}
			}

			return &csi.NodeExpandVolumeResponse{}, nil
		}
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

func (s *service) nodeProbe(ctx context.Context, arrayID string) error {
	return s.probe(ctx, "Node", arrayID)
}

//Get NFS Share from Filesystem
func (s *service) getNFSShare(ctx context.Context, filesystemID, arrayID string) (*types.NFSShare, bool, bool, error) {
	ctx, _, rid := GetRunidLog(ctx)
	ctx, _ = setArrayIDContext(ctx, arrayID)

	unity, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return nil, false, false, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Get Unity client for array %s failed. Error: %v ", arrayID, err))
	}

	isSnapshot := false
	fileAPI := gounity.NewFilesystem(unity)
	filesystem, err := fileAPI.FindFilesystemById(ctx, filesystemID)
	var snapResp *types.Snapshot
	if err != nil {
		snapshotAPI := gounity.NewSnapshot(unity)
		snapResp, err = snapshotAPI.FindSnapshotById(ctx, filesystemID)
		if err != nil {
			return nil, false, false, err
		}
		isSnapshot = true
		filesystem, err = s.getFilesystemByResourceID(ctx, snapResp.SnapshotContent.StorageResource.Id, arrayID)
		if err != nil {
			return nil, false, false, err
		}
	}

	var nfsShareID string

	for _, nfsShare := range filesystem.FileContent.NFSShare {
		if isSnapshot {
			if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.Id == filesystemID {
				nfsShareID = nfsShare.Id
			}
		} else {
			if nfsShare.Path == NFSShareLocalPath && nfsShare.ParentSnap.Id == "" {
				nfsShareID = nfsShare.Id
			}
		}
	}

	if nfsShareID == "" {
		return nil, false, false, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "NFS Share for filesystem: %s not found. Error: %v", filesystemID, err))
	}

	nfsShare, err := fileAPI.FindNFSShareById(ctx, nfsShareID)
	if err != nil {
		return nil, false, false, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "NFS Share: %s not found. Error: %v", nfsShareID, err))
	}

	nasServer, err := fileAPI.FindNASServerById(ctx, filesystem.FileContent.NASServer.Id)
	if err != nil {
		return nil, false, false, status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "NAS Server: %s not found. Error: %v", filesystem.FileContent.NASServer.Id, err))
	}

	if !nasServer.NASServerContent.NFSServer.NFSv3Enabled && !nasServer.NASServerContent.NFSServer.NFSv4Enabled {
		return nil, false, false, status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Nas Server: %s does not support NFSv3 and NFSv4. At least one of the versions should be supported", nasServer.NASServerContent.Id))
	}

	return nfsShare, nasServer.NASServerContent.NFSServer.NFSv3Enabled, nasServer.NASServerContent.NFSServer.NFSv4Enabled, nil
}

//Check if the Filesystem has access to the node
func (s *service) checkFilesystemMapping(ctx context.Context, nfsShare *types.NFSShare, am *csi.VolumeCapability_AccessMode, arrayID string) error {
	ctx, _, rid := GetRunidLog(ctx)
	ctx, _ = setArrayIDContext(ctx, arrayID)
	_, err := s.getUnityClient(ctx, arrayID)
	var accessType gounity.AccessType
	if err != nil {
		return err
	}

	host, err := s.getHostID(ctx, arrayID, s.opts.NodeName, s.opts.LongNodeName)
	if err != nil {
		return err
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
		return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Host: %s does not have access: %s on NFS Share: %s", host.HostContent.Name, accessType, nfsShare.NFSShareContent.Id))
	}
	return nil
}

// Check if the volume is published to the node
func (s *service) checkVolumeMapping(ctx context.Context, volume *types.Volume, arrayID string) (int, error) {
	rid, log := utils.GetRunidAndLogger(ctx)

	_, err := s.getUnityClient(ctx, arrayID)
	if err != nil {
		return 0, err
	}

	host, err := s.getHostID(ctx, arrayID, s.opts.NodeName, s.opts.LongNodeName)
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
			log.Debugf(fmt.Sprintf("Volume %s has been published to the current node %s.", volName, host.HostContent.Name))
			return hostaccess.HLU, nil
		}
	}

	return 0, status.Error(codes.Aborted, utils.GetMessageWithRunID(rid, "Volume %s has not been published to this node %s.", volName, host.HostContent.Name))
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

func (s *service) getArrayHostInitiators(ctx context.Context, host *types.Host, arrayID string) ([]string, error) {
	var hostInitiatorWwns []string
	hostContent := host.HostContent
	unity, err := s.getUnityClient(ctx, arrayID)
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
		if utils.IPReachable(ctx, ip, IScsiPort, TCPDialTimeout) {
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
	srcFile, err = os.Open(filepath.Clean(nodeRoot + "/etc/multipath.conf"))
	if err == nil {
		dstFile, err = os.Create("/etc/multipath.conf")
		if err != nil {
			log.Error("Could not open /etc/multipath.conf for writing")
		} else {
			written, _ := io.Copy(dstFile, srcFile)
			log.Debugf("copied %d bytes to /etc/multipath.conf", written)
			err = dstFile.Close()
			if err != nil {
				log.Infof("Error closing file: %v", err)
			}
		}
		err = srcFile.Close()
		if err != nil {
			log.Infof("Error closing file: %v", err)
		}
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
		var err error
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
			err = s.fcConnector.DisconnectVolumeByDeviceName(nodeUnstageCtx, deviceName)
			if err != nil {
				log.Infof("Error disconnecting volume by device name: %v", err)
			}
		} else if protocol == ISCSI {
			err = s.iscsiConnector.DisconnectVolumeByDeviceName(nodeUnstageCtx, deviceName)
			if err != nil {
				log.Infof("Error disconnecting volume by device name: %v", err)
			}
		}

		cancel()
		time.Sleep(disconnectVolumeRetryTime)

		// Check that the /sys/block/DeviceName actually exists
		if _, err := ioutil.ReadDir(sysBlock + deviceName); err != nil {
			// If not, make sure the symlink is removed
			var err2 error
			log.Debugf("Removing device %s", symlinkPath)
			err2 = os.Remove(symlinkPath)
			if err2 != nil {
				log.Infof("Error removing symlinkpath: %v", err2)
			}
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
	ctx, log = setArrayIDContext(ctx, array.ArrayID)
	unity := array.UnityClient
	hostAPI := gounity.NewHost(unity)

	if err := s.requireProbe(ctx, array.ArrayID); err != nil {
		log.Debug("AutoProbe has not been called. Executing manual probe")
		err = s.nodeProbe(ctx, array.ArrayID)
		if err != nil {
			return err
		}
	}

	//Get FC Initiator WWNs
	wwns, errFc := utils.GetFCInitiators(ctx)
	if errFc != nil {
		log.Warn("'FC Initiators' cannot be retrieved.")
	}

	//Get iSCSI Initiator IQN
	iqns, errIscsi := s.iscsiClient.GetInitiators("")
	if errIscsi != nil {
		log.Warn("'iSCSI Initiators' cannot be retrieved.")
	}

	if errFc != nil && errIscsi != nil {
		log.Infof("Node %s does not have FC or iSCSI initiators and can only be used for NFS exports", s.opts.NodeName)
	}

	nodeIps, err := utils.GetHostIP()
	if err != nil {
		return status.Error(codes.Unknown, utils.GetMessageWithRunID(rid, "Unable to get node IP. Error: %v", err))
	}

	fqdnHost := false
	//Find Host on the Array
	host, err := hostAPI.FindHostByName(ctx, s.opts.NodeName)
	if err != nil {
		if err == gounity.HostNotFoundError {
			host, err = hostAPI.FindHostByName(ctx, s.opts.LongNodeName)
			if err == nil {
				fqdnHost = true
			} else {
				var addHostErr error
				if err == gounity.HostNotFoundError {
					addHostErr = s.addNewNodeToArray(ctx, array, nodeIps, iqns, wwns)
				} else {
					return status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Unable to add host. Error: %v", err))
				}
				if addHostErr != nil {
					return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Unable to add host. Error: %v", addHostErr))
				}
			}
		} else {
			return status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Unable to find host. Error: %v", err))
		}
	}
	if err == nil || fqdnHost {
		log.Debugf("Host %s exists on the array", s.opts.NodeName)
		hostContent := host.HostContent
		fqdnHost, addNewInitiators, err := s.checkHostIdempotency(ctx, array, host, iqns, wwns)
		if err != nil {
			return err
		}
		if fqdnHost {
			host, err = hostAPI.FindHostByName(ctx, s.opts.LongNodeName)
			if err != nil {
				if err == gounity.HostNotFoundError {
					addHostErr := s.addNewNodeToArray(ctx, array, nodeIps, iqns, wwns)
					if addHostErr != nil {
						return addHostErr
					}
					addNewInitiators = false
				} else {
					return status.Error(codes.NotFound, utils.GetMessageWithRunID(rid, "Unable to find host. Error: %v", err))
				}
			} else {
				hostContent = host.HostContent
				_, addNewInitiators, err = s.checkHostIdempotency(ctx, array, host, iqns, wwns)
				if err != nil {
					return err
				}
			}
		}
		if addNewInitiators {
			//Modify host operation
			for _, wwn := range wwns {
				log.Debugf("Adding wwn Initiator: %s to host: %s ", hostContent.ID, wwn)
				_, err = hostAPI.CreateHostInitiator(ctx, hostContent.ID, wwn, gounityapi.FCInitiatorType)
				if err != nil {
					return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Adding wwn initiator error: %v", err))
				}
			}
			for _, iqn := range iqns {
				log.Debugf("Adding iSCSI Initiator: %s to host: %s ", hostContent.ID, iqn)
				_, err = hostAPI.CreateHostInitiator(ctx, hostContent.ID, iqn, gounityapi.ISCSCIInitiatorType)
				if err != nil {
					return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Adding iSCSI initiator error: %v", err))
				}
			}
		}
		//Check Ip of the host with Host IP Port
		findHostNamePort := false
		for _, ipPort := range hostContent.IpPorts {
			hostIPPort, err := hostAPI.FindHostIpPortById(ctx, ipPort.Id)
			if err != nil {
				continue
			}
			if hostIPPort != nil && hostIPPort.HostIpContent.Address == s.opts.LongNodeName {
				findHostNamePort = true
				continue
			}
			if hostIPPort != nil {
				for i, nodeIP := range nodeIps {
					if hostIPPort.HostIpContent.Address == nodeIP {
						nodeIps[i] = nodeIps[len(nodeIps)-1]
						nodeIps = nodeIps[:len(nodeIps)-1]
						break
					}
				}
			}
		}
		var ipFormat = regexp.MustCompile(`(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)){3}`)
		if findHostNamePort == false {
			//Create Host Ip Port
			_, err = hostAPI.CreateHostIpPort(ctx, hostContent.ID, s.opts.LongNodeName)
			if err != nil {
				return err
			}
		}
		for _, nodeIP := range nodeIps {
			_, err = hostAPI.CreateHostIpPort(ctx, hostContent.ID, nodeIP)
			if err != nil && !ipFormat.MatchString(s.opts.NodeName) {
				return err
			}
		}
	}

	if len(iqns) > 0 {
		err = s.copyMultipathConfigFile(ctx, s.opts.Chroot)
		if err != nil {
			log.Infof("Error copying multipath config file: %v", err)
		}
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

//Host idempotency check
func (s *service) checkHostIdempotency(ctx context.Context, array *StorageArrayConfig, host *types.Host, iqns, wwns []string) (bool, bool, error) {
	ctx, log, rid := GetRunidLog(ctx)
	hostContent := host.HostContent
	arrayHostWwns, err := s.getArrayHostInitiators(ctx, host, array.ArrayID)
	if err != nil {
		return false, false, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Error while finding initiators for host %s on the array: %s error: %v", hostContent.ID, array, err))
	}

	//Check if all elements of wwns is present inside arrayHostWwns
	if utils.ArrayContainsAll(append(wwns, iqns...), arrayHostWwns) && len(append(wwns, iqns...)) == len(arrayHostWwns) {
		log.Info("Node initiators are synchronized with the Host Wwns on the array")
		return false, true, nil
	}
	extraWwns := utils.FindAdditionalWwns(append(wwns, iqns...), arrayHostWwns)
	if len(extraWwns) > 0 {
		if host.HostContent.Name == s.opts.LongNodeName {
			return false, false, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Host has got foreign Initiators. Host initiators on the array require correction before proceeding further."))
		}
		return true, false, nil
	}
	return false, true, nil
}

//Adding a new node to array
func (s *service) addNewNodeToArray(ctx context.Context, array *StorageArrayConfig, nodeIps, iqns, wwns []string) error {
	ctx, log, rid := GetRunidLog(ctx)
	ctx, log = setArrayIDContext(ctx, array.ArrayID)
	unity := array.UnityClient

	// Variable which will be comsumed by hostApi.CreateHost once gounity code change
	tenantName := s.opts.TenantName
	var tenantID string

	//Create Host

	hostAPI := gounity.NewHost(unity)
	tenants, err:= hostAPI.FindTenants(ctx)
	for eachtenant := range tenants.Entries {
        if tenants.Entries[eachtenant].Content.Name == s.opts.TenantName{
            tenantID := tenants.Entries[eachtenant].Content.Id
            log.Debugf("tenant details: %s ", tenants.Entries[eachtenant].Content.Name)
        }
	}
	host, err := hostAPI.CreateHost(ctx, s.opts.LongNodeName, tenantID)

	if err != nil {
		return err
	}
	hostContent := host.HostContent
	log.Debugf("New Host Id: %s", hostContent.ID)

	//Create Host Ip Port
	_, err = hostAPI.CreateHostIpPort(ctx, hostContent.ID, s.opts.LongNodeName)
	if err != nil {
		return err
	}
	var ipFormat = regexp.MustCompile(`(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)){3}`)
	for _, nodeIP := range nodeIps {
		_, err = hostAPI.CreateHostIpPort(ctx, hostContent.ID, nodeIP)
		if err != nil && !ipFormat.MatchString(s.opts.NodeName) {
			return err
		}
	}

	if len(wwns) > 0 {
		//Create Host FC Initiators
		log.Debugf("FC Initiators found: %s", wwns)
		for _, wwn := range wwns {
			log.Debugf("Adding wwn Initiator: %s to host: %s ", hostContent.ID, wwn)
			_, err = hostAPI.CreateHostInitiator(ctx, hostContent.ID, wwn, gounityapi.FCInitiatorType)
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
			_, err = hostAPI.CreateHostInitiator(ctx, hostContent.ID, iqn, gounityapi.ISCSCIInitiatorType)
			if err != nil {
				return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Adding iSCSI initiator error: %v", err))
			}
		}
	}
	return nil
}

func (s *service) syncNodeInfoRoutine(ctx context.Context) {
	ctx, log := setRunIDContext(ctx, "node-0")
	log.Info("Starting goroutine to add Node information to storage array")
	for {
		select {
		case <-syncNodeInfoChan:
			log.Debug("Config change identified. Adding node info")
			s.syncNodeInfo(ctx)
			ctx, log = incrementLogID(ctx, "node")
		case <-time.After(time.Duration(s.opts.SyncNodeInfoTimeInterval) * time.Minute):
			log.Debug("Checking if host information is added to array")
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
				log.Debug("Some of the hosts are not added, invoking add host information to array")
				s.syncNodeInfo(ctx)
				ctx, log = incrementLogID(ctx, "node")
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

	ctx, log := incrementLogID(ctx, "node")
	log.Debug("Synchronizing Node Info")
	// Add a count of three, one for each goroutine.
	s.arrays.Range(func(key, value interface{}) bool {
		array := value.(*StorageArrayConfig)
		if !array.IsHostAdded {
			go func() {
				ctx, log := incrementLogID(ctx, "node")
				err := s.addNodeInformationIntoArray(ctx, array)
				if err == nil {
					array.IsHostAdded = true
					log.Debugf("Node [%s] Added successfully", array.ArrayID)
				} else {
					log.Debugf("Adding node [%s] failed, Error: [%v]", array.ArrayID, err)
				}
			}()
		}
		return true
	})
}

func getTopology() map[string]string {
	// Create the topology keys
	// csi-unity.dellemc.com/<arrayID>/<protocol>: true
	topology := map[string]string{}

	for _, sysID := range connectedSystemID {
		// In connected system ID we will get slice in this format [arrayID/protcol]
		tokens := strings.Split(sysID, "/")
		arrayID := tokens[0]
		protocol := tokens[1]
		// whatever array and protocol present in connected systems is already validated hence it is set to true
		topology[Name+"/"+arrayID+"-"+protocol] = "true"
	}
	return topology
}

// validateProtocols will check for iSCSI and FC connectivity and updates same in connectedSystemID list
func (s *service) validateProtocols(ctx context.Context, arraysList []*StorageArrayConfig) {
	ctx, log, _ := GetRunidLog(ctx)
	for _, array := range arraysList {
		if array.IsHostAdded {
			iscsiInitiators, err := s.iscsiClient.GetInitiators("")
			fcInitiators, err := utils.GetFCInitiators(ctx)
			// we will enable NFS by default
			connectedSystemID = append(connectedSystemID, array.ArrayID+"/"+strings.ToLower(NFS))

			if len(iscsiInitiators) != 0 || len(fcInitiators) != 0 {
				log.Info("iSCSI/FC package found in this node proceeding to further validations")
				//To get all iSCSI initiators and FC Initiators
				ctx, _ = setArrayIDContext(ctx, array.ArrayID)
				unity, err := s.getUnityClient(ctx, array.ArrayID)

				if err != nil {
					log.Infof("Unable to get unity client for topology validation: %v", err)
				}

				hostAPI := gounity.NewHost(unity)
				host, err := s.getHostID(ctx, array.ArrayID, s.opts.NodeName, s.opts.LongNodeName)
				if err != nil {
					log.Infof("Host not found. Error: %v", err)
				}
				if host != nil && len(host.HostContent.FcInitiators) != 0 {
					log.Infof("Got FC Initiators, Checking health of initiators:%s", host.HostContent.FcInitiators)
					for _, initiator := range host.HostContent.FcInitiators {
						initiatorID := initiator.Id
						hostInitiator, err := hostAPI.FindHostInitiatorById(ctx, initiatorID)
						if err != nil {
							log.Infof("Unable to get initiators: %s", err)
						}
						if hostInitiator != nil {
							healtContent := hostInitiator.HostInitiatorContent.Health
							if healtContent.DescriptionIDs[0] == componentOkMessage {
								log.Infof("FC Health is good for array:%s, Health:%s", array.ArrayID, healtContent.DescriptionIDs[0])
								connectedSystemID = append(connectedSystemID, array.ArrayID+"/"+strings.ToLower(FC))
							} else {
								log.Infof("FC Health is bad for array:%s, Health:%s", array.ArrayID, healtContent.DescriptionIDs[0])
							}
						}
					}
				}
				if host != nil && len(host.HostContent.IscsiInitiators) != 0 {
					log.Infof("Got iSCSI Initiators, Checking health of initiators:%s", host.HostContent.IscsiInitiators)
					for _, initiator := range host.HostContent.IscsiInitiators {
						initiatorID := initiator.Id
						hostInitiator, err := hostAPI.FindHostInitiatorById(ctx, initiatorID)
						if err != nil {
							log.Infof("Unable to get initiators: %s", err)
						}
						if hostInitiator != nil {
							healtContent := hostInitiator.HostInitiatorContent.Health
							if healtContent.DescriptionIDs[0] == componentOkMessage {
								log.Infof("iSCSI health is good for array:%s, Health:%s", array.ArrayID, healtContent.DescriptionIDs[0])
								connectedSystemID = append(connectedSystemID, array.ArrayID+"/"+strings.ToLower(ISCSI))
							} else {
								log.Infof("iSCSI Health is bad for array:%s, Health:%s", array.ArrayID, healtContent.DescriptionIDs[0])
							}
						}
					}
				}
			} else {
				log.Info("this node doesn't support either iSCSI or FC protocol, only NFS is supported", err)
			}
		}
	}
}
