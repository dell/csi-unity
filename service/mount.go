/*
 Copyright © 2019-2025 Dell Inc. or its subsidiaries. All Rights Reserved.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at
      http://www.apache.org/licenses/LICENSE-2.0
 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dell/csi-unity/service/serviceutils"

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

// Define a function variable to allow mocking in tests
var readFileFunc = os.ReadFile

func stagePublishNFS(ctx context.Context, req *csi.NodeStageVolumeRequest, exportPaths []string, arrayID string, nfsv3, nfsv4 bool) error {
	ctx, log, rid := GetRunidLog(ctx)
	ctx, log = setArrayIDContext(ctx, arrayID)

	stagingTargetPath := req.GetStagingTargetPath()

	volCap := req.GetVolumeCapability()
	mntVol := volCap.GetMount()
	mntFlags := mntVol.GetMountFlags()
	// make sure target is created
	err := createDirIfNotExist(ctx, stagingTargetPath, arrayID)
	if err != nil {
		return err
	}

	rwo := "rw"

	mntFlags = append(mntFlags, rwo)

	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return status.Errorf(codes.Internal,
			"could not reliably determine existing mount status: %v",
			err)
	}

	if len(mnts) != 0 {
		for _, m := range mnts {
			// Idempotency check
			for _, exportPathURL := range exportPaths {
				if m.Device == exportPathURL {
					if m.Path == stagingTargetPath {
						if serviceutils.ArrayContains(m.Opts, rwo) {
							log.Debugf("Staging target path: %s is already mounted to export path: %s", stagingTargetPath, exportPathURL)
							return nil
						}
						return status.Error(codes.InvalidArgument, serviceutils.GetMessageWithRunID(rid, "Staging target path: %s is already mounted to export path: %s with conflicting access modes", stagingTargetPath, exportPathURL))
					}
					// It is possible that a different export path URL is used to mount stage target path
					continue
				}
			}
		}
	}

	log.Debugf("Stage - Mount flags for NFS: %s", mntFlags)

	mountOverride := false
	for _, flag := range mntFlags {
		if strings.Contains(flag, "vers") {
			mountOverride = true
			break
		}
	}

	// if nfsv4 specified or mount options is provided, mount as is
	if nfsv4 || mountOverride {
		nfsv4 = false
		for _, exportPathURL := range exportPaths {
			err = gofsutil.Mount(ctx, exportPathURL, stagingTargetPath, "nfs", mntFlags...)
			if err == nil {
				nfsv4 = true
				break
			}
		}
	}

	// Try remount as nfsv3 only if NAS server supports nfs v3
	if nfsv3 && !nfsv4 && !mountOverride {
		mntFlags = append(mntFlags, "vers=3")
		for _, exportPathURL := range exportPaths {
			err = gofsutil.Mount(ctx, exportPathURL, stagingTargetPath, "nfs", mntFlags...)
			if err == nil {
				break
			}
		}
	}

	if err != nil {
		return status.Error(codes.InvalidArgument, serviceutils.GetMessageWithRunID(rid, "Mount failed for NFS export paths: %s. Error: %v", exportPaths, err))
	}

	// Update permissions with 1777 in nfs share so every user can use it
	if err := os.Chmod(stagingTargetPath, os.ModeSticky|os.ModePerm); err != nil {
		return status.Errorf(codes.Internal, "can't change permissions of folder %s: %s", stagingTargetPath, err.Error())
	}

	return nil
}

func publishNFS(ctx context.Context, req *csi.NodePublishVolumeRequest, exportPaths []string, arrayID, chroot string, nfsv3, nfsv4, allowRWOmultiPodAccess bool) error {
	ctx, log, rid := GetRunidLog(ctx)
	ctx, log = setArrayIDContext(ctx, arrayID)

	targetPath := req.GetTargetPath()
	stagingTargetPath := req.GetStagingTargetPath()
	volCap := req.GetVolumeCapability()
	accMode := volCap.GetAccessMode()

	// make sure target is created
	err := createDirIfNotExist(ctx, targetPath, arrayID)
	if err != nil {
		return err
	}

	var rwoArray []string
	rwo := "rw"
	if req.Readonly {
		rwo = "ro"
	}
	rwoArray = append(rwoArray, rwo)
	mntVol := volCap.GetMount()
	mntFlags := mntVol.GetMountFlags()
	rwoArray = append(rwoArray, mntFlags...)
	// Check if stage target mount exists
	var stageExportPathURL string
	stageMountExists := false
	for _, exportPathURL := range exportPaths {
		mnts, err := gofsutil.GetDevMounts(ctx, exportPathURL)
		if err != nil {
			return status.Error(codes.InvalidArgument, serviceutils.GetMessageWithRunID(rid, "could not reliably determine existing staging target path mount status. Error: %v", err))
		}
		for _, m := range mnts {
			if m.Path == stagingTargetPath {
				stageMountExists = true
				stageExportPathURL = exportPathURL
			}
		}
	}
	if !stageMountExists {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Filesystem not mounted on staging target path: %s", stagingTargetPath))
	}

	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "could not reliably determine existing mount status. Error: %v", err))
	}

	if len(mnts) != 0 {
		for _, m := range mnts {
			// Idempotency check
			if m.Device == stageExportPathURL {
				if m.Path == targetPath {
					if serviceutils.ArrayContains(m.Opts, rwo) {
						log.Debugf("Target path: %s is already mounted to export path: %s", targetPath, stageExportPathURL)
						return nil
					}
					return status.Error(codes.InvalidArgument, serviceutils.GetMessageWithRunID(rid, "Target path: %s is already mounted to export path: %s with conflicting access modes", targetPath, stageExportPathURL))
				} else if m.Path == stagingTargetPath || m.Path == chroot+stagingTargetPath {
					continue
				}
				if accMode.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER || (accMode.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER && !allowRWOmultiPodAccess) {
					return status.Error(codes.InvalidArgument, serviceutils.GetMessageWithRunID(rid, "Export path: %s is already mounted to different target path: %s", stageExportPathURL, m.Path))
				}
				// For multi-node access modes and when allowRWOmultiPodAccess is true for single-node access, target mount will be executed
				continue
			}
		}
	}

	log.Debugf("Publish - Mount flags for NFS: %s", mntFlags)

	mountOverride := false
	for _, flag := range mntFlags {
		if strings.Contains(flag, "vers") {
			mountOverride = true
			break
		}
	}

	// if nfsv4 specified or mount options is provided, mount as is
	// Proceeding to perform bind mount to target path
	if nfsv4 || mountOverride {
		nfsv4 = false
		err = gofsutil.BindMount(ctx, stagingTargetPath, targetPath, rwoArray...)
		if err == nil {
			nfsv4 = true
		}
	}

	if nfsv3 && !nfsv4 && !mountOverride {
		rwo += ",vers=3"
		rwoArray = append(rwoArray, "vers=3")
		err = gofsutil.BindMount(ctx, stagingTargetPath, targetPath, rwoArray...)
	}

	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Error publish filesystem to target path. Error: %v", err))
	}
	return nil
}

func stageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest, stagingPath, symlinkPath string) error {
	rid, log := serviceutils.GetRunidAndLogger(ctx)

	volCap := req.GetVolumeCapability()
	id := req.GetVolumeId()
	mntVol := volCap.GetMount()
	ro := false // since only SINGLE_NODE_WRITER is supported

	// make sure device is valid
	sysDevice, err := GetDevice(ctx, symlinkPath)
	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "error getting block device for volume: %s, err: %s", id, err.Error()))
	}

	// Check if device is already mounted
	devMnts, err := getDevMounts(ctx, sysDevice)
	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "could not reliably determine existing mount status: %s", err.Error()))
	}

	alreadyMounted := false
	if len(devMnts) == 0 {
		// Device isn't mounted anywhere, do the staging mount
		// Make sure private mount point exists
		var created bool

		created, err = mkdir(ctx, stagingPath)
		if err != nil {
			return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Unable to create staging mount point: %s", err.Error()))
		}
		if !created {
			// The place where our device is supposed to be mounted
			// already exists, but we also know that our device is not mounted anywhere
			// Either something didn't clean up correctly, or something else is mounted
			// If the staging mount is not in use, it's okay to re-use it. But make sure
			// it's not in use first

			mnts, err := gofsutil.GetMounts(ctx)
			if err != nil {
				return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "could not reliably determine existing mount status: %s", err.Error()))
			}

			for _, m := range mnts {
				if m.Path == stagingPath {
					resolvedMountDevice := evalSymlinks(ctx, m.Device)
					if resolvedMountDevice != sysDevice.RealDev {
						return status.Errorf(codes.FailedPrecondition, "Staging mount point: %s mounted by different device: %s", stagingPath, resolvedMountDevice)
					}
					alreadyMounted = true
				}
			}
		}

		if !alreadyMounted {
			fs := mntVol.GetFsType()
			mntFlags := mntVol.GetMountFlags()

			log.Debugf("Stage - Mount flags for Volume: %s", mntFlags)

			if fs != "ext3" && fs != "ext4" && fs != "xfs" {
				log.Info("Using default FS Type ext4 since no FS Type is provided")
				fs = "ext4"
			}

			if fs == "xfs" {
				mntFlags = append(mntFlags, "nouuid")
			}

			if err := handleStageMount(ctx, mntFlags, sysDevice, fs, stagingPath); err != nil {
				return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Staging mount failed: %v", err))
			}
		} else {
			log.Debug("Staging mount is already in place")
		}
	} else {
		// Device is already mounted. Need to ensure that it is already
		// mounted to the expected staging path, with correct rw/ro permissions
		mounted := false
		for _, m := range devMnts {
			if m.Path == stagingPath {
				mounted = true
				rwo := "rw"
				if ro {
					rwo = "ro"
				}
				//@TODO: check contents of m.Opts if it has fs type and verify fs type as well
				//Remove below debug once resolved
				log.Debug("m.Opts: ", m.Opts)
				if serviceutils.ArrayContains(m.Opts, rwo) {
					log.Warn("staging mount already in place")
					break
				}
				return status.Error(codes.InvalidArgument, serviceutils.GetMessageWithRunID(rid, "access mode conflicts with existing mounts"))

			}
			// It is ok if the device is mounted elsewhere - could be targetPath. If not this will be caught during NodePublish
		}
		if !mounted {
			return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "device already in use and mounted elsewhere. Cannot do private mount"))
		}
	}

	return nil
}

func publishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest, targetPath, symlinkPath, chroot string, allowRWOmultiPodAccess bool) error {
	rid, log := serviceutils.GetRunidAndLogger(ctx)
	stagingPath := req.GetStagingTargetPath()
	id := req.GetVolumeId()
	volCap := req.GetVolumeCapability()
	accMode := volCap.GetAccessMode()
	mntVol := volCap.GetMount()
	isBlock := accTypeBlock(volCap)

	// make sure device is valid
	sysDevice, err := GetDevice(ctx, symlinkPath)
	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "error getting block device for volume: %s, err: %s", id, err.Error()))
	}

	// Check if target is not mounted
	devMnts, err := getDevMounts(ctx, sysDevice)
	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "could not reliably determine existing mount status: %s", err.Error()))
	}

	// If mounts already existed for this device, check if mount to
	// target path was already there
	if len(devMnts) > 0 {
		for _, m := range devMnts {
			if m.Path == targetPath {
				// volume already published to target
				// if mount options look good, do nothing
				rwo := "rw"
				if accMode.GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY {
					rwo = "ro"
				}
				if !serviceutils.ArrayContains(m.Opts, rwo) {
					return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "volume previously published with different options"))
				}
				// Existing mount satisfies request
				log.Debug("volume already published to target")
				return nil
			} else if m.Path == stagingPath || m.Path == chroot+stagingPath {
				continue
			} else if accMode.GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER || (accMode.GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER && !allowRWOmultiPodAccess) {
				// Device has been mounted aleady to another target
				return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "device already in use and mounted elsewhere"))
			}
		}
	}

	pathMnts, err := getPathMounts(ctx, targetPath)
	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "could not reliably determine existing mount status: %s", err.Error()))
	}

	if len(pathMnts) > 0 {
		for _, m := range pathMnts {
			if !(m.Source == sysDevice.FullPath || m.Device == sysDevice.FullPath) {
				// target is mounted by some other device
				return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Target is mounted using a different device %s", m.Device))
			}
		}
	}

	if isBlock {
		_, err = mkfile(ctx, targetPath)
		if err != nil {
			return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Could not create %s: %v", targetPath, err))
		}
		mntFlags := mntVol.GetMountFlags()
		err = mountBlock(ctx, sysDevice, targetPath, mntFlags, SingleAccessMode(accMode), allowRWOmultiPodAccess)
		return err
	}

	// Make sure target is created. The spec says the driver is responsible
	// for creating the target, but Kubernetes generally creates the target.

	_, err = mkdir(ctx, targetPath)
	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Could not create %s: %v", targetPath, err))
	}

	var mntFlags []string
	mntFlags = mntVol.GetMountFlags()
	if accMode.GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY {
		mntFlags = append(mntFlags, "ro")
	}

	log.Debugf("Publish - Mount flags for Volume: %s", mntFlags)

	if err := gofsutil.BindMount(ctx, stagingPath, targetPath, mntFlags...); err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "error publish volume to target path: %v", err))
	}

	log.Debugf("Volume %s has been mounted successfully to the target path %s", id, targetPath)
	return nil
}

// unpublishVolume removes the bind mount to the target path
func unpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) error {
	rid, log := serviceutils.GetRunidAndLogger(ctx)
	target := req.GetTargetPath()

	// Look through the mount table for the target path.
	targetMount, err := getTargetMount(ctx, target)
	if err != nil {
		return err
	}
	log.Debugf("Target Mount: %s", targetMount)

	if targetMount.Device == "" {
		// This should not happen normally. idempotent requests should be rare.
		// If we incorrectly exit here, conflicting devices will be left
		log.Debugf("No target mount found. waiting %v to re-verify no target %s mount", targetMountRecheckSleepTime, target)
		time.Sleep(targetMountRecheckSleepTime)

		targetMount, err := getTargetMount(ctx, target)

		if err != nil || targetMount.Device == "" {
			log.Debugf("Still no mount entry for target, so assuming this is an idempotent call: %s", target)
			return nil
		}

		// It is alright if the device is mounted elsewhere - could be staging mount
	}

	devicePath := targetMount.Device
	if devicePath == devtmpfs || devicePath == "udev" || devicePath == "" {
		devicePath = targetMount.Source
	}
	log.Debugf("TargetMount: %s", targetMount)
	log.Debugf("DevicePath: %s", devicePath)

	// make sure device is valid
	sysDevice, err := GetDevice(ctx, devicePath)
	if err != nil {
		// This error needs to be idempotent since device was not found
		return nil
	}

	// Get existing mounts
	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "could not reliably determine existing mount status: %s", err.Error()))
	}

	tgtMnt := false
	for _, m := range mnts {
		if m.Source == sysDevice.FullPath || m.Device == sysDevice.FullPath {
			if m.Path == target {
				tgtMnt = true
				break
			}
		} else if (m.Source == ubuntuNodeRoot+sysDevice.RealDev || m.Source == sysDevice.RealDev) && m.Device == "udev" {
			// For Ubuntu mounts
			if m.Path == target {
				tgtMnt = true
				break
			}
		}
	}

	if tgtMnt {
		log.Debugf("Unmounting target %s", target)
		if err := gofsutil.Unmount(ctx, target); err != nil {
			return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Error unmounting target: %s", err.Error()))
		}
		log.Debugf("Device %s unmounted from target path %s successfully", sysDevice.Name, target)
	} else {
		log.Debugf("Device %s has not been mounted to target path %s. Skipping unmount", sysDevice.Name, target)
	}

	return nil
}

// unstage volume removes staging mount and makes sure no other mounts are left for the given device path
func unstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest, deviceWWN, chroot string) (bool, string, error) {
	rid, log := serviceutils.GetRunidAndLogger(ctx)
	lastUnmounted := false
	id := req.GetVolumeId()
	stagingTarget := req.GetStagingTargetPath()

	// Look through the mount table for the target path.
	stageMount, err := getTargetMount(ctx, stagingTarget)
	if err != nil {
		return lastUnmounted, "", err
	}
	log.Debugf("Stage Mount: %s", stageMount)

	if stageMount.Device == "" {
		// This should not happen normally. idempotent requests should be rare.
		// If we incorrectly exit here, conflicting devices will be left
		log.Debugf("No stage mount found. waiting %v to re-verify no target %s mount", targetMountRecheckSleepTime, stagingTarget)
		time.Sleep(targetMountRecheckSleepTime)

		stageMount, err := getTargetMount(ctx, stagingTarget)

		// Make sure volume is not mounted elsewhere
		devMnts := make([]gofsutil.Info, 0)

		symlinkPath, devicePath, err := gofsutil.WWNToDevicePathX(ctx, deviceWWN)
		if err != nil {
			log.Debugf("Disk path not found. Error: %v", err)
		}

		sysDevice, err := GetDevice(ctx, symlinkPath)

		if sysDevice != nil {
			devMnts, _ = getDevMounts(ctx, sysDevice)
		}

		if (err != nil || stageMount.Device == "") && len(devMnts) == 0 {
			lastUnmounted = true
			log.Debugf("Still no mount entry for target, so assuming this is an idempotent call: %s", stagingTarget)
			return lastUnmounted, devicePath, nil
		}

		return lastUnmounted, "", status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Volume %s has been mounted outside the provided target path %s", id, stagingTarget))
	}

	devicePath := stageMount.Device
	if devicePath == "devtmpfs" || devicePath == "" {
		devicePath = stageMount.Source
	}

	// make sure device is valid
	sysDevice, err := GetDevice(ctx, devicePath)
	if err != nil {
		// This error needs to be idempotent since device was not found
		return true, devicePath, nil
	}

	// Get existing mounts
	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return lastUnmounted, "", status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "could not reliably determine existing mount status: %s", err.Error()))
	}

	stgMnt := false
	log.Debug("SysDevice:", sysDevice.Name, sysDevice.FullPath, sysDevice.RealDev, "Staging Target", stagingTarget, "DevicePath", devicePath)

	for _, m := range mnts {
		if m.Source == sysDevice.FullPath || m.Device == sysDevice.FullPath {
			if m.Path == stagingTarget || m.Path == chroot+stagingTarget {
				stgMnt = true
				break
			}
			log.Infof("Device %s has been mounted outside staging target on %s", sysDevice.FullPath, m.Path)

		} else if (m.Path == stagingTarget || m.Path == chroot+stagingTarget) && !(m.Source == sysDevice.FullPath || m.Device == sysDevice.FullPath) {
			log.Infof("Staging path %s has been mounted by foreign device %s", stagingTarget, m.Device)
		}
	}

	if stgMnt {
		log.Debugf("Unmount sysDevice: %v staging target:  %s", sysDevice, stagingTarget)
		if lastUnmounted, err = unmountStagingMount(ctx, sysDevice, stagingTarget, chroot); err != nil {
			return lastUnmounted, "", status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Error unmounting staging mount %s: %s", stagingTarget, err.Error()))
		}
		log.Debugf("Device %s unmounted from private mount path %s successfully", sysDevice.Name, stagingTarget)
	} else {
		mnts, err := getDevMounts(ctx, sysDevice)
		if err == nil && len(mnts) == 0 {
			log.Debugf("No private mount or remaining mounts device: %s", sysDevice.Name)
			lastUnmounted = true
		}
	}

	return lastUnmounted, devicePath, nil
}

// unpublishNFS removes the mount from staging target path or target path
func unpublishNFS(ctx context.Context, targetPath, arrayID string, exportPaths []string) error {
	ctx, log, rid := GetRunidLog(ctx)
	ctx, log = setArrayIDContext(ctx, arrayID)

	// Get existing mounts
	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "could not reliably determine existing mount status: %v", err))
	}
	mountExists := false
	var exportPath string
	for _, m := range mnts {
		if m.Path == targetPath {
			for _, exportPathURL := range exportPaths {
				if m.Device == exportPathURL {
					mountExists = true
					exportPath = exportPathURL
					break
				}
			}
			if !mountExists {
				// Path is mounted but with some other NFS Share
				return status.Error(codes.Unknown, serviceutils.GetMessageWithRunID(rid, "Path: %s mounted by different NFS Share with export path: %s", targetPath, m.Device))
			}
		}
		if mountExists {
			break
		}
	}
	if !mountExists {
		// Idempotent case
		log.Debugf("Path: %s not mounted", targetPath)
		return nil
	}
	log.Debugf("Unmounting target path: %s", targetPath)
	if err := gofsutil.Unmount(ctx, targetPath); err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Error unmounting path: %s. Error: %v", targetPath, err))
	}
	log.Debugf("Filesystem with NFS share export path: %s unmounted from path: %s successfully", exportPath, targetPath)
	return nil
}

func getDevMounts(ctx context.Context, sysDevice *Device) ([]gofsutil.Info, error) {
	log := serviceutils.GetRunidLogger(ctx)
	devMnts := make([]gofsutil.Info, 0)
	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return devMnts, err
	}
	for _, m := range mnts {
		if m.Device == sysDevice.RealDev || m.Device == sysDevice.FullPath || (m.Device == "devtmpfs" && m.Source == sysDevice.RealDev) {
			devMnts = append(devMnts, m)
		} else if (m.Source == ubuntuNodeRoot+sysDevice.RealDev || m.Source == sysDevice.RealDev) && m.Device == "udev" {
			devMnts = append(devMnts, m)
		} else {
			// Find the multipath device mapper from the device obtained
			mpDevName := strings.TrimPrefix(sysDevice.RealDev, "/dev/")
			filename := fmt.Sprintf("/sys/devices/virtual/block/%s/dm/name", mpDevName)
			if name, err := readFileFunc(filepath.Clean(filename)); err != nil {
				log.Warn("Could not read mp dev name file ", filename, err)
			} else {
				mpathDev := strings.TrimSpace(string(name))
				mapperDevice := fmt.Sprintf("/dev/mapper/%s", mpathDev)
				if m.Source == mapperDevice || m.Device == mapperDevice || m.Path == mapperDevice {
					devMnts = append(devMnts, m)
				}
			}
		}
	}
	return devMnts, nil
}

func getMpathDevFromWwn(ctx context.Context, volumeWwn string) (string, error) {
	ctx, log, rid := GetRunidLog(ctx)
	symlinkPath, _, err := gofsutil.WWNToDevicePathX(ctx, volumeWwn)
	if err != nil {
		return "", status.Error(codes.NotFound, serviceutils.GetMessageWithRunID(rid, "Disk path not found. Error: %v", err))
	}

	sysDevice, err := GetDevice(ctx, symlinkPath)
	if err != nil {
		return "", status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "error getting block device for volume wwn: %s, err: %s", volumeWwn, err.Error()))
	}

	mpDevName := strings.TrimPrefix(sysDevice.RealDev, "/dev/")
	filename := fmt.Sprintf("/sys/devices/virtual/block/%s/dm/name", mpDevName)
	name, err := readFileFunc(filepath.Clean(filename))
	if err != nil {
		log.Error("Could not read mp dev name file ", filename, err)
		return "", err
	}
	mpathDev := strings.TrimPrefix(strings.TrimSpace(string(name)), "3")
	return mpathDev, nil
}

func createDirIfNotExist(ctx context.Context, path, arrayID string) error {
	ctx, _, rid := GetRunidLog(ctx)
	ctx, _ = setArrayIDContext(ctx, arrayID)
	tgtStat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Create target directory if it doesnt exist
			_, err := mkdir(ctx, path)
			if err != nil {
				return status.Error(codes.FailedPrecondition, serviceutils.GetMessageWithRunID(rid, "Could not create path: %s. Error: %v", path, err))
			}
		} else {
			return status.Error(codes.Unknown, serviceutils.GetMessageWithRunID(rid, "failed to stat path: %s, Error: %v", path, err))
		}
	} else {
		// check that the target is right type for vol type
		if !tgtStat.IsDir() {
			return status.Error(codes.FailedPrecondition, serviceutils.GetMessageWithRunID(rid, "Path: %s wrong type (file)", path))
		}
	}
	return nil
}

// mkdir creates the directory specified by path if needed.
// return pair is a bool flag of whether dir was created, and an error
func mkdir(ctx context.Context, path string) (bool, error) {
	log := serviceutils.GetRunidLogger(ctx)
	st, err := os.Stat(path)
	if err == nil {
		if !st.IsDir() {
			return false, fmt.Errorf("existing path is not a directory")
		}
		return false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		log.WithField("dir", path).WithError(err).Error("Unable to stat dir")
		return false, err
	}

	// Case when there is error and the error is fs.ErrNotExists.
	if err := os.MkdirAll(path, 0o750); err != nil {
		log.WithField("dir", path).WithError(err).Error("Unable to create dir")
		return false, err
	}

	log.WithField("path", path).Debug("created directory")
	return true, nil
}

// mkfile creates a file specified by the path
// returna a pair - bool flag of whether file was created, and an error
func mkfile(ctx context.Context, path string) (bool, error) {
	log := serviceutils.GetRunidLogger(ctx)
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE, 0o600)
		if err != nil {
			log.WithField("path", path).WithError(
				err).Error("Unable to create file")
			return false, err
		}
		err = file.Close()
		if err != nil {
			log.Infof("Error closing file: %v", err)
		}
		log.WithField("path", path).Debug("created file")
		return true, nil
	}
	if st.IsDir() {
		return false, fmt.Errorf("existing path is a directory")
	}
	return false, nil
}

func handleStageMount(ctx context.Context, mntFlags []string, sysDevice *Device, fs, stageTgt string) error {
	rid, _ := serviceutils.GetRunidAndLogger(ctx)
	if err := gofsutil.FormatAndMount(ctx, sysDevice.FullPath, stageTgt, fs, mntFlags...); err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "error performing private mount: %v", err))
	}
	return nil
}

// GetDevice returns a Device struct with info about the given device, or
// an error if it doesn't exist or is not a block device
func GetDevice(ctx context.Context, path string) (*Device, error) {
	rid, log := serviceutils.GetRunidAndLogger(ctx)
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Could not lstat path: %s ", path))
	}

	// eval any symlinks and make sure it points to a device
	d, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Could not evaluate symlinks path: %s ", path))
	}

	ds, _ := os.Stat(d)
	dm := ds.Mode()

	if dm&os.ModeDevice == 0 {
		return nil, status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "%s is not a block device", path))
	}

	dev := &Device{
		Name:     fi.Name(),
		FullPath: path,
		RealDev:  strings.Replace(d, "\\", "/", -1),
	}
	log.Debugf("Device: %#v", dev)
	return dev, nil
}

func unmountStagingMount(
	ctx context.Context,
	dev *Device,
	target, chroot string,
) (bool, error) {
	log := serviceutils.GetRunidLogger(ctx)
	lastUnmounted := false

	mnts, err := getDevMounts(ctx, dev)
	if err != nil {
		return lastUnmounted, err
	}

	// Handle no staging mount (which is odd because we had one to call here)
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
	// mnts length will be 1 for coreos and 2 for other operating systems
	if (len(mnts) == 1 || len(mnts) == 2) && (mnts[0].Path == target || mnts[0].Path == chroot+target) {
		if err := gofsutil.Unmount(ctx, target); err != nil {
			return lastUnmounted, err
		}
		lastUnmounted = true
		log.WithField("directory", target).Debug("removing directory")
		err := removeWithRetry(ctx, target)
		if err != nil {
			log.Warnf("Error removing private mount target: %s Error:%v", target, err)
		}
	} else {
		for _, m := range mnts {
			log.Warnf("Remaining dev mount: %#v", m)
		}
	}

	return lastUnmounted, nil
}

// removeWithRetry removes directory, if it exists, with retry.
func removeWithRetry(ctx context.Context, target string) error {
	log := serviceutils.GetRunidLogger(ctx)
	var err error
	for i := 0; i < 3; i++ {
		log.Debug("Removing target:", target)
		err = os.Remove(target)
		if err != nil && !os.IsNotExist(err) {
			log.Warnf("Error removing private mount target: %v", err)
			cmd := exec.Command("/usr/bin/rmdir", target)
			textBytes, err := cmd.CombinedOutput()
			if err != nil {
				log.Errorf("error calling rmdir: %v", err)
			} else {
				log.Debugf("rmdir output: %s", string(textBytes))
			}
			time.Sleep(3 * time.Second)
		} else {
			log.Debug("Target removed:", target)
			return nil
		}
	}
	return err
}

// Evaluate symlinks to a resolution. In case of an error,
// logs the error but returns the original path.
func evalSymlinks(ctx context.Context, path string) string {
	log := serviceutils.GetRunidLogger(ctx)
	// eval any symlinks and make sure it points to a device
	d, err := filepath.EvalSymlinks(path)
	if err != nil {
		log.Warn("Could not evaluate symlinks for path: " + path)
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

// mountBlock bind mounts the device to the required target
func mountBlock(ctx context.Context, device *Device, target string, mntFlags []string, singleAccess, allowRWOMultiPodAccess bool) error {
	rid, log := serviceutils.GetRunidAndLogger(ctx)
	log.Debugf("mountBlock called for device %#v target %s mntFlags %#v", device, target, mntFlags)
	// Check to see if already mounted
	mnts, err := getDevMounts(ctx, device)
	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Could not getDevMounts for: %s", device.RealDev))
	}
	for _, mnt := range mnts {
		if mnt.Path == target {
			log.Info("Block volume target is already mounted")
			return nil
		} else if singleAccess && !allowRWOMultiPodAccess {
			return status.Error(codes.InvalidArgument, serviceutils.GetMessageWithRunID(rid, "Access mode conflicts with existing mounts"))
		}
	}

	err = gofsutil.BindMount(ctx, device.RealDev, target, mntFlags...)
	if err != nil {
		return status.Error(codes.Internal, serviceutils.GetMessageWithRunID(rid, "Block Mount error: bind mounting to target path: %s", target))
	}
	return nil
}
