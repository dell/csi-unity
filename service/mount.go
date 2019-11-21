package service

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/gofsutil"
	csictx "github.com/rexray/gocsi/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	rescanHostSleepTime        = 1 * time.Second
	MultipathDevDiskByIDPrefix = "/dev/disk/by-id/dm-uuid-mpath-3"
)

// Device is a struct for holding details about a block device
type Device struct {
	FullPath string
	Name     string
	RealDev  string
}

func publishVolume(req *csi.NodePublishVolumeRequest, privDir, symlinkPath string) error {
	id := req.GetVolumeId()
	target := req.GetTargetPath()
	if target == "" {
		return status.Error(codes.InvalidArgument, "target path required")
	}

	ro := req.GetReadonly()

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return status.Error(codes.InvalidArgument, "volume capability required")
	}

	accMode := volCap.GetAccessMode()
	if accMode == nil {
		return status.Error(codes.InvalidArgument, "volume access mode required")
	}

	// make sure device is valid
	sysDevice, err := GetDevice(symlinkPath)
	if err != nil {
		return status.Errorf(codes.Internal, "error getting block device for volume: %s, err: %s", id, err.Error())
	}

	// make sure target is created
	tgtStat, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return status.Errorf(codes.FailedPrecondition, "publish target: %s not pre-created", target)
		}
		return status.Errorf(codes.Internal, "failed to stat target, err: %s", err.Error())
	}

	// make sure privDir exists and is a directory
	if _, err := mkdir(privDir); err != nil {
		return err
	}

	typeSet := false
	if blockVol := volCap.GetBlock(); blockVol != nil {
		// BlockVolume is not supported
		return status.Error(codes.InvalidArgument, "Block Volume not supported")
	}
	mntVol := volCap.GetMount()
	if mntVol != nil {
		if ro {
			return status.Error(codes.InvalidArgument, "read only not supported for Mount Volume")
		}
		typeSet = true
	} else {
		return status.Error(codes.InvalidArgument, "Volume capability - Mount param has not been provided")
	}

	if !typeSet {
		return status.Error(codes.InvalidArgument, "volume access type required")
	}

	// check that the target is right type for vol type
	if !tgtStat.IsDir() {
		return status.Errorf(codes.FailedPrecondition, "target: %s wrong type (file) Access Type", target)
	}

	// Path to mount device to (private mount)
	privTgt := getPrivateMountPoint(privDir, id)

	// Check if device is already mounted
	devMnts, err := getDevMounts(sysDevice)
	if err != nil {
		return status.Errorf(codes.Internal, "could not reliably determine existing mount status: %s", err.Error())
	}

	ctx := context.Background()
	if len(devMnts) == 0 {
		// Device isn't mounted anywhere, do the private mount
		// Make sure private mount point exists
		var created bool

		created, err = mkdir(privTgt)

		if err != nil {
			return status.Errorf(codes.Internal, "Unable to create private mount point: %s", err.Error())
		}
		if !created {
			// The place where our device is supposed to be mounted
			// already exists, but we also know that our device is not mounted anywhere
			// Either something didn't clean up correctly, or something else is mounted
			// If the private mount is not in use, it's okay to re-use it. But make sure
			// it's not in use first

			mnts, err := gofsutil.GetMounts(ctx)
			if err != nil {
				return status.Errorf(codes.Internal, "could not reliably determine existing mount status: %s", err.Error())
			}
			if len(mnts) == 0 {
				return status.Errorf(codes.Unavailable, "volume %s not published to node", id)
			}
			for _, m := range mnts {
				if m.Path == privTgt {
					return status.Error(codes.Internal, "Unable to use private mount point")
				}
			}
		}

		fs := mntVol.GetFsType()
		mntFlags := mntVol.GetMountFlags()

		if fs != "ext3" && fs != "ext4" && fs != "xfs" {
			return status.Errorf(codes.Unavailable, "Fs type %s not supported", fs)
		}

		//Private mount - mount the device to private mount target
		if err := handlePrivFSMount(accMode, mntFlags, sysDevice, fs, privTgt); err != nil {
			return err
		}

	} else {
		// Device is already mounted. Need to ensure that it is already
		// mounted to the expected private mount, with correct rw/ro perms
		mounted := false
		for _, m := range devMnts {
			if m.Path == privTgt {
				mounted = true
				rwo := "rw"
				if ro {
					rwo = "ro"
				}
				if contains(m.Opts, rwo) {
					log.Debug("private mount already in place")
					break
				} else {
					return status.Error(codes.InvalidArgument, "access mode conflicts with existing mounts")
				}
			}
		}
		if !mounted {
			return status.Error(codes.Internal, "device already in use and mounted elsewhere. Cannot do private mount")
		}
	}

	// Private mount in place, now bind mount to target path
	devMnts, err = getDevMounts(sysDevice)
	if err != nil {
		return status.Errorf(codes.Internal, "could not reliably determine existing mount status: %s", err.Error())
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
				if !contains(m.Opts, rwo) {
					return status.Error(codes.Internal, "volume previously published with different options")

				}
				// Existing mount satisfies request
				log.Info("volume already published to target")
				return nil
			} else if m.Path == privTgt {
				continue
			} else {
				//Device has been mounted aleady to another target
				return status.Error(codes.Internal, "device already in use and mounted elsewhere")
			}
		}

	}

	var mntFlags []string
	mntFlags = mntVol.GetMountFlags()
	if accMode.GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY {
		mntFlags = append(mntFlags, "ro")
	}

	if err := gofsutil.BindMount(ctx, privTgt, target, mntFlags...); err != nil {
		return status.Errorf(codes.Internal,
			"error publish volume to target path: %s",
			err.Error())
	}

	log.Info(fmt.Sprintf("Volume %s has been mounted successfully to the target path %s", id, target))

	return nil
}

// unpublishVolume removes the bind mount to the target path, and also removes
// the mount to the private mount directory if the volume is no longer in use.
// It determines this by checking to see if the volume is mounted anywhere else
// other than the private mount.
// The req.TargetPath should be a path starting with "/"
func unpublishVolume(
	req *csi.NodeUnpublishVolumeRequest,
	privDir, device, devicePath string) (bool, error) {
	lastUnmounted := false
	ctx := context.Background()
	id := req.GetVolumeId()

	target := req.GetTargetPath()
	if target == "" {
		return lastUnmounted, status.Error(codes.InvalidArgument,
			"target path required")
	}
	// make sure device is valid
	sysDevice, err := GetDevice(devicePath)
	if err != nil {
		//return status.Errorf(codes.Internal,
		//	"error getting block device for volume: %s, err: %s",
		//	id, err.Error())
		// This error needs to be idempotent since device was not found
		return true, nil
	}
	//  Get the private mount path
	privTgt := getPrivateMountPoint(privDir, id)

	//Get existing mounts
	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return lastUnmounted, status.Errorf(codes.Internal,
			"could not reliably determine existing mount status: %s",
			err.Error())
	}

	tgtMnt := false
	privMnt := false
	log.Debug("UnpublishVolume: mnts:", len(mnts))
	log.Debug("Device:", device)
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
		log.Debug(fmt.Sprintf("Unmounting target %s", target))
		if err := gofsutil.Unmount(ctx, target); err != nil {
			return lastUnmounted, status.Errorf(codes.Internal, "Error unmounting target: %s", err.Error())
		}
		log.Info(fmt.Sprintf("Device %s unmounted from target path %s successfully", device, target))
	} else {
		log.Info(fmt.Sprintf("Device %s has not been mounted to target path %s. Skipping unmount", device, target))
	}

	if privMnt {
		log.Debug(fmt.Sprintf("Unmounting sysDevice %v privTgt  %s", sysDevice, privTgt))
		if lastUnmounted, err = unmountPrivMount(ctx, sysDevice, privTgt); err != nil {
			return lastUnmounted, status.Errorf(codes.Internal, "Error unmounting private mount: %s", err.Error())
		}
		log.Info(fmt.Sprintf("Device %s unmounted from private mount path %s successfully", device, privTgt))
	} else {
		mnts, err := getDevMounts(sysDevice)
		if err == nil && len(mnts) == 0 {
			log.Info("No private mount or remaining mounts device: " + sysDevice.Name)
			lastUnmounted = true
		}
	}

	return lastUnmounted, nil
}

func getDevMounts(sysDevice *Device) ([]gofsutil.Info, error) {

	ctx := context.Background()
	devMnts := make([]gofsutil.Info, 0)

	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return devMnts, err
	}
	for _, m := range mnts {
		if m.Device == sysDevice.RealDev || m.Device == sysDevice.FullPath || (m.Device == "devtmpfs" && m.Source == sysDevice.RealDev) {
			devMnts = append(devMnts, m)
		}
	}
	return devMnts, nil
}

func getPrivateMountPoint(privDir string, name string) string {
	return filepath.Join(privDir, name)
}

// mkdir creates the directory specified by path if needed.
// return pair is a bool flag of whether dir was created, and an error
func mkdir(path string) (bool, error) {
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		if err := os.Mkdir(path, 0755); err != nil {
			log.WithField("dir", path).WithError(
				err).Error("Unable to create dir")
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

func handlePrivFSMount(
	accMode *csi.VolumeCapability_AccessMode,
	mntFlags []string, sysDevice *Device, fs, privTgt string) error {
	ctx := context.Background()
	if err := gofsutil.FormatAndMount(ctx, sysDevice.FullPath, privTgt, fs, mntFlags...); err != nil {
		return status.Errorf(codes.Internal,
			"error performing private mount: %s",
			err.Error())
	}
	return nil
}

// GetDevice returns a Device struct with info about the given device, or
// an error if it doesn't exist or is not a block device
func GetDevice(path string) (*Device, error) {

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

	// TODO does EvalSymlinks throw error if link is to non-
	// existent file? assuming so by masking error below
	ds, _ := os.Stat(d)
	dm := ds.Mode()

	if dm&os.ModeDevice == 0 {
		return nil, fmt.Errorf("%s is not a block device", path)
	}

	return &Device{
		Name:     fi.Name(),
		FullPath: path,
		RealDev:  strings.Replace(d, "\\", "/", -1),
	}, nil
}

func unmountPrivMount(
	ctx context.Context,
	dev *Device,
	target string) (bool, error) {
	lastUnmounted := false

	mnts, err := getDevMounts(dev)
	if err != nil {
		return lastUnmounted, err
	}

	// Handle no private mount (which is odd because we had one to call here)
	// It implies deleting the target mount also cleaned up the private mount
	if len(mnts) == 0 {
		log.Info("No private mounts for device: " + dev.RealDev)
		err := removeWithRetry(target)
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
		err := removeWithRetry(target)
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

func (s *service) rescanHba(wwn string) error {
	fcHostsDir := "/sys/class/fc_host"
	hostEntries, err := ioutil.ReadDir(fcHostsDir)
	if err != nil {
		log.Errorf("Unable to open path %s Error: %v", fcHostsDir, err)
		return err
	}

	// Look through the hosts retrieving the port_name
	for _, host := range hostEntries {
		symlinkPath, _, err := s.wwnToDevicePath(wwn)
		sysDevice, err := GetDevice(symlinkPath)
		if err == nil && sysDevice != nil {
			break
		}

		if !strings.HasPrefix(host.Name(), "host") {
			continue
		}
		filePath := fmt.Sprintf("%s/%s/issue_lip", fcHostsDir, host.Name())
		file, err := os.OpenFile(filePath, os.O_WRONLY, os.ModeDevice)
		if err != nil {
			log.Errorf("Error opening file:", err)
			continue
		}
		if file != nil {
			_, err = file.WriteString("1")
			if err != nil {
				log.Errorf("Error during rescan of FC %s.", filePath, err)
				file.Close()
				continue
			}

			log.Infof("Rescan FC %s has been issued successfully", filePath)
			file.Close()
		}

		// Wait for the devices to show up
		time.Sleep(rescanHostSleepTime)
	}

	return nil
}

//RescanSCSIHost scsi host
func RescanSCSIHost() error {
	scsiHostsDir := "/sys/class/scsi_host"
	hostEntries, err := ioutil.ReadDir(scsiHostsDir)
	if err != nil {
		log.Errorf("Unable to open path %s Error: %v", scsiHostsDir, err)
		return err
	}

	// Look through the hosts retrieving the port_name
	for _, host := range hostEntries {
		if !strings.HasPrefix(host.Name(), "host") {
			continue
		}
		filePath := fmt.Sprintf("%s/%s/scan", scsiHostsDir, host.Name())
		file, err := os.OpenFile(filePath, os.O_WRONLY, os.ModeDevice)
		if err != nil {
			log.Errorf("Error opening file:", err)
			continue
		}
		if file != nil {
			_, err = file.WriteString("- - -")
			if err != nil {
				log.Errorf("Error during rescan of FC %s.", filePath, err)
				file.Close()
				continue
			}

			log.Infof("Rescan scsi %s has been issued successfully", filePath)
			file.Close()
		}

		// Wait for the devices to show up
		time.Sleep(rescanHostSleepTime)
	}
	return nil
}

func contains(list []string, item string) bool {
	for _, x := range list {
		if x == item {
			return true
		}
	}
	return false
}

// Execute the multipath command with a timeout and various arguments.
// Optionally a chroot directory can be specified for changing root directory.
// This only works in a container or another environment where it can chroot to /noderoot.
func multipathCommand(ctx context.Context, timeoutSeconds time.Duration, chroot string, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutSeconds*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	args := make([]string, 0)
	if chroot == "" {
		args = append(args, arguments...)
		log.Debug("/usr/sbin/multipath %v", args)
		cmd = exec.CommandContext(ctx, "/usr/sbin/multipath", args...)
	} else {
		args = append(args, chroot)
		args = append(args, "/usr/sbin/multipath")
		args = append(args, arguments...)
		log.Debug("/usr/sbin/chroot %v", args)
		cmd = exec.CommandContext(ctx, "/usr/sbin/chroot", args...)
	}
	textBytes, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("multipath command failed: " + err.Error())
	}
	if len(textBytes) > 0 {
		log.Debug(fmt.Printf("multipath output: " + string(textBytes)))
	}
	return textBytes, err
}

// wwnToDevicePath looks up a volume WWN in /dev/disk/by-id and returns
// a) the symlink path in /dev/disk/by-id and
// b) the corresponding device entry in /dev.
func (s *service) wwnToDevicePath(wwn string) (string, string, error) {
	wwn = strings.ReplaceAll(wwn, ":", "")
	wwn = strings.ToLower(wwn)
	// Look for multipath device.
	symlinkPath := fmt.Sprintf("%s%s", MultipathDevDiskByIDPrefix, wwn)
	devPath, err := os.Readlink(symlinkPath)
	log.Debugf("SymlinkPath:%s DevPath:%s Error:%v", symlinkPath, devPath, err)

	// Look for regular path device.
	if err != nil || devPath == "" {
		symlinkPath = fmt.Sprintf("/dev/disk/by-id/wwn-0x%s", wwn)
		devPath, err = os.Readlink(symlinkPath)
		if err != nil {
			log.Printf("Check for disk path %s not found", symlinkPath)
			return "", "", status.Error(codes.NotFound, fmt.Sprintf("Check for disk path %s not found %v", symlinkPath, err))
		}
	}
	components := strings.Split(devPath, "/")
	lastPart := components[len(components)-1]
	devPath = "/dev/" + lastPart
	log.Infof("Check for disk path %s found: %s", symlinkPath, devPath)
	return symlinkPath, devPath, err
}

// nodeDeleteBlockDevices deletes the block devices associated with a volume WWN (including multipath) on that node.
// This should be done when the volume will no longer be used on the node.
func (s *service) nodeDeleteBlockDevices(wwn string, target string) error {
	for i := 0; i < maxBlockDevicesPerWWN; i++ {
		// Wait for the next device to show up
		time.Sleep(removeDeviceSleepTime)
		symlinkPath, devicePath, _ := s.wwnToDevicePath(wwn)
		if devicePath == "" {
			// All done, no more paths for WWN
			log.Debug("All done, no more paths for WWN", devicePath)
			return nil
		}
		log.WithField("WWN", wwn).WithField("SymlinkPath", symlinkPath).WithField("DevicePath", devicePath).Info("Removing block device")
		isMultipath := strings.HasPrefix(symlinkPath, MultipathDevDiskByIDPrefix)
		if isMultipath {
			chroot, _ := csictx.LookupEnv(context.Background(), s.opts.PvtMountDir)
			// List the current multipath status
			ctx := context.Background()
			// Attempt to flush the devicePath
			multipathMutex.Lock()
			textBytes, err := multipathCommand(ctx, 10, chroot, "-f", devicePath)
			multipathMutex.Unlock()
			if textBytes != nil && len(textBytes) > 0 {
				log.Info(fmt.Sprintf("multipath -f %s: %s", devicePath, string(textBytes)))
			}
			if err != nil {
				log.Infof("Multipath flush error: %s: %s", devicePath, err.Error())
			}
			continue
		}
		err := gofsutil.RemoveBlockDevice(context.Background(), devicePath)
		if err != nil {
			log.WithField("DevicePath", devicePath).Error(err)
		} else {
			log.Debug("RemoveBlockDevice successful for ", devicePath)
		}
	}
	return status.Error(codes.Internal, fmt.Sprintf("WWN %s had more than %d block devices or they weren't successfully deleted", wwn, maxBlockDevicesPerWWN))
}

func removeWithRetry(target string) error {
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
