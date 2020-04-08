package service

import (
	"context"
	"fmt"
	"github.com/dell/csi-unity/service/utils"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/gofsutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Device is a struct for holding details about a block device
type Device struct {
	FullPath string
	Name     string
	RealDev  string
}

func publishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest, privDir, symlinkPath string) error {
	rid, log := utils.GetRunidAndLogger(ctx)
	id := req.GetVolumeId()
	target := req.GetTargetPath()
	if target == "" {
		return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "target path required"))
	}

	ro := req.GetReadonly()

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume capability required"))
	}

	accMode := volCap.GetAccessMode()
	if accMode == nil {
		return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume access mode required"))
	}

	// make sure device is valid
	sysDevice, err := GetDevice(ctx, symlinkPath)
	if err != nil {
		return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "error getting block device for volume: %s, err: %s", id, err.Error()))
	}

	// make sure target is created
	tgtStat, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			//Create target directory if it doesnt exist
			_, err = mkdir(ctx, target)
			if err != nil {
				return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Could not create target %s : %s", target, err.Error()))
			}
		}
		return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "failed to stat target, err: %s", err.Error()))
	}

	// make sure privDir exists and is a directory
	if _, err := mkdir(ctx, privDir); err != nil {
		log.Errorf("Could not create target %s: %v", privDir, err)
		return err
	}

	typeSet := false
	if blockVol := volCap.GetBlock(); blockVol != nil {
		// BlockVolume is not supported
		return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Block Volume not supported"))
	}
	mntVol := volCap.GetMount()
	if mntVol != nil {
		if ro {
			return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "read only not supported for Mount Volume"))
		}
		typeSet = true
	} else {
		return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "Volume capability - Mount param has not been provided"))
	}

	if !typeSet {
		return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "volume access type required"))
	}

	// check that the target is right type for vol type
	if !tgtStat.IsDir() {
		return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "target: %s wrong type (file) Access Type", target))
	}

	// Path to mount device to (private mount)
	privTgt := getPrivateMountPoint(privDir, id)

	// Check if device is already mounted
	devMnts, err := getDevMounts(ctx, sysDevice)
	if err != nil {
		return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "could not reliably determine existing mount status: %s", err.Error()))
	}

	alreadyMounted := false
	if len(devMnts) == 0 {
		// Device isn't mounted anywhere, do the private mount
		// Make sure private mount point exists
		var created bool

		created, err = mkdir(ctx, privTgt)

		if err != nil {
			return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Unable to create private mount point: %s", err.Error()))
		}
		if !created {
			// The place where our device is supposed to be mounted
			// already exists, but we also know that our device is not mounted anywhere
			// Either something didn't clean up correctly, or something else is mounted
			// If the private mount is not in use, it's okay to re-use it. But make sure
			// it's not in use first

			mnts, err := gofsutil.GetMounts(ctx)
			if err != nil {
				return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "could not reliably determine existing mount status: %s", err.Error()))
			}
			if len(mnts) == 0 {
				return status.Error(codes.Unavailable, utils.GetMessageWithRunID(rid, "volume %s not published to node", id))
			}
			for _, m := range mnts {
				if m.Path == privTgt {
					resolvedMountDevice := evalSymlinks(ctx, m.Device)
					if resolvedMountDevice != sysDevice.RealDev {
						return status.Errorf(codes.FailedPrecondition, "Private mount point: %s mounted by different device: %s", privTgt, resolvedMountDevice)
					}
					alreadyMounted = true
				}
			}
		}

		if !alreadyMounted {
			fs := mntVol.GetFsType()
			mntFlags := mntVol.GetMountFlags()

			if fs != "ext3" && fs != "ext4" && fs != "xfs" {
				return status.Error(codes.Unavailable, utils.GetMessageWithRunID(rid, "Fs type %s not supported", fs))
			}

			if err := handlePrivFSMount(ctx, accMode, mntFlags, sysDevice, fs, privTgt); err != nil {
				// K8S may have removed the desired mount point. Clean up the private target.
				log.Debugf("Private mount failed: %v", err)
				cleanupPrivateTarget(ctx, privTgt)
				return err
			}
		} else {
			log.Debug("Private mount is already in place")
		}
	} else {
		// Device is already mounted. Need to ensure that it is already
		// mounted to the expected private mount, with correct rw/ro permissions
		mounted := false

		log.Printf("Devmounts: %#v", devMnts)
		for _, m := range devMnts {
			if m.Path == target {
				log.Debugf("mount %#v already mounted to requested target %s", m, target)
				mounted = true
			}
			if m.Path == privTgt {
				mounted = true
				rwo := "rw"
				if ro {
					rwo = "ro"
				}
				if utils.ArrayContains(m.Opts, rwo) {
					log.Debug("private mount already in place")
					break
				} else {
					return status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "access mode conflicts with existing mounts"))
				}
			}
		}
		if !mounted {
			return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "device already in use and mounted elsewhere. Cannot do private mount"))
		}
	}

	// Private mount in place, now bind mount to target path
	// Check if target is not mounted
	devMnts, err = getPathMounts(ctx, target)
	if err != nil {
		return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "could not reliably determine existing mount status: %s", err.Error()))
	}

	// If mounts already existed for this device, check if mount to
	// target path was already there
	if len(devMnts) > 0 {
		for _, m := range devMnts {
			if m.Path == target {
				// volume already published to target
				// if mount options look good, do nothing
				rwo := "rw"
				if accMode.GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY {
					rwo = "ro"
				}
				if !utils.ArrayContains(m.Opts, rwo) {
					return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "volume previously published with different options"))

				}
				// Existing mount satisfies request
				log.Info("volume already published to target")
				return nil
			} else if m.Path == privTgt {
				continue
			} else {
				//Device has been mounted aleady to another target
				return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "device already in use and mounted elsewhere"))
			}
		}

	}

	// Make sure target is created. The spec says the driver is responsible
	// for creating the target, but Kubernetes generally creates the target.
	_, err = mkdir(ctx, target)
	if err != nil {
		// Unmount and remove the private directory for the retry so clean start next time.
		// K8S probably removed part of the path.
		cleanupPrivateTarget(ctx, privTgt)
		return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Could not create %s: %s", target, err.Error()))
	}

	var mntFlags []string
	mntFlags = mntVol.GetMountFlags()
	if accMode.GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY {
		mntFlags = append(mntFlags, "ro")
	}

	if err := gofsutil.BindMount(ctx, privTgt, target, mntFlags...); err != nil {
		// Unmount and remove the private directory for the retry so clean start next time.
		// K8S probably removed part of the path.
		cleanupPrivateTarget(ctx, privTgt)
		return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "error publish volume to target path: %s", err.Error()))
	}

	log.Infof("Volume %s has been mounted successfully to the target path %s", id, target)
	return nil
}

// unpublishVolume removes the bind mount to the target path, and also removes
// the mount to the private mount directory if the volume is no longer in use.
// It determines this by checking to see if the volume is mounted anywhere else
// other than the private mount.
// The req.TargetPath should be a path starting with "/"
func unpublishVolume(ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest,
	privDir, devicePath string) (bool, error) {
	rid, log := utils.GetRunidAndLogger(ctx)
	lastUnmounted := false
	id := req.GetVolumeId()

	target := req.GetTargetPath()
	if target == "" {
		return lastUnmounted, status.Error(codes.InvalidArgument, utils.GetMessageWithRunID(rid, "target path required"))
	}
	// make sure device is valid
	sysDevice, err := GetDevice(ctx, devicePath)
	if err != nil {
		// This error needs to be idempotent since device was not found
		return true, nil
	}
	//  Get the private mount path
	privTgt := getPrivateMountPoint(privDir, id)

	//Get existing mounts
	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return lastUnmounted, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "could not reliably determine existing mount status: %s", err.Error()))
	}

	tgtMnt := false
	privMnt := false
	log.Debug("UnpublishVolume: mnts:", len(mnts))
	log.Debug("SysDevice:", sysDevice.Name, sysDevice.FullPath, sysDevice.RealDev)
	log.Debug("PrivateDir:", privDir)
	log.Debug("PrivateTarget:", privTgt)
	log.Debug("Target", target)
	log.Debug("DevicePath", devicePath)
	for _, m := range mnts {
		if m.Source == sysDevice.FullPath || m.Device == sysDevice.RealDev || m.Path == target || m.Path == privTgt || m.Device == sysDevice.FullPath {
			if m.Path == privTgt {
				privMnt = true
			} else if m.Path == target {
				tgtMnt = true
			}
		}
	}

	if tgtMnt {
		log.Debugf("Unmounting target %s", target)
		if err := gofsutil.Unmount(ctx, target); err != nil {
			return lastUnmounted, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Error unmounting target: %s", err.Error()))
		}
		log.Debugf("Device %s unmounted from target path %s successfully", sysDevice.Name, target)
	} else {
		log.Debugf("Device %s has not been mounted to target path %s. Skipping unmount", sysDevice.Name, target)
	}

	if privMnt {
		log.Debugf("Unmounting sysDevice %v privTgt  %s", sysDevice, privTgt)
		if lastUnmounted, err = unmountPrivMount(ctx, sysDevice, privTgt); err != nil {
			return lastUnmounted, status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Error unmounting private mount: %s", err.Error()))
		}
		log.Infof("Device %s unmounted from private mount path %s successfully", sysDevice.Name, privTgt)
	} else {
		mnts, err := getDevMounts(ctx, sysDevice)
		if err == nil && len(mnts) == 0 {
			log.Infof("No private mount or remaining mounts device: %s", sysDevice.Name)
			lastUnmounted = true
		}
	}

	return lastUnmounted, nil
}

func getDevMounts(ctx context.Context, sysDevice *Device) ([]gofsutil.Info, error) {
	log := utils.GetRunidLogger(ctx)
	devMnts := make([]gofsutil.Info, 0)
	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return devMnts, err
	}
	for _, m := range mnts {
		if m.Device == sysDevice.RealDev || m.Device == sysDevice.FullPath || (m.Device == "devtmpfs" && m.Source == sysDevice.RealDev) {
			devMnts = append(devMnts, m)
		} else {
			//Find the multipath device mapper from the device obtained
			mpDevName := strings.TrimPrefix(sysDevice.RealDev, "/dev/")
			filename := fmt.Sprintf("/sys/devices/virtual/block/%s/dm/name", mpDevName)
			if name, err := ioutil.ReadFile(filename); err != nil {
				log.Error("Could not read mp dev name file ", filename, err)
			} else {
				mpathDev := strings.TrimPrefix(strings.TrimSpace(string(name)), "3")
				mapperDevice := fmt.Sprintf("/dev/mapper/%s", mpathDev)
				if m.Source == mapperDevice || m.Device == mapperDevice || m.Path == mapperDevice {
					devMnts = append(devMnts, m)
				}
			}
		}
	}
	return devMnts, nil
}

func getPrivateMountPoint(privDir string, name string) string {
	return filepath.Join(privDir, name)
}

// mkdir creates the directory specified by path if needed.
// return pair is a bool flag of whether dir was created, and an error
func mkdir(ctx context.Context, path string) (bool, error) {
	log := utils.GetRunidLogger(ctx)
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		if err := os.Mkdir(path, 0755); err != nil {
			log.WithField("dir", path).WithError(err).Error("Unable to create dir")
			return false, err
		}
		log.WithField("path", path).Debug("created directory")
		return true, nil
	}
	if !st.IsDir() {
		return false, fmt.Errorf("existing path is not a directory")
	}
	return false, nil
}

func handlePrivFSMount(ctx context.Context,
	accMode *csi.VolumeCapability_AccessMode,
	mntFlags []string, sysDevice *Device, fs, privTgt string) error {
	rid, _ := utils.GetRunidAndLogger(ctx)
	if err := gofsutil.FormatAndMount(ctx, sysDevice.FullPath, privTgt, fs, mntFlags...); err != nil {
		return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "error performing private mount: %s", err.Error()))
	}
	return nil
}

// GetDevice returns a Device struct with info about the given device, or
// an error if it doesn't exist or is not a block device
func GetDevice(ctx context.Context, path string) (*Device, error) {
	log := utils.GetRunidLogger(ctx)
	fi, err := os.Lstat(path)
	if err != nil {
		log.Errorf("Could not lstat path: %s ", path)
		return nil, err
	}

	// eval any symlinks and make sure it points to a device
	d, err := filepath.EvalSymlinks(path)
	if err != nil {
		log.Error("Could not evaluate symlinks path: " + path)
		return nil, err
	}

	ds, _ := os.Stat(d)
	dm := ds.Mode()

	if dm&os.ModeDevice == 0 {
		return nil, fmt.Errorf("%s is not a block device", path)
	}

	dev := &Device{
		Name:     fi.Name(),
		FullPath: path,
		RealDev:  strings.Replace(d, "\\", "/", -1),
	}
	log.Debugf("Device: %#v", dev)
	return dev, nil
}

func unmountPrivMount(
	ctx context.Context,
	dev *Device,
	target string) (bool, error) {
	log := utils.GetRunidLogger(ctx)
	lastUnmounted := false

	mnts, err := getDevMounts(ctx, dev)
	if err != nil {
		return lastUnmounted, err
	}

	// Handle no private mount (which is odd because we had one to call here)
	// It implies deleting the target mount also cleaned up the private mount
	if len(mnts) == 0 {
		log.Info("No private mounts for device: " + dev.RealDev)
		err := removeWithRetry(ctx, target)
		if err != nil {
			log.Errorf("Error removing private mount target: %s Error:%v", target, err)
		}
		return true, err
	}

	// remove private mount if we can (if there are no other mounts
	if len(mnts) == 1 && mnts[0].Path == target {
		if err := gofsutil.Unmount(ctx, target); err != nil {
			return lastUnmounted, err
		}
		lastUnmounted = true
		log.WithField("directory", target).Debug("removing directory")
		err := removeWithRetry(ctx, target)
		if err != nil {
			log.Errorf("Error removing private mount target: %s Error:%v", target, err)
		}
	} else {
		for _, m := range mnts {
			log.Debugf("Remaining dev mount: %#v", m)
		}
	}

	return lastUnmounted, nil
}

// nodeDeleteBlockDevices deletes the block devices associated with a volume WWN (including multipath) on that node.
// This should be done when the volume will no longer be used on the node.
func (s *service) nodeDeleteBlockDevices(ctx context.Context, wwn string, target string) error {
	rid, log := utils.GetRunidAndLogger(ctx)
	var err error
	wwn = strings.ReplaceAll(wwn, ":", "")
	wwn = strings.ToLower(wwn)
	for i := 0; i < maxBlockDevicesPerWWN; i++ {
		// Wait for the next device to show up
		symlinkPath, devicePath, _ := gofsutil.WWNToDevicePathX(ctx, wwn)
		if devicePath == "" {
			// All done, no more paths for WWN
			log.Debug("Multipath devices flushed, no more dm paths for WWN: ", wwn)
			break
		}

		log.WithField("WWN", wwn).WithField("SymlinkPath", symlinkPath).WithField("DevicePath", devicePath).Info("Removing block device")

		isMultipath := strings.HasPrefix(symlinkPath, gofsutil.MultipathDevDiskByIDPrefix)
		if isMultipath {
			var textBytes []byte

			// Attempt to flush the devicePath
			multipathMutex.Lock()
			textBytes, err = gofsutil.MultipathCommand(ctx, 2, "", "-f", devicePath)
			multipathMutex.Unlock()

			if textBytes != nil && len(textBytes) > 0 {
				log.Infof("multipath -f %s: %s", devicePath, string(textBytes))
			}
			if err != nil {
				log.Infof("Multipath flush error: %s: %s", devicePath, err.Error())
				time.Sleep(nodePublishSleepTime)
				//Attempting to flush via chroot
				multipathMutex.Lock()
				log.Debugf("Attempting to flush via chroot: %s", devicePath)
				textBytes, err = gofsutil.MultipathCommand(ctx, 2, s.opts.Chroot, "-f", devicePath)
				multipathMutex.Unlock()
				if err != nil {
					log.Infof("Multipath flush error: %s: %s", devicePath, err.Error())
				} else {
					log.Infof("Multipath flush success: %s", devicePath)
				}
			} else {
				log.Infof("Multipath flush success: %s", devicePath)
				time.Sleep(multipathSleepTime)
			}
		}
	}

	for i := 0; i < maxBlockDevicesPerWWN; i++ {

		// Wait for the next device to show up
		symlinkPath, devicePath, _ := gofsutil.WWNToDevicePathX(ctx, wwn)
		if devicePath == "" {
			// All done, no more paths for WWN
			log.Debug("All done, no more paths for WWN: ", wwn)
			break
		}

		log.WithField("WWN", wwn).WithField("SymlinkPath", symlinkPath).WithField("DevicePath", devicePath).Info("Removing block device")

		isMultipath := strings.HasPrefix(symlinkPath, gofsutil.MultipathDevDiskByIDPrefix)
		if isMultipath {
			log.Info("Skipping delete dm device since multipath flush failed for: ", devicePath)
			continue
		}

		// RemoveBlockDevice is called in a goroutine in case it takes an extended period. It will return error status in channel errorChan.
		// After that time we check to see if we have received a response from the goroutine.
		// If there was no response, we return an error that we timed out.
		// Otherwise, we wait any additional necessary to have slept for the removeDeviceSleepTime.
		deviceDeleteMutex.Lock()
		startTime := time.Now()
		endTime := startTime.Add(removeDeviceSleepTime)
		errorChan := make(chan error, 1)
		go func(devicePath string, errorChan chan error) {
			err := gofsutil.RemoveBlockDevice(ctx, devicePath)
			errorChan <- err
		}(devicePath, errorChan)

		done := false
		timeout := time.After(deviceDeletionTimeout)
		curTime := time.Now()

		for {
			breakLoop := false
			select {
			case err = <-errorChan:
				done = true
				log.Debugf("device delete took %v", curTime.Sub(startTime))
				if err != nil {
					log.Debugf("Remove block device failed with error: %s: %v", devicePath, err)
				} else {
					log.Debugf("Remove block device successful for: %s", devicePath)
					breakLoop = true
					break
				}
			case <-timeout:
				log.Debugf("Routine remove block device timed out")
				breakLoop = true
				break
			default:
				time.Sleep(deviceDeletionPoll)
			}

			if breakLoop {
				break
			}
		}
		close(errorChan)
		deviceDeleteMutex.Unlock()
		if !done {
			//log.Debugf("Removing block %s device timed out after %v", devicePath, deviceDeletionTimeout)
			return status.Error(codes.Internal, utils.GetMessageWithRunID(rid, "Removing block device timed out after %v", deviceDeletionTimeout))
		}

		//Wait untill remainingTime so that delete device can refresh new stats
		remainingTime := endTime.Sub(time.Now())
		if remainingTime > (100 * time.Millisecond) {
			time.Sleep(remainingTime)
		}
	}

	if err != nil {
		return err
	}
	// Do a linear scan.
	return linearScanToRemoveDevices(ctx, wwn)
}

// linearScanToRemoveDevices does a linear scan through /sys/block for devices matching a volumeWWN, and attempts to remove them if any.
func linearScanToRemoveDevices(ctx context.Context, volumeWWN string) error {
	log := utils.GetRunidLogger(ctx)
	devs, _ := gofsutil.GetSysBlockDevicesForVolumeWWN(ctx, volumeWWN)
	if len(devs) == 0 {
		return nil
	}
	log.Debugf("volume devices wwn %s still to be deleted: %s", volumeWWN, devs)
	for _, dev := range devs {
		devicePath := "/dev/" + dev
		err := gofsutil.RemoveBlockDevice(ctx, devicePath)
		if err != nil {
			log.Debugf("Remove block device %s failed with error: %v", devicePath, err)
		}
	}
	time.Sleep(removeDeviceSleepTime)
	devs, _ = gofsutil.GetSysBlockDevicesForVolumeWWN(ctx, volumeWWN)
	if len(devs) > 0 {
		return status.Error(codes.Internal, fmt.Sprintf("volume WWN %s had %d block devices that weren't successfully deleted: %s", volumeWWN, len(devs), devs))
	}
	return nil
}

// removeWithRetry removes directory, if it exists, with retry.
func removeWithRetry(ctx context.Context, target string) error {
	log := utils.GetRunidLogger(ctx)
	var err error
	for i := 0; i < 3; i++ {
		log.Debug("Removing target:", target)
		err = os.Remove(target)
		if err != nil && !os.IsNotExist(err) {
			log.Error("error removing private mount target: " + err.Error())
			cmd := exec.Command("/usr/bin/rmdir", target)
			textBytes, err := cmd.CombinedOutput()
			if err != nil {
				log.Error("error calling rmdir: " + err.Error())
			} else {
				log.Debugf("rmdir output: %s", string(textBytes))
			}
			time.Sleep(3 * time.Second)
		} else {
			log.Debug("Target removed:", target)
			break
		}
	}
	return err
}

//Not required - delete later
// cleanupPrivateTarget unmounts and removes the private directory for the retry so clean start next time.
func cleanupPrivateTarget(ctx context.Context, privTgt string) {
	log := utils.GetRunidLogger(ctx)
	log.Debugf("Cleaning up private target %s", privTgt)
	if privErr := gofsutil.Unmount(ctx, privTgt); privErr != nil {
		log.Debugf("Error unmounting privTgt %s: %v", privTgt, privErr)
	}
	if privErr := removeWithRetry(ctx, privTgt); privErr != nil {
		log.Debugf("Error removing privTgt %s: %v", privTgt, privErr)
	}
}

// Evaulate symlinks to a resolution. In case of an error,
// logs the error but returns the original path.
func evalSymlinks(ctx context.Context, path string) string {
	log := utils.GetRunidLogger(ctx)
	// eval any symlinks and make sure it points to a device
	d, err := filepath.EvalSymlinks(path)
	if err != nil {
		log.Error("Could not evaluate symlinks for path: " + path)
		return path
	}
	return d
}

// getPathMounts finds all the mounts for a given path.
func getPathMounts(ctx context.Context, path string) ([]gofsutil.Info, error) {
	devMnts := make([]gofsutil.Info, 0)
	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return devMnts, err
	}
	for _, m := range mnts {
		if m.Path == path {
			devMnts = append(devMnts, m)
		}
	}
	return devMnts, nil
}
