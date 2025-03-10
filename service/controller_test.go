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
	"strconv"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/gounity"
	gounitymocks "github.com/dell/gounity/mocks"
	gounitytypes "github.com/dell/gounity/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGetCapacity(t *testing.T) {
	testConf.service.opts.AutoProbe = true
	// Test case: ArrayID is empty
	_, err := testConf.service.GetCapacity(context.Background(), &csi.GetCapacityRequest{
		Parameters: map[string]string{
			keyArrayID: "",
		},
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "Expected InvalidArgument status code")

	// Test case: ArrayID is not empty
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	queryResult := &gounitytypes.SystemCapacityMetricsQueryResult{
		Base: "system-capacity",
		Entries: []gounitytypes.SystemCapacityMetricsResultEntry{
			{
				Base:    "entry-1",
				Updated: "2024-02-24T10:00:00Z",
				Content: gounitytypes.SystemCapacityMetricResult{
					ID:               "array-001",
					SizeFree:         500000,
					SizeTotal:        1000000,
					SizeUsed:         450000,
					SizePreallocated: 10000,
					SizeSubscribed:   950000,
					TotalLogicalSize: 1200000,
				},
			},
		},
	}

	maxVolumSizeContent := gounitytypes.MaxVolumSizeContent{
		Limit: 20,
	}

	// MaxVolumSizeInfo is a response from querying systemLimit
	maxVolumSizeInfo := gounitytypes.MaxVolumSizeInfo{
		MaxVolumSizeContent: maxVolumSizeContent,
	}
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("GetCapacity", mock.Anything).Return(queryResult, nil)
	mockUnity.On("GetMaxVolumeSize", mock.Anything, "Limit_MaxLUNSize").Return(&maxVolumSizeInfo, nil)
	_, err = testConf.service.GetCapacity(context.Background(), &csi.GetCapacityRequest{
		Parameters: map[string]string{
			keyArrayID: arrayID,
		},
	})
	assert.Equal(t, nil, err)
}

func TestCreateVolume(t *testing.T) {
	// Test case: empty arrayID
	s := &service{}
	req := &csi.CreateVolumeRequest{
		Parameters: map[string]string{
			keyArrayID: " ",
		},
	}

	resp, err := s.CreateVolume(context.Background(), req)
	assert.Nil(t, resp)
	assert.Error(t, err)

	// Test case: non-empty arrayID

	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.CreateVolume(context.Background(), &csi.CreateVolumeRequest{
		Parameters: map[string]string{
			keyArrayID: arrayID,
		},
	})
	assert.Error(t, err)

	// test-case: ValidateCreateVolumeRequest returns nil
	req = &csi.CreateVolumeRequest{
		Name: "testVolume",
		Parameters: map[string]string{
			keyProtocol:             FC,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
		},
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 1024 * 1024 * 1024, // 1GB
		},
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Mount{ // Ensures it's not block storage
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
		},
		AccessibilityRequirements: &csi.TopologyRequirement{
			Preferred: []*csi.Topology{
				{
					Segments: map[string]string{
						"topology.kubernetes.io/zone": "us-east-1a",
					},
				},
			},
			Requisite: []*csi.Topology{
				{
					Segments: map[string]string{
						"topology.kubernetes.io/region": "us-east-1",
					},
				},
			},
		},
		VolumeContentSource: &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Volume{
				Volume: &csi.VolumeContentSource_VolumeSource{
					VolumeId: "source-volume-id", // Replace with actual source volume ID
				},
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.CreateVolume(context.Background(), req)
	assert.Error(t, err)

	// test-case: contentSource.GetVolume() is nil
	req = &csi.CreateVolumeRequest{
		Name: "testVolume",
		Parameters: map[string]string{
			keyProtocol:             FC,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
		},
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 1024 * 1024 * 1024, // 1GB
		},
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Mount{ // Ensures it's not block storage
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
		},
		AccessibilityRequirements: &csi.TopologyRequirement{
			Preferred: []*csi.Topology{
				{
					Segments: map[string]string{
						"topology.kubernetes.io/zone": "us-east-1a",
					},
				},
			},
			Requisite: []*csi.Topology{
				{
					Segments: map[string]string{
						"topology.kubernetes.io/region": "us-east-1",
					},
				},
			},
		},
		VolumeContentSource: &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Snapshot{
				Snapshot: &csi.VolumeContentSource_SnapshotSource{
					SnapshotId: "snapshot-id",
				},
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.CreateVolume(context.Background(), req)
	assert.Error(t, err)

	// test-case: protocol is NFS
	req = &csi.CreateVolumeRequest{
		Name: "1234",
		Parameters: map[string]string{
			keyProtocol:             NFS,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
			keyNasServer:            "nas1",
		},
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 1024 * 1024 * 1024, // 1GB
		},
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Mount{ // Ensures it's not block storage
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
		},
		AccessibilityRequirements: &csi.TopologyRequirement{
			Preferred: []*csi.Topology{
				{
					Segments: map[string]string{
						"topology.kubernetes.io/zone": "us-east-1a",
					},
				},
			},
			Requisite: []*csi.Topology{
				{
					Segments: map[string]string{
						"topology.kubernetes.io/region": "us-east-1",
					},
				},
			},
		},
		VolumeContentSource: &csi.VolumeContentSource{},
	}
	fc := gounitytypes.FileContent{
		ID: "id",
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}

	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(&fs, nil)
	_, err = testConf.service.CreateVolume(context.Background(), req)
	assert.Error(t, err)

	// test-case: FindFilesystemByName returns nil fs
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(nil, nil)
	mockUnity.On("CreateFilesystem", mock.Anything, "1234", "pool1", "", "nas1", mock.Anything, 1, 8192, 0, true, false).Return(&fs, errors.New("error"))
	_, err = testConf.service.CreateVolume(context.Background(), req)
	assert.Error(t, err)

	// test-case: CreateFilesystem returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(nil, nil).Once()
	mockUnity.On("CreateFilesystem", mock.Anything, "1234", "pool1", "", "nas1", mock.Anything, 1, 8192, 0, true, false).Return(&fs, nil)
	mockUnity.On("FindFilesystemByName", mock.Anything, "1234").Return(&fs, errors.New("error")).Once()
	_, err = testConf.service.CreateVolume(context.Background(), req)
	assert.NoError(t, err)

	// test-case: protocol != NFS
	req = &csi.CreateVolumeRequest{
		Name: "1234",
		Parameters: map[string]string{
			keyProtocol:             FC,
			keyStoragePool:          "pool1",
			keyTieringPolicy:        strconv.FormatInt(1, 10),
			keyHostIoSize:           strconv.FormatInt(8192, 10),
			keyThinProvisioned:      "true",
			keyDataReductionEnabled: "false",
			keyArrayID:              arrayID,
			keyHostIOLimitName:      "limit",
		},
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 1024 * 1024 * 1024, // 1GB
		},
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Mount{ // Ensures it's not block storage
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
		},
		AccessibilityRequirements: &csi.TopologyRequirement{
			Preferred: []*csi.Topology{
				{
					Segments: map[string]string{
						"topology.kubernetes.io/zone": "us-east-1a",
					},
				},
			},
			Requisite: []*csi.Topology{
				{
					Segments: map[string]string{
						"topology.kubernetes.io/region": "us-east-1",
					},
				},
			},
		},
		VolumeContentSource: &csi.VolumeContentSource{},
	}
	policy := gounitytypes.IoLimitPolicy{
		IoLimitPolicyContent: gounitytypes.IoLimitPolicyContent{
			ID:   "policy-id-123",
			Name: "HighPerformancePolicy",
		},
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
			Wwn:                    "6000ABC123456789",
			IsThinEnabled:          true,
			IsDataReductionEnabled: false,
			TieringPolicy:          2,
			Pool: gounitytypes.Pool{
				ID: "pool-456",
			},
		},
	}

	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindHostIOLimitByName", mock.Anything, "limit").Return(&policy, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.CreateVolume(context.Background(), req)
	assert.Error(t, err)

	// test-case: FindVolumeByName returns nil volume
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindHostIOLimitByName", mock.Anything, "limit").Return(&policy, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "1234").Return(nil, nil)
	mockUnity.On("CreateLun", mock.Anything, "1234", "pool1", "", mock.Anything, 1, mock.Anything, true, false).Return(&volume, gounity.ErrorVolumeNotFound)
	_, err = testConf.service.CreateVolume(context.Background(), req)
	assert.Error(t, err)

	// test-case: CreateLun returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindHostIOLimitByName", mock.Anything, "limit").Return(&policy, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "1234").Return(nil, nil).Once()
	mockUnity.On("CreateLun", mock.Anything, "1234", "pool1", "", mock.Anything, 1, mock.Anything, true, false).Return(&volume, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "1234").Return(&volume, nil).Once()
	_, err = testConf.service.CreateVolume(context.Background(), req)
	assert.NoError(t, err)
}

func TestDeleteVolume(t *testing.T) {
	ctx := context.Background()
	req := &csi.DeleteVolumeRequest{
		VolumeId: "test-volume-id",
	}

	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.DeleteVolume(ctx, req)
	assert.Error(t, err)

	// test case 2 : ValidateAndGetResourceDetails returns nil and protocl !=nfs
	snpshot := []gounitytypes.Snapshot{
		{
			SnapshotContent: gounitytypes.SnapshotContent{
				ResourceID: "snap-001",
				AccessType: 1,
			},
		},
	}
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("ListSnapshots", mock.Anything, 0, 0, "1234", "").Return(snpshot, 0, nil)
	testConf.service.opts.AutoProbe = true
	req = &csi.DeleteVolumeRequest{
		VolumeId: "tests-iscsi-testarrayid-1234",
	}
	_, err = testConf.service.DeleteVolume(ctx, req)
	assert.Error(t, err)

	// test case 3 : ValidateAndGetResourceDetails returns nil and protocl == NFS
	fc := gounitytypes.FileContent{
		ID: "id",
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("ListSnapshots", mock.Anything, 0, 0, "", "").Return(snpshot, 0, nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	req = &csi.DeleteVolumeRequest{
		VolumeId: "tests-NFS-testarrayid-1234",
	}
	_, err = testConf.service.DeleteVolume(ctx, req)
	assert.Error(t, err)

	// test-case 4: deleteFilesystem returns nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("ListSnapshots", mock.Anything, 0, 0, "", "").Return(nil, 0, nil)
	mockUnity.On("DeleteFilesystem", mock.Anything, "1234").Return(nil)
	testConf.service.opts.AutoProbe = true
	_, err = testConf.service.DeleteVolume(ctx, req)
	assert.NoError(t, err)

	// test-case 5: deleteFilesystem returns error as ErrorFilesystemNotFound
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("ListSnapshots", mock.Anything, 0, 0, "", "").Return(nil, 0, nil)
	mockUnity.On("DeleteFilesystem", mock.Anything, "1234").Return(gounity.ErrorFilesystemNotFound)
	testConf.service.opts.AutoProbe = true
	_, err = testConf.service.DeleteVolume(ctx, req)
	assert.NoError(t, err)

	// test-case 5: deleteFilesystem returns error
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("ListSnapshots", mock.Anything, 0, 0, "", "").Return(nil, 0, nil)
	mockUnity.On("DeleteFilesystem", mock.Anything, "1234").Return(errors.New("error"))
	testConf.service.opts.AutoProbe = true
	_, err = testConf.service.DeleteVolume(ctx, req)
	assert.Error(t, err)
}

func TestControllerPublishVolume(t *testing.T) {
	ctx := context.Background()
	req := &csi.ControllerPublishVolumeRequest{
		VolumeId: "test-volume-id",
	}

	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.ControllerPublishVolume(ctx, req)
	assert.Error(t, err)

	// test case 2 : ValidateAndGetResourceDetails returns nil

	testConf.service.opts.AutoProbe = true

	req = &csi.ControllerPublishVolumeRequest{
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER, // Valid mode
			},
			AccessType: &csi.VolumeCapability_Mount{ // Ensures it's not block storage
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		},
		NodeId: "test1,node-id",
		VolumeContext: map[string]string{
			"protocol": "FC",
		},
		VolumeId: "tests-NFS-testarrayid-1234",
	}
	hc := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "192.168.1.1"},
			{ID: "ip-port-2", Address: "192.168.1.2"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
	}
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindHostByName", mock.Anything, "test1").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "node-id").Return(&h, nil).Once()
	_, err = testConf.service.ControllerPublishVolume(ctx, req)
	assert.Error(t, err)
}

func TestControllerUnpublishVolume(t *testing.T) {
	ctx := context.Background()
	req := &csi.ControllerUnpublishVolumeRequest{
		VolumeId: "test-volume-id",
	}

	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.ControllerUnpublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: validateAndGetResourceDetails returns nil
	req = &csi.ControllerUnpublishVolumeRequest{
		VolumeId: "tests-NFS-testarrayid-1234",
	}
	testConf.service.opts.AutoProbe = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.ControllerUnpublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: nodeID is not empty
	hc := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "node-id"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
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
			Wwn:                    "6000ABC123456789",
			IsThinEnabled:          true,
			IsDataReductionEnabled: false,
		},
	}
	req = &csi.ControllerUnpublishVolumeRequest{
		VolumeId: "tests-FC-testarrayid-1234",
		NodeId:   "test1,node-id",
	}
	testConf.service.opts.AutoProbe = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindHostByName", mock.Anything, "test1").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "node-id").Return(&h, nil).Once()
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, gounity.ErrorVolumeNotFound)
	_, err = testConf.service.ControllerUnpublishVolume(ctx, req)
	assert.NoError(t, err)

	// test-case: FindVolumeByID returns error
	testConf.service.opts.AutoProbe = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindHostByName", mock.Anything, "test1").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "node-id").Return(&h, nil).Once()
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, gounity.ErrorHostNotFound)
	_, err = testConf.service.ControllerUnpublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: FindVolumeByID returns nil
	testConf.service.opts.AutoProbe = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindHostByName", mock.Anything, "test1").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "node-id").Return(&h, nil).Once()
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	mockUnity.On("ModifyVolumeExport", mock.Anything, "1234", []string{"hostID"}).Return(errors.New("error"))
	_, err = testConf.service.ControllerUnpublishVolume(ctx, req)
	assert.Error(t, err)

	// test-case: len(content.HostAccessResponse) == 0
	volume = gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			ResourceID:             "vol-123",
			Name:                   "TestVolume",
			HostAccessResponse:     []gounitytypes.HostAccessResponse{},
			IsThinEnabled:          true,
			IsDataReductionEnabled: false,
		},
	}
	testConf.service.opts.AutoProbe = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindHostByName", mock.Anything, "test1").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "node-id").Return(&h, nil).Once()
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)

	_, err = testConf.service.ControllerUnpublishVolume(ctx, req)
	assert.NoError(t, err)

	// test-case: protocol == NFS
	req = &csi.ControllerUnpublishVolumeRequest{
		VolumeId: "tests-NFS-testarrayid-1234",
		NodeId:   "test1,node-id",
	}

	sr := gounitytypes.StorageResource{
		ID: "resource-id",
	}
	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID:      "resource-id",
		Name:            "name",
		StorageResource: sr,
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}
	fc := gounitytypes.FileContent{
		ID: "id",
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	testConf.service.opts.AutoProbe = true
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindHostByName", mock.Anything, "test1").Return(&h, nil).Once()
	mockUnity.On("FindHostByName", mock.Anything, "node-id").Return(&h, nil).Once()
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	_, err = testConf.service.ControllerUnpublishVolume(ctx, req)
	assert.NoError(t, err)
}

func TestValidateVolumeCapabilities(t *testing.T) {
	ctx := context.Background()
	req := &csi.ValidateVolumeCapabilitiesRequest{
		VolumeId: "test-volume-id",
	}

	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.ValidateVolumeCapabilities(ctx, req)
	assert.Error(t, err)

	// test-case 2: validateAndGetResourceDetails return nil
	volume := gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			ResourceID:         "vol-123",
			Name:               "TestVolume",
			HostAccessResponse: []gounitytypes.HostAccessResponse{},
			Wwn:                "6000ABC123456789",
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, errors.New("error"))
	testConf.service.opts.AutoProbe = true
	req = &csi.ValidateVolumeCapabilitiesRequest{
		VolumeId: "tests-NFS-testarrayid-1234",
	}
	_, err = testConf.service.ValidateVolumeCapabilities(ctx, req)
	assert.Error(t, err)

	// test-case 3: FindVolumeByID return nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	testConf.service.opts.AutoProbe = true
	req = &csi.ValidateVolumeCapabilitiesRequest{
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		},
		Parameters: map[string]string{
			"protocol": "FC", // Ensure this key is set
		},
		VolumeContext: map[string]string{
			"protocol": "FC",
		},
		VolumeId: "tests-FC-testarrayid-1234",
	}
	_, err = testConf.service.ValidateVolumeCapabilities(ctx, req)
	assert.Error(t, err)

	// test-case 4: protocol is empty
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	testConf.service.opts.AutoProbe = true
	req = &csi.ValidateVolumeCapabilitiesRequest{
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		},
		Parameters: map[string]string{
			"protocol": "", // Ensure this key is set
		},
		VolumeContext: map[string]string{
			"protocol": "FC",
		},
		VolumeId: "tests-FC-testarrayid-1234",
	}
	_, err = testConf.service.ValidateVolumeCapabilities(ctx, req)
	assert.Error(t, err)
}

func TestCreateSnapshot(t *testing.T) {
	ctx := context.Background()
	req := &csi.CreateSnapshotRequest{
		SourceVolumeId: "",
	}

	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.CreateSnapshot(ctx, req)
	assert.Error(t, err)

	// SourceVolumeId is not empty
	req = &csi.CreateSnapshotRequest{
		SourceVolumeId: "dummy",
	}
	_, err = testConf.service.CreateSnapshot(ctx, req)
	assert.Error(t, err)

	// test-case 3: validateAndGetResourceDetails return error
	req = &csi.CreateSnapshotRequest{
		SourceVolumeId: "dummy",
		Name:           "name",
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	testConf.service.opts.AutoProbe = true
	_, err = testConf.service.CreateSnapshot(ctx, req)
	assert.Error(t, err)

	//test:case validateAndGetResourceDetails returns nil
	req = &csi.CreateSnapshotRequest{
		SourceVolumeId: "tests-NFS-testarrayid-1234",
		Name:           "name",
	}
	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}
	fc := gounitytypes.FileContent{
		ID: "id",
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, errors.New("error"))
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)

	_, err = testConf.service.CreateSnapshot(ctx, req)
	assert.Error(t, err)
}

func TestDeleteSnapshot(t *testing.T) {
	ctx := context.Background()
	req := &csi.DeleteSnapshotRequest{}

	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.DeleteSnapshot(ctx, req)
	assert.Error(t, err)

	// test-case: validateAndGetResourceDetails returns nil and FindSnapshotByID returns error
	req = &csi.DeleteSnapshotRequest{
		SnapshotId: "tests-NFS-testarrayid-1234",
	}
	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, errors.New("error"))
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("DeleteSnapshot", mock.Anything, "1234").Return(errors.New("error"))

	_, err = testConf.service.DeleteSnapshot(ctx, req)
	assert.Error(t, err)

	// test-case: DeleteSnapshot returns nil
	req = &csi.DeleteSnapshotRequest{
		SnapshotId: "tests-NFS-testarrayid-1234",
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, errors.New("error"))
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("DeleteSnapshot", mock.Anything, "1234").Return(nil)
	_, err = testConf.service.DeleteSnapshot(ctx, req)
	assert.NoError(t, err)
}

func TestListSnapshots(t *testing.T) {
	ctx := context.Background()
	req := &csi.ListSnapshotsRequest{}

	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.ListSnapshots(ctx, req)
	assert.Error(t, err)

	// test-case: validateAndGetResourceDetails returns nil
	req = &csi.ListSnapshotsRequest{
		SnapshotId:    "tests-NFS-testarrayid-1234",
		MaxEntries:    0,
		StartingToken: "10",
	}
	snpshot := []gounitytypes.Snapshot{
		{
			SnapshotContent: gounitytypes.SnapshotContent{
				ResourceID: "snap-001",
				AccessType: 1,
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true

	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("ListSnapshots", mock.Anything, 10, 100, "", "1234").Return(snpshot, 0, errors.New("error"))
	_, err = testConf.service.ListSnapshots(ctx, req)
	assert.Error(t, err)

	// test-case: ListSnapshots returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("ListSnapshots", mock.Anything, 10, 100, "", "1234").Return(snpshot, 0, nil)
	_, err = testConf.service.ListSnapshots(ctx, req)
	assert.NoError(t, err)
}

func TestControllerGetCapabilities(t *testing.T) {
	ctx := context.Background()
	req := &csi.ControllerGetCapabilitiesRequest{}

	o := Opts{
		IsVolumeHealthMonitorEnabled: true,
	}
	s := service{
		opts: o,
	}

	resp, err := s.ControllerGetCapabilities(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestControllerExpandVolume(t *testing.T) {
	// vol id is empty
	ctx := context.Background()
	req := &csi.ControllerExpandVolumeRequest{}

	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.ControllerExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: vol id is not empty
	req = &csi.ControllerExpandVolumeRequest{
		VolumeId: "vol-id",
	}
	_, err = testConf.service.ControllerExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: validateAndGetResourceDetails returns nil
	req = &csi.ControllerExpandVolumeRequest{
		VolumeId: "tests-NFS-testarrayid-1234",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10737418240, // 10 GiB (example size)
			LimitBytes:    21474836480, // 20 GiB (optional limit)
		},
	}
	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}
	fc := gounitytypes.FileContent{
		ID:        "id",
		SizeTotal: 21474836480,
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)

	_, err = testConf.service.ControllerExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: FindSnapshotByID return error
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, errors.New("error"))

	_, err = testConf.service.ControllerExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: FindFilesystemByID return nil and ExpandFilesystem returns error
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("ExpandFilesystem", mock.Anything, "1234", uint64(0x560000000)).Return(errors.New("error"))

	_, err = testConf.service.ControllerExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: ExpandFilesystem returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("ExpandFilesystem", mock.Anything, "1234", uint64(0x560000000)).Return(nil)

	_, err = testConf.service.ControllerExpandVolume(ctx, req)
	assert.NoError(t, err)

	// test-case: protocol != NFS
	req = &csi.ControllerExpandVolumeRequest{
		VolumeId: "tests-FC-testarrayid-1234",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10737418240, // 10 GiB (example size)
			LimitBytes:    21474836480, // 20 GiB (optional limit)
		},
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
			Wwn:                    "6000ABC123456789",
			IsThinEnabled:          true,
			IsDataReductionEnabled: false,
		},
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, errors.New("error"))

	_, err = testConf.service.ControllerExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: protocol != NFS and FindVolumeByID returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	mockUnity.On("ExpandVolume", mock.Anything, "1234", uint64(0x500000000)).Return(errors.New("error"))

	_, err = testConf.service.ControllerExpandVolume(ctx, req)
	assert.Error(t, err)

	// test-case: ExpandVolume returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	mockUnity.On("ExpandVolume", mock.Anything, "1234", uint64(0x500000000)).Return(nil)

	_, err = testConf.service.ControllerExpandVolume(ctx, req)
	assert.NoError(t, err)
}

func TestGetCSIVolumes(t *testing.T) {
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	volumes := []gounitytypes.Volume{
		{
			VolumeContent: gounitytypes.VolumeContent{
				Name:       "test-vol",
				Type:       1,
				Wwn:        "test-wwn",
				ResourceID: "vol-123",
				SizeTotal:  1000000000,
				Pool: gounitytypes.Pool{
					ID: "pool-1",
				},
			},
		},
	}

	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.getCSIVolumes(volumes)
	assert.Equal(t, nil, err)
}

func TestGetCSISnapshots(t *testing.T) {
	volID := "test-volume-id"
	protocol := "NFS"
	arrayID := "test-array-id"

	snaps := []gounitytypes.Snapshot{
		{
			SnapshotContent: gounitytypes.SnapshotContent{
				Name:         "snap1",
				State:        2,
				CreationTime: time.Now(),
				ResourceID:   "snap-123",
				Size:         1024,
			},
		},
		{
			SnapshotContent: gounitytypes.SnapshotContent{
				Name:         "snap2",
				State:        1,
				CreationTime: time.Time{},
				ResourceID:   "snap-456",
				Size:         2048,
			},
		},
	}

	s := &service{}
	_, err := s.getCSISnapshots(snaps, volID, protocol, arrayID)

	assert.NoError(t, err)
}

func TestGetFilesystemByResourceID(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	// test case: passing empty array id
	_, err := testConf.service.getFilesystemByResourceID(ctx, "", "")
	assert.Error(t, err)

	// test case: passing non-empty array id

	fc := gounitytypes.FileContent{
		ID: "id",
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "resource-id").Return("filesystem-id", nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "filesystem-id").Return(&fs, nil)
	_, err = testConf.service.getFilesystemByResourceID(ctx, "resource-id", arrayID)
	assert.Equal(t, nil, err)
}

func TestCreateFilesystemFromSnapshot(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	// test case: passing empty array id
	_, err := testConf.service.createFilesystemFromSnapshot(ctx, "", "", "")
	assert.Error(t, err)

	// test case: passing non-empty array id
	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}

	mockUnity.On("CopySnapshot", mock.Anything, "snap-id", "vol-name").Return(&snapshot, nil)
	mockUnity.On("FindSnapshotByName", mock.Anything, "vol-name").Return(&snapshot, nil)
	_, err = testConf.service.createFilesystemFromSnapshot(ctx, "snap-id", "vol-name", arrayID)
	assert.Equal(t, nil, err)
}

func TestCreateIdempotentSnapshot(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	// test case: passing empty array id
	_, err := testConf.service.createIdempotentSnapshot(ctx, "", "", "", "", "", "", false)
	assert.Error(t, err)

	// test case: passing non-empty array id
	sr := gounitytypes.StorageResource{
		ID: "resource-id",
	}
	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID:      "resource-id",
		Name:            "name",
		StorageResource: sr,
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}
	fc := gounitytypes.FileContent{
		ID: "id",
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}

	mockUnity.On("FindFilesystemByID", mock.Anything, "sourceVolID").Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, "sourceVolID").Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "resource-id").Return("sourceVolID", nil)
	_, err = testConf.service.createIdempotentSnapshot(ctx, "snapshotName", "sourceVolID", "description", "retentionDuration", "NFS", arrayID, true)
	assert.Error(t, err)

	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "sourceVolID").Return(&fs, nil)
	mockUnity.On("FindSnapshotByName", mock.Anything, "snapshotName").Return(&snapshot, nil)
	_, err = testConf.service.createIdempotentSnapshot(ctx, "snapshotName", "sourceVolID", "description", "retentionDuration", "NFS", arrayID, true)
	assert.Error(t, err)

	// test-case: protocol is FC
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindSnapshotByName", mock.Anything, "snapshotName").Return(nil, errors.New("error"))
	mockUnity.On("CreateSnapshotWithFsAccesType", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("error"))
	_, err = testConf.service.createIdempotentSnapshot(ctx, "snapshotName", "sourceVolID", "description", "retentionDuration", "FC", arrayID, true)
	assert.Error(t, err)

	// test-case: isClone is false
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindSnapshotByName", mock.Anything, "snapshotName").Return(nil, errors.New("error")).Once()
	mockUnity.On("CreateSnapshot", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
	mockUnity.On("FindSnapshotByName", mock.Anything, "snapshotName").Return(&snapshot, nil).Once()
	_, err = testConf.service.createIdempotentSnapshot(ctx, "snapshotName", "sourceVolID", "description", "retentionDuration", "FC", arrayID, false)
	assert.NoError(t, err)

	// test-case: FindSnapshotByName returns nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindSnapshotByName", mock.Anything, "snapshotName").Return(nil, errors.New("error")).Once()
	mockUnity.On("CreateSnapshot", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
	mockUnity.On("FindSnapshotByName", mock.Anything, "snapshotName").Return(nil, nil).Once()
	_, err = testConf.service.createIdempotentSnapshot(ctx, "snapshotName", "sourceVolID", "description", "retentionDuration", "FC", arrayID, false)
	assert.Error(t, err)
}

func TestGetHostID(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	// test case: passing empty array id
	_, err := testConf.service.getHostID(ctx, "", "", "")
	assert.Error(t, err)

	// test case: passing non-empty array id
	hc := gounitytypes.HostContent{
		ID: "id",
		IPPorts: []gounitytypes.IPPorts{
			{ID: "ip-port-1", Address: "192.168.1.1"},
			{ID: "ip-port-2", Address: "192.168.1.2"},
		},
	}
	h := gounitytypes.Host{
		HostContent: hc,
	}
	mockUnity.On("FindHostByName", mock.Anything, "shortHostname").Return(&h, nil)
	_, err = testConf.service.getHostID(ctx, arrayID, "shortHostname", "shortHostname")
	assert.Error(t, err)
}

func TestCreateVolumeClone(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	// test case: passing empty array id
	crParams := CRParams{
		VolumeName: "test-clone",
		Protocol:   "NFS",
	}
	sourceVolID := "source-volume-id"
	contentSource := &csi.VolumeContentSource{}
	preferredAccessibility := []*csi.Topology{}

	_, err := testConf.service.createVolumeClone(ctx, &crParams, "", arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// Test case: Source volume ID is non empty
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// Test case: validateAndGetResourceDetails returns nil
	sourceVolID = "tests-NFS-testarrayid-1234"
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, sourceVolID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// Test case: sourceVolID == arrayID
	sourceVolID = "tests-NFS-testarrayid-1234"
	fc := gounitytypes.FileContent{
		ID: "id",
		StorageResource: gounitytypes.Pool{
			ID:   "sr-001",
			Name: "StorageResource1",
		},
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}

	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, errors.New("error"))
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// Test case: FindSnapshotByID returns nil
	sourceVolID = "tests-NFS-testarrayid-1234"
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "").Return("", errors.New("error"))
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// Test case: protocol != fs
	sourceVolID = "tests-iscsi-testarrayid-1234"
	crParams = CRParams{
		VolumeName: "test-clone",
		Protocol:   "iscsi",
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
			Wwn:                    "6000ABC123456789",
			IsThinEnabled:          true,
			IsDataReductionEnabled: false,
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, errors.New("error"))
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: FindVolumeByID returns nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: validateCreateVolumeFromSource returns nil
	crParams = CRParams{
		VolumeName:    "test-clone",
		Protocol:      "iscsi",
		DataReduction: false,
		Thin:          true,
		TieringPolicy: int64(2),
		StoragePool:   "pool-456",
	}
	volume = gounitytypes.Volume{
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
			TieringPolicy:          2,
			Pool: gounitytypes.Pool{
				ID: "pool-456",
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "test-clone").Return(&volume, errors.New("error"))
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: FindVolumeByName returns nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "test-clone").Return(nil, nil)
	mockUnity.On("CreateCloneFromVolume", mock.Anything, "test-clone", "1234").Return(&volume, gounity.ErrorCreateSnapshotFailed)
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: CreateCloneFromVolume return error
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "test-clone").Return(nil, nil)
	mockUnity.On("CreateCloneFromVolume", mock.Anything, "test-clone", "1234").Return(&volume, gounity.ErrorCloningFailed)
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: CreateCloneFromVolume return nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "test-clone").Return(nil, nil)
	mockUnity.On("CreateCloneFromVolume", mock.Anything, "test-clone", "1234").Return(&volume, nil)
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: FindFilesystemByID returns nil
	crParams = CRParams{
		VolumeName:    "test-clone",
		Protocol:      "NFS",
		DataReduction: false,
		Thin:          true,
		TieringPolicy: int64(2),
		StoragePool:   "pool-456",
	}
	fc = gounitytypes.FileContent{
		ID:                     "id",
		IsThinEnabled:          true,
		IsDataReductionEnabled: false,
		TieringPolicy:          2,
		HostIOSize:             128 * 1024,
		Pool: gounitytypes.Pool{
			ID:   "pool-456",
			Name: "StorageResource1",
		},
		NFSShare: []gounitytypes.Share{
			{
				ID:   "nfs-001",
				Name: "NFS-Share1",
				Path: "/mnt/nfs",
				ParentSnap: gounitytypes.StorageResource{
					ID:   "vol-ID",
					Name: "Parent Snapshot",
				},
			},
		},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: validateCreateFsFromSnapshot returns nil
	crParams = CRParams{
		VolumeName:    "test-clone",
		Protocol:      "NFS",
		DataReduction: false,
		Thin:          true,
		TieringPolicy: int64(2),
		StoragePool:   "pool-456",
		HostIoSize:    128 * 1024,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: size == fsSize
	crParams = CRParams{
		VolumeName:    "test-clone",
		Protocol:      "NFS",
		DataReduction: false,
		Thin:          true,
		TieringPolicy: int64(2),
		StoragePool:   "pool-456",
		HostIoSize:    128 * 1024,
		Size:          -1610612736,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil)
	mockUnity.On("FindSnapshotByName", mock.Anything, mock.Anything).Return(&snapshot, nil)
	_, err = testConf.service.createVolumeClone(ctx, &crParams, sourceVolID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.NoError(t, err)
}

func TestCreateVolumeFromSnap(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	// test case: passing empty snapshotID
	crParams := CRParams{
		VolumeName: "test-clone",
	}
	snapshotID := ""
	contentSource := &csi.VolumeContentSource{}
	preferredAccessibility := []*csi.Topology{}

	_, err := testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test case: passing non- empty snapshotID
	snapshotID = "snapshot-ID"
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test case: validateAndGetResourceDetails returns nil
	snapshotID = "tests-NFS-testarrayid-1234"
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, snapshotID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test case: arrayID == sourceArrayID
	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID: "1234",
		Name:       "name",
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, errors.New("error"))
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: FindSnapshotByID returns nil
	crParams = CRParams{
		VolumeName:    "test-clone",
		Protocol:      "NFS",
		DataReduction: false,
		Thin:          true,
		TieringPolicy: int64(2),
		StoragePool:   "pool-456",
		HostIoSize:    128 * 1024,
		Size:          -1610612736,
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "").Return("", errors.New("error"))
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: GetFilesystemIDFromResID returns nil
	fc := gounitytypes.FileContent{
		ID: "id",
		StorageResource: gounitytypes.Pool{
			ID:   "sr-001",
			Name: "StorageResource1",
		},
		NFSShare: []gounitytypes.Share{
			{
				ID:   "nfs-001",
				Name: "NFS-Share1",
				Path: "/mnt/nfs",
				ParentSnap: gounitytypes.StorageResource{
					ID:   "vol-ID",
					Name: "Parent Snapshot",
				},
			},
		},
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "").Return("", nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "").Return(&fs, nil).Once()
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: validateCreateFsFromSnapshot returns nil
	fc = gounitytypes.FileContent{
		ID:                     "id",
		IsThinEnabled:          true,
		IsDataReductionEnabled: false,
		TieringPolicy:          2,
		HostIOSize:             128 * 1024,
		Pool: gounitytypes.Pool{
			ID:   "pool-456",
			Name: "StorageResource1",
		},
		NFSShare: []gounitytypes.Share{
			{
				ID:   "nfs-001",
				Name: "NFS-Share1",
				Path: "/mnt/nfs",
				ParentSnap: gounitytypes.StorageResource{
					ID:   "vol-ID",
					Name: "Parent Snapshot",
				},
			},
		},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	snapshotContent = gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
		AccessType: int(gounity.ProtocolAccessType),
		ParentSnap: gounitytypes.StorageResource{
			ID:   "1234",
			Name: "ParentSnapshot",
		},
	}
	snapshot = gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}

	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "").Return("", nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "").Return(&fs, nil).Once()
	mockUnity.On("FindSnapshotByName", mock.Anything, "test-clone").Return(&snapshot, nil)
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.NoError(t, err)

	// test-case: protocol is FC
	crParams = CRParams{
		VolumeName:    "test-clone",
		Protocol:      "FC",
		DataReduction: false,
		Thin:          true,
		TieringPolicy: int64(2),
		StoragePool:   "pool-456",
		HostIoSize:    128 * 1024,
		Size:          -1610612736,
	}
	snapshotContent = gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
		AccessType: int(gounity.ProtocolAccessType),
		StorageResource: gounitytypes.StorageResource{
			ID:   "1234",
			Name: "ParentSnapshot",
		},
	}
	snapshot = gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}
	volume := gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			ResourceID: "vol-123",
			Name:       "TestVolume",
			HostAccessResponse: []gounitytypes.HostAccessResponse{
				{
					HostContent: gounitytypes.HostContent{
						ID: "hostID",
					},
				},
			},
			IsThinEnabled:          true,
			IsDataReductionEnabled: false,
			TieringPolicy:          2,
			Pool: gounitytypes.Pool{
				ID: "pool-456",
			},
		},
	}

	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, errors.New("error"))
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: FindVolumeByID returns nil
	crParams = CRParams{
		VolumeName:    "test-clone",
		Protocol:      "FC",
		DataReduction: false,
		Thin:          true,
		TieringPolicy: int64(2),
		StoragePool:   "pool-456",
		Size:          -1610612736,
		HostIoSize:    128 * 1024,
	}

	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: snapResp.SnapshotContent.Size == size
	snapshotContent = gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
		AccessType: int(gounity.ProtocolAccessType),
		StorageResource: gounitytypes.StorageResource{
			ID:   "1234",
			Name: "ParentSnapshot",
		},
		Size: -1610612736,
	}
	snapshot = gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}
	volume = gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			ResourceID: "vol-123",
			HostAccessResponse: []gounitytypes.HostAccessResponse{
				{
					HostContent: gounitytypes.HostContent{
						ID: "hostID",
					},
				},
			},
			IsThinClone:            true,
			IsThinEnabled:          true,
			IsDataReductionEnabled: false,
			TieringPolicy:          2,
			Pool: gounitytypes.Pool{
				ID: "pool-456",
			},
			ParentSnap: gounitytypes.ParentSnap{
				ID: "1234",
			},
		},
	}

	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "test-clone").Return(&volume, nil)
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.NoError(t, err)

	// test-case: FindVolumeByName returns empty response
	snapshotContent = gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
		AccessType: int(gounity.ProtocolAccessType),
		StorageResource: gounitytypes.StorageResource{
			ID:   "1234",
			Name: "ParentSnapshot",
		},
		Size:         -1610612736,
		IsAutoDelete: true,
	}
	snapshot = gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "test-clone").Return(nil, nil)
	mockUnity.On("ModifySnapshotAutoDeleteParameter", mock.Anything, "1234").Return(errors.New("error"))
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: ModifySnapshotAutoDeleteParameter returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "test-clone").Return(nil, nil)
	mockUnity.On("ModifySnapshotAutoDeleteParameter", mock.Anything, "1234").Return(nil)
	mockUnity.On("CreteLunThinClone", mock.Anything, "test-clone", "1234", "1234").Return(&volume, errors.New("error"))
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.Error(t, err)

	// test-case: CreteLunThinClone returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "test-clone").Return(nil, nil).Once()
	mockUnity.On("ModifySnapshotAutoDeleteParameter", mock.Anything, "1234").Return(nil)
	mockUnity.On("CreteLunThinClone", mock.Anything, "test-clone", "1234", "1234").Return(&volume, nil)
	mockUnity.On("FindVolumeByName", mock.Anything, "test-clone").Return(&volume, errors.New("error"))
	_, err = testConf.service.createVolumeFromSnap(ctx, &crParams, snapshotID, arrayID, contentSource, mockUnity, preferredAccessibility)
	assert.NoError(t, err)
}

func TestDeleteFilesystem(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	volID := "vol-ID"
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	// test case: length of nfsshare is > 0
	fc := gounitytypes.FileContent{
		ID: "id",
		NFSShare: []gounitytypes.Share{
			{
				ID:   "nfs-001",
				Name: "NFS-Share1",
				Path: "/mnt/nfs",
			},
		},
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}

	mockUnity.On("FindFilesystemByID", mock.Anything, "vol-ID").Return(&fs, nil)
	err1, err2, err3 := testConf.service.deleteFilesystem(ctx, volID, mockUnity)
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Error(t, err3)

	// test case: length of nfsshare is <= 0
	fc = gounitytypes.FileContent{
		ID: "id",
		StorageResource: gounitytypes.Pool{
			ID:   "sr-001",
			Name: "StorageResource1",
		},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	snpshot := []gounitytypes.Snapshot{
		{
			SnapshotContent: gounitytypes.SnapshotContent{
				ResourceID: "snap-001",
				AccessType: 1,
			},
		},
	}

	mockUnity.On("FindFilesystemByID", mock.Anything, "vol-ID").Return(&fs, nil)
	mockUnity.On("ListSnapshots", mock.Anything, 0, 0, "sr-001", "").Return(snpshot, 0, errors.New("error"))

	err1, err2, err3 = testConf.service.deleteFilesystem(ctx, volID, mockUnity)
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Error(t, err3)

	// test-case: ListSnapshots does not return error
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "vol-ID").Return(&fs, nil)
	mockUnity.On("ListSnapshots", mock.Anything, 0, 0, "sr-001", "").Return(snpshot, 0, nil)

	err1, err2, err3 = testConf.service.deleteFilesystem(ctx, volID, mockUnity)
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Error(t, err3)

	// test-case: snapresponse is empty
	mockUnity.ExpectedCalls = nil
	snpshot = []gounitytypes.Snapshot{}
	mockUnity.On("FindFilesystemByID", mock.Anything, "vol-ID").Return(&fs, nil)
	mockUnity.On("ListSnapshots", mock.Anything, 0, 0, "sr-001", "").Return(snpshot, 0, nil)
	mockUnity.On("DeleteFilesystem", mock.Anything, "vol-ID").Return(nil)

	err1, err2, err3 = testConf.service.deleteFilesystem(ctx, volID, mockUnity)
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NoError(t, err3)

	// test case : FindFilesystemByID returns error
	mockUnity.ExpectedCalls = nil

	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}

	mockUnity.On("FindFilesystemByID", mock.Anything, "vol-ID").Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, "vol-ID").Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "").Return("", errors.New("error"))

	err1, err2, err3 = testConf.service.deleteFilesystem(ctx, volID, mockUnity)
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Error(t, err3)

	// test-case: GetFilesystemIDFromResID does not return error
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "vol-ID").Return(&fs, errors.New("error")).Once()
	mockUnity.On("FindSnapshotByID", mock.Anything, "vol-ID").Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "").Return("", nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "").Return(&fs, errors.New("error")).Once()
	err1, err2, err3 = testConf.service.deleteFilesystem(ctx, volID, mockUnity)
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Error(t, err3)

	// test-case: FindFilesystemByID  returns nil as error
	fc = gounitytypes.FileContent{
		ID: "id",
		StorageResource: gounitytypes.Pool{
			ID:   "sr-001",
			Name: "StorageResource1",
		},
		NFSShare: []gounitytypes.Share{
			{
				ID:   "nfs-001",
				Name: "NFS-Share1",
				Path: "/mnt/nfs",
				ParentSnap: gounitytypes.StorageResource{
					ID:   "vol-ID",
					Name: "Parent Snapshot",
				},
			},
		},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "vol-ID").Return(&fs, errors.New("error")).Once()
	mockUnity.On("FindSnapshotByID", mock.Anything, "vol-ID").Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "").Return("", nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "").Return(&fs, nil).Once()
	err1, err2, err3 = testConf.service.deleteFilesystem(ctx, volID, mockUnity)
	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Error(t, err3)

	// test-case: nfs share is empty
	fc = gounitytypes.FileContent{
		ID: "id",
		StorageResource: gounitytypes.Pool{
			ID:   "sr-001",
			Name: "StorageResource1",
		},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, "vol-ID").Return(&fs, errors.New("error")).Once()
	mockUnity.On("FindSnapshotByID", mock.Anything, "vol-ID").Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "").Return("", nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "").Return(&fs, nil).Once()
	mockUnity.On("DeleteFilesystemAsSnapshot", mock.Anything, "vol-ID", &fs).Return(nil).Once()

	err1, err2, err3 = testConf.service.deleteFilesystem(ctx, volID, mockUnity)
	assert.Error(t, err1)
	assert.NoError(t, err2)
	assert.NoError(t, err3)
}

func TestDeleteBlockVolume(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	volID := "vol-ID"
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	snpshot := []gounitytypes.Snapshot{
		{
			SnapshotContent: gounitytypes.SnapshotContent{
				ResourceID: "snap-001",
				AccessType: 1,
				Name:       "csi-snapforclone-",
			},
		},
	}

	// test-case: ListSnapshots returns error
	mockUnity.On("ListSnapshots", mock.Anything, 0, 0, "vol-ID", "").Return(snpshot, 0, errors.New("error"))

	err1, err2 := testConf.service.deleteBlockVolume(ctx, volID, mockUnity)
	assert.NoError(t, err1)
	assert.Error(t, err2)

	// test-case: ListSnapshots returns nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("ListSnapshots", mock.Anything, 0, 0, "vol-ID", "").Return(snpshot, 0, nil)

	err1, err2 = testConf.service.deleteBlockVolume(ctx, volID, mockUnity)
	assert.NoError(t, err1)
	assert.Error(t, err2)
}

func TestExportFilesystem(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	volID := "vol-ID"
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	hostID := "host-456"
	nodeID := "node-789"
	pinfo := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}
	accessMode := &csi.VolumeCapability_AccessMode{
		Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
	}

	// test-case: FindFilesystemByID returns error
	fc := gounitytypes.FileContent{
		ID:   "id",
		Name: "name",
		NFSShare: []gounitytypes.Share{
			{
				ID:   "nfs-001",
				Name: "NFS-Share1",
				Path: "/ ",
				ParentSnap: gounitytypes.StorageResource{
					ID:   "",
					Name: "Parent Snapshot",
				},
			},
		},
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}

	sr := gounitytypes.StorageResource{
		ID: volID,
	}
	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID:      volID,
		Name:            "name",
		StorageResource: sr,
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, volID).Return(&snapshot, errors.New("error"))
	_, err := testConf.service.exportFilesystem(ctx, volID, hostID, nodeID, arrayID, mockUnity, pinfo, accessMode)
	assert.Error(t, err)

	// test-case: FindSnapshotByID does not return error
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, volID).Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, volID).Return(volID, nil)
	_, err = testConf.service.exportFilesystem(ctx, volID, hostID, nodeID, arrayID, mockUnity, pinfo, accessMode)
	assert.Error(t, err)

	// test-case: FindFilesystemByID and CreateNFSShare return error
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", volID, gounity.NFSShareDefaultAccess("0")).Return(&fs, errors.New("error"))
	_, err = testConf.service.exportFilesystem(ctx, volID, hostID, nodeID, arrayID, mockUnity, pinfo, accessMode)
	assert.Error(t, err)

	// test-case:CreateNFSShare return nil
	nfsShare := gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
			Filesystem: gounitytypes.Pool{
				ID:   "fs-456",
				Name: "TestFilesystem",
			},
			ReadOnlyHosts: []gounitytypes.HostContent{
				{
					ID:      hostID,
					Name:    "ReadOnlyHost1",
					Address: "192.168.1.1",
				},
			},
			ReadWriteHosts: []gounitytypes.HostContent{
				{
					ID:      hostID,
					Name:    "ReadWriteHost1",
					Address: "192.168.1.2",
				},
			},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      hostID,
					Name:    "ReadOnlyRootHost1",
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
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", volID, gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, errors.New("error"))

	_, err = testConf.service.exportFilesystem(ctx, volID, hostID, nodeID, arrayID, mockUnity, pinfo, accessMode)
	assert.Error(t, err)

	// test-case: FindNFSShareByID does not return error
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", volID, gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)

	_, err = testConf.service.exportFilesystem(ctx, volID, hostID, nodeID, arrayID, mockUnity, pinfo, accessMode)
	assert.Error(t, err)

	// test-case: nfsShare.Path == NFSShareLocalPath
	fc = gounitytypes.FileContent{
		ID:   "id",
		Name: "name",
		NFSShare: []gounitytypes.Share{
			{
				ID:   "",
				Name: "NFS-Share1",
				Path: "/",
				ParentSnap: gounitytypes.StorageResource{
					ID:   "",
					Name: "Parent Snapshot",
				},
			},
		},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	nfsShare = gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
			Filesystem: gounitytypes.Pool{
				ID:   "fs-456",
				Name: "TestFilesystem",
			},
			ReadOnlyHosts: []gounitytypes.HostContent{},
			ReadWriteHosts: []gounitytypes.HostContent{
				{
					ID:      hostID,
					Name:    "ReadWriteHost1",
					Address: "192.168.1.2",
				},
			},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      hostID,
					Name:    "ReadOnlyRootHost1",
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
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", volID, gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)

	_, err = testConf.service.exportFilesystem(ctx, volID, hostID, nodeID, arrayID, mockUnity, pinfo, accessMode)
	assert.Error(t, err)

	// test-case: empty readWriteHosts
	nfsShare = gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
			Filesystem: gounitytypes.Pool{
				ID:   "fs-456",
				Name: "TestFilesystem",
			},
			ReadOnlyHosts:  []gounitytypes.HostContent{},
			ReadWriteHosts: []gounitytypes.HostContent{},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      hostID,
					Name:    "ReadOnlyRootHost1",
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
	accessMode = &csi.VolumeCapability_AccessMode{
		Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", volID, gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)

	_, err = testConf.service.exportFilesystem(ctx, volID, hostID, nodeID, arrayID, mockUnity, pinfo, accessMode)
	assert.Error(t, err)

	// test-case: empty readOnlyRootHosts
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
					ID:      hostID,
					Name:    "RootAccessHost1",
					Address: "192.168.1.4",
				},
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", volID, gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)

	_, err = testConf.service.exportFilesystem(ctx, volID, hostID, nodeID, arrayID, mockUnity, pinfo, accessMode)
	assert.NoError(t, err)

	// test-case: empty RootAccessHosts
	accessMode = &csi.VolumeCapability_AccessMode{
		Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
	}

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

	hostIDs := []string{"host-456"}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", volID, gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	mockUnity.On("ModifyNFSShareHostAccess", mock.Anything, volID, "", hostIDs, gounity.AccessType("READ_ONLY_ROOT")).Return(nil)

	_, err = testConf.service.exportFilesystem(ctx, volID, hostID, nodeID, arrayID, mockUnity, pinfo, accessMode)
	assert.NoError(t, err)

	// test-case: empty RootAccessHosts and access mode != VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	accessMode = &csi.VolumeCapability_AccessMode{
		Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
	}

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
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("CreateNFSShare", mock.Anything, "csishare-name", "/", volID, gounity.NFSShareDefaultAccess("0")).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	mockUnity.On("ModifyNFSShareHostAccess", mock.Anything, volID, "", hostIDs, gounity.AccessType("READ_WRITE_ROOT")).Return(errors.New("error"))

	_, err = testConf.service.exportFilesystem(ctx, volID, hostID, nodeID, arrayID, mockUnity, pinfo, accessMode)
	assert.Error(t, err)
}

func TestExportVolume(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	volID := "vol-ID"
	protocol := "FC"
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	hostID := "host-456"
	pinfo := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}
	// test-case: length of FcInitiators = 0
	host := &gounitytypes.Host{
		HostContent: gounitytypes.HostContent{
			ID:           "host-456",
			Name:         "example-host",
			FcInitiators: []gounitytypes.Initiators{
				// {ID: "fc-init-1"},
			},
			IscsiInitiators: []gounitytypes.Initiators{
				{ID: "iscsi-init-1"},
			},
			IPPorts: []gounitytypes.IPPorts{
				{ID: "ip-port-1", Address: "192.168.1.10"},
			},
			Address: "192.168.1.20",
		},
	}

	vc := &csi.VolumeCapability{
		AccessMode: &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		},
		AccessType: &csi.VolumeCapability_Mount{
			Mount: &csi.VolumeCapability_MountVolume{
				FsType: "ext4",
			},
		},
	}
	_, err := testConf.service.exportVolume(ctx, protocol, volID, hostID, "", "", mockUnity, pinfo, host, vc)
	assert.Error(t, err)

	// test-case: FindVolumeByID returns error
	protocol = "ISCSI"
	host = &gounitytypes.Host{
		HostContent: gounitytypes.HostContent{
			ID:   "host-456",
			Name: "example-host",
			FcInitiators: []gounitytypes.Initiators{
				{ID: "fc-init-1"},
			},
			IscsiInitiators: []gounitytypes.Initiators{},
			IPPorts: []gounitytypes.IPPorts{
				{ID: "ip-port-1", Address: "192.168.1.10"},
			},
			Address: "192.168.1.20",
		},
	}

	volume := gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			ResourceID: "vol-123",
			Name:       "TestVolume",
			HostAccessResponse: []gounitytypes.HostAccessResponse{
				{
					HostContent: gounitytypes.HostContent{
						ID:   hostID,
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
	mockUnity.On("FindVolumeByID", mock.Anything, volID).Return(&volume, errors.New("error"))
	_, err = testConf.service.exportVolume(ctx, protocol, volID, hostID, "", "", mockUnity, pinfo, host, vc)
	assert.Error(t, err)

	// test-case: FindVolumeByID returns nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindVolumeByID", mock.Anything, volID).Return(&volume, nil)
	_, err = testConf.service.exportVolume(ctx, protocol, volID, hostID, "", "", mockUnity, pinfo, host, vc)
	assert.NoError(t, err)

	// test-case: hostid is different
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindVolumeByID", mock.Anything, volID).Return(&volume, nil)
	_, err = testConf.service.exportVolume(ctx, protocol, volID, "hostID", "", "", mockUnity, pinfo, host, vc)
	assert.Error(t, err)

	// test-case: HostAccessResponse is empty
	volume = gounitytypes.Volume{
		VolumeContent: gounitytypes.VolumeContent{
			ResourceID:         "vol-123",
			Name:               "TestVolume",
			HostAccessResponse: []gounitytypes.HostAccessResponse{},
			Wwn:                "6000ABC123456789",
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindVolumeByID", mock.Anything, volID).Return(&volume, nil)
	mockUnity.On("ModifyVolumeExport", mock.Anything, volID, []string{"hostID"}).Return(errors.New("error"))
	_, err = testConf.service.exportVolume(ctx, protocol, volID, "hostID", "", "", mockUnity, pinfo, host, vc)
	assert.Error(t, err)

	// test-case: ModifyVolumeExport returns nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindVolumeByID", mock.Anything, volID).Return(&volume, nil)
	mockUnity.On("ModifyVolumeExport", mock.Anything, volID, []string{"hostID"}).Return(nil)
	_, err = testConf.service.exportVolume(ctx, protocol, volID, "hostID", "", "", mockUnity, pinfo, host, vc)
	assert.NoError(t, err)
}

func TestUnexportFilesystem(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	volID := "vol-ID"
	hostID := "host-456"
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	// test-case: FindSnapshotByID returns ErrorFilesystemNotFound error
	fc := gounitytypes.FileContent{
		ID: "id",
		NFSShare: []gounitytypes.Share{
			{
				ID:   "",
				Name: "NFS-Share1",
				Path: "/",
				ParentSnap: gounitytypes.StorageResource{
					ID:   "",
					Name: "Parent Snapshot",
				},
			},
		},
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}

	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}

	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, volID).Return(&snapshot, gounity.ErrorFilesystemNotFound)
	err := testConf.service.unexportFilesystem(ctx, volID, hostID, "nodeID", "volumeContextID", arrayID, mockUnity)
	assert.NoError(t, err)

	// test: case FindSnapshotByID return error
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, volID).Return(&snapshot, errors.New("error"))
	err = testConf.service.unexportFilesystem(ctx, volID, hostID, "nodeID", "volumeContextID", arrayID, mockUnity)
	assert.Error(t, err)

	// test: case FindSnapshotByID return nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, volID).Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "").Return("filesystem-id", errors.New("error"))
	err = testConf.service.unexportFilesystem(ctx, volID, hostID, "nodeID", "volumeContextID", arrayID, mockUnity)
	assert.Error(t, err)

	// test: case FindFilesystemByID return nil
	nfsShare := gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
			Filesystem: gounitytypes.Pool{
				ID:   "fs-456",
				Name: "TestFilesystem",
			},
			ReadOnlyHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "ReadOnlyHost1",
					Address: "192.168.1.1",
				},
			},
			ReadWriteHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "ReadWriteHost1",
					Address: "192.168.1.2",
				},
			},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "ReadOnlyRootHost1",
					Address: "192.168.1.3",
				},
			},
			RootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "RootAccessHost1",
					Address: "192.168.1.4",
				},
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, errors.New("error"))
	err = testConf.service.unexportFilesystem(ctx, volID, hostID, "nodeID", "volumeContextID", arrayID, mockUnity)
	assert.Error(t, err)

	// test-case: FindNFSShareByID returns nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	err = testConf.service.unexportFilesystem(ctx, volID, hostID, "nodeID", "volumeContextID", arrayID, mockUnity)
	assert.NoError(t, err)

	// test: case FindFilesystemByID return nil and nfsshare is empty
	fc = gounitytypes.FileContent{
		ID:       "id",
		NFSShare: []gounitytypes.Share{},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	err = testConf.service.unexportFilesystem(ctx, volID, hostID, "nodeID", "volumeContextID", arrayID, mockUnity)
	assert.NoError(t, err)

	// test-case : ReadOnlyHosts Id is same as hostID
	fc = gounitytypes.FileContent{
		ID: "id",
		NFSShare: []gounitytypes.Share{
			{
				ID:   "",
				Name: "NFS-Share1",
				Path: "/",
				ParentSnap: gounitytypes.StorageResource{
					ID:   "",
					Name: "Parent Snapshot",
				},
			},
		},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	nfsShare = gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
			Filesystem: gounitytypes.Pool{
				ID:   "fs-456",
				Name: "TestFilesystem",
			},
			ReadOnlyHosts: []gounitytypes.HostContent{
				{
					ID:      hostID,
					Name:    "ReadOnlyHost1",
					Address: "192.168.1.1",
				},
			},
			ReadWriteHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "ReadWriteHost1",
					Address: "192.168.1.2",
				},
			},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "ReadOnlyRootHost1",
					Address: "192.168.1.3",
				},
			},
			RootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "RootAccessHost1",
					Address: "192.168.1.4",
				},
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	err = testConf.service.unexportFilesystem(ctx, volID, hostID, "nodeID", "volumeContextID", arrayID, mockUnity)
	assert.Error(t, err)

	// test-case: ReadWriteHosts Id is same as hostID
	nfsShare = gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
			Filesystem: gounitytypes.Pool{
				ID:   "fs-456",
				Name: "TestFilesystem",
			},
			ReadOnlyHosts: []gounitytypes.HostContent{},
			ReadWriteHosts: []gounitytypes.HostContent{
				{
					ID:      hostID,
					Name:    "ReadWriteHost1",
					Address: "192.168.1.2",
				},
			},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "ReadOnlyRootHost1",
					Address: "192.168.1.3",
				},
			},
			RootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "RootAccessHost1",
					Address: "192.168.1.4",
				},
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	err = testConf.service.unexportFilesystem(ctx, volID, hostID, "nodeID", "volumeContextID", arrayID, mockUnity)
	assert.Error(t, err)

	// test-case: readOnlyRootHosts Id is same as hostID
	nfsShare = gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ID:   "nfs-share-123",
			Name: "TestNFSShare",
			Filesystem: gounitytypes.Pool{
				ID:   "fs-456",
				Name: "TestFilesystem",
			},
			ReadOnlyHosts:  []gounitytypes.HostContent{},
			ReadWriteHosts: []gounitytypes.HostContent{},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      hostID,
					Name:    "ReadOnlyRootHost1",
					Address: "192.168.1.3",
				},
			},
			RootAccessHosts: []gounitytypes.HostContent{
				{
					ID:      "hostID",
					Name:    "RootAccessHost1",
					Address: "192.168.1.4",
				},
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	mockUnity.On("ModifyNFSShareHostAccess", mock.Anything, volID, "", []string(nil), gounity.AccessType("READ_ONLY_ROOT")).Return(errors.New("error"))

	err = testConf.service.unexportFilesystem(ctx, volID, hostID, "nodeID", "volumeContextID", arrayID, mockUnity)
	assert.Error(t, err)

	// test-case: readWriteRootHosts  Id is same as hostID
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
					ID:      hostID,
					Name:    "RootAccessHost1",
					Address: "192.168.1.4",
				},
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	mockUnity.On("FindFilesystemByID", mock.Anything, volID).Return(&fs, nil)
	mockUnity.On("FindNFSShareByID", mock.Anything, "").Return(&nfsShare, nil)
	mockUnity.On("ModifyNFSShareHostAccess", mock.Anything, volID, "", []string(nil), gounity.AccessType("READ_WRITE_ROOT")).Return(errors.New("error"))

	err = testConf.service.unexportFilesystem(ctx, volID, hostID, "nodeID", "volumeContextID", arrayID, mockUnity)
	assert.Error(t, err)
}

func TestCreateMetricsCollection(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	// test-case: array id is empty
	_, err := testConf.service.createMetricsCollection(ctx, "", []string{}, 0)
	assert.Error(t, err)

	// test-case: array id is non-empty
	response := gounitytypes.MetricQueryCreateResponse{}
	mockUnity.On("CreateRealTimeMetricsQuery", mock.Anything, []string{}, 0).Return(&response, errors.New("error"))
	_, err = testConf.service.createMetricsCollection(ctx, arrayID, []string{}, 0)
	assert.Error(t, err)

	// test-case: CreateRealTimeMetricsQuery returns nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("CreateRealTimeMetricsQuery", mock.Anything, []string{}, 0).Return(&response, nil)
	_, err = testConf.service.createMetricsCollection(ctx, arrayID, []string{}, 0)
	assert.NoError(t, err)
}

func TestGetMetricsCollection(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	// test-case: array id is empty
	_, err := testConf.service.getMetricsCollection(ctx, "", 0)
	assert.Error(t, err)

	// test-case: array id is non-empty
	response := gounitytypes.MetricQueryResult{}
	mockUnity.On("GetMetricsCollection", mock.Anything, 0).Return(&response, errors.New("error"))
	_, err = testConf.service.getMetricsCollection(ctx, arrayID, 0)
	assert.Error(t, err)

	// test-case: GetMetricsCollection returns nil
	mockUnity.ExpectedCalls = nil
	mockUnity.On("GetMetricsCollection", mock.Anything, 0).Return(&response, nil)
	_, err = testConf.service.getMetricsCollection(ctx, arrayID, 0)
	assert.NoError(t, err)
}

func TestControllerGetVolume(t *testing.T) {
	ctx := context.Background()
	arrayID := "testarrayid"
	mockUnity := &gounitymocks.UnityClient{}
	boolTrue := true
	req := &csi.ControllerGetVolumeRequest{
		VolumeId: "test-volume-id",
	}
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{ArrayID: arrayID, UnityClient: mockUnity, SkipCertificateValidation: &boolTrue, IsDefault: &boolTrue})

	_, err := testConf.service.ControllerGetVolume(ctx, req)
	assert.Error(t, err)

	// test-case: validateAndGetResourceDetails returns nil

	req = &csi.ControllerGetVolumeRequest{
		VolumeId: "tests-ISCSI-testarrayid-1234",
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
			Wwn: "6000ABC123456789",
			Health: gounitytypes.HealthContent{
				Value: 1,
			},
		},
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, errors.New("error"))
	_, err = testConf.service.ControllerGetVolume(ctx, req)
	assert.Error(t, err)

	// test-case: FindVolumeByID returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindVolumeByID", mock.Anything, "1234").Return(&volume, nil)
	_, err = testConf.service.ControllerGetVolume(ctx, req)
	assert.NoError(t, err)

	// test-case: protocol == NFS
	req = &csi.ControllerGetVolumeRequest{
		VolumeId: "tests-NFS-testarrayid-1234",
	}
	fc := gounitytypes.FileContent{
		ID: "id",
		NFSShare: []gounitytypes.Share{
			{
				ID:   "nfs-001",
				Name: "NFS-Share1",
				Path: "/",
				ParentSnap: gounitytypes.StorageResource{
					ID:   "1234",
					Name: "Parent Snapshot",
				},
			},
		},
	}
	fs := gounitytypes.Filesystem{
		FileContent: fc,
	}
	snapshotContent := gounitytypes.SnapshotContent{
		ResourceID: "resource-id",
		Name:       "name",
	}
	snapshot := gounitytypes.Snapshot{
		SnapshotContent: snapshotContent,
	}
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, errors.New("error"))
	_, err = testConf.service.ControllerGetVolume(ctx, req)
	assert.Error(t, err)

	// test-case: FindSnapshotByID returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, errors.New("error"))
	mockUnity.On("FindSnapshotByID", mock.Anything, "1234").Return(&snapshot, nil)
	mockUnity.On("GetFilesystemIDFromResID", mock.Anything, "").Return("", errors.New("error"))
	_, err = testConf.service.ControllerGetVolume(ctx, req)
	assert.Error(t, err)

	// test-case: FindFilesystemByID returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil).Once()
	_, err = testConf.service.ControllerGetVolume(ctx, req)
	assert.NoError(t, err)

	// test-case: nfsShare.ParentSnap.ID == ""
	fc = gounitytypes.FileContent{
		ID: "id",
		NFSShare: []gounitytypes.Share{
			{
				ID:   "nfs-001",
				Name: "NFS-Share1",
				Path: "/",
				ParentSnap: gounitytypes.StorageResource{
					ID:   "",
					Name: "Parent Snapshot",
				},
			},
		},
	}
	fs = gounitytypes.Filesystem{
		FileContent: fc,
	}
	nfsShare := gounitytypes.NFSShare{
		NFSShareContent: gounitytypes.NFSShareContent{
			ReadOnlyHosts: []gounitytypes.HostContent{
				{
					ID: "hostID",
				},
			},
			ReadWriteHosts: []gounitytypes.HostContent{
				{
					ID: "hostID",
				},
			},
			ReadOnlyRootAccessHosts: []gounitytypes.HostContent{
				{
					ID: "hostID",
				},
			},
			RootAccessHosts: []gounitytypes.HostContent{
				{
					ID: "hostID",
				},
			},
		},
	}

	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil).Once()
	mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-001").Return(&nfsShare, errors.New("error"))

	_, err = testConf.service.ControllerGetVolume(ctx, req)
	assert.Error(t, err)

	// test-case: FindNFSShareByID returns nil
	mockUnity.ExpectedCalls = nil
	testConf.service.opts.AutoProbe = true
	mockUnity.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	mockUnity.On("FindFilesystemByID", mock.Anything, "1234").Return(&fs, nil).Once()
	mockUnity.On("FindNFSShareByID", mock.Anything, "nfs-001").Return(&nfsShare, nil)

	_, err = testConf.service.ControllerGetVolume(ctx, req)
	assert.NoError(t, err)
}
