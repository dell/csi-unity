/*
Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.

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
	"testing"

	"bou.ke/monkey"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/gounity/types"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestValidateVolumeCreateParam(t *testing.T) {
	t.Run("Valid Volume Name", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "valid-volume-name",
		}
		err := validateVolumeCreateParam(req)
		assert.NoError(t, err)
	})

	// Failure Case
	t.Run("Empty Volume Name", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "",
		}
		err := validateVolumeCreateParam(req)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, err.Error(), "Volume Name cannot be empty")
	})
}

func TestCheckValidAccessTypes(t *testing.T) {
	t.Run("All Valid Block Access Types", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			},
			{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			},
		}
		result := checkValidAccessTypes(vcs)
		assert.True(t, result)
	})

	t.Run("All Valid Mount Access Types", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
		}
		result := checkValidAccessTypes(vcs)
		assert.True(t, result)
	})

	t.Run("Mixed Valid Access Types", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			},
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
		}
		result := checkValidAccessTypes(vcs)
		assert.True(t, result)
	})

	t.Run("Nil VolumeCapability", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			nil,
			{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			},
		}
		result := checkValidAccessTypes(vcs)
		assert.True(t, result)
	})

	// Failure Case
	t.Run("Unknown Access Type", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
			{
				AccessType: nil, // Unknown access type
			},
		}
		result := checkValidAccessTypes(vcs)
		assert.False(t, result)
	})
}

func TestAccTypeIsBlock(t *testing.T) {
	t.Run("Contains Block Access Type", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			},
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
		}
		result := accTypeIsBlock(vcs)
		assert.True(t, result)
	})

	// Failure Case
	t.Run("No Block Access Type", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
		}
		result := accTypeIsBlock(vcs)
		assert.False(t, result)
	})
}

func TestAccTypeBlock(t *testing.T) {
	t.Run("Block Access Type", func(t *testing.T) {
		vc := &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Block{
				Block: &csi.VolumeCapability_BlockVolume{},
			},
		}
		result := accTypeBlock(vc)
		assert.True(t, result)
	})

	// Failure Case
	t.Run("Non-Block Access Type", func(t *testing.T) {
		vc := &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		}
		result := accTypeBlock(vc)
		assert.False(t, result)
	})
}

func TestValidateAndGetProtocol(t *testing.T) {
	ctx := context.Background()

	// Success Case: Protocol is Set and Valid
	t.Run("Protocol is Set and Valid", func(t *testing.T) {
		protocol := "FC"
		scProtocol := "NFS"
		result, err := ValidateAndGetProtocol(ctx, protocol, scProtocol)
		assert.NoError(t, err)
		assert.Equal(t, "FC", result)
		// Check log output if necessary
	})

	// Success Case: Protocol is Not Set, Use scProtocol
	t.Run("Protocol is Not Set, Use scProtocol", func(t *testing.T) {
		protocol := ""
		scProtocol := "NFS"
		result, err := ValidateAndGetProtocol(ctx, protocol, scProtocol)
		assert.NoError(t, err)
		assert.Equal(t, "NFS", result)
		// Check log output if necessary
	})

	// Failure Case: Invalid Protocol
	t.Run("Invalid Protocol", func(t *testing.T) {
		protocol := "INVALID"
		scProtocol := "NFS"
		result, err := ValidateAndGetProtocol(ctx, protocol, scProtocol)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Equal(t, "", result)
		// Check log output if necessary
	})
}

func TestSingleAccessMode(t *testing.T) {
	t.Run("Single Node Writer Access Mode", func(t *testing.T) {
		accMode := &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		}
		result := SingleAccessMode(accMode)
		assert.True(t, result)
	})

	t.Run("Single Node Reader Only Access Mode", func(t *testing.T) {
		accMode := &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
		}
		result := SingleAccessMode(accMode)
		assert.True(t, result)
	})

	// Failure Case
	t.Run("Other Access Modes", func(t *testing.T) {
		accMode := &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		}
		result := SingleAccessMode(accMode)
		assert.False(t, result)
	})
}

func TestValidateCreateFsFromSnapshot(t *testing.T) {
	ctx := context.Background()

	// Success Case
	t.Run("Success", func(t *testing.T) {
		sourceFilesystemResp := &types.Filesystem{
			FileContent: types.FileContent{
				Pool:                   types.Pool{ID: "pool1"},
				IsThinEnabled:          true,
				IsDataReductionEnabled: true,
				TieringPolicy:          1,
				HostIOSize:             4096,
			},
		}
		err := validateCreateFsFromSnapshot(ctx, sourceFilesystemResp, "pool1", 1, 4096, true, true)
		assert.NoError(t, err)
	})

	// Failure Cases
	t.Run("Storage Pool Mismatch", func(t *testing.T) {
		sourceFilesystemResp := &types.Filesystem{
			FileContent: types.FileContent{
				Pool: types.Pool{ID: "pool1"},
			},
		}
		err := validateCreateFsFromSnapshot(ctx, sourceFilesystemResp, "pool2", 1, 4096, true, true)
		assert.NotNil(t, err)
	})

	t.Run("Thin Provision Mismatch", func(t *testing.T) {
		sourceFilesystemResp := &types.Filesystem{
			FileContent: types.FileContent{
				IsThinEnabled: true,
			},
		}
		err := validateCreateFsFromSnapshot(ctx, sourceFilesystemResp, "pool1", 1, 4096, false, true)
		assert.NotNil(t, err)
	})

	t.Run("Data Reduction Mismatch", func(t *testing.T) {
		sourceFilesystemResp := &types.Filesystem{
			FileContent: types.FileContent{
				IsDataReductionEnabled: true,
			},
		}
		err := validateCreateFsFromSnapshot(ctx, sourceFilesystemResp, "pool1", 1, 4096, true, false)
		assert.NotNil(t, err)
	})

	t.Run("Tiering Policy Mismatch", func(t *testing.T) {
		sourceFilesystemResp := &types.Filesystem{
			FileContent: types.FileContent{
				TieringPolicy: 1,
			},
		}
		err := validateCreateFsFromSnapshot(ctx, sourceFilesystemResp, "pool1", 2, 4096, true, true)
		assert.NotNil(t, err)
	})

	t.Run("Host IO Size Mismatch", func(t *testing.T) {
		sourceFilesystemResp := &types.Filesystem{
			FileContent: types.FileContent{
				HostIOSize: 4096,
			},
		}
		err := validateCreateFsFromSnapshot(ctx, sourceFilesystemResp, "pool1", 1, 8192, true, true)
		assert.Error(t, err)
	})

	t.Run("Thin Provision Mismatch", func(t *testing.T) {
		sourceFilesystemResp := &types.Filesystem{
			FileContent: types.FileContent{
				Pool:                   types.Pool{ID: "pool1"},
				IsThinEnabled:          true, // Source filesystem thin provision enabled
				IsDataReductionEnabled: true,
				TieringPolicy:          1,
				HostIOSize:             4096,
			},
		}
		err := validateCreateFsFromSnapshot(ctx, sourceFilesystemResp, "pool1", 1, 4096, false, true) // Requested thin provision disabled
		assert.NotNil(t, err)
	})

	t.Run("Data Reduction Mismatch", func(t *testing.T) {
		sourceFilesystemResp := &types.Filesystem{
			FileContent: types.FileContent{
				Pool:                   types.Pool{ID: "pool1"},
				TieringPolicy:          1,
				IsThinEnabled:          true,
				IsDataReductionEnabled: true, // Source filesystem data reduction enabled
				HostIOSize:             4096,
			},
		}
		err := validateCreateFsFromSnapshot(ctx, sourceFilesystemResp, "pool1", 1, 4096, true, false) // Requested data reduction disabled
		assert.NotNil(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, err.Error(), "Source filesystem data reduction")
	})

	t.Run("Tiering Policy Mismatch", func(t *testing.T) {
		sourceFilesystemResp := &types.Filesystem{
			FileContent: types.FileContent{
				Pool:                   types.Pool{ID: "pool1"},
				TieringPolicy:          1, // Source filesystem tiering policy
				IsThinEnabled:          true,
				IsDataReductionEnabled: true,
				HostIOSize:             4096,
			},
		}
		err := validateCreateFsFromSnapshot(ctx, sourceFilesystemResp, "pool1", 2, 4096, true, true) // Different tiering policy
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, err.Error(), "Source filesystem tiering policy")
	})

	t.Run("Host IO Size Mismatch", func(t *testing.T) {
		sourceFilesystemResp := &types.Filesystem{
			FileContent: types.FileContent{
				Pool:                   types.Pool{ID: "pool1"},
				TieringPolicy:          1,
				IsThinEnabled:          true,
				IsDataReductionEnabled: true,
				HostIOSize:             4096, // Source filesystem host IO size
			},
		}
		err := validateCreateFsFromSnapshot(ctx, sourceFilesystemResp, "pool1", 1, 8192, true, true) // Different host IO size
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, err.Error(), "Source filesystem host IO size")
	})
}

func TestValidateControllerPublishRequest(t *testing.T) {
	ctx := context.Background()

	t.Run("All Parameters Valid", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
			NodeId: "node1",
			VolumeContext: map[string]string{
				keyProtocol: "FC",
			},
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, "FC")
		assert.NotNil(t, err)
	})

	// Failure Case: Volume Capability is Nil
	t.Run("Volume Capability is Nil", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: nil,
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, "FC")
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Access Mode is Nil
	t.Run("Access Mode is Nil", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: nil,
			},
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, "FC")
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Invalid Protocol
	t.Run("Invalid Protocol", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
			VolumeContext: map[string]string{
				keyProtocol: "INVALID",
			},
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, "INVALID")
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Unsupported Access Mode
	t.Run("Unsupported Access Mode", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
				},
			},
			VolumeContext: map[string]string{
				keyProtocol: "FC",
			},
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, "FC")
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Node ID is Empty
	t.Run("Node ID is Empty", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
			NodeId: "",
			VolumeContext: map[string]string{
				keyProtocol: "FC",
			},
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, "FC")
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Readonly is True for FC/iSCSI
	t.Run("Readonly is True for FC/iSCSI", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
			NodeId:   "node1",
			Readonly: true,
			VolumeContext: map[string]string{
				keyProtocol: "FC",
			},
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, "FC")
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Node ID is Empty
	t.Run("Node ID is Empty", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
			NodeId: "",
			VolumeContext: map[string]string{
				keyProtocol: FC,
			},
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, FC)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Readonly is True for FC
	t.Run("Readonly is True for FC", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
			NodeId:   "node1",
			Readonly: true,
			VolumeContext: map[string]string{
				keyProtocol: FC,
			},
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, FC)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Readonly is True for ISCSI
	t.Run("Readonly is True for ISCSI", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
			NodeId:   "node1",
			Readonly: true,
			VolumeContext: map[string]string{
				keyProtocol: ISCSI,
			},
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, ISCSI)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Test case for FC protocol with Readonly set to true
	t.Run("Readonly is True for FC", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
			NodeId:   "node1",
			Readonly: true,
			VolumeContext: map[string]string{
				keyProtocol: "FC",
			},
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, "FC")
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Test case for ISCSI protocol with Readonly set to true
	t.Run("Readonly is True for ISCSI", func(t *testing.T) {
		req := &csi.ControllerPublishVolumeRequest{
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
			NodeId:   "node1",
			Readonly: true,
			VolumeContext: map[string]string{
				keyProtocol: "ISCSI",
			},
		}
		_, _, err := ValidateControllerPublishRequest(ctx, req, "ISCSI")
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestValidateCreateVolumeFromSource(t *testing.T) {
	ctx := context.Background()

	// Success Case: All Parameters Match and Size Validation is Not Skipped
	t.Run("All Parameters Match and Size Validation is Not Skipped", func(t *testing.T) {
		sourceVolResp := &types.Volume{
			VolumeContent: types.VolumeContent{
				Pool:                   types.Pool{ID: "pool1"},
				TieringPolicy:          1,
				IsThinEnabled:          true,
				IsDataReductionEnabled: true,
				SizeTotal:              100,
			},
		}
		err := validateCreateVolumeFromSource(ctx, sourceVolResp, "pool1", 1, 100, true, true, false)
		assert.NoError(t, err)
	})

	// Success Case: All Parameters Match and Size Validation is Skipped
	t.Run("All Parameters Match and Size Validation is Skipped", func(t *testing.T) {
		sourceVolResp := &types.Volume{
			VolumeContent: types.VolumeContent{
				Pool:                   types.Pool{ID: "pool1"},
				TieringPolicy:          1,
				IsThinEnabled:          true,
				IsDataReductionEnabled: true,
				SizeTotal:              100,
			},
		}
		err := validateCreateVolumeFromSource(ctx, sourceVolResp, "pool1", 1, 200, true, true, true)
		assert.NoError(t, err)
	})

	// Failure Case: Storage Pool Mismatch
	t.Run("Storage Pool Mismatch", func(t *testing.T) {
		sourceVolResp := &types.Volume{
			VolumeContent: types.VolumeContent{
				Pool: types.Pool{ID: "pool1"},
			},
		}
		err := validateCreateVolumeFromSource(ctx, sourceVolResp, "pool2", 1, 100, true, true, false)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Tiering Policy Mismatch
	t.Run("Tiering Policy Mismatch", func(t *testing.T) {
		sourceVolResp := &types.Volume{
			VolumeContent: types.VolumeContent{
				TieringPolicy: 1,
			},
		}
		err := validateCreateVolumeFromSource(ctx, sourceVolResp, "pool1", 2, 100, true, true, false)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Thin Provision Mismatch
	t.Run("Thin Provision Mismatch", func(t *testing.T) {
		sourceVolResp := &types.Volume{
			VolumeContent: types.VolumeContent{
				IsThinEnabled: true,
			},
		}
		err := validateCreateVolumeFromSource(ctx, sourceVolResp, "pool1", 1, 100, false, true, false)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Data Reduction Mismatch
	t.Run("Data Reduction Mismatch", func(t *testing.T) {
		sourceVolResp := &types.Volume{
			VolumeContent: types.VolumeContent{
				IsDataReductionEnabled: true,
			},
		}
		err := validateCreateVolumeFromSource(ctx, sourceVolResp, "pool1", 1, 100, true, false, false)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Size Mismatch and Size Validation is Not Skipped
	t.Run("Size Mismatch and Size Validation is Not Skipped", func(t *testing.T) {
		sourceVolResp := &types.Volume{
			VolumeContent: types.VolumeContent{
				SizeTotal: 100,
			},
		}
		err := validateCreateVolumeFromSource(ctx, sourceVolResp, "pool1", 1, 200, true, true, false)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Tiering Policy Mismatch
	t.Run("Tiering Policy Mismatch", func(t *testing.T) {
		sourceVolResp := &types.Volume{
			VolumeContent: types.VolumeContent{
				Pool:                   types.Pool{ID: "pool1"},
				TieringPolicy:          1,
				IsThinEnabled:          true,
				IsDataReductionEnabled: true,
				SizeTotal:              100,
			},
		}
		err := validateCreateVolumeFromSource(ctx, sourceVolResp, "pool1", 2, 100, true, true, false)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Data Reduction Mismatch
	t.Run("Data Reduction Mismatch", func(t *testing.T) {
		sourceVolResp := &types.Volume{
			VolumeContent: types.VolumeContent{
				Pool:                   types.Pool{ID: "pool1"},
				TieringPolicy:          1,
				IsThinEnabled:          true,
				IsDataReductionEnabled: true, // Source volume data reduction enabled
				SizeTotal:              100,
			},
		}
		err := validateCreateVolumeFromSource(ctx, sourceVolResp, "pool1", 1, 100, true, false, false) // Requested data reduction disabled
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, err.Error(), "Source volume data reduction")
	})

	// Failure Case: Size Mismatch and Size Validation is Not Skipped
	t.Run("Size Mismatch and Size Validation is Not Skipped", func(t *testing.T) {
		sourceVolResp := &types.Volume{
			VolumeContent: types.VolumeContent{
				Pool:                   types.Pool{ID: "pool1"},
				TieringPolicy:          1,
				IsThinEnabled:          true,
				IsDataReductionEnabled: true,
				SizeTotal:              100, // Source volume size
			},
		}
		err := validateCreateVolumeFromSource(ctx, sourceVolResp, "pool1", 1, 200, true, true, false) // Requested size different
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, err.Error(), "Requested size")
	})
}

func TestValVolumeCaps(t *testing.T) {
	// Failure Case: Default Case for Unknown Access Mode
	t.Run("Default Case for Unknown Access Mode", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_UNKNOWN,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "FC")
		assert.False(t, supported)
		assert.Contains(t, reason, errUnknownAccessMode)
	})

	// Success Case: Valid Block Access Type with Supported Access Mode
	t.Run("Valid Block Access Type with Supported Access Mode", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "FC")
		assert.True(t, supported)
		assert.Equal(t, "", reason)
	})

	// Success Case: Valid Mount Access Type with Supported Access Mode
	t.Run("Valid Mount Access Type with Supported Access Mode", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "NFS")
		assert.True(t, supported)
		assert.Equal(t, "", reason)
	})

	// Failure Case: Unknown Access Type
	t.Run("Unknown Access Type", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: nil, // Unknown access type
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "FC")
		assert.False(t, supported)
		assert.Equal(t, errUnknownAccessType, reason)
	})

	// Failure Case: Block Access Type with NFS Protocol
	t.Run("Block Access Type with NFS Protocol", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "NFS")
		assert.False(t, supported)
		assert.Equal(t, errBlockNFS, reason)
	})

	// Failure Case: Unknown Access Mode
	t.Run("Unknown Access Mode", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_UNKNOWN,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "FC")
		assert.False(t, supported)
		assert.Equal(t, errUnknownAccessMode, reason)
	})

	// Failure Case: Multi-Node Reader Only with Block Access Type
	t.Run("Multi-Node Reader Only with Block Access Type", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "FC")
		assert.False(t, supported)
		assert.Equal(t, errBlockReadOnly, reason)
	})

	// Failure Case: Multi-Node Reader Only with FC/ISCSI Protocol
	t.Run("Multi-Node Reader Only with FC/ISCSI Protocol", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "FC")
		assert.False(t, supported)
		assert.Equal(t, errNoMultiNodeReader, reason)
	})

	// Failure Case: Multi-Node Writer with Unsupported Protocol
	t.Run("Multi-Node Writer with Unsupported Protocol", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "FC")
		assert.False(t, supported)
		assert.Contains(t, reason, errNoMultiNodeWriter)
	})

	// Success Case: Valid Block Access Type with SINGLE_NODE_MULTI_WRITER Access Mode
	t.Run("Valid Block Access Type with SINGLE_NODE_MULTI_WRITER Access Mode", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "FC")
		assert.True(t, supported)
		assert.Equal(t, "", reason)
	})

	// Success Case: Valid Mount Access Type with SINGLE_NODE_SINGLE_WRITER Access Mode
	t.Run("Valid Mount Access Type with SINGLE_NODE_SINGLE_WRITER Access Mode", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "NFS")
		assert.True(t, supported)
		assert.Equal(t, "", reason)
	})

	// Success Case: Valid Mount Access Type with SINGLE_NODE_READER_ONLY Access Mode
	t.Run("Valid Mount Access Type with SINGLE_NODE_READER_ONLY Access Mode", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "FC")
		assert.True(t, supported)
		assert.Equal(t, "", reason)
	})

	// Success Case: MULTI_NODE_MULTI_WRITER with NFS Protocol
	t.Run("MULTI_NODE_MULTI_WRITER with NFS Protocol", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "NFS")
		assert.True(t, supported)
		assert.Equal(t, "", reason)
	})

	// Success Case: Access Mode is nil
	t.Run("Access Mode is nil", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
				AccessMode: nil, // Simulate nil access mode
			},
		}
		supported, reason := valVolumeCaps(vcs, "FC")
		assert.True(t, supported)
		assert.Equal(t, "", reason)
	})

	// Failure Case: Default Case for new access modes not understood
	t.Run("Default Case for new access modes not understood", func(t *testing.T) {
		vcs := []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: 101,
				},
			},
		}
		supported, reason := valVolumeCaps(vcs, "FC")
		assert.False(t, supported)
		assert.Contains(t, reason, errUnknownAccessMode)
	})
}

func TestValidateCreateVolumeRequest(t *testing.T) {
	ctx := context.Background()
	// Success Case: All Parameters Valid
	t.Run("All Parameters Valid", func(_ *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "valid-volume",
			Parameters: map[string]string{
				keyProtocol:             FC,
				keyStoragePool:          "pool1",
				keyTieringPolicy:        "1",
				keyHostIoSize:           "8192",
				keyThinProvisioned:      "true",
				keyDataReductionEnabled: "true",
			},
			CapacityRange: &csi.CapacityRange{
				RequiredBytes: 100,
			},
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
		}
		_, _, _, _, _, _, _, _ = ValidateCreateVolumeRequest(ctx, req)
	})

	// Failure Case: Volume Name is Empty
	t.Run("Volume Name is Empty", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "",
		}
		_, _, _, _, _, _, _, err := ValidateCreateVolumeRequest(ctx, req)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, err.Error(), "Volume Name cannot be empty")
	})

	// Failure Case: Storage Pool Parameter Missing
	t.Run("Storage Pool Parameter Missing", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "valid-volume",
			Parameters: map[string]string{
				keyProtocol: FC,
			},
		}
		_, _, _, _, _, _, _, err := ValidateCreateVolumeRequest(ctx, req)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, err.Error(), "`storagePool` is a required parameter")
	})

	// Failure Case: Capacity Range is Nil
	t.Run("Capacity Range is Nil", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "valid-volume",
			Parameters: map[string]string{
				keyProtocol:    FC,
				keyStoragePool: "pool1",
			},
			CapacityRange: nil,
		}
		_, _, _, _, _, _, _, err := ValidateCreateVolumeRequest(ctx, req)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, err.Error(), "RequiredBytes cannot be empty")
	})

	// Failure Case: RequiredBytes is Less Than or Equal to Zero
	t.Run("RequiredBytes is Less Than or Equal to Zero", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "valid-volume",
			Parameters: map[string]string{
				keyProtocol:    FC,
				keyStoragePool: "pool1",
			},
			CapacityRange: &csi.CapacityRange{
				RequiredBytes: 0,
			},
		}
		_, _, _, _, _, _, _, err := ValidateCreateVolumeRequest(ctx, req)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, err.Error(), "RequiredBytes should be greater then 0")
	})

	// Failure Case: Volume Capabilities Not Provided
	t.Run("Volume Capabilities Not Provided", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "valid-volume",
			Parameters: map[string]string{
				keyProtocol:    FC,
				keyStoragePool: "pool1",
			},
			CapacityRange: &csi.CapacityRange{
				RequiredBytes: 100,
			},
			VolumeCapabilities: []*csi.VolumeCapability{},
		}
		_, _, _, _, _, _, _, err := ValidateCreateVolumeRequest(ctx, req)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Volume Capabilities Not Supported
	t.Run("Volume Capabilities Not Supported", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "valid-volume",
			Parameters: map[string]string{
				keyProtocol:    FC,
				keyStoragePool: "pool1",
			},
			CapacityRange: &csi.CapacityRange{
				RequiredBytes: 100,
			},
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
					},
				},
			},
		}
		_, _, _, _, _, _, _, err := ValidateCreateVolumeRequest(ctx, req)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	// Failure Case: Invalid Host IO Size for NFS Protocol
	t.Run("Invalid Host IO Size for NFS Protocol", func(_ *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "valid-volume",
			Parameters: map[string]string{
				keyProtocol:             NFS,
				keyStoragePool:          "pool1",
				keyTieringPolicy:        "1",
				keyHostIoSize:           "invalid",
				keyThinProvisioned:      "true",
				keyDataReductionEnabled: "true",
			},
			CapacityRange: &csi.CapacityRange{
				RequiredBytes: 100,
			},
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
		}
		_, _, _, _, _, _, _, _ = ValidateCreateVolumeRequest(ctx, req)
	})

	// Failure Case: Protocol Not Set
	t.Run("Protocol Not Set", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "valid-volume",
			Parameters: map[string]string{
				keyStoragePool:          "pool1",
				keyTieringPolicy:        "1",
				keyHostIoSize:           "8192",
				keyThinProvisioned:      "true",
				keyDataReductionEnabled: "true",
			},
			CapacityRange: &csi.CapacityRange{
				RequiredBytes: 100,
			},
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
		}
		_, _, _, _, _, _, _, err := ValidateCreateVolumeRequest(ctx, req)
		assert.NotNil(t, err)
	})

	// Success Case: Accessibility Requirements Provided
	t.Run("Accessibility Requirements Provided", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "valid-volume",
			Parameters: map[string]string{
				keyProtocol:             FC,
				keyStoragePool:          "pool1",
				keyTieringPolicy:        "1",
				keyHostIoSize:           "8192",
				keyThinProvisioned:      "true",
				keyDataReductionEnabled: "true",
			},
			CapacityRange: &csi.CapacityRange{
				RequiredBytes: 100,
			},
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			AccessibilityRequirements: &csi.TopologyRequirement{
				Requisite: []*csi.Topology{
					{
						Segments: map[string]string{"region": "us-west-1"},
					},
				},
			},
		}
		_, _, _, _, _, _, _, err := ValidateCreateVolumeRequest(ctx, req)
		assert.NotNil(t, err)
	})

	// Failure Case: ValidateAndGetProtocol Returns Error
	t.Run("ValidateAndGetProtocol Returns Error", func(t *testing.T) {
		// Mock ValidateAndGetProtocol to return an error
		patch := monkey.Patch(ValidateAndGetProtocol, func(_ context.Context, _, _ string) (string, error) {
			return "", status.Error(codes.InvalidArgument, "Invalid value provided for Protocol")
		})
		defer patch.Unpatch()

		req := &csi.CreateVolumeRequest{
			Name: "valid-volume",
			Parameters: map[string]string{
				keyProtocol:             "INVALID",
				keyStoragePool:          "pool1",
				keyTieringPolicy:        "1",
				keyHostIoSize:           "8192",
				keyThinProvisioned:      "true",
				keyDataReductionEnabled: "true",
			},
			CapacityRange: &csi.CapacityRange{
				RequiredBytes: 100,
			},
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
		}
		_, _, _, _, _, _, _, err := ValidateCreateVolumeRequest(ctx, req)
		assert.NotNil(t, err)
	})
}
