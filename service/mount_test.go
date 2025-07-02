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
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/service/serviceutils"
	"github.com/dell/gofsutil"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newTestContext returns a context with a logger injected for testing.
func newTestContext() context.Context {
	ctx := context.Background()
	log := serviceutils.GetLogger()
	entry := log.WithField(serviceutils.RUNID, "1111")
	return context.WithValue(ctx, serviceutils.UnityLogger, entry)
}

func TestStagePublishNFS(t *testing.T) {
	ctx := context.Background()
	req := &csi.NodeStageVolumeRequest{
		StagingTargetPath: "/test/path",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{
					MountFlags: []string{},
				},
			},
		},
	}
	exportPaths := []string{"192.168.1.100:/export/path1", "192.168.1.101:/export/path2"}
	arrayID := "arrayID"
	nfsv3 := true
	nfsv4 := false

	// Use mock filesystem
	gofsutil.UseMockFS()

	// Successful stage publish with NFSv4
	err := stagePublishNFS(ctx, req, exportPaths, arrayID, false, true)
	assert.NoError(t, err)

	// Successful stage publish with NFSv3
	err = stagePublishNFS(ctx, req, exportPaths, arrayID, nfsv3, nfsv4)
	assert.NoError(t, err)

	// Error when target path is already mounted
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{
			Device: "192.168.1.100:/export/path1",
			Path:   "/test/path",
		},
	}
	err = stagePublishNFS(ctx, req, exportPaths, arrayID, nfsv3, nfsv4)
	assert.Error(t, err)

	// Induce error in GetMounts
	gofsutil.GOFSMock.InduceGetMountsError = true
	err = stagePublishNFS(ctx, req, exportPaths, arrayID, nfsv3, nfsv4)
	assert.Error(t, err)

	// Restore mock filesystem state
	gofsutil.GOFSMockMounts = nil
	gofsutil.GOFSMock.InduceGetMountsError = false

	// Induce error for NFSv3 mount with "vers=3"
	gofsutil.GOFSMock.InduceMountError = true
	err = stagePublishNFS(ctx, req, exportPaths, arrayID, true, false)
	assert.Error(t, err)

	// Restore mock filesystem state
	gofsutil.GOFSMock.InduceMountError = false

	// Induce failure when trying all export paths
	gofsutil.GOFSMock.InduceMountError = true
	err = stagePublishNFS(ctx, req, exportPaths, arrayID, nfsv3, nfsv4)
	assert.Error(t, err)

	// Restore mock filesystem state
	gofsutil.GOFSMock.InduceMountError = false
}

func TestPublishNFS(t *testing.T) {
	testCases := []struct {
		desc                   string
		req                    *csi.NodePublishVolumeRequest
		exportPaths            []string
		arrayID                string
		chroot                 string
		nfsv3                  bool
		nfsv4                  bool
		allowRWOmultiPodAccess bool
		gofsutilMockMounts     []gofsutil.Info
		expectError            bool
	}{
		{
			desc: "Successful publish NFSv3 with valid staging target mount",
			req: &csi.NodePublishVolumeRequest{
				TargetPath:        "/target/path",
				StagingTargetPath: "/test/path",
				VolumeCapability: &csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							MountFlags: []string{"rw"},
						},
					},
				},
				Readonly: false,
			},
			exportPaths: []string{"/export/path"},
			arrayID:     "arrayID",
			chroot:      "/chroot",
			nfsv3:       true,
			nfsv4:       false,
			gofsutilMockMounts: []gofsutil.Info{
				{Device: "/export/path", Path: "/test/path", Opts: []string{"rw"}},
			},
			expectError: false,
		},
		{
			desc: "Error when target path is already mounted with conflicting access modes",
			req: &csi.NodePublishVolumeRequest{
				TargetPath:        "/target/path",
				StagingTargetPath: "/test/path",
				VolumeCapability: &csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							MountFlags: []string{"rw"},
						},
					},
				},
				Readonly: false,
			},
			exportPaths: []string{"/export/path"},
			arrayID:     "arrayID",
			chroot:      "/chroot",
			nfsv3:       true,
			nfsv4:       false,
			gofsutilMockMounts: []gofsutil.Info{
				{Device: "/export/path", Path: "/test/path", Opts: []string{"rw"}},
				{Device: "/export/path", Path: "/target/path", Opts: []string{"ro"}},
			},
			expectError: true,
		},
		{
			desc: "Error when export path is already mounted to different target path",
			req: &csi.NodePublishVolumeRequest{
				TargetPath:        "/target/path",
				StagingTargetPath: "/test/path",
				VolumeCapability: &csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
					},
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							MountFlags: []string{"rw"},
						},
					},
				},
				Readonly: false,
			},
			exportPaths: []string{"/export/path"},
			arrayID:     "arrayID",
			chroot:      "/chroot",
			nfsv3:       true,
			nfsv4:       false,
			gofsutilMockMounts: []gofsutil.Info{
				{Device: "/export/path", Path: "/different/path", Opts: []string{"rw"}},
			},
			expectError: true,
		},
		{
			desc: "Successful publish NFSv4 with valid staging target mount",
			req: &csi.NodePublishVolumeRequest{
				TargetPath:        "/target/path",
				StagingTargetPath: "/test/path",
				VolumeCapability: &csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							MountFlags: []string{"rw"},
						},
					},
				},
				Readonly: false,
			},
			exportPaths: []string{"/export/path"},
			arrayID:     "arrayID",
			chroot:      "/chroot",
			nfsv3:       false,
			nfsv4:       true,
			gofsutilMockMounts: []gofsutil.Info{
				{Device: "/export/path", Path: "/test/path", Opts: []string{"rw"}},
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			ctx := context.Background()
			gofsutil.UseMockFS()
			gofsutil.GOFSMockMounts = tc.gofsutilMockMounts

			err := publishNFS(ctx, tc.req, tc.exportPaths, tc.arrayID, tc.chroot, tc.nfsv3, tc.nfsv4, tc.allowRWOmultiPodAccess)
			if (err != nil) != tc.expectError {
				t.Errorf("Test failed: %s. Expected error: %v, Got: %v", tc.desc, tc.expectError, err)
			}
		})
	}

	// Restore mock filesystem state
	gofsutil.GOFSMockMounts = nil
	gofsutil.GOFSMock.InduceGetMountsError = false
}

func TestStageVolume(t *testing.T) {
	ctx := newTestContext()

	// Define a sample NodeStageVolumeRequest
	req := &csi.NodeStageVolumeRequest{
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{
					FsType:     "ext4",
					MountFlags: []string{},
				},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
		VolumeId: "test-volume-id",
	}

	stagingPath := "/test/staging/path"

	// Case: stageVolume should return an error if device path is invalid
	err := stageVolume(ctx, req, stagingPath, "test/path")
	assert.Error(t, err)

	// Create a temporary directory for a mock device node
	tmpDir := t.TempDir()
	tempDevice := filepath.Join(tmpDir, "device")
	// Create a mock device node (block device)
	if err := syscall.Mknod(tempDevice, syscall.S_IFBLK|0o666, 0); err != nil {
		t.Fatalf("Failed to create mock device node: %v", err)
	}

	// Enable mock filesystem for gofsutil
	gofsutil.UseMockFS()

	// Case: Successful stageVolume execution
	err = stageVolume(ctx, req, stagingPath, tempDevice)
	assert.NoError(t, err)

	// Case: Induce an error in gofsutil.GetMounts and validate error handling
	gofsutil.GOFSMock.InduceGetMountsError = true
	err = stageVolume(ctx, req, stagingPath, tempDevice)
	assert.Error(t, err)
	gofsutil.GOFSMock.InduceGetMountsError = false

	// Case: Device is mounted but no path specified
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: tempDevice}}
	err = stageVolume(ctx, req, stagingPath, tempDevice)
	assert.Error(t, err)

	// Case: Device is already mounted at stagingPath
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: tempDevice, Path: stagingPath}}
	err = stageVolume(ctx, req, stagingPath, tempDevice)
	assert.Error(t, err)

	// Case: Staging path exists but is not mounted anywhere
	gofsutil.GOFSMockMounts = []gofsutil.Info{}
	err = os.MkdirAll(stagingPath, 0o755)
	if err != nil {
		t.Fatalf("Failed to create staging path: %v", err)
	}
	err = stageVolume(ctx, req, stagingPath, tempDevice)
	assert.NoError(t, err)

	// Case: Staging path exists and is mounted by a different device
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: "/dev/differentDevice", Path: stagingPath}}
	err = stageVolume(ctx, req, stagingPath, tempDevice)
	assert.Error(t, err)

	// Case: FS Type defaults to ext4 when not provided
	req.VolumeCapability.AccessType.(*csi.VolumeCapability_Mount).Mount.FsType = ""
	err = stageVolume(ctx, req, stagingPath, tempDevice)
	assert.Error(t, err)
}

func TestPublishVolume(t *testing.T) {
	ctx := newTestContext()

	req := &csi.NodePublishVolumeRequest{
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
		VolumeId: "test-volume-id",
	}
	targetPath := "/test/target/path"
	stagingPath := "/test/staging/path"

	// Case: Invalid device retrieval
	err := publishVolume(ctx, req, targetPath, stagingPath, "", false)
	assert.Error(t, err)

	tmpDir := t.TempDir()
	tmpTargetPath := filepath.Join(tmpDir, "target")
	tempDevice := filepath.Join(tmpDir, "device")
	if err := syscall.Mknod(tempDevice, syscall.S_IFBLK|0o666, 0); err != nil {
		t.Fatalf("Failed to create mock device node: %v", err)
	}

	gofsutil.UseMockFS()
	// Case: Block volume publishing should fail
	req.VolumeCapability.AccessType = &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}}
	err = publishVolume(ctx, req, targetPath, tempDevice, "", false)
	assert.Error(t, err)

	// Case: Block volume publishing should Pass
	err = publishVolume(ctx, req, tmpTargetPath, tempDevice, "", false)
	assert.NoError(t, err)

	req.VolumeCapability.AccessType = &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{FsType: "ext4", MountFlags: []string{}}}
	// Case: bind mount
	err = publishVolume(ctx, req, targetPath, tempDevice, "", false)
	assert.Error(t, err)

	// Case: Device already mounted at target path
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: tempDevice, Path: targetPath}}
	err = publishVolume(ctx, req, targetPath, tempDevice, "", false)
	assert.Error(t, err)

	// Case: Device mounted elsewhere, should fail
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: tempDevice, Path: "/some/other/path"}}
	err = publishVolume(ctx, req, targetPath, tempDevice, "", false)
	assert.Error(t, err)

	// Case: Access mode SINGLE_NODE_READER_ONLY
	req.VolumeCapability.AccessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY
	err = publishVolume(ctx, req, targetPath, tempDevice, "", false)
	assert.NoError(t, err)

	// Case: Simulate mount failure (error in mounting)
	gofsutil.GOFSMock.InduceBindMountError = true
	err = publishVolume(ctx, req, targetPath, tempDevice, "", false)
	assert.Error(t, err)
	gofsutil.GOFSMock.InduceBindMountError = false
}

func TestUnpublishVolume(t *testing.T) {
	ctx := newTestContext()
	req := &csi.NodeUnpublishVolumeRequest{
		TargetPath: "/test/target/path",
	}

	// Case: No target mount found (idempotent case)
	gofsutil.GOFSMockMounts = []gofsutil.Info{}
	err := unpublishVolume(ctx, req)
	assert.NoError(t, err)

	// Case: Successful unmount
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: "/dev/sda", Path: req.TargetPath}}
	err = unpublishVolume(ctx, req)
	assert.NoError(t, err)

	// Case: getTargetMount returns empty DevicePath (fallback to Source)
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: "", Source: "/dev/sdb", Path: req.TargetPath}}
	err = unpublishVolume(ctx, req)
	assert.NoError(t, err)
}

func TestUnstageVolume(t *testing.T) {
	ctx := newTestContext()
	req := &csi.NodeUnstageVolumeRequest{
		VolumeId:          "test-volume-id",
		StagingTargetPath: "/test/staging/path",
	}
	deviceWWN := "test-wwn"
	chroot := ""

	tmpDir := t.TempDir()
	tempDevice := filepath.Join(tmpDir, "device")
	if err := syscall.Mknod(tempDevice, syscall.S_IFBLK|0o666, 0); err != nil {
		t.Fatalf("Failed to create mock device node: %v", err)
	}

	gofsutil.UseMockFS()

	// Case: getTargetMount Error
	gofsutil.GOFSMock.InduceGetMountsError = true
	_, _, err := unstageVolume(ctx, req, deviceWWN, chroot)
	assert.Error(t, err)
	gofsutil.GOFSMock.InduceGetMountsError = false

	// // Case: Device not found, idempotent case
	gofsutil.GOFSMockMounts = nil
	lastUnmounted, devicePath, err := unstageVolume(ctx, req, deviceWWN, chroot)
	assert.NoError(t, err)
	assert.True(t, lastUnmounted)
	assert.Empty(t, devicePath)

	// Case WWNToDevicePathX Error
	gofsutil.GOFSMock.InduceWWNToDevicePathError = true
	_, _, err = unstageVolume(ctx, req, deviceWWN, chroot)
	assert.NoError(t, err)
	gofsutil.GOFSMock.InduceWWNToDevicePathError = false

	// Case: Successful unstage
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: "mock-device", Path: req.StagingTargetPath}}
	lastUnmounted, devicePath, err = unstageVolume(ctx, req, deviceWWN, chroot)
	assert.NoError(t, err)
	assert.True(t, lastUnmounted)
	assert.Equal(t, "mock-device", devicePath)

	// Case: Unmount failure simulation
	gofsutil.GOFSMock.InduceUnmountError = true
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: "mock-device", Path: req.StagingTargetPath}}
	lastUnmounted, devicePath, err = unstageVolume(ctx, req, deviceWWN, chroot)
	assert.NoError(t, err)
	gofsutil.GOFSMock.InduceUnmountError = false

	// Case: Existing mount with a real device node
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: tempDevice, Path: req.StagingTargetPath}}
	lastUnmounted, devicePath, err = unstageVolume(ctx, req, deviceWWN, chroot)
	assert.NoError(t, err)
	assert.True(t, lastUnmounted)
	assert.NotEmpty(t, devicePath)
}

func TestUnpublishNFS(t *testing.T) {
	ctx := newTestContext()
	targetPath := "/test/target/path"
	arrayID := "test-array-id"
	exportPaths := []string{"nfs://export1", "nfs://export2"}

	gofsutil.UseMockFS()

	// Case: Successful unmount
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: "nfs://export1", Path: targetPath}}
	err := unpublishNFS(ctx, targetPath, arrayID, exportPaths)
	assert.NoError(t, err)

	// Case: Path not mounted (idempotent case)
	gofsutil.GOFSMockMounts = nil
	err = unpublishNFS(ctx, targetPath, arrayID, exportPaths)
	assert.NoError(t, err)

	// Case: Path mounted by a different NFS export
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: "nfs://different-export", Path: targetPath}}
	err = unpublishNFS(ctx, targetPath, arrayID, exportPaths)
	assert.Error(t, err)

	// Case: Error retrieving mounts
	gofsutil.GOFSMock.InduceGetMountsError = true
	err = unpublishNFS(ctx, targetPath, arrayID, exportPaths)
	assert.Error(t, err)
	gofsutil.GOFSMock.InduceGetMountsError = false

	// Case: Unmount failure simulation
	gofsutil.GOFSMock.InduceUnmountError = true
	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: "nfs://export1", Path: targetPath}}
	err = unpublishNFS(ctx, targetPath, arrayID, exportPaths)
	assert.Error(t, err)
	gofsutil.GOFSMock.InduceUnmountError = false
}

func TestGetMpathDevFromWwn(t *testing.T) {
	ctx := context.Background()
	volumeWwn := ""
	gofsutil.UseMockFS()

	gofsutil.GOFSMock.InduceWWNToDevicePathError = true
	mpath, err := getMpathDevFromWwn(ctx, volumeWwn)
	assert.Error(t, err)
	assert.Empty(t, mpath)
	gofsutil.GOFSMock.InduceWWNToDevicePathError = false

	gofsutil.GOFSWWNPath = "/test/path"
	mpath, err = getMpathDevFromWwn(ctx, volumeWwn)
	assert.Error(t, err)
	assert.Empty(t, mpath)

	tmpDir, err := os.MkdirTemp("", "testdevice-*")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	tempDevice := filepath.Join(tmpDir, "device")
	if err := syscall.Mknod(tempDevice, syscall.S_IFBLK|0o666, 0); err != nil {
		t.Fatalf("Failed to create mock device node: %v", err)
	}
	gofsutil.GOFSWWNPath = tempDevice
	mpath, err = getMpathDevFromWwn(ctx, volumeWwn)
	assert.Error(t, err)
	assert.Empty(t, mpath)

	// Override readFileFunc in the test
	originalReadFile := readFileFunc
	defer func() { readFileFunc = originalReadFile }()

	readFileFunc = func(filename string) ([]byte, error) {
		if strings.Contains(filename, "dm/name") {
			return []byte("mpathX\n"), nil // Simulate a valid multipath name
		}
		return nil, errors.New("file not found")
	}
	mpath, err = getMpathDevFromWwn(ctx, volumeWwn)
	assert.NoError(t, err)
	assert.NotEmpty(t, mpath)
}

func TestEvalSymlinks(t *testing.T) {
	ctx := newTestContext()
	tmpDir, err := os.MkdirTemp("", "testsymlink-*")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalFile := filepath.Join(tmpDir, "original")
	err = os.WriteFile(originalFile, []byte("test"), 0o600)
	assert.NoError(t, err)

	symlinkFile := filepath.Join(tmpDir, "symlink")
	err = os.Symlink(originalFile, symlinkFile)
	assert.NoError(t, err)

	// Valid symlink resolution
	resolvedPath := evalSymlinks(ctx, symlinkFile)
	assert.Equal(t, originalFile, resolvedPath)

	// Invalid symlink should return the input path as-is
	invalidSymlink := filepath.Join(tmpDir, "invalid_symlink")
	resolvedPath = evalSymlinks(ctx, invalidSymlink)
	assert.Equal(t, invalidSymlink, resolvedPath)
}

func TestMkdir(t *testing.T) {
	ctx := newTestContext()

	// Create a temporary base directory for testing.
	basepath := t.TempDir()
	dirPath := filepath.Join(basepath, "test")

	// Test: Creating a new directory.
	created, err := mkdir(ctx, dirPath)
	assert.NoError(t, err)
	assert.True(t, created)

	// Test: Creating an already existing directory.
	created, err = mkdir(ctx, dirPath)
	assert.NoError(t, err)
	assert.False(t, created) // Directory already exists.

	// Test: Attempt to create a directory where a file exists.
	filePath := filepath.Join(basepath, "file")
	file, err := os.Create(filePath)
	assert.NoError(t, err)
	file.Close()

	created, err = mkdir(ctx, filePath)
	assert.Error(t, err)
	assert.False(t, created)

	// 1. Simulate os.Stat error that's not fs.ErrNotExist.
	// Create a directory with no permissions so that stat on a subdirectory fails.
	invalidPath := string([]byte{'i', 'n', 'v', 'a', 'l', 'i', 'd', 0, 'p'})
	created, err = mkdir(ctx, invalidPath)
	assert.Error(t, err)
	assert.False(t, created)

	// 2. Simulate os.MkdirAll failure.
	// Create a parent directory without write permissions.
	parentFile := filepath.Join(basepath, "parentFile")
	err = os.WriteFile(parentFile, []byte("content"), 0o600)
	assert.NoError(t, err)
	childPath := filepath.Join(parentFile, "child")
	created, err = mkdir(ctx, childPath)
	assert.Error(t, err)
	assert.False(t, created)
}

func TestMkfile(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "create file",
			path:    "/tmp/test.txt",
			wantErr: false,
		},
		{
			name:    "create file in non-existent dir",
			path:    "/tmp/dir/test.txt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newTestContext()
			created, err := mkfile(ctx, tt.path)

			if (err != nil) != tt.wantErr {
				t.Errorf("mkfile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !created && !tt.wantErr {
				t.Errorf("mkfile() created = %v, expected to be true", created)
			}

			_, err = os.Stat(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("mkfile() stat error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				err = os.Remove(tt.path)
				if err != nil {
					t.Errorf("mkfile() error removing file: %v", err)
				}
			}
		})
	}
}

func TestMountBlock(t *testing.T) {
	ctx := newTestContext()
	dummyDevice := &Device{
		FullPath: "/dev/dummy",
		Name:     "dummy",
		RealDev:  "/dev/dummy",
	}
	target := "/test/target/block"
	gofsutil.UseMockFS()

	// Case 1: Not mounted – override BindMount to simulate failure.
	gofsutil.GOFSMock.InduceGetMountsError = true
	err := mountBlock(ctx, dummyDevice, target, []string{"rw"}, true, false)
	assert.Error(t, err)
	gofsutil.GOFSMock.InduceGetMountsError = false

	gofsutil.GOFSMockMounts = []gofsutil.Info{{Device: dummyDevice.RealDev}}
	err = mountBlock(ctx, dummyDevice, target, []string{"rw"}, true, false)
	assert.Error(t, err)

	// Case 2: Not mounted – override BindMount to simulate failure.
	gofsutil.GOFSMock.InduceMountError = true
	err = mountBlock(ctx, dummyDevice, target, []string{"rw"}, true, false)
	assert.Error(t, err)
	gofsutil.GOFSMock.InduceMountError = false

	// Case 3: Already mounted – simulate by setting a mount in GOFSMockMounts.
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: dummyDevice.RealDev, Path: target},
	}
	err = mountBlock(ctx, dummyDevice, target, []string{"rw"}, true, false)
	assert.NoError(t, err)

	// Reset mock mounts.
	gofsutil.GOFSMockMounts = nil

	// Case 4: Not mounted – override BindMount to simulate success.
	err = mountBlock(ctx, dummyDevice, target, []string{"rw"}, true, false)
	assert.NoError(t, err)
}

func TestHandleStageMount(t *testing.T) {
	ctx := newTestContext()
	tmpDir, err := os.MkdirTemp("", "test-handle-stage")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dummyDevice := &Device{
		FullPath: "/dev/dummy",
		Name:     "dummy",
		RealDev:  "/dev/dummy",
	}

	// Override gofsutil.FormatAndMount to simulate success.
	gofsutil.UseMockFS()
	err = handleStageMount(ctx, []string{"flag1"}, dummyDevice, "ext4", tmpDir)
	assert.NoError(t, err)

	// Override to simulate a failure.
	gofsutil.GOFSMock.InduceBindMountError = true
	err = handleStageMount(ctx, []string{"flag1"}, dummyDevice, "ext4", tmpDir)
	assert.Error(t, err)
	gofsutil.GOFSMock.InduceBindMountError = false
}

func TestGetDevice(t *testing.T) {
	ctx := newTestContext()

	// Case: Non-existent path.
	_, err := GetDevice(ctx, "/non/existing/path")
	assert.Error(t, err)

	// Case: Existing path but not a block device.
	tmpFile, err := os.CreateTemp("", "test-getdevice")
	assert.NoError(t, err)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())
	_, err = GetDevice(ctx, tmpFile.Name())
	assert.Error(t, err) // Expected error since it is not a block device.

	// Case: Valid block device.
	tmpDir, err := os.MkdirTemp("", "test-getdevice-block")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	blockPath := filepath.Join(tmpDir, "blockdev")
	// Create a mock block device (may require privileges; skip if not possible).
	err = syscall.Mknod(blockPath, syscall.S_IFBLK|0o666, 0)
	if err != nil {
		t.Skip("Skipping block device test, cannot create block device: ", err)
	}
	dev, err := GetDevice(ctx, blockPath)
	assert.NoError(t, err)
	assert.NotNil(t, dev)
}

func TestUnmountStagingMount(t *testing.T) {
	ctx := newTestContext()
	// Create a temporary directory to act as the staging mount target.
	tmpDir, err := os.MkdirTemp("", "test-unmount-staging")
	assert.NoError(t, err)
	// Note: do not defer removal here because unmountStagingMount is expected to remove it.

	dummyDevice := &Device{
		FullPath: "/dev/dummy",
		Name:     "dummy",
		RealDev:  "/dev/dummy",
	}

	// Case 1: No staging mount exists (simulate getDevMounts returns empty).
	gofsutil.GOFSMockMounts = []gofsutil.Info{}
	unmounted, err := unmountStagingMount(ctx, dummyDevice, tmpDir, "")
	// removeWithRetry should remove the directory.
	assert.NoError(t, err)
	assert.True(t, unmounted)
	_, statErr := os.Stat(tmpDir)
	assert.True(t, os.IsNotExist(statErr))

	// Case 2: Staging mount exists.
	// Create a new staging directory.
	stagingDir := filepath.Join(os.TempDir(), "stagingDir")
	err = os.Mkdir(stagingDir, 0o755)
	assert.NoError(t, err)
	defer os.RemoveAll(stagingDir)
	// Simulate a mount on stagingDir.
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: dummyDevice.FullPath, Path: stagingDir},
	}
	// Override Unmount to simulate success.
	unmounted, err = unmountStagingMount(ctx, dummyDevice, stagingDir, "")
	// In this branch the unmount may or may not remove the directory.
	assert.NoError(t, err)
}

func TestRemoveWithRetry(t *testing.T) {
	ctx := newTestContext()
	// Case: Non-existent path.
	err := removeWithRetry(ctx, "/non/existing/path")
	assert.NoError(t, err)

	// Case: Removable file.
	tmpFile := filepath.Join(t.TempDir(), "test_remove_with_retry.txt")
	err = os.WriteFile(tmpFile, []byte("data"), 0o600)
	assert.NoError(t, err)
	err = removeWithRetry(ctx, tmpFile)
	assert.NoError(t, err)
	_, err = os.Stat(tmpFile)
	assert.True(t, os.IsNotExist(err))

	// Case: Non-empty directory (should fail, as rmdir only removes empty directories).
	tmpDir, err := os.MkdirTemp("", "test_remove_with_retry_dir")
	assert.NoError(t, err)
	// Create a file inside to make it non-empty.
	err = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0o600)
	assert.NoError(t, err)
	err = removeWithRetry(ctx, tmpDir)
	assert.Error(t, err)
	// Cleanup manually.
	os.RemoveAll(tmpDir)
}

func TestCreateDirIfNotExist(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) string
		expectErr codes.Code
	}{
		{
			name: "Directory Exists",
			setup: func(t *testing.T) string {
				// Create a temporary directory that exists.
				return t.TempDir()
			},
			expectErr: codes.OK,
		},
		{
			name: "File Exists Instead of Directory",
			setup: func(t *testing.T) string {
				base := t.TempDir()
				filePath := filepath.Join(base, "testfile")
				f, err := os.Create(filePath)
				assert.NoError(t, err)
				f.Close()
				return filePath
			},
			expectErr: codes.FailedPrecondition,
		},
		{
			name: "Directory Does Not Exist, Creation Succeeds",
			setup: func(t *testing.T) string {
				// Return a non-existent directory path.
				return filepath.Join(t.TempDir(), "nonexistent")
			},
			expectErr: codes.OK,
		},
		{
			name: "Invalid Stat Path",
			setup: func(_ *testing.T) string {
				// Use an invalid path (contains a null byte) to force os.Stat error that is not os.IsNotExist.
				return string([]byte{'i', 'n', 'v', 'a', 'l', 'i', 'd', 0, 'p'})
			},
			expectErr: codes.Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			path := tt.setup(t)

			err := createDirIfNotExist(ctx, path, "array1")
			if tt.expectErr == codes.OK {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				st, _ := status.FromError(err)
				assert.Equal(t, tt.expectErr, st.Code())
			}
		})
	}
}

func TestGetDevMounts_Multipath(t *testing.T) {
	ctx := newTestContext()

	sysDevice := &Device{
		RealDev:  "/dev/sda",
		FullPath: "/dev/disk/by-id/sda",
	}

	gofsutil.UseMockFS()
	// Updated mock mount entry for multipath:
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{
			Device: "/dev/mapper/mpathX", // Use multipath device here
			Source: "/dev/mapper/mpathX",
			Path:   "/mnt/data",
		},
	}

	// Override readFileFunc in the test
	originalReadFile := readFileFunc
	defer func() { readFileFunc = originalReadFile }()

	readFileFunc = func(filename string) ([]byte, error) {
		if strings.Contains(filename, "dm/name") {
			return []byte("mpathX\n"), nil // Simulate a valid multipath name
		}
		return nil, errors.New("file not found")
	}

	// Call the function
	devMnts, err := getDevMounts(ctx, sysDevice)

	// Assertions
	assert.NoError(t, err)
	assert.Len(t, devMnts, 1, "Expected exactly 1 mount entry")
	assert.Equal(t, "/mnt/data", devMnts[0].Path, "Unexpected mount path")
	assert.Equal(t, "/dev/mapper/mpathX", devMnts[0].Source, "Unexpected mount source")
}
