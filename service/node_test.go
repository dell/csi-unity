/*
 *
 * Copyright © 2021-2024 Dell Inc. or its subsidiaries. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

/*
 Copyright © 2020-2025 Dell Inc. or its subsidiaries. All Rights Reserved.

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
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"bou.ke/monkey"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/service/csiutils"
	"github.com/dell/gobrick"
	"github.com/dell/gofsutil"
	"github.com/dell/goiscsi"
	"github.com/dell/gounity"
	gounitytypes "github.com/dell/gounity/apitypes"
	gounitymocks "github.com/dell/gounity/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGetTopology(t *testing.T) {
	// Mock the connectedSystemID and Name
	connectedSystemID = []string{"array1/NFS", "array2/FC"}
	Name = "csi-unity.dellemc.com"

	expectedTopology := map[string]string{
		"csi-unity.dellemc.com/array1-NFS": "true",
		"csi-unity.dellemc.com/array2-FC":  "true",
	}

	topology := getTopology()

	assert.Equal(t, expectedTopology, topology)
}

// Mock dependencies
type MockFCConnector struct {
	mock.Mock
}

func (m *MockFCConnector) ConnectVolume(ctx context.Context, volumeInfo gobrick.FCVolumeInfo) (gobrick.Device, error) {
	args := m.Called(ctx, volumeInfo)
	return args.Get(0).(gobrick.Device), args.Error(1)
}

func (m *MockFCConnector) GetInitiatorPorts(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockFCConnector) DisconnectVolumeByDeviceName(ctx context.Context, deviceName string) error {
	args := m.Called(ctx, deviceName)
	return args.Error(0)
}

func TestConnectFCDevice(t *testing.T) {
	ctx := context.Background()
	lun := 1
	data := publishContextData{
		fcTargets: []string{"wwn1", "wwn2"},
	}

	expectedDevice := gobrick.Device{
		Name: "deviceName",
	}

	mockConnector := new(MockFCConnector)
	s := &service{
		fcConnector: mockConnector,
	}
	mockConnector.On("ConnectVolume", mock.Anything, gobrick.FCVolumeInfo{
		Targets: []gobrick.FCTargetInfo{
			{WWPN: "wwn1"},
			{WWPN: "wwn2"},
		},
		Lun: lun,
	}).Return(expectedDevice, nil).Once()

	// Success case
	device, err := s.connectFCDevice(ctx, lun, data)
	assert.NoError(t, err)
	assert.Equal(t, expectedDevice, device)

	// Failure case: ConnectVolume returns an error
	mockConnector.On("ConnectVolume", mock.Anything, gobrick.FCVolumeInfo{
		Targets: []gobrick.FCTargetInfo{
			{WWPN: "wwn1"},
			{WWPN: "wwn2"},
		},
		Lun: lun,
	}).Return(gobrick.Device{}, assert.AnError).Once()

	_, err = s.connectFCDevice(ctx, lun, data)
	assert.Error(t, err)
}

func (m *MockISCSIConnector) ConnectVolume(ctx context.Context, volumeInfo gobrick.ISCSIVolumeInfo) (gobrick.Device, error) {
	args := m.Called(ctx, volumeInfo)
	return args.Get(0).(gobrick.Device), args.Error(1)
}

func (m *MockISCSIConnector) DisconnectVolumeByDeviceName(ctx context.Context, deviceName string) error {
	args := m.Called(ctx, deviceName)
	return args.Error(0)
}

func (m *MockISCSIConnector) GetInitiatorName(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}

func TestConnectISCSIDevice(t *testing.T) {
	ctx := context.Background()
	lun := 1
	data := publishContextData{
		iscsiTargets: []goiscsi.ISCSITarget{
			{Target: "iqn.2023-10.com.example:target1", Portal: "10.0.0.1:3260"},
			{Target: "iqn.2023-10.com.example:target2", Portal: "10.0.0.2:3260"},
		},
	}

	mockConnector := new(MockISCSIConnector)
	s := &service{
		iscsiConnector: mockConnector,
	}

	targets := []gobrick.ISCSITargetInfo{
		{Target: "iqn.2023-10.com.example:target1", Portal: "10.0.0.1:3260"},
		{Target: "iqn.2023-10.com.example:target2", Portal: "10.0.0.2:3260"},
	}

	expectedDevice := gobrick.Device{WWN: "/dev/sdX"}

	mockConnector.On("ConnectVolume", mock.Anything, gobrick.ISCSIVolumeInfo{
		Targets: targets,
		Lun:     lun,
	}).Return(expectedDevice, nil).Once()

	device, err := s.connectISCSIDevice(ctx, lun, data)
	assert.NoError(t, err)
	assert.Equal(t, expectedDevice, device)
}

func TestConnectDevice(t *testing.T) {
	ctx := context.Background()
	data := publishContextData{
		volumeLUNAddress: 1,
		fcTargets:        []string{"wwn1", "wwn2"},
		iscsiTargets: []goiscsi.ISCSITarget{
			{Target: "iqn.2023-10.com.example:target1", Portal: "10.0.0.1:3260"},
		},
	}

	mockFCConnector := new(MockFCConnector)
	mockISCSIConnector := new(MockISCSIConnector)
	s := &service{
		fcConnector:    mockFCConnector,
		iscsiConnector: mockISCSIConnector,
	}

	expectedDevice := gobrick.Device{Name: "sdX"}
	devicePath := "/dev/sdX"

	mockFCConnector.On("ConnectVolume", mock.Anything, mock.Anything).Return(expectedDevice, nil).Once()
	mockISCSIConnector.On("ConnectVolume", mock.Anything, mock.Anything).Return(expectedDevice, nil).Once()

	t.Run("FC Connection", func(t *testing.T) {
		path, err := s.connectDevice(ctx, data, true)
		assert.NoError(t, err)
		assert.Equal(t, devicePath, path)
	})

	t.Run("iSCSI Connection", func(t *testing.T) {
		path, err := s.connectDevice(ctx, data, false)
		assert.NoError(t, err)
		assert.Equal(t, devicePath, path)
	})
}

func TestAddNodeInformationIntoArray(t *testing.T) {
	// Setup common variables and helper for bool pointer
	truePtr := func(b bool) *bool { return &b }
	testConf.service.opts.NodeName = "test-node"
	testConf.service.opts.LongNodeName = "long-test-node"
	ctx := context.Background()

	// Configure storage array with a mocked Unity client
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	array := &StorageArrayConfig{
		ArrayID:                   arrayID,
		UnityClient:               mockUnity,
		SkipCertificateValidation: truePtr(true),
		IsDefault:                 truePtr(true),
	}
	testConf.service.arrays.Store(arrayID, array)

	// Setup host content and related structures
	hostContent := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "test-node"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hostContent,
	}
	expectedIPInterface := gounitytypes.IPInterfaceEntries{
		IPInterfaceContent: gounitytypes.IPInterfaceContent{IPAddress: "test-node"},
	}
	host := gounitytypes.Host{HostContent: hostContent}
	hostIPPortPtr := &gounitytypes.HostIPPort{HostIPContent: hostContent}
	expectedHostIPPort := gounitytypes.HostIPPort{HostIPContent: hostContent}

	// Set up mock expectations
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&host, nil).Once()
	mockUnity.On("FindHostIPPortByID", mock.Anything, "ip-port-1").Return(hostIPPortPtr, nil).Once()
	mockUnity.On("CreateHostIPPort", mock.Anything, "id", mock.Anything).Return(&expectedHostIPPort, nil)

	// Execute the function under test
	err := testConf.service.addNodeInformationIntoArray(ctx, array)
	assert.NoError(t, err)

	// Autoprobe Error
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(errors.New("test error")).Twice()
	err = testConf.service.addNodeInformationIntoArray(ctx, array)
	assert.Error(t, err)

	originalIscsiClient := testConf.service.iscsiClient
	testConf.service.iscsiClient = goiscsi.NewMockISCSI(nil)
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&host, nil).Once()
	mockUnity.On("CreateHostInitiator", mock.Anything, "id", mock.Anything, mock.Anything).Return(nil, nil).Once()
	mockUnity.On("FindHostIPPortByID", mock.Anything, "ip-port-1").Return(hostIPPortPtr, nil).Once()
	mockUnity.On("ListIscsiIPInterfaces", mock.Anything).Return([]gounitytypes.IPInterfaceEntries{expectedIPInterface}, nil).Once()
	err = testConf.service.addNodeInformationIntoArray(ctx, array)
	assert.NoError(t, err)
	testConf.service.iscsiClient = originalIscsiClient

	// Case FindHostByName returns ErrorHostNotFound
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, mock.Anything).Return(nil, gounity.ErrorHostNotFound).Twice()
	mockUnity.On("CreateHost", mock.Anything, "long-test-node", mock.Anything).Return(&h, nil)
	err = testConf.service.addNodeInformationIntoArray(ctx, array)
	assert.NoError(t, err)

	// FindHostByName
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, mock.Anything).Return(nil, errors.New("test error")).Twice()
	err = testConf.service.addNodeInformationIntoArray(ctx, array)
	assert.Error(t, err)
}

func TestNodeStageVolume(t *testing.T) {
	arrayID := "testarrayid"
	gofsutil.UseMockFS()
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})
	testConf.service.opts.NodeName = "test-node"
	testConf.service.opts.LongNodeName = "long-test-node"
	hc := gounitytypes.HostContent{
		ID: "hostID",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
		FcInitiators: []gounitytypes.Initiators{
			{ID: "fc-initiator-1"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
	}
	hi := mockAnInitiator("fc-initiator-1")
	hi.HostInitiatorContent.Paths = []gounitytypes.Path{
		{ID: "fc-initiator-1"},
	}
	hip := gounitytypes.HostInitiatorPath{
		HostInitiatorPathContent: gounitytypes.HostInitiatorPathContent{
			FcPortID: gounitytypes.FcPortID{ID: "fc-initiator-1"},
		},
	}
	fcp := gounitytypes.FcPort{
		FcPortContent: gounitytypes.FcPortContent{
			Wwn: "20:00:00:00:c9:a1:b2:c3:d4:e5:f6:78:90:ab:cd:ef",
		},
	}
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil)
	mockUnity.On("FindHostInitiatorByID", mock.Anything, "fc-initiator-1").Return(hi, nil)
	mockUnity.On("FindHostInitiatorPathByID", mock.Anything, "fc-initiator-1").Return(&hip, nil)
	mockUnity.On("FindFcPortByID", mock.Anything, "fc-initiator-1").Return(&fcp, nil)
	orgIPReachable := csiutils.IPReachable
	csiutils.IPReachable = func(_ context.Context, _, _ string, _ int) bool {
		return true
	}
	defer func() { csiutils.IPReachable = orgIPReachable }()
	testConf.service.iscsiClient = goiscsi.NewMockISCSI(nil)

	type testCase struct {
		name      string
		req       *csi.NodeStageVolumeRequest
		mockSetup func()
		expRes    *csi.NodeStageVolumeResponse
		expError  string
		expCode   codes.Code
	}

	testCases := []testCase{
		{
			name: "Empty volume ID",
			req: &csi.NodeStageVolumeRequest{
				VolumeId: "",
			},
			expError: "volumeId can't be empty",
			expCode:  codes.InvalidArgument,
		},
		{
			name: "Empty staging target path",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				StagingTargetPath: "",
			},
			expError: "staging target path required",
			expCode:  codes.InvalidArgument,
		},
		{
			name: "Nil volume capability",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				StagingTargetPath: "test-staging-path",
			},
			expError: "volume capability is required",
			expCode:  codes.InvalidArgument,
		},
		{
			name: "Nil access mode",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				StagingTargetPath: "test-staging-path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							FsType: "ext4",
						},
					},
				},
			},
			expError: "access mode is required",
			expCode:  codes.InvalidArgument,
		},
		{
			name: "Unknown protocol",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-ABC-testarrayid-sv_250440",
				StagingTargetPath: "test-staging-path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							FsType: "ext4",
						},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			expError: "Invalid value provided for Protocol",
			expCode:  codes.InvalidArgument,
		},
		{
			name: "NFS protocol - Success",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-NFS-testarrayid-sv_250440",
				StagingTargetPath: "test-staging-path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							FsType: "ext4",
						},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			mockSetup: func() {
				fc := gounitytypes.FileContent{
					ID: "id",
					NFSShare: []gounitytypes.Share{
						{
							ID:   "nfs-id",
							Path: "/",
							ParentSnap: gounitytypes.StorageResource{
								ID: "",
							},
						},
					},
				}
				fs := gounitytypes.Filesystem{
					FileContent: fc,
				}
				nfsShare := gounitytypes.NFSShare{
					NFSShareContent: gounitytypes.NFSShareContent{
						ID:   "nfs-share-123",
						Name: "TestNFSShare",
						Filesystem: gounitytypes.Pool{
							ID:   "fs-456",
							Name: "TestFilesystem",
						},
						RootAccessHosts: []gounitytypes.HostContent{
							{
								ID:      "hostID",
								Name:    "RootAccessHost1",
								Address: "192.168.1.4",
							},
						},
						ExportPaths: []string{"192.168.1.4:/export/path1"},
					},
				}
				nasServer := gounitytypes.NASServer{
					NASServerContent: gounitytypes.NASServerContent{
						ID:   "nas-123",
						Name: "MyNASServer",
						NFSServer: gounitytypes.NFSServer{
							ID:           "nfs-456",
							Name:         "MyNFSServer",
							NFSv3Enabled: true,
							NFSv4Enabled: true,
						},
					},
				}
				mockUnity.On("FindFilesystemByID", mock.Anything, "sv_250440").Return(&fs, nil).Once()
				mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-id").Return(&nfsShare, nil).Once()
				mockUnity.On("FindNASServerByID", mock.Anything, "").Return(&nasServer, nil).Once()
			},
			expRes: &csi.NodeStageVolumeResponse{},
		},
		{
			name: "iSCSI protocol - Volume not found",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				StagingTargetPath: "test-staging-path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			mockSetup: func() {
				mockUnity.On("FindVolumeByID", mock.Anything, "sv_250440").Return(nil, errors.New("not found")).Once()
			},
			expError: "Volume not found",
			expCode:  codes.NotFound,
		},
		{
			name: "iSCSI protocol - Volume has not been published to this node",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				StagingTargetPath: "test-staging-path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			mockSetup: func() {
				volume := gounitytypes.Volume{
					VolumeContent: gounitytypes.VolumeContent{
						ResourceID: "vol-123",
						Wwn:        "6000ABC123456789",
					},
				}
				mockUnity.On("FindVolumeByID", mock.Anything, "sv_250440").Return(&volume, nil).Once()
			},
			expError: "Volume  has not been published to this node",
			expCode:  codes.Aborted,
		},
		{
			name: "iSCSI protocol - Failed to connect device",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				StagingTargetPath: "test-staging-path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			mockSetup: func() {
				volume := gounitytypes.Volume{
					VolumeContent: gounitytypes.VolumeContent{
						ResourceID: "vol-123",
						Wwn:        "6000ABC123456789",
						HostAccessResponse: []gounitytypes.HostAccessResponse{
							{
								HostContent: gounitytypes.HostContent{
									ID:      "hostID",
									Name:    "RootAccessHost1",
									Address: "192.168.1.4",
								},
							},
						},
					},
				}
				ipInterfaces := []gounitytypes.IPInterfaceEntries{
					{
						IPInterfaceContent: gounitytypes.IPInterfaceContent{
							IPAddress: "1.2.3.4",
						},
					},
				}
				mockUnity.On("FindVolumeByID", mock.Anything, "sv_250440").Return(&volume, nil).Once()
				mockUnity.On("ListIscsiIPInterfaces", mock.Anything).Return(ipInterfaces, nil).Once()
			},
			expError: "Unable to find device after multiple discovery attempts",
			expCode:  codes.Internal,
		},
		{
			name: "iSCSI protocol - Success",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				StagingTargetPath: "test-staging-path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			mockSetup: func() {
				volume := gounitytypes.Volume{
					VolumeContent: gounitytypes.VolumeContent{
						ResourceID: "vol-123",
						Wwn:        "6000ABC123456789",
						HostAccessResponse: []gounitytypes.HostAccessResponse{
							{
								HostContent: gounitytypes.HostContent{
									ID:      "hostID",
									Name:    "RootAccessHost1",
									Address: "192.168.1.4",
								},
							},
						},
					},
				}
				ipInterfaces := []gounitytypes.IPInterfaceEntries{
					{
						IPInterfaceContent: gounitytypes.IPInterfaceContent{
							IPAddress: "10.0.0.1",
						},
					},
				}
				mockConnector := new(MockISCSIConnector)
				testConf.service.iscsiConnector = mockConnector
				targets := []gobrick.ISCSITargetInfo{
					{Target: "iqn.1992-04.com.mock:600009700bcbb70e3287017400000000", Portal: "10.0.0.1:3260"},
				}
				expectedDevice := gobrick.Device{WWN: "/dev/sdX"}

				mockUnity.On("FindVolumeByID", mock.Anything, "sv_250440").Return(&volume, nil).Once()
				mockUnity.On("ListIscsiIPInterfaces", mock.Anything).Return(ipInterfaces, nil).Once()
				mockConnector.On("ConnectVolume", mock.Anything, gobrick.ISCSIVolumeInfo{
					Targets: targets,
					Lun:     0,
				}).Return(expectedDevice, nil).Once()
			},
			expRes: &csi.NodeStageVolumeResponse{},
		},
		{
			name: "FC protocol - Volume has not been published to this node",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-FC-testarrayid-sv_250440",
				StagingTargetPath: "test-staging-path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			mockSetup: func() {
				volume := gounitytypes.Volume{
					VolumeContent: gounitytypes.VolumeContent{
						ResourceID: "vol-123",
						Wwn:        "6000ABC123456789",
					},
				}
				mockUnity.On("FindVolumeByID", mock.Anything, "sv_250440").Return(&volume, nil).Once()
			},
			expError: "Volume  has not been published to this node",
			expCode:  codes.Aborted,
		},
		{
			name: "FC protocol - Failed to connect device",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-FC-testarrayid-sv_250440",
				StagingTargetPath: "test-staging-path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			mockSetup: func() {
				volume := gounitytypes.Volume{
					VolumeContent: gounitytypes.VolumeContent{
						ResourceID: "vol-123",
						Wwn:        "6000ABC123456789",
						HostAccessResponse: []gounitytypes.HostAccessResponse{
							{
								HostContent: gounitytypes.HostContent{
									ID:      "hostID",
									Name:    "RootAccessHost1",
									Address: "192.168.1.4",
								},
							},
						},
					},
				}
				ipInterfaces := []gounitytypes.IPInterfaceEntries{
					{
						IPInterfaceContent: gounitytypes.IPInterfaceContent{
							IPAddress: "1.2.3.4",
						},
					},
				}
				mockUnity.On("FindVolumeByID", mock.Anything, "sv_250440").Return(&volume, nil).Once()
				mockUnity.On("ListIscsiIPInterfaces", mock.Anything).Return(ipInterfaces, nil).Once()
			},
			expError: "Unable to find device after multiple discovery attempts",
			expCode:  codes.Internal,
		},
		{
			name: "FC protocol - Success",
			req: &csi.NodeStageVolumeRequest{
				VolumeId:          "csivol-8cd275903e-FC-testarrayid-sv_250440",
				StagingTargetPath: "test-staging-path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			mockSetup: func() {
				volume := gounitytypes.Volume{
					VolumeContent: gounitytypes.VolumeContent{
						ResourceID: "vol-123",
						Wwn:        "6000ABC123456789",
						HostAccessResponse: []gounitytypes.HostAccessResponse{
							{
								HostContent: gounitytypes.HostContent{
									ID:      "hostID",
									Name:    "RootAccessHost1",
									Address: "192.168.1.4",
								},
							},
						},
					},
				}
				mockConnector := new(MockFCConnector)
				testConf.service.fcConnector = mockConnector
				targets := []gobrick.FCTargetInfo{
					{WWPN: "d4e5f67890abcdef"},
				}
				expectedDevice := gobrick.Device{WWN: "/dev/sdX"}

				mockUnity.On("FindVolumeByID", mock.Anything, "sv_250440").Return(&volume, nil).Once()
				mockConnector.On("ConnectVolume", mock.Anything, gobrick.FCVolumeInfo{
					Targets: targets,
					Lun:     0,
				}).Return(expectedDevice, nil).Once()
			},
			expRes: &csi.NodeStageVolumeResponse{},
		},
	}

	testConf.service.opts.IsVolumeHealthMonitorEnabled = false
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mockSetup != nil {
				tt.mockSetup()
			}
			resp, err := testConf.service.NodeStageVolume(context.Background(), tt.req)
			if tt.expError != "" {
				assert.Nil(t, resp)
				assert.ErrorContains(t, err, tt.expError)
				assert.Equal(t, tt.expCode, status.Code(err))
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expRes, resp)
			}
		})
	}
}

func TestEphemeralNodeUnpublish(t *testing.T) {
	svc := &service{}

	req := &csi.NodeUnpublishVolumeRequest{
		VolumeId:   "vol-test",
		TargetPath: "/mnt/test",
	}

	// Helper function to patch methods
	patchMethod := func(method string, fn interface{}) {
		monkey.PatchInstanceMethod(reflect.TypeOf(svc), method, fn)
	}

	// Successful case
	patchMethod("NodeUnpublishVolume", func(_ *service, _ context.Context, _ *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
		return &csi.NodeUnpublishVolumeResponse{}, nil
	})
	patchMethod("NodeUnstageVolume", func(_ *service, _ context.Context, _ *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
		return &csi.NodeUnstageVolumeResponse{}, nil
	})
	patchMethod("ControllerUnpublishVolume", func(_ *service, _ context.Context, _ *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	})
	patchMethod("DeleteVolume", func(_ *service, _ context.Context, _ *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
		return &csi.DeleteVolumeResponse{}, nil
	})

	resp, err := svc.ephemeralNodeUnpublish(context.Background(), req, "vol-test")
	assert.NoError(t, err)
	assert.NotNil(t, resp)

	// Failure cases
	testCases := []struct {
		desc     string
		patch    func()
		expected error
	}{
		{
			"NodeUnpublishVolume failure",
			func() {
				patchMethod("NodeUnpublishVolume", func(_ *service, _ context.Context, _ *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
					return nil, errors.New("failed to unpublish volume")
				})
			},
			errors.New("failed to unpublish volume"),
		},
		{
			"NodeUnstageVolume failure",
			func() {
				patchMethod("NodeUnpublishVolume", func(_ *service, _ context.Context, _ *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
					return &csi.NodeUnpublishVolumeResponse{}, nil
				})
				patchMethod("NodeUnstageVolume", func(_ *service, _ context.Context, _ *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
					return nil, errors.New("failed to unstage volume")
				})
			},
			errors.New("failed to unstage volume"),
		},
		{
			"ControllerUnpublishVolume failure",
			func() {
				patchMethod("NodeUnpublishVolume", func(_ *service, _ context.Context, _ *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
					return &csi.NodeUnpublishVolumeResponse{}, nil
				})
				patchMethod("NodeUnstageVolume", func(_ *service, _ context.Context, _ *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
					return &csi.NodeUnstageVolumeResponse{}, nil
				})
				patchMethod("ControllerUnpublishVolume", func(_ *service, _ context.Context, _ *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
					return nil, errors.New("failed to unpublish volume")
				})
			},
			errors.New("failed to unpublish volume"),
		},
		{
			"DeleteVolume failure",
			func() {
				patchMethod("ControllerUnpublishVolume", func(_ *service, _ context.Context, _ *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
					return &csi.ControllerUnpublishVolumeResponse{}, nil
				})
				patchMethod("DeleteVolume", func(_ *service, _ context.Context, _ *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
					return nil, errors.New("failed to delete volume")
				})
			},
			errors.New("failed to delete volume"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			tc.patch()
			resp, err := svc.ephemeralNodeUnpublish(context.Background(), req, "vol-test")
			assert.Error(t, err)
			assert.Nil(t, resp)
		})
	}

	// Unpatch all methods
	monkey.UnpatchAll()
}

func TestNodeGetInfo(t *testing.T) {
	// Test case: Successful execution
	testConf.service.opts.NodeName = "test-node"
	testConf.service.opts.LongNodeName = "long-test-node"
	testConf.service.arrays = &sync.Map{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &csi.NodeGetInfoRequest{}

	res, err := testConf.service.NodeGetInfo(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, testConf.service.opts.NodeName+","+testConf.service.opts.LongNodeName, res.NodeId)
	assert.NotNil(t, res.AccessibleTopology)
	assert.Equal(t, testConf.service.opts.MaxVolumesPerNode, res.MaxVolumesPerNode)

	// Test case: Context timeout
	testConf.service.arrays.Store("test-array", &StorageArrayConfig{
		IsHostAdded: false,
	})

	ctxTimeout, cancelTimeout := context.WithTimeout(ctx, 1*time.Second)
	defer cancelTimeout()
	res, err = testConf.service.NodeGetInfo(ctxTimeout, req)
	assert.Error(t, err)
	assert.Nil(t, res)
}

func TestNodeUnpublishVolume(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup common test data
	volume := gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			ResourceID: "vol-123",
			Name:       "TestVolume",
			HostAccessResponse: []gounitytypes.HostAccessResponse{
				{
					HostContent: gounitytypes.HostContent{
						ID:   "hostID",
						Name: "TestHost",
					},
					HLU: 10,
				},
			},
			Wwn:                    "6000abc123456789",
			IsThinEnabled:          true,
			IsDataReductionEnabled: false,
		},
	}

	fc := gounitytypes.FileContent{
		ID:   "id",
		Name: "name",
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}

	// Configure service options and mock array
	testConf.service.opts.NodeName = "test-node"
	testConf.service.opts.LongNodeName = "long-test-node"
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:                   arrayID,
		UnityClient:               mockUnity,
		SkipCertificateValidation: &boolTrue,
		IsDefault:                 &boolTrue,
	})

	// Subtest: Ephemeral volume error case
	t.Run("EphemeralVolumeError", func(t *testing.T) {
		testConf.service.opts.EnvEphemeralStagingTargetPath = t.TempDir()
		volID := "csivol-8cd275903e-iSCSI-testarrayid-sv_250440"
		tmpFile := filepath.Join(testConf.service.opts.EnvEphemeralStagingTargetPath+volID, "id")
		err := os.MkdirAll(filepath.Dir(tmpFile), 0o600)
		assert.NoError(t, err)
		err = os.WriteFile(tmpFile, []byte("real-vol-id"), 0o600)
		assert.NoError(t, err)
		defer os.RemoveAll(filepath.Dir(tmpFile))

		req := &csi.NodeUnpublishVolumeRequest{
			VolumeId:   volID,
			TargetPath: "/mnt/test",
		}
		resp, err := testConf.service.NodeUnpublishVolume(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, resp)

		// Clear the staging target path for subsequent tests.
		testConf.service.opts.EnvEphemeralStagingTargetPath = ""
	})

	// Subtest: Volume not found (gounity ErrorVolumeNotFound)
	t.Run("VolumeNotFound_ErrorVolumeNotFound", func(t *testing.T) {
		req := &csi.NodeUnpublishVolumeRequest{
			VolumeId:   "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
			TargetPath: "/mnt/test",
		}
		mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
		mockUnity.On("FindVolumeByID", mock.Anything, "sv_250440").Return(nil, gounity.ErrorVolumeNotFound).Once()

		resp, err := testConf.service.NodeUnpublishVolume(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	// Subtest: Volume not found (random error)
	t.Run("VolumeNotFound_RandomError", func(t *testing.T) {
		req := &csi.NodeUnpublishVolumeRequest{
			VolumeId:   "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
			TargetPath: "/mnt/test",
		}
		mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
		mockUnity.On("FindVolumeByID", mock.Anything, "sv_250440").Return(nil, errors.New("random error")).Once()

		resp, err := testConf.service.NodeUnpublishVolume(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	// Subtest: Successful volume unpublish for non-NFS volume
	t.Run("VolumeUnpublishSuccess", func(t *testing.T) {
		req := &csi.NodeUnpublishVolumeRequest{
			VolumeId:   "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
			TargetPath: "/mnt/test",
		}
		mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
		mockUnity.On("FindVolumeByID", mock.Anything, "sv_250440").Return(&volume, nil).Once()

		resp, err := testConf.service.NodeUnpublishVolume(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	// Subtest: NFSShare not found
	t.Run("NFSShareNotFound", func(t *testing.T) {
		req := &csi.NodeUnpublishVolumeRequest{
			VolumeId:   "csivol-8cd275903e-NFS-testarrayid-sv_250440",
			TargetPath: "/mnt/test",
		}
		mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
		mockUnity.On("FindFilesystemByID", mock.Anything, "sv_250440").Return(&fs, nil).Once()

		resp, err := testConf.service.NodeUnpublishVolume(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	// Subtest: Successful NFS unpublish
	t.Run("NFSUnpublishSuccess", func(t *testing.T) {
		// Setup Filesystem with an NFSShare
		fc := gounitytypes.FileContent{
			ID: "id",
			NFSShare: []gounitytypes.Share{
				{
					ID:   "nfs-id",
					Path: "/",
					ParentSnap: gounitytypes.StorageResource{
						ID: "",
					},
				},
			},
		}
		fs := gounitytypes.Filesystem{
			FileContent: fc,
		}

		nfsShare := gounitytypes.NFSShare{
			NFSShareContent: gounitytypes.NFSShareContent{
				ID:   "nfs-share-123",
				Name: "TestNFSShare",
				Filesystem: gounitytypes.Pool{
					ID:   "fs-456",
					Name: "TestFilesystem",
				},
				RootAccessHosts: []gounitytypes.HostContent{
					{
						ID:      "hostID",
						Name:    "RootAccessHost1",
						Address: "192.168.1.4",
					},
				},
				ExportPaths: []string{"192.168.1.4:/export/path1"},
			},
		}
		nasServer := gounitytypes.NASServer{
			NASServerContent: gounitytypes.NASServerContent{
				ID:   "nas-123",
				Name: "MyNASServer",
				NFSServer: gounitytypes.NFSServer{
					ID:           "nfs-456",
					Name:         "MyNFSServer",
					NFSv3Enabled: true,
					NFSv4Enabled: true,
				},
			},
		}

		req := &csi.NodeUnpublishVolumeRequest{
			VolumeId:   "csivol-8cd275903e-NFS-testarrayid-sv_250440",
			TargetPath: "/mnt/test",
		}
		mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
		mockUnity.On("FindFilesystemByID", mock.Anything, "sv_250440").Return(&fs, nil).Once()
		mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-id").Return(&nfsShare, nil).Once()
		mockUnity.On("FindNASServerByID", mock.Anything, mock.Anything).Return(&nasServer, nil).Once()

		resp, err := testConf.service.NodeUnpublishVolume(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestNodeGetCapabilities(t *testing.T) {
	// Test case: When volume health monitor is disabled
	testConf.service.opts.IsVolumeHealthMonitorEnabled = false
	resp, err := testConf.service.NodeGetCapabilities(context.Background(), &csi.NodeGetCapabilitiesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, []*csi.NodeServiceCapability{
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
					Type: csi.NodeServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
				},
			},
		},
	}, resp.Capabilities)

	// Test case: When volume health monitor is enabled
	testConf.service.opts.IsVolumeHealthMonitorEnabled = true
	resp, err = testConf.service.NodeGetCapabilities(context.Background(), &csi.NodeGetCapabilitiesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, []*csi.NodeServiceCapability{
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
					Type: csi.NodeServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
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
	}, resp.Capabilities)
}

func TestNodeGetVolumeStats(t *testing.T) {
	tempDir := t.TempDir()
	arrayID := "testarrayid"
	gofsutil.UseMockFS()
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})
	testConf.service.opts.AutoProbe = false

	tests := []struct {
		name          string
		req           *csi.NodeGetVolumeStatsRequest
		mockSetup     func()
		expectedResp  *csi.NodeGetVolumeStatsResponse
		expectedError string
		expectedCode  codes.Code
	}{
		{
			name: "Empty volume ID",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId: "",
			},
			expectedError: "volumeId is mandatory parameter",
			expectedCode:  codes.InvalidArgument,
		},
		{
			name: "Invalid volume ID format",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId: "invalid-format",
			},
			expectedError: "volumeId can't be empty",
			expectedCode:  codes.InvalidArgument,
		},
		{
			name: "Node probe error",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId: "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
			},
			mockSetup: func() {
				mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(errors.New("probe error")).Once()
			},
			expectedError: "Unable Get basic system info from Unity. Verify hostname/IP Address of unity.",
			expectedCode:  codes.FailedPrecondition,
		},
		{
			name: "Volume path required",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId: "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
			},
			mockSetup: func() {
				mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
			},
			expectedError: "Volume path required",
			expectedCode:  codes.InvalidArgument,
		},
		{
			name: "Filesystem not found with `Filesystem not found` error",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId:   "csivol-8cd275903e-NFS-testarrayid-sv_250440",
				VolumePath: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/csivol-8cd275903e/33f9cb0a-27b6-4911-a8e9-154577b6e6fe",
			},
			mockSetup: func() {
				mockUnity.On("FindFilesystemByID", mock.Anything, mock.Anything).Return(nil, gounity.ErrorFilesystemNotFound).Once()
			},
			expectedResp: &csi.NodeGetVolumeStatsResponse{
				VolumeCondition: &csi.VolumeCondition{
					Abnormal: true,
					Message:  "Filesystem not found",
				},
			},
		},
		{
			name: "Filesystem not found with `random error` error",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId:   "csivol-8cd275903e-NFS-testarrayid-sv_250440",
				VolumePath: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/csivol-8cd275903e/33f9cb0a-27b6-4911-a8e9-154577b6e6fe",
			},
			mockSetup: func() {
				mockUnity.On("FindFilesystemByID", mock.Anything, mock.Anything).Return(nil, errors.New("random error")).Once()
			},
			expectedResp:  nil,
			expectedError: "FileSystem not found. [random error]",
			expectedCode:  codes.NotFound,
		},
		{
			name: "Volume not found with `volume not found` error",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId:   "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				VolumePath: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/csivol-8cd275903e/33f9cb0a-27b6-4911-a8e9-154577b6e6fe",
			},
			mockSetup: func() {
				mockUnity.On("FindVolumeByID", mock.Anything, mock.Anything).Return(nil, gounity.ErrorVolumeNotFound).Once()
			},
			expectedResp: &csi.NodeGetVolumeStatsResponse{
				VolumeCondition: &csi.VolumeCondition{
					Abnormal: true,
					Message:  "Volume not found",
				},
			},
		},
		{
			name: "Volume not found with `random error` error",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId:   "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				VolumePath: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/csivol-8cd275903e/33f9cb0a-27b6-4911-a8e9-154577b6e6fe",
			},
			mockSetup: func() {
				mockUnity.On("FindVolumeByID", mock.Anything, mock.Anything).Return(nil, errors.New("random error")).Once()
			},
			expectedResp:  nil,
			expectedError: "Volume not found. [random error]",
			expectedCode:  codes.NotFound,
		},
		{
			name: "Volume path not mounted",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId:   "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				VolumePath: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/csivol-8cd275903e/33f9cb0a-27b6-4911-a8e9-154577b6e6fe",
			},
			mockSetup: func() {
				gofsutil.GOFSMockMounts = []gofsutil.Info{
					{
						Path: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/csivol-8cd275903e/33f9cb0a-27b6-4911-a8e9-154577b6e6fe",
					},
				}
				mockUnity.On("FindVolumeByID", mock.Anything, mock.Anything).Return(nil, nil).Once()
			},
			expectedResp: &csi.NodeGetVolumeStatsResponse{
				VolumeCondition: &csi.VolumeCondition{
					Abnormal: true,
					Message:  "Volume path is not mounted",
				},
			},
		},
		{
			name: "Volume path not accessible",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId:   "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				VolumePath: "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/csivol-8cd275903e/33f9cb0a-27b6-4911-a8e9-154577b6e6fe",
			},
			mockSetup: func() {
				gofsutil.GOFSMockMounts = []gofsutil.Info{
					{
						Device: "some-device",
						Path:   "/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/csivol-8cd275903e/33f9cb0a-27b6-4911-a8e9-154577b6e6fe",
					},
				}
				mockUnity.On("FindVolumeByID", mock.Anything, mock.Anything).Return(nil, nil).Once()
			},
			expectedResp: &csi.NodeGetVolumeStatsResponse{
				VolumeCondition: &csi.VolumeCondition{
					Abnormal: true,
					Message:  "Volume path is not accessible",
				},
			},
		},
		{
			name: "Error retrieval of volume metrics",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId:   "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				VolumePath: tempDir,
			},
			mockSetup: func() {
				gofsutil.GOFSMockMounts = []gofsutil.Info{
					{
						Device: "some-device",
						Path:   tempDir,
					},
				}
				gofsutil.GOFSMock.InduceFilesystemInfoError = true
				mockUnity.On("FindVolumeByID", mock.Anything, mock.Anything).Return(nil, nil).Once()
			},
			expectedResp:  nil,
			expectedError: "failed to get metrics for volume with error",
			expectedCode:  codes.Internal,
		},
		{
			name: "Successful retrieval of volume metrics",
			req: &csi.NodeGetVolumeStatsRequest{
				VolumeId:   "csivol-8cd275903e-iSCSI-testarrayid-sv_250440",
				VolumePath: tempDir,
			},
			mockSetup: func() {
				gofsutil.GOFSMockMounts = []gofsutil.Info{
					{
						Device: "some-device",
						Path:   tempDir,
					},
				}
				gofsutil.GOFSMock.InduceFilesystemInfoError = false
				mockUnity.On("FindVolumeByID", mock.Anything, mock.Anything).Return(nil, nil).Once()
			},
			expectedResp: &csi.NodeGetVolumeStatsResponse{
				Usage: []*csi.VolumeUsage{
					{
						Available: 1000,
						Total:     2000,
						Used:      1000,
						Unit:      csi.VolumeUsage_BYTES,
					},
					{
						Available: 2,
						Total:     4,
						Used:      2,
						Unit:      csi.VolumeUsage_INODES,
					},
				},
				VolumeCondition: &csi.VolumeCondition{
					Abnormal: false,
					Message:  "",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mockSetup != nil {
				tt.mockSetup()
			}
			resp, err := testConf.service.NodeGetVolumeStats(context.Background(), tt.req)
			if tt.expectedError != "" {
				assert.Nil(t, resp)
				assert.ErrorContains(t, err, tt.expectedError)
				assert.Equal(t, tt.expectedCode, status.Code(err))
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedResp, resp)
			}
		})
	}
}

func TestNodeUnstageVolume(t *testing.T) {
	testConf.service.opts.NodeName = "test-node"
	testConf.service.opts.LongNodeName = "long-test-node"
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	req := &csi.NodeUnstageVolumeRequest{
		VolumeId:          "test-volume-id",
		StagingTargetPath: "/path/to/stage",
	}
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.NodeUnstageVolume(ctx, req)
	assert.Error(t, err)

	// test-case: validateAndGetResourceDetails returns nil
	req = &csi.NodeUnstageVolumeRequest{
		VolumeId:          "tests-NFS-testarrayid-1234",
		StagingTargetPath: "",
	}
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.NodeUnstageVolume(ctx, req)
	assert.Error(t, err)

	// test-case: StagingTargetPath is not empty
	req = &csi.NodeUnstageVolumeRequest{
		VolumeId:          "tests-NFS-testarrayid-1234",
		StagingTargetPath: "/path/to/stage",
	}
	testConf.service.opts.AutoProbe = true
	fc := gounitytypes.FileContent{
		ID: "id",
		NFSShare: []gounitytypes.Share{
			{
				ID:   "nfs-id",
				Path: "/",
				ParentSnap: gounitytypes.StorageResource{
					ID: "",
				},
			},
		},
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	nfsShare := gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
		},
	}
	nasServer := gounitytypes.NASServer{
		NASServerContent: gounitytypes.NASServerContent{
			ID:   "nas-123",
			Name: "MyNASServer",
			NFSServer: gounitytypes.NFSServer{
				ID:           "nfs-456",
				Name:         "MyNFSServer",
				NFSv3Enabled: true,
				NFSv4Enabled: true,
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-id").Return(&nfsShare, nil)
	mockUnity.On("FindNASServerByID", mock.Anything, mock.Anything).Return(&nasServer, nil)
	_, err = testConf.service.NodeUnstageVolume(ctx, req)
	assert.Error(t, err)

	// test-case: getNFSShare returns error
	fc = gounitytypes.FileContent{
		ID: "id",
		NFSShare: []gounitytypes.Share{
			{
				ID:   "",
				Path: "/",
				ParentSnap: gounitytypes.StorageResource{
					ID: "",
				},
			},
		},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	mockUnity.On("FindNASServerByID", mock.Anything, mock.Anything).Return(&nasServer, nil)
	_, err = testConf.service.NodeUnstageVolume(ctx, req)
	assert.Error(t, err)
	// test-case: export path is not empty
	fc = gounitytypes.FileContent{
		ID: "id",
		NFSShare: []gounitytypes.Share{
			{
				ID:   "nfs-id",
				Path: "/",
				ParentSnap: gounitytypes.StorageResource{
					ID: "",
				},
			},
		},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	nfsShare = gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:          "nfs-share-123",
			Name:        "TestNFSShare",
			ExportPaths: []string{"/path"},
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-id").Return(&nfsShare, nil)
	mockUnity.On("FindNASServerByID", mock.Anything, "").Return(&nasServer, nil)
	_, err = testConf.service.NodeUnstageVolume(ctx, req)
	assert.NoError(t, err)

	// test-case: protocol == ProtocolUnknown
	req = &csi.NodeUnstageVolumeRequest{
		VolumeId:          "tests-Unknown-testarrayid-1234",
		StagingTargetPath: "/path/to/stage",
	}
	hc := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-id").Return(&nfsShare, nil)
	mockUnity.On("FindNASServerByID", mock.Anything, "").Return(&nasServer, nil)
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, errors.New("error")).Once()
	_, err = testConf.service.NodeUnstageVolume(ctx, req)
	assert.Error(t, err)

	// test-case: getHostId doesnot return error
	req = &csi.NodeUnstageVolumeRequest{
		VolumeId:          "tests-Unknown-testarrayid-1234",
		StagingTargetPath: "/path/to/stage",
	}
	volume := gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			ResourceID: "vol-123",
			Wwn:        "6000ABC123456789",
		},
	}

	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-id").Return(&nfsShare, nil)
	mockUnity.On("FindNASServerByID", mock.Anything, "").Return(&nasServer, nil)
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "long-test-node").Return(&h, nil).Once()
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, gounity.ErrorVolumeNotFound)
	_, err = testConf.service.NodeUnstageVolume(ctx, req)
	assert.NoError(t, err)

	// test-case: FindVolumeByID returns error
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-id").Return(&nfsShare, nil)
	mockUnity.On("FindNASServerByID", mock.Anything, "").Return(&nasServer, nil)
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "long-test-node").Return(&h, nil).Once()
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, gounity.ErrorFilesystemNotFound)
	_, err = testConf.service.NodeUnstageVolume(ctx, req)
	assert.Error(t, err)

	// test-case: FindVolumeByID returns nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-id").Return(&nfsShare, nil)
	mockUnity.On("FindNASServerByID", mock.Anything, "").Return(&nasServer, nil)
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "long-test-node").Return(&h, nil).Once()
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.NodeUnstageVolume(ctx, req)
	assert.NoError(t, err)

	// protocol == nvme
	req = &csi.NodeUnstageVolumeRequest{
		VolumeId:          "tests-nvme-testarrayid-1234",
		StagingTargetPath: "/path/to/stage",
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.NodeUnstageVolume(ctx, req)
	assert.Error(t, err)
}

func TestEphemeralNodePublishVolume(t *testing.T) {
	testConf.service.opts.NodeName = "test-node"
	testConf.service.opts.LongNodeName = "long-test-node"
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true

	req := &csi.NodePublishVolumeRequest{
		VolumeId: "test-volume-id",
	}
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.ephemeralNodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: size is not empty in the req parameter
	req = &csi.NodePublishVolumeRequest{
		VolumeId: "test-volume-id",
		VolumeContext: map[string]string{
			"size": "1073741824",
		},
		VolumeCapability: &csi.VolumeCapability{},
		Secrets:          map[string]string{},
	}
	_, err = testConf.service.ephemeralNodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: size is valid in the req parameter
	req = &csi.NodePublishVolumeRequest{
		VolumeId: "1234",
		VolumeContext: map[string]string{
			"size":                  "32 Gi",
			keyProtocol:             NFS,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
			keyNasServer:            "nas1",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
			},
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		},
		Secrets: map[string]string{},
	}
	fc := gounitytypes.FileContent{
		ID:   "id",
		Name: "name",
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	hc := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
	}
	nfsShare := gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
			Filesystem: gounitytypes.Pool{
				ID:   "fs-456",
				Name: "TestFilesystem",
			},
			ReadOnlyHosts:           []gounitytypes.HostContent{},
			ReadWriteHosts:          []gounitytypes.HostContent{},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{},
			RootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "RootAccessHost1",
					Address: "192.168.1.4",
				},
			},
		},
	}
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(nil, nil).Once()
	mockUnity.On("CreateFilesystem", mock.Anything, "1234", "pool1", "", "nas1", mock.Anything, 1, 8192, 0, true, false).Return(&fs, nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(&fs, errors.New("error")).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "long-test-node").Return(&h, nil).Once()
	mockUnity.On("FindFilesystemByID", mock.Anything, "id").Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", "id", gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	mockUnity.On("ModifyNFSShareHostAccess", mock.Anything, "id", "", []string{"id"}, gounity.AccessType("READ_ONLY_ROOT")).Return(nil)
	_, err = testConf.service.ephemeralNodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: ControllerPublishVolume returns nil as err
	nfsShare = gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
			Filesystem: gounitytypes.Pool{
				ID:   "fs-456",
				Name: "TestFilesystem",
			},
			ReadOnlyHosts:           []gounitytypes.HostContent{},
			ReadWriteHosts:          []gounitytypes.HostContent{},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{},
			RootAccessHosts:         []gounitytypes.HostContent{},
		},
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(nil, nil).Once()
	mockUnity.On("CreateFilesystem", mock.Anything, "1234", "pool1", "", "nas1", mock.Anything, 1, 8192, 0, true, false).Return(&fs, nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(&fs, errors.New("error")).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "long-test-node").Return(&h, nil).Once()
	mockUnity.On("FindFilesystemByID", mock.Anything, "id").Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", "id", gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	mockUnity.On("ModifyNFSShareHostAccess", mock.Anything, "id", "", []string{"id"}, gounity.AccessType("READ_ONLY_ROOT")).Return(nil)
	_, err = testConf.service.ephemeralNodePublishVolume(ctx, req)
	assert.Error(t, err)
}

func TestAddNewNodeToArray(t *testing.T) {
	testConf.service.opts.NodeName = "test-node"
	testConf.service.opts.LongNodeName = "long-test-node"
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.opts.TenantName = "Tenant A"
	nodeIps := []string{}
	iqns := []string{}
	wwns := []string{}
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})
	array := &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue}

	mockUnity.On("FindTenants", mock.Anything).Return(nil, errors.New("error"))
	err := testConf.service.addNewNodeToArray(ctx, array, nodeIps, iqns, wwns)
	assert.Error(t, err)

	// test-case: FindTenants returns err as nil
	tenantInfo := gounitytypes.TenantInfo{
		Entries: []gounitytypes.TenantEntry{
			{
				Content: gounitytypes.TenantContent{
					ID:   "123",
					Name: "Tenant A",
				},
			},
		},
	}
	hc := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindTenants", mock.Anything).Return(&tenantInfo, nil)
	mockUnity.On("CreateHost", mock.Anything, "long-test-node", "123").Return(&h, errors.New("error"))
	err = testConf.service.addNewNodeToArray(ctx, array, nodeIps, iqns, wwns)
	assert.Error(t, err)

	// test-case: CreateHost returns nil as err
	hostport := gounitytypes.HostIPPort{
		HostIPContent: hc,
	}
	nodeIps = []string{"long-test-node"}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindTenants", mock.Anything).Return(&tenantInfo, nil)
	mockUnity.On("CreateHost", mock.Anything, "long-test-node", "123").Return(&h, nil)
	mockUnity.On("CreateHostIPPort", mock.Anything, "id", "long-test-node").Return(&hostport, nil)
	mockUnity.On("CreateHostIPPort", mock.Anything, "id", "wwn01").Return(&hostport, nil)
	err = testConf.service.addNewNodeToArray(ctx, array, nodeIps, iqns, wwns)
	assert.NoError(t, err)

	// test-case: wwns > 0
	nodeIps = []string{}
	wwns = []string{"wwn01"}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindTenants", mock.Anything).Return(&tenantInfo, nil)
	mockUnity.On("CreateHost", mock.Anything, "long-test-node", "123").Return(&h, nil)
	mockUnity.On("CreateHostIPPort", mock.Anything, "id", "long-test-node").Return(&hostport, nil).Once()
	mockUnity.On("CreateHostInitiator", mock.Anything, "id", "wwn01", gounitytypes.InitiatorType("1")).Return(nil, nil)

	err = testConf.service.addNewNodeToArray(ctx, array, nodeIps, iqns, wwns)
	assert.NoError(t, err)

	// test-case: iqns > 0
	nodeIps = []string{}
	wwns = []string{}
	iqns = []string{"iqn01"}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindTenants", mock.Anything).Return(&tenantInfo, nil)
	mockUnity.On("CreateHost", mock.Anything, "long-test-node", "123").Return(&h, nil)
	mockUnity.On("CreateHostIPPort", mock.Anything, "id", "long-test-node").Return(&hostport, nil).Once()
	mockUnity.On("CreateHostInitiator", mock.Anything, "id", "iqn01", gounitytypes.InitiatorType("2")).Return(nil, nil)

	err = testConf.service.addNewNodeToArray(ctx, array, nodeIps, iqns, wwns)
	assert.NoError(t, err)
}

func TestNodePublishVolume(t *testing.T) {
	testConf.service.opts.NodeName = "test-node"
	testConf.service.opts.LongNodeName = "long-test-node"
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true

	// test-case: set arrayID context and verify logger initialization
	ctx, log := setArrayIDContext(ctx, arrayID)
	assert.NotNil(t, log) // Ensure the logger is not nil

	// test-case: when volume request has no size parameter
	req := &csi.NodePublishVolumeRequest{
		VolumeId: "test-volume-id",
	}
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue,
	})

	_, err := testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: size is not empty in the req parameter
	req = &csi.NodePublishVolumeRequest{
		VolumeId: "test-volume-id",
		VolumeContext: map[string]string{
			"size": "1073741824",
		},
		VolumeCapability: &csi.VolumeCapability{},
		Secrets:          map[string]string{},
	}
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: size is valid in the req parameter
	req = &csi.NodePublishVolumeRequest{
		VolumeId: "1234",
		VolumeContext: map[string]string{
			"size":                  "32 Gi",
			keyProtocol:             NFS,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
			keyNasServer:            "nas1",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
			},
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		},
		Secrets: map[string]string{},
	}
	fc := gounitytypes.FileContent{
		ID:   "id",
		Name: "name",
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	hc := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
	}
	nfsShare := gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
			Filesystem: gounitytypes.Pool{
				ID:   "fs-456",
				Name: "TestFilesystem",
			},
			ReadOnlyHosts:           []gounitytypes.HostContent{},
			ReadWriteHosts:          []gounitytypes.HostContent{},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{},
			RootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "RootAccessHost1",
					Address: "192.168.1.4",
				},
			},
		},
	}

	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(nil, nil).Once()
	mockUnity.On("CreateFilesystem", mock.Anything, "1234", "pool1", "", "nas1", mock.Anything, 1, 8192, 0, true, false).Return(&fs, nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(&fs, errors.New("error")).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "long-test-node").Return(&h, nil).Once()
	mockUnity.On("FindFilesystemByID", mock.Anything, "id").Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", "id", gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	mockUnity.On("ModifyNFSShareHostAccess", mock.Anything, "id", "", []string{"id"}, gounity.AccessType("READ_ONLY_ROOT")).Return(nil)

	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: validateAndGetResourceDetails returns nil
	req = &csi.NodePublishVolumeRequest{
		VolumeId: "tests-NFS-testarrayid-1234",
		VolumeContext: map[string]string{
			"size":                  "32 Gi",
			keyProtocol:             NFS,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
			keyNasServer:            "nas1",
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(errors.New("error"))
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: requireProbe returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: target path is not empty
	req = &csi.NodePublishVolumeRequest{
		VolumeId: "tests-NFS-testarrayid-1234",
		VolumeContext: map[string]string{
			"size":                  "32 Gi",
			keyProtocol:             NFS,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
			keyNasServer:            "nas1",
		},
		TargetPath: "/path",
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: staging path is not empty
	req = &csi.NodePublishVolumeRequest{
		VolumeId: "tests-NFS-testarrayid-1234",
		VolumeContext: map[string]string{
			"size":                  "32 Gi",
			keyProtocol:             NFS,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
			keyNasServer:            "nas1",
		},
		TargetPath:        "/path",
		StagingTargetPath: "/path/stag",
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: volume capability is not empty
	req = &csi.NodePublishVolumeRequest{
		VolumeId: "tests-NFS-testarrayid-1234",
		VolumeContext: map[string]string{
			"size":                  "32 Gi",
			keyProtocol:             NFS,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
			keyNasServer:            "nas1",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		},
		TargetPath:        "/path",
		StagingTargetPath: "/path/stag",
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: access mode is not empty
	req = &csi.NodePublishVolumeRequest{
		VolumeId: "tests-NFS-testarrayid-1234",
		VolumeContext: map[string]string{
			"size":                  "32 Gi",
			keyProtocol:             NFS,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
			keyNasServer:            "nas1",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
			},
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		},
		TargetPath:        "/path",
		StagingTargetPath: "/path/stag",
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(nil, errors.New("error"))
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: getNFSShare returns nil as error
	fc = gounitytypes.FileContent{
		ID: "id",
		NFSShare: []gounitytypes.Share{
			{
				ID:   "nfs-id",
				Path: "/",
				ParentSnap: gounitytypes.StorageResource{
					ID: "",
				},
			},
		},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	nasServer := gounitytypes.NASServer{
		NASServerContent: gounitytypes.NASServerContent{
			ID:   "nas-123",
			Name: "MyNASServer",
			NFSServer: gounitytypes.NFSServer{
				ID:           "nfs-456",
				Name:         "MyNFSServer",
				NFSv3Enabled: true,
				NFSv4Enabled: true,
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-id").Return(&nfsShare, nil)
	mockUnity.On("FindNASServerByID", mock.Anything, "").Return(&nasServer, nil)
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: export path is not empty
	nfsShare = gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
			Filesystem: gounitytypes.Pool{
				ID:   "fs-456",
				Name: "TestFilesystem",
			},
			ReadOnlyHosts:           []gounitytypes.HostContent{},
			ReadWriteHosts:          []gounitytypes.HostContent{},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{},
			RootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "RootAccessHost1",
					Address: "192.168.1.4",
				},
			},
			ExportPaths: []string{"192.168.1.4:/export/path1"},
		},
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-id").Return(&nfsShare, nil)
	mockUnity.On("FindNASServerByID", mock.Anything, "").Return(&nasServer, nil)
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// protocol is FC
	req = &csi.NodePublishVolumeRequest{
		VolumeId: "tests-FC-testarrayid-1234",
		VolumeContext: map[string]string{
			"size":                  "32 Gi",
			keyProtocol:             NFS,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
			keyNasServer:            "nas1",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
			},
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		},
		TargetPath:        "/path",
		StagingTargetPath: "/path/stag",
	}

	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(nil, gounity.ErrorVolumeNotFound)
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: returns nil as err
	volume := gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			ResourceID: "vol-123",
			Name:       "TestVolume",
			HostAccessResponse: []gounitytypes.HostAccessResponse{
				{
					HostContent: gounitytypes.HostContent{
						ID:   "hostID",
						Name: "TestHost",
					},
					HLU: 10,
				},
			},
			Wwn:                    "6000ABC123456789",
			IsThinEnabled:          true,
			IsDataReductionEnabled: false,
		},
	}
	gofsutil.UseMockFS()
	gofsutil.GOFSMock.InduceWWNToDevicePathError = true
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: WWNToDevicePathX returns err as nil
	gofsutil.UseMockFS()
	gofsutil.GOFSMock.InduceWWNToDevicePathError = false
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.NodePublishVolume(ctx, req)
	assert.Error(t, err)
}

func TestCheckFilesystemMapping(t *testing.T) {
	ctx := context.Background()
	testConf.service.opts.NodeName = "test-node"
	testConf.service.opts.LongNodeName = "long-test-node"
	arrayID := "testarrayid"
	hostID := "testhost"
	nfsShareID := "testnfsshare"
	fileSystemID := "testfilesystemid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true

	// Set up array
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:                   arrayID,
		UnityClient:               mockUnity,
		SkipCertificateValidation: &boolTrue,
		IsDefault:                 &boolTrue,
	})

	nfsShare := gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID: nfsShareID,
			Filesystem: gounitytypes.Pool{
				ID: fileSystemID,
			},
			ReadOnlyHosts:  []gounitytypes.HostContent{},
			ReadWriteHosts: []gounitytypes.HostContent{},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      hostID,
					Name:    "ReadOnlyRootAccessHost1",
					Address: "192.168.1.3",
				},
			},
			RootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      hostID,
					Name:    "RootAccessHost1",
					Address: "192.168.1.4",
				},
			},
		},
	}
	am := &csi.VolumeCapability_AccessMode{
		Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
	}

	fc := gounitytypes.FileContent{
		ID: fileSystemID,
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	hc := gounitytypes.HostContent{
		ID: hostID,
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
	}

	// Test Case: MULTI_NODE_READER_ONLY
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(nil, nil).Once()
	mockUnity.On("CreateFilesystem", mock.Anything, "1234", "pool1", "", "nas1", mock.Anything, 1, 8192, 0, true, false).Return(&fs, nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(&fs, errors.New("error")).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "long-test-node").Return(&h, nil).Once()
	mockUnity.On("FindFilesystemByID", mock.Anything, fileSystemID).Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", nfsShareID, gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)

	err := testConf.service.checkFilesystemMapping(ctx, &nfsShare, am, arrayID)
	assert.NoError(t, err)

	// Test Case: SINGLE_NODE_READER_ONLY
	am = &csi.VolumeCapability_AccessMode{
		Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(nil, nil).Once()
	mockUnity.On("CreateFilesystem", mock.Anything, "1234", "pool1", "", "nas1", mock.Anything, 1, 8192, 0, true, false).Return(&fs, nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(&fs, errors.New("error")).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "long-test-node").Return(&h, nil).Once()
	mockUnity.On("FindFilesystemByID", mock.Anything, fileSystemID).Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", nfsShareID, gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	err = testConf.service.checkFilesystemMapping(ctx, &nfsShare, am, arrayID)
	assert.NoError(t, err)

	// Test Case: host ID mismatch
	hc = gounitytypes.HostContent{
		ID: "other-host-id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
	}
	h = gounitytypes.Host{
		HostContent: hc,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(nil, nil).Once()
	mockUnity.On("CreateFilesystem", mock.Anything, "1234", "pool1", "", "nas1", mock.Anything, 1, 8192, 0, true, false).Return(&fs, nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(&fs, errors.New("error")).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "long-test-node").Return(&h, nil).Once()
	mockUnity.On("FindFilesystemByID", mock.Anything, fileSystemID).Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", nfsShareID, gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	err = testConf.service.checkFilesystemMapping(ctx, &nfsShare, am, arrayID)
	assert.Error(t, err)
}

func TestGetArrayHostInitiators(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	hc := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
		FcInitiators: []gounitytypes.Initiators{
			{ID: "fc-init-1"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
	}
	hostInitiator := gounitytypes.HostInitiator{
		HostInitiatorContent: gounitytypes.HostInitiatorContent{
			ID:          "initiator-123",
			InitiatorID: "iqn.1993-08.org.debian:01:abc123",
		},
	}
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	mockUnity.On("FindHostInitiatorByID", mock.Anything, "fc-init-1").Return(nil, errors.New("error"))
	_, err := testConf.service.getArrayHostInitiators(ctx, &h, arrayID)
	assert.Error(t, err)

	// test-case: FindHostInitiatorByID returns nil as error
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindHostInitiatorByID", mock.Anything, "fc-init-1").Return(&hostInitiator, nil)
	_, err = testConf.service.getArrayHostInitiators(ctx, &h, arrayID)
	assert.NoError(t, err)
}

func TestCheckVolumeMapping(t *testing.T) {
	testConf.service.opts.NodeName = "test-node"
	testConf.service.opts.LongNodeName = "long-test-node"
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	volume := gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			ResourceID: "vol-123",
			Wwn:        "6000ABC123456789",
		},
	}
	hc := gounitytypes.HostContent{
		Name: "host-name",
		ID:   "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
	}
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "long-test-node").Return(&h, nil).Once()
	_, err := testConf.service.checkVolumeMapping(ctx, &volume, arrayID)
	assert.Error(t, err)

	// test-case: HostAccessResponse  is not empty

	volume = gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			Name:       "vol-name",
			ResourceID: "vol-123",
			Wwn:        "6000ABC123456789",
			HostAccessResponse: []gounitytypes.HostAccessResponse{
				{
					HostContent: gounitytypes.HostContent{
						ID:   "id1",
						Name: "TestHost",
					},
					HLU: 10,
				},
			},
		},
	}
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "long-test-node").Return(&h, nil).Once()
	_, err = testConf.service.checkVolumeMapping(ctx, &volume, arrayID)
	assert.Error(t, err)

	// test-case: get hostid returns error
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, errors.New("error"))
	_, err = testConf.service.checkVolumeMapping(ctx, &volume, arrayID)
	assert.Error(t, err)
}

func TestIScsiDiscoverAndLogin(_ *testing.T) {
	ctx := context.Background()
	mockConnector := new(MockISCSIConnector)
	s := &service{
		iscsiClient:    goiscsi.NewMockISCSI(nil),
		iscsiConnector: mockConnector,
		arrays:         new(sync.Map),
		opts: Opts{
			LongNodeName: "long-test-node",
			NodeName:     "test-node",
		},
	}

	validIPs := []string{"127.0.0.1"} // Localhost is always reachable
	orgIPReachable := csiutils.IPReachable
	csiutils.IPReachable = func(_ context.Context, _, _ string, _ int) bool {
		return true
	}
	defer func() { csiutils.IPReachable = orgIPReachable }()
	s.iScsiDiscoverAndLogin(ctx, validIPs)

	goiscsi.GOISCSIMock.InduceDiscoveryError = true
	s.iScsiDiscoverAndLogin(ctx, validIPs)
	goiscsi.GOISCSIMock.InduceDiscoveryError = false

	goiscsi.GOISCSIMock.InduceLoginError = true
	s.iScsiDiscoverAndLogin(ctx, validIPs)
	goiscsi.GOISCSIMock.InduceLoginError = false
}

func TestNodeExpandVolume(t *testing.T) {
	testConf.service.opts.NodeName = "test-node"
	testConf.service.opts.LongNodeName = "long-test-node"
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	req := &csi.NodeExpandVolumeRequest{
		VolumeId: "",
	}
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	// test-case: volume Id is empty
	_, err := testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: volume Id is not empty
	req = &csi.NodeExpandVolumeRequest{
		VolumeId: "test-volume-id",
	}
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: validateAndGetResourceDetails returns nil
	req = &csi.NodeExpandVolumeRequest{
		VolumeId: "tests-iscsi-testarrayid-1234",
	}
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: volumePath is not empty
	req = &csi.NodeExpandVolumeRequest{
		VolumeId:   "tests-iscsi-testarrayid-1234",
		VolumePath: "/path",
	}
	volume := gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			ResourceID: "vol-123",
			Name:       "TestVolume",
			HostAccessResponse: []gounitytypes.HostAccessResponse{
				{
					HostContent: gounitytypes.HostContent{
						ID:   "hostID",
						Name: "TestHost",
					},
					HLU: 10,
				},
			},
			Wwn:                    "6000abc123456789",
			IsThinEnabled:          true,
			IsDataReductionEnabled: false,
		},
	}

	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(nil, gounity.ErrorVolumeNotFound)
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: GetSysBlockDevicesForVolumeWWN returns devicename
	gofsutil.UseMockFS()
	gofsutil.GOFSMock.InduceGetMountInfoFromDeviceError = true
	gofsutil.GOFSMock.InduceDeviceRescanError = true
	gofsutil.GOFSMockWWNToDevice = map[string]string{
		"6000abc123456789": "/dev/sda",
	}

	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: DeviceRescan returns nil as err

	gofsutil.GOFSMock.InduceDeviceRescanError = false
	gofsutil.GOFSMock.InduceWWNToDevicePathError = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: GetMountInfoFromDevice returns nil as err
	gofsutil.GOFSMock.InduceGetMountInfoFromDeviceError = false
	gofsutil.GOFSMock.InduceDeviceRescanError = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: DeviceRescan returns nil as err
	gofsutil.GOFSMock.InduceGetMountInfoFromDeviceError = false
	gofsutil.GOFSMock.InduceDeviceRescanError = false
	gofsutil.GOFSMock.InduceResizeMultipathError = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: ResizeMultipath returns nil as err
	gofsutil.GOFSMock.InduceGetMountInfoFromDeviceError = false
	gofsutil.GOFSMock.InduceDeviceRescanError = false
	gofsutil.GOFSMock.InduceResizeMultipathError = false
	gofsutil.GOFSMock.InduceFSTypeError = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: FindFSType returns nil as err
	gofsutil.GOFSMock.InduceGetMountInfoFromDeviceError = false
	gofsutil.GOFSMock.InduceDeviceRescanError = false
	gofsutil.GOFSMock.InduceResizeMultipathError = false
	gofsutil.GOFSMock.InduceFSTypeError = false
	gofsutil.GOFSMock.InduceResizeFSError = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: ResizeFS returns nil as err
	gofsutil.GOFSMock.InduceGetMountInfoFromDeviceError = false
	gofsutil.GOFSMock.InduceDeviceRescanError = false
	gofsutil.GOFSMock.InduceResizeMultipathError = false
	gofsutil.GOFSMock.InduceFSTypeError = false
	gofsutil.GOFSMock.InduceResizeFSError = false
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.NoError(t, err)

	// test-case: requireprobe returns error
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(errors.New("error"))
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: GetSysBlockDevicesForVolumeWWN returns empty devicename
	gofsutil.GOFSMock.InduceGetMountInfoFromDeviceError = true
	gofsutil.GOFSMock.InduceGetSysBlockDevicesError = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.NodeExpandVolume(ctx, req)
	assert.Error(t, err)
}

func TestCheckHostIdempotency(t *testing.T) {
	testConf.service.opts.LongNodeName = "long-test-node"
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	iqns := []string{}
	wwns := []string{}

	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})
	array := &StorageArrayConfig{}
	hc := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
	}
	// test-case: array is empty
	_, _, err := testConf.service.checkHostIdempotency(ctx, array, &h, iqns, wwns)
	assert.Error(t, err)

	// test-case: array is not empty
	array = &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue}
	_, _, err = testConf.service.checkHostIdempotency(ctx, array, &h, iqns, wwns)
	assert.NoError(t, err)

	// test-case: wwn is not empty
	wwns = []string{"wwn01"}
	_, _, err = testConf.service.checkHostIdempotency(ctx, array, &h, iqns, wwns)
	assert.NoError(t, err)

	// test-case: extrawwns is not empty
	iqns = []string{"iqn01,iqn02,iqn03"}
	hc = gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
		FcInitiators: []gounitytypes.Initiators{
			{ID: "fc-init-1"},
		},
	}
	h = gounitytypes.Host{
		HostContent: hc,
	}
	hostInitiator := gounitytypes.HostInitiator{
		HostInitiatorContent: gounitytypes.HostInitiatorContent{
			ID:          "initiator-123",
			InitiatorID: "iqn.1993-08.org.debian:01:abc123",
		},
	}
	mockUnity.On("FindHostInitiatorByID", mock.Anything, "fc-init-1").Return(&hostInitiator, nil)
	_, _, err = testConf.service.checkHostIdempotency(ctx, array, &h, iqns, wwns)
	assert.NoError(t, err)

	// test-case: host.HostContent.Name == s.opts.LongNodeName
	hc = gounitytypes.HostContent{
		ID:   "id",
		Name: "long-test-node",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "long-test-node"},
		},
		FcInitiators: []gounitytypes.Initiators{
			{ID: "fc-init-1"},
		},
	}
	h = gounitytypes.Host{
		HostContent: hc,
	}

	mockUnity.On("FindHostInitiatorByID", mock.Anything, "fc-init-1").Return(&hostInitiator, nil)
	_, _, err = testConf.service.checkHostIdempotency(ctx, array, &h, iqns, wwns)
	assert.Error(t, err)
}

func TestValidateProtocols(t *testing.T) {
	mockConnector := new(MockISCSIConnector)
	s := &service{
		iscsiClient:    goiscsi.NewMockISCSI(nil),
		iscsiConnector: mockConnector,
		arrays:         new(sync.Map),
		opts: Opts{
			LongNodeName: "long-test-node",
			NodeName:     "test-node",
		},
	}

	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	s.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue, IsHostAdded: true})
	arraysList := s.getStorageArrayList()

	// Test case: Error in getting NFS servers
	connectedSystemID = make([]string, 0)
	goiscsi.GOISCSIMock.InduceInitiatorError = true
	mockNFSServerresponse := gounitytypes.NFSServersResponse{
		Entries: []gounitytypes.NFSServerEntry{},
	}
	mockUnity.On("GetAllNFSServers", mock.Anything).Return(&mockNFSServerresponse, errors.New("Error in getting NFS servers")).Once()
	s.validateProtocols(context.Background(), arraysList)
	expectedConnectedSystemID := []string{}
	if !reflect.DeepEqual(connectedSystemID, expectedConnectedSystemID) {
		t.Errorf("Expected connectedSystemID to be %v, but got %v", expectedConnectedSystemID, connectedSystemID)
	}

	// Test case: no iSCSI or FC initiators found
	connectedSystemID = make([]string, 0)
	goiscsi.GOISCSIMock.InduceInitiatorError = true
	mockNFSServerresponse = gounitytypes.NFSServersResponse{
		Entries: []gounitytypes.NFSServerEntry{
			{
				Content: gounitytypes.NFSServer{
					NFSv3Enabled: true,
					NFSv4Enabled: true,
				},
			},
		},
	}
	mockUnity.On("GetAllNFSServers", mock.Anything).Return(&mockNFSServerresponse, nil).Once()
	s.validateProtocols(context.Background(), arraysList)
	expectedConnectedSystemID = []string{arrayID + "/nfs"}
	if !reflect.DeepEqual(connectedSystemID, expectedConnectedSystemID) {
		t.Errorf("Expected connectedSystemID to be %v, but got %v", expectedConnectedSystemID, connectedSystemID)
	}

	// Test case: FC initiators found
	connectedSystemID = make([]string, 0)
	goiscsi.GOISCSIMock.InduceInitiatorError = false
	h := gounitytypes.Host{
		HostContent: gounitytypes.HostContent{
			ID: "hostID",
			FcInitiators: []gounitytypes.Initiators{
				{ID: "fc-initiator-1"},
			},
			IPPorts: []gounitytypes.IPPorts{
				{ID: "ip-port-1", Address: s.opts.LongNodeName},
			},
		},
	}
	hi := mockAnInitiator("fc-initiator-1")
	mockUnity.On("GetAllNFSServers", mock.Anything).Return(&mockNFSServerresponse, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostInitiatorByID", mock.Anything, "fc-initiator-1").Return(hi, nil).Once()
	s.validateProtocols(context.Background(), arraysList)
	expectedConnectedSystemID = []string{
		arrayID + "/nfs",
		arrayID + "/fc",
	}
	if !reflect.DeepEqual(connectedSystemID, expectedConnectedSystemID) {
		t.Errorf("Expected connectedSystemID to be %v, but got %v", expectedConnectedSystemID, connectedSystemID)
	}

	// Test case: FC initiators error
	connectedSystemID = make([]string, 0)
	mockUnity.On("GetAllNFSServers", mock.Anything).Return(&mockNFSServerresponse, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostInitiatorByID", mock.Anything, "fc-initiator-1").Return(nil, errors.New("not found")).Once()
	s.validateProtocols(context.Background(), arraysList)
	expectedConnectedSystemID = []string{
		arrayID + "/nfs",
	}
	if !reflect.DeepEqual(connectedSystemID, expectedConnectedSystemID) {
		t.Errorf("Expected connectedSystemID to be %v, but got %v", expectedConnectedSystemID, connectedSystemID)
	}

	// Test case: FC initiators bad health
	connectedSystemID = make([]string, 0)
	hi.HostInitiatorContent.Health.DescriptionIDs = []string{""}
	mockUnity.On("GetAllNFSServers", mock.Anything).Return(&mockNFSServerresponse, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostInitiatorByID", mock.Anything, "fc-initiator-1").Return(hi, nil).Once()
	s.validateProtocols(context.Background(), arraysList)
	expectedConnectedSystemID = []string{
		arrayID + "/nfs",
	}
	if !reflect.DeepEqual(connectedSystemID, expectedConnectedSystemID) {
		t.Errorf("Expected connectedSystemID to be %v, but got %v", expectedConnectedSystemID, connectedSystemID)
	}

	// Test case: iSCSI initiators found
	connectedSystemID = make([]string, 0)
	h.HostContent.IscsiInitiators = []gounitytypes.Initiators{
		{ID: "iscsi-initiator-1"},
	}
	h.HostContent.FcInitiators = nil
	hi = mockAnInitiator("iscsi-initiator-1")
	mockUnity.On("GetAllNFSServers", mock.Anything).Return(&mockNFSServerresponse, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostInitiatorByID", mock.Anything, "iscsi-initiator-1").Return(hi, nil).Once()
	s.validateProtocols(context.Background(), arraysList)
	expectedConnectedSystemID = []string{
		arrayID + "/nfs",
		arrayID + "/iscsi",
	}
	if !reflect.DeepEqual(connectedSystemID, expectedConnectedSystemID) {
		t.Errorf("Expected connectedSystemID to be %v, but got %v", expectedConnectedSystemID, connectedSystemID)
	}

	// Test case: iSCSI initiators error
	connectedSystemID = make([]string, 0)
	mockUnity.On("GetAllNFSServers", mock.Anything).Return(&mockNFSServerresponse, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostInitiatorByID", mock.Anything, "iscsi-initiator-1").Return(nil, errors.New("not found")).Once()
	s.validateProtocols(context.Background(), arraysList)
	expectedConnectedSystemID = []string{
		arrayID + "/nfs",
	}
	if !reflect.DeepEqual(connectedSystemID, expectedConnectedSystemID) {
		t.Errorf("Expected connectedSystemID to be %v, but got %v", expectedConnectedSystemID, connectedSystemID)
	}

	// Test case: iSCSI initiators bad health
	connectedSystemID = make([]string, 0)
	hi.HostInitiatorContent.Health.DescriptionIDs = []string{""}
	mockUnity.On("GetAllNFSServers", mock.Anything).Return(&mockNFSServerresponse, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostInitiatorByID", mock.Anything, "iscsi-initiator-1").Return(hi, nil).Once()
	s.validateProtocols(context.Background(), arraysList)
	expectedConnectedSystemID = []string{
		arrayID + "/nfs",
	}
	if !reflect.DeepEqual(connectedSystemID, expectedConnectedSystemID) {
		t.Errorf("Expected connectedSystemID to be %v, but got %v", expectedConnectedSystemID, connectedSystemID)
	}

	// Test case: FC and iSCSI initiators found AND NFS is disabled
	connectedSystemID = make([]string, 0)
	h.HostContent.IscsiInitiators = []gounitytypes.Initiators{
		{ID: "iscsi-initiator-1"},
	}
	h.HostContent.FcInitiators = []gounitytypes.Initiators{
		{ID: "fc-initiator-1"},
	}
	mockNFSServerresponseWithoutNFS := gounitytypes.NFSServersResponse{
		Entries: []gounitytypes.NFSServerEntry{
			{
				Content: gounitytypes.NFSServer{
					NFSv3Enabled: false,
					NFSv4Enabled: false,
				},
			},
		},
	}
	fcHi := mockAnInitiator("fc-initiator-1")
	iscsiHi := mockAnInitiator("iscsi-initiator-1")
	mockUnity.On("GetAllNFSServers", mock.Anything).Return(&mockNFSServerresponseWithoutNFS, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(&h, nil).Once()
	mockUnity.On("FindHostInitiatorByID", mock.Anything, "fc-initiator-1").Return(fcHi, nil).Once()
	mockUnity.On("FindHostInitiatorByID", mock.Anything, "iscsi-initiator-1").Return(iscsiHi, nil).Once()
	s.validateProtocols(context.Background(), arraysList)
	expectedConnectedSystemID = []string{
		arrayID + "/fc",
		arrayID + "/iscsi",
	}
	if !reflect.DeepEqual(connectedSystemID, expectedConnectedSystemID) {
		t.Errorf("Expected connectedSystemID to be %v, but got %v", expectedConnectedSystemID, connectedSystemID)
	}

	// Test case: Find Host Failed
	connectedSystemID = make([]string, 0)
	mockUnity.On("GetAllNFSServers", mock.Anything).Return(&mockNFSServerresponse, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(nil, errors.New("not found")).Once()
	s.validateProtocols(context.Background(), arraysList)
	expectedConnectedSystemID = []string{arrayID + "/nfs"}
	if !reflect.DeepEqual(connectedSystemID, expectedConnectedSystemID) {
		t.Errorf("Expected connectedSystemID to be %v, but got %v", expectedConnectedSystemID, connectedSystemID)
	}

	// Test case: Failed to get UnityClient
	connectedSystemID = make([]string, 0)
	arraysList[0].ArrayID = ""
	mockUnity.On("FindHostByName", mock.Anything, "test-node").Return(nil, errors.New("not found")).Once()
	s.validateProtocols(context.Background(), arraysList)
	expectedConnectedSystemID = []string{}
	if !reflect.DeepEqual(connectedSystemID, expectedConnectedSystemID) {
		t.Errorf("Expected connectedSystemID to be %v, but got %v", expectedConnectedSystemID, connectedSystemID)
	}
}

func TestSyncNodeInfo(_ *testing.T) {
	mockConnector := new(MockISCSIConnector)
	s := &service{
		iscsiClient:    goiscsi.NewMockISCSI(nil),
		iscsiConnector: mockConnector,
		arrays:         new(sync.Map),
		opts: Opts{
			LongNodeName: "long-test-node",
			NodeName:     "test-node",
		},
	}

	mockUnityClient := &gounitymocks.UnityClient{}
	arrayID := "testarrayid"
	boolTrue := true

	// Test case: Host is added to array
	s.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:                   arrayID,
		UnityClient:               mockUnityClient,
		SkipCertificateValidation: &boolTrue,
		IsDefault:                 &boolTrue,
		IsHostAdded:               true,
	})
	s.syncNodeInfo(context.Background())

	// Test case: Host is not added to array - host addition success
	s.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:                   arrayID,
		UnityClient:               mockUnityClient,
		SkipCertificateValidation: &boolTrue,
		IsDefault:                 &boolTrue,
		IsHostAdded:               false,
	})

	hostContent := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "test-node"},
		},
	}
	host := gounitytypes.Host{HostContent: hostContent}
	hostIPPortPtr := &gounitytypes.HostIPPort{HostIPContent: hostContent}
	expectedHostIPPort := gounitytypes.HostIPPort{HostIPContent: hostContent}
	expectedIPInterface := gounitytypes.IPInterfaceEntries{
		IPInterfaceContent: gounitytypes.IPInterfaceContent{IPAddress: "test-node"},
	}

	mockUnityClient.Calls = nil
	mockUnityClient.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		resp := args.Get(1).(*gounity.ConfigConnect)
		*resp = gounity.ConfigConnect{
			Insecure: true,
		}
	}).Once()
	mockUnityClient.On("FindHostByName", mock.Anything, "test-node").Return(&host, nil).Once()
	mockUnityClient.On("FindHostIPPortByID", mock.Anything, "ip-port-1").Return(hostIPPortPtr, nil).Once()
	mockUnityClient.On("CreateHostIPPort", mock.Anything, "id", mock.Anything).Return(&expectedHostIPPort, nil)
	mockUnityClient.On("CreateHostInitiator", mock.Anything, hostContent.ID, mock.Anything, mock.Anything).Return(nil, nil).Once()
	mockUnityClient.On("ListIscsiIPInterfaces", mock.Anything).Return([]gounitytypes.IPInterfaceEntries{expectedIPInterface}, nil).Once()
	s.syncNodeInfo(context.Background())

	// Test case: Host is not added to array - host addition failed
	s.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:                   arrayID,
		UnityClient:               mockUnityClient,
		SkipCertificateValidation: &boolTrue,
		IsDefault:                 &boolTrue,
		IsHostAdded:               false,
	})

	mockUnityClient.Calls = nil
	mockUnityClient.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil).Once()
	mockUnityClient.On("FindHostByName", mock.Anything, "test-node").Return(&host, errors.New("error")).Once()
	s.syncNodeInfo(context.Background())
}

func TestSyncNodeInfoRoutine(t *testing.T) {
	mockConnector := new(MockISCSIConnector)
	s := &service{
		iscsiClient:    goiscsi.NewMockISCSI(nil),
		iscsiConnector: mockConnector,
		arrays:         new(sync.Map),
		opts: Opts{
			SyncNodeInfoTimeInterval: 1, // Set the interval to 1 minute for testing
			LongNodeName:             "long-test-node",
			NodeName:                 "test-node",
		},
	}

	mockUnityClient := &gounitymocks.UnityClient{}
	arrayID := "testarrayid"
	boolTrue := true

	// Test case: Host is added to array
	s.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:                   arrayID,
		UnityClient:               mockUnityClient,
		SkipCertificateValidation: &boolTrue,
		IsDefault:                 &boolTrue,
		IsHostAdded:               true,
	})

	// Create a context with a timeout to ensure the test completes
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a channel to simulate syncNodeInfoChan
	syncNodeInfoChan := make(chan struct{})
	go func() {
		s.syncNodeInfoRoutine(ctx)
	}()

	// Trigger syncNodeInfo via channel
	t.Log("Sending signal to syncNodeInfoChan")
	go func() {
		syncNodeInfoChan <- struct{}{}
	}()
	time.Sleep(2 * time.Second) // Wait for goroutine to process

	// Test case: Host is not added to array - host addition success
	s.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:                   arrayID,
		UnityClient:               mockUnityClient,
		SkipCertificateValidation: &boolTrue,
		IsDefault:                 &boolTrue,
		IsHostAdded:               false,
	})

	hostContent := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "test-node"},
		},
	}
	host := gounitytypes.Host{HostContent: hostContent}
	hostIPPortPtr := &gounitytypes.HostIPPort{HostIPContent: hostContent}
	expectedHostIPPort := gounitytypes.HostIPPort{HostIPContent: hostContent}
	expectedIPInterface := gounitytypes.IPInterfaceEntries{
		IPInterfaceContent: gounitytypes.IPInterfaceContent{IPAddress: "test-node"},
	}

	mockUnityClient.Calls = nil
	mockUnityClient.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		resp := args.Get(1).(*gounity.ConfigConnect)
		*resp = gounity.ConfigConnect{
			Insecure: true,
		}
	}).Once()
	mockUnityClient.On("FindHostByName", mock.Anything, "test-node").Return(&host, nil).Once()
	mockUnityClient.On("FindHostIPPortByID", mock.Anything, "ip-port-1").Return(hostIPPortPtr, nil).Once()
	mockUnityClient.On("CreateHostIPPort", mock.Anything, "id", mock.Anything).Return(&expectedHostIPPort, nil)
	mockUnityClient.On("CreateHostInitiator", mock.Anything, hostContent.ID, mock.Anything, mock.Anything).Return(nil, nil).Once()
	mockUnityClient.On("ListIscsiIPInterfaces", mock.Anything).Return([]gounitytypes.IPInterfaceEntries{expectedIPInterface}, nil).Once()
}

func TestGetNFSShare(t *testing.T) {
	mockUnityClient := &gounitymocks.UnityClient{}
	boolTrue := true
	arrayID := "testarrayid"
	fileSystemID := "testfilesystemid"

	s := &service{
		arrays: new(sync.Map),
		opts: Opts{
			LongNodeName: "long-test-node",
			NodeName:     "test-node",
		},
	}

	// Test Case: Unable to get unity client
	_, _, _, err := s.getNFSShare(context.Background(), fileSystemID, arrayID)
	assert.Error(t, err)

	// Set up array
	s.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:                   arrayID,
		UnityClient:               mockUnityClient,
		SkipCertificateValidation: &boolTrue,
		IsDefault:                 &boolTrue,
	})

	// Test Case: No snapshot with the associated filesystemID
	mockUnityClient.ExpectedCalls = nil
	mockUnityClient.On("FindFilesystemByID", mock.Anything, mock.Anything).Return(nil, gounity.ErrorFilesystemNotFound)
	mockUnityClient.On("FindSnapshotByID", mock.Anything, mock.Anything).Return(nil, gounity.ErrorFilesystemNotFound)
	_, _, _, err = s.getNFSShare(context.Background(), fileSystemID, arrayID)
	assert.Error(t, err)

	// Test Case: Unable to get filesystem from the snapshot resource ID
	snapContent := gounitytypes.SnapshotContent{
		ResourceID: fileSystemID,
	}
	snap := gounitytypes.Snapshot{
		SnapshotContent: snapContent,
	}
	mockUnityClient.ExpectedCalls = nil
	mockUnityClient.On("FindFilesystemByID", mock.Anything, mock.Anything).Return(nil, gounity.ErrorFilesystemNotFound)
	mockUnityClient.On("FindSnapshotByID", mock.Anything, fileSystemID).Return(&snap, nil)
	mockUnityClient.On("GetFilesystemIDFromResID", mock.Anything, mock.Anything).Return("", gounity.ErrorFilesystemNotFound)
	_, _, _, err = s.getNFSShare(context.Background(), fileSystemID, arrayID)
	assert.Error(t, err)

	// Test Case: FindNFSShareByID returns an error
	fsContent := gounitytypes.FileContent{
		ID: fileSystemID,
		NFSShare: []gounitytypes.Share{
			{
				ID:   fileSystemID,
				Path: NFSShareLocalPath,
			},
		},
	}
	fs := gounitytypes.Filesystem{
		FileContent: fsContent,
	}
	mockUnityClient.ExpectedCalls = nil
	mockUnityClient.On("FindFilesystemByID", mock.Anything, fileSystemID).Return(&fs, nil)
	mockUnityClient.On("FindNFSShareByID", mock.Anything, mock.Anything).Return(nil, errors.New("error"))
	_, _, _, err = s.getNFSShare(context.Background(), fileSystemID, arrayID)
	assert.Error(t, err)

	// Test Case: FindNASServerByID returns an error
	mockUnityClient.ExpectedCalls = nil
	mockUnityClient.On("FindFilesystemByID", mock.Anything, mock.Anything).Return(&fs, nil)
	mockUnityClient.On("FindNFSShareByID", mock.Anything, mock.Anything).Return(nil, nil)
	mockUnityClient.On("FindNASServerByID", mock.Anything, mock.Anything).Return(nil, errors.New("error"))
	_, _, _, err = s.getNFSShare(context.Background(), fileSystemID, arrayID)
	assert.Error(t, err)

	// Test Case: NAS Server does not support NFSv3 and NFSv4 error
	nasContent := gounitytypes.NASServerContent{
		NFSServer: gounitytypes.NFSServer{NFSv3Enabled: false, NFSv4Enabled: false},
	}
	nas := gounitytypes.NASServer{
		NASServerContent: nasContent,
	}
	mockUnityClient.ExpectedCalls = nil
	mockUnityClient.On("FindFilesystemByID", mock.Anything, mock.Anything).Return(&fs, nil)
	mockUnityClient.On("FindNFSShareByID", mock.Anything, mock.Anything).Return(nil, nil)
	mockUnityClient.On("FindNASServerByID", mock.Anything, mock.Anything).Return(&nas, nil)
	_, _, _, err = s.getNFSShare(context.Background(), fileSystemID, arrayID)
	assert.Error(t, err)
}
