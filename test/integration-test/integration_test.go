/*
 Copyright © 2019 Dell Inc. or its subsidiaries. All Rights Reserved.

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

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/cucumber/godog"
)

const (
	RetrySleepTime = 1 * time.Second
	SleepTime      = 100 * time.Millisecond
)

type feature struct {
	errs                              []error
	createVolumeRequest               *csi.CreateVolumeRequest
	createVolumeResponse              *csi.CreateVolumeResponse
	createSnapshotRequest             *csi.CreateSnapshotRequest
	createSnapshotResponse            *csi.CreateSnapshotResponse
	deleteSnapshotRequest             *csi.DeleteSnapshotRequest
	deleteSnapshotResponse            *csi.DeleteSnapshotResponse
	capability                        *csi.VolumeCapability
	capabilities                      []*csi.VolumeCapability
	validateVolumeCapabilitiesRequest *csi.ValidateVolumeCapabilitiesRequest
	controllerGetCapabilitiesRequest  *csi.ControllerGetCapabilitiesRequest
	controllerExpandVolumeRequest     *csi.ControllerExpandVolumeRequest
	nodePublishVolumeRequest          *csi.NodePublishVolumeRequest
	nodeUnpublishVolumeRequest        *csi.NodeUnpublishVolumeRequest
	nodeStageVolumeRequest            *csi.NodeStageVolumeRequest
	nodeUnstageVolumeRequest          *csi.NodeUnstageVolumeRequest
	volID                             string
	volIDList                         []string
	maxRetryCount                     int
	volumeContext                     map[string]string
}

// addError method appends an error to the error list
func (f *feature) addError(err error) {
	f.errs = append(make([]error, 0), err)
}

// thereAreNoErrors method verifies if there are is any error that has been added to the error list during scenario execution
func (f *feature) thereAreNoErrors() error {
	if len(f.errs) == 0 {
		return nil
	}
	return f.errs[0]
}

// aCSIService method is used to initialize/reset variables and errors before a test scenario begins
func (f *feature) aCSIService() error {
	f.errs = make([]error, 0)
	f.createVolumeRequest = nil
	f.createVolumeResponse = nil
	f.volIDList = f.volIDList[:0]

	ctx := context.Background()
	fmt.Printf("testing Identity Probe\n")
	client := csi.NewIdentityClient(grpcClient)
	probeResp, err := client.Probe(ctx, &csi.ProbeRequest{})
	if err != nil {
		fmt.Printf("Probe failed with error: %s:\n", err.Error())
	} else {
		fmt.Printf("Probe passed: %s\n", probeResp.Ready)
	}

	return nil
}

// aBasicBlockVolumeRequest method to buils a Create volume request
func (f *feature) aBasicBlockVolumeRequest(volumeName, arrayID, protocol string, size int) error {
	f.createVolumeRequest = nil
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagePool"] = os.Getenv("STORAGE_POOL")
	params["thinProvisioned"] = "true"
	params["isDataReductionEnabled"] = "false"
	params["tieringPolicy"] = "0"
	params["description"] = "CSI Volume Unit Test"
	params["arrayId"] = os.Getenv(arrayID)
	params["protocol"] = protocol
	params["nasServer"] = os.Getenv("NAS_SERVER")
	req.Parameters = params
	req.Name = volumeName
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = int64(size * 1024 * 1024 * 1024)
	req.CapacityRange = capacityRange
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	f.capability = capability
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	f.createVolumeRequest = req
	return nil
}

// aBasicBlockVolumeRequest method with volume content source
func (f *feature) aBasicBlockVolumeRequestWithVolumeContentSource(volumeName, arrayID, protocol string, size int) error {
	f.createVolumeRequest = nil
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagePool"] = os.Getenv("STORAGE_POOL")
	params["arrayId"] = os.Getenv(arrayID)
	params["protocol"] = protocol
	req.Parameters = params
	req.Name = volumeName
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = int64(size * 1024 * 1024 * 1024)
	req.CapacityRange = capacityRange
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	f.capability = capability
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	volumeContentSourceSnapshotSource := new(csi.VolumeContentSource_SnapshotSource)
	volumeContentSourceSnapshotSource.SnapshotId = f.createSnapshotResponse.GetSnapshot().GetSnapshotId()
	volumeContentSourceSnapshot := new(csi.VolumeContentSource_Snapshot)
	volumeContentSourceSnapshot.Snapshot = volumeContentSourceSnapshotSource
	volumeContentSource := new(csi.VolumeContentSource)
	volumeContentSource.Type = volumeContentSourceSnapshot
	req.VolumeContentSource = volumeContentSource
	f.createVolumeRequest = req
	return nil
}

// iCallCreateVolume - Test case to create volume
func (f *feature) iCallCreateVolume() error {
	volResp, err := f.createVolume(f.createVolumeRequest)
	if err != nil {
		fmt.Printf("CreateVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("CreateVolume %s (%s) %s\n", volResp.GetVolume().VolumeContext["Name"],
			volResp.GetVolume().VolumeId, volResp.GetVolume().VolumeContext["CreationTime"])
		f.volID = volResp.GetVolume().VolumeId
		f.volIDList = append(f.volIDList, volResp.GetVolume().VolumeId)
		f.volumeContext = f.createVolumeRequest.Parameters
	}
	f.createVolumeResponse = volResp
	return nil
}

// createVolume is utility method that creates volume
func (f *feature) createVolume(req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	volResp, err := client.CreateVolume(ctx, req)
	return volResp, err
}

// whenICallDeleteVolume - Test case to delete volume
func (f *feature) whenICallDeleteVolume() error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	delVolReq := new(csi.DeleteVolumeRequest)
	delVolReq.VolumeId = f.volID
	var err error

	_, err = client.DeleteVolume(ctx, delVolReq)

	if err != nil {
		fmt.Printf("DeleteVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("DeleteVolume %s completed successfully\n", f.volID)
	}
	return nil
}

// whenICallDeleteAllCreatedVolumes - Test case to delete all created volumes in a scenario
func (f *feature) whenICallDeleteAllCreatedVolumes() error {
	for i := 0; i < len(f.volIDList); i++ {
		err := f.deleteVolume(f.volIDList[i])

		if err != nil {
			fmt.Printf("DeleteVolume %s:\n", err.Error())
			f.addError(err)
		} else {
			fmt.Printf("DeleteVolume %s completed successfully\n", f.volID)
		}
	}
	return nil
}

// deleteVolume is utility method that deletes volume
func (f *feature) deleteVolume(volID string) error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	delVolReq := new(csi.DeleteVolumeRequest)
	delVolReq.VolumeId = volID
	var err error

	_, err = client.DeleteVolume(ctx, delVolReq)

	if err != nil {
		fmt.Printf("DeleteVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("DeleteVolume %s completed successfully\n", f.volID)
	}
	return nil
}

// whenICallPublishVolume - Test case to Publish volume to the given host
func (f *feature) whenICallPublishVolume() error {
	err := f.controllerPublishVolume(f.volID, os.Getenv("X_CSI_UNITY_NODENAME"))
	if err != nil {
		fmt.Printf("ControllerPublishVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerPublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

// controllerPublishVolume is utility method that calls controller publish volume
func (f *feature) controllerPublishVolume(volID, hostName string) error {
	req := new(csi.ControllerPublishVolumeRequest)
	req.VolumeId = volID
	req.NodeId = hostName
	fmt.Printf("req.NodeId %s\n", req.NodeId)
	req.Readonly = false
	req.VolumeCapability = f.capability
	req.VolumeContext = f.volumeContext

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerPublishVolume(ctx, req)
	return err
}

// whenICallUnpublishVolume - Test case to unpublish volume
func (f *feature) whenICallUnpublishVolume() error {
	err := f.controllerUnpublishVolume(f.volID, os.Getenv("X_CSI_UNITY_NODENAME"))
	if err != nil {
		fmt.Printf("ControllerUnpublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerUnpublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

// controllerUnpublishVolume is utility method that calls controller unpublish volume
func (f *feature) controllerUnpublishVolume(volID, hostName string) error {
	req := new(csi.ControllerUnpublishVolumeRequest)
	req.VolumeId = volID
	req.NodeId = hostName
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerUnpublishVolume(ctx, req)
	return err
}

// theErrorMessageShouldContain is verification method to evaluate expected errors
func (f *feature) theErrorMessageShouldContain(expected string) error {
	// If expected is none, we expect no error, any error received is unexpected
	if expected == "none" {
		if len(f.errs) == 0 {
			return nil
		}
		err := f.errs[0]
		f.errs = make([]error, 0)
		return fmt.Errorf("Unexpected error(s): %s", err)

	}
	// We expect an error...
	if len(f.errs) == 0 {
		return errors.New("there were no errors but we expected: " + expected)
	}
	err0 := f.errs[0]
	f.errs = make([]error, 0)
	if !strings.Contains(err0.Error(), expected) {
		return fmt.Errorf("Error %s does not contain the expected message: %s", err0.Error(), expected)
	}
	return nil
}

// aCreateSnapshotRequest method is used to build a Create Snapshot request
func (f *feature) aCreateSnapshotRequest(name string) error {
	f.createSnapshotRequest = nil
	req := new(csi.CreateSnapshotRequest)
	params := make(map[string]string)
	params["description"] = ""
	params["retentionDuration"] = ""
	params["isReadOnly"] = ""
	req.Parameters = params
	if f.createVolumeResponse != nil {
		req.SourceVolumeId = f.createVolumeResponse.Volume.VolumeId
	} else {
		req.SourceVolumeId = ""
	}
	req.Name = name
	f.createSnapshotRequest = req
	return nil
}

// iCallCreateSnapshot - Test case to create snapshot
func (f *feature) iCallCreateSnapshot() error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	snapResp, err := client.CreateSnapshot(ctx, f.createSnapshotRequest)
	if err != nil {
		fmt.Printf("CreateSnapshot %s:\n", err.Error())
		f.addError(err)
	}
	if err == nil {
		fmt.Printf("Snapshot ID: %s \n", snapResp.Snapshot.SnapshotId)
	}
	f.createSnapshotResponse = snapResp
	return nil
}

// aDeleteSnapshotRequest method is used to build a Delete Snapshot request
func (f *feature) aDeleteSnapshotRequest() error {
	f.deleteSnapshotRequest = nil
	req := new(csi.DeleteSnapshotRequest)
	if f.createSnapshotResponse != nil {
		req.SnapshotId = f.createSnapshotResponse.Snapshot.SnapshotId
	} else {
		req.SnapshotId = ""
	}
	f.deleteSnapshotRequest = req
	return nil
}

// aDeleteSnapshotRequestWithID method is used to build a Delete Snapshot request with ID
func (f *feature) aDeleteSnapshotRequestWithID(snapID string) error {
	f.deleteSnapshotRequest = nil
	req := new(csi.DeleteSnapshotRequest)
	req.SnapshotId = snapID
	f.deleteSnapshotRequest = req
	return nil
}

// iCallDeleteSnapshot - Test case to delete snapshot
func (f *feature) iCallDeleteSnapshot() error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	delSnapResponse, err := client.DeleteSnapshot(ctx, f.deleteSnapshotRequest)
	if err != nil {
		fmt.Printf("DeleteSnapshot %s:\n", err.Error())
		f.addError(err)
	}
	f.deleteSnapshotResponse = delSnapResponse
	return nil
}

// iCallValidateVolumeCapabilitiesWithSameAccessMode - Test case to validate volume capabilities
func (f *feature) iCallValidateVolumeCapabilitiesWithSameAccessMode(protocol string) error {
	f.validateVolumeCapabilitiesRequest = nil
	req := new(csi.ValidateVolumeCapabilitiesRequest)
	params := make(map[string]string)
	params["protocol"] = protocol
	req.Parameters = params
	req.VolumeId = f.volID
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	f.capability = capability
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	f.validateVolumeCapabilitiesRequest = req

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ValidateVolumeCapabilities(ctx, req)
	if err != nil {
		fmt.Printf("ValidateVolumeCapabilities failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ValidateVolumeCapabilities completed successfully\n")
	}
	return nil
}

// iCallValidateVolumeCapabilitiesWithDifferentAccessMode - Test case to validate volume capabilities
func (f *feature) iCallValidateVolumeCapabilitiesWithDifferentAccessMode(protocol string) error {
	f.validateVolumeCapabilitiesRequest = nil
	req := new(csi.ValidateVolumeCapabilitiesRequest)
	params := make(map[string]string)
	params["protocol"] = protocol
	req.Parameters = params
	req.VolumeId = f.volID
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	capability.AccessMode = accessMode
	f.capability = capability
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	f.validateVolumeCapabilitiesRequest = req

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ValidateVolumeCapabilities(ctx, req)
	if err != nil {
		fmt.Printf("ValidateVolumeCapabilities failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ValidateVolumeCapabilities completed successfully\n")
	}
	return nil
}

// iCallValidateVolumeCapabilitiesWithVolumeID - Test case to validate volume capabilities with volume Id as parameter
func (f *feature) iCallValidateVolumeCapabilitiesWithVolumeID(protocol, volID string) error {
	f.validateVolumeCapabilitiesRequest = nil
	req := new(csi.ValidateVolumeCapabilitiesRequest)
	params := make(map[string]string)
	params["protocol"] = protocol
	req.Parameters = params
	req.VolumeId = volID
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	f.capability = capability
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	f.validateVolumeCapabilitiesRequest = req

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ValidateVolumeCapabilities(ctx, req)
	if err != nil {
		fmt.Printf("ValidateVolumeCapabilities failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ValidateVolumeCapabilities completed successfully\n")
	}
	return nil
}

// iCallControllerGetCapabilities - Test case for controller get capabilities
func (f *feature) iCallControllerGetCapabilities() error {
	f.controllerGetCapabilitiesRequest = nil
	req := new(csi.ControllerGetCapabilitiesRequest)
	f.controllerGetCapabilitiesRequest = req

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerGetCapabilities(ctx, req)
	if err != nil {
		fmt.Printf("Controller get capabilities failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Controller get capabilities completed successfully\n")
	}
	return nil
}

// iCallControllerExpandVolume - Test case for controller expand volume
func (f *feature) iCallControllerExpandVolume(newSize int) error {
	f.controllerExpandVolumeRequest = nil
	req := new(csi.ControllerExpandVolumeRequest)
	req.VolumeId = f.volID
	capRange := new(csi.CapacityRange)
	capRange.RequiredBytes = int64(newSize * 1024 * 1024 * 1024)
	capRange.LimitBytes = int64(newSize * 1024 * 1024 * 1024)
	req.CapacityRange = capRange
	f.controllerExpandVolumeRequest = req

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerExpandVolume(ctx, req)
	if err != nil {
		fmt.Printf("Controller expand volume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Controller expand volume completed successfully\n")
	}
	return nil
}

// iCallControllerExpandVolume - Test case for controller expand volume with volume id as parameter
func (f *feature) iCallControllerExpandVolumeWithVolume(newSize int, volID string) error {
	f.controllerExpandVolumeRequest = nil
	req := new(csi.ControllerExpandVolumeRequest)
	req.VolumeId = volID
	capRange := new(csi.CapacityRange)
	capRange.RequiredBytes = int64(newSize * 1024 * 1024 * 1024)
	capRange.LimitBytes = int64(newSize * 1024 * 1024 * 1024)
	req.CapacityRange = capRange
	f.controllerExpandVolumeRequest = req

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerExpandVolume(ctx, req)
	if err != nil {
		fmt.Printf("Controller expand volume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Controller expand volume completed successfully\n")
	}
	return nil
}

// whenICallNodePublishVolume - Test case for node publish volume
func (f *feature) whenICallNodePublishVolume(fsType, readonly string) error {
	f.nodePublishVolumeRequest = nil
	req := new(csi.NodePublishVolumeRequest)
	if f.createVolumeResponse != nil {
		req.VolumeId = f.volID
	} else {
		req.VolumeId = ""
	}
	req.StagingTargetPath = os.Getenv("X_CSI_STAGING_TARGET_PATH")
	req.TargetPath = os.Getenv("X_CSI_PUBLISH_TARGET_PATH")
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mount.FsType = fsType
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	req.VolumeCapability = capability
	read, _ := strconv.ParseBool(readonly)
	req.Readonly = read
	f.nodePublishVolumeRequest = req

	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodePublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("Node publish volume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node publish volume completed successfully\n")
	}
	return nil
}

// whenICallNodeUnPublishVolume - Test case for node unpublish volume
func (f *feature) whenICallNodeUnPublishVolume() error {
	f.nodeUnpublishVolumeRequest = nil
	req := new(csi.NodeUnpublishVolumeRequest)
	if f.createVolumeResponse != nil {
		req.VolumeId = f.volID
	} else {
		req.VolumeId = ""
	}
	if f.nodePublishVolumeRequest != nil {
		req.TargetPath = f.nodePublishVolumeRequest.TargetPath
	} else {
		req.TargetPath = ""
	}
	f.nodeUnpublishVolumeRequest = req

	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodeUnpublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("Node unpublish volume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node unpublish volume completed successfully\n")
	}
	return nil
}

// whenICallNodeStageVolume - Test case for node stage volume
func (f *feature) whenICallNodeStageVolume(fsType string) error {
	f.nodeStageVolumeRequest = nil
	req := new(csi.NodeStageVolumeRequest)
	req.VolumeId = f.volID
	if f.createVolumeResponse == nil {
		req.VolumeId = "NoID"
	}
	req.StagingTargetPath = os.Getenv("X_CSI_STAGING_TARGET_PATH")
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mount.FsType = fsType
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	req.VolumeCapability = capability
	f.nodeStageVolumeRequest = req

	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodeStageVolume(ctx, req)
	if err != nil {
		fmt.Printf("Node stage volume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node stage volume completed successfully\n")
	}
	return nil
}

// whenICallNodeUnstageVolume - Test case for node unstage volume
func (f *feature) whenICallNodeUnstageVolume() error {
	f.nodeUnstageVolumeRequest = nil
	req := new(csi.NodeUnstageVolumeRequest)
	if f.createVolumeResponse == nil {
		req.VolumeId = "NoID"
	} else {
		req.VolumeId = f.volID
	}
	if f.nodeStageVolumeRequest == nil {
		req.StagingTargetPath = ""
	} else {
		req.StagingTargetPath = f.nodeStageVolumeRequest.StagingTargetPath
	}
	f.nodeUnstageVolumeRequest = req

	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodeUnstageVolume(ctx, req)
	if err != nil {
		fmt.Printf("Node unstage volume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node unstage volume completed successfully\n")
	}
	return nil
}

// whenICallNodeGetInfo - Test case for node get info
func (f *feature) whenICallNodeGetInfo() error {
	req := new(csi.NodeGetInfoRequest)

	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodeGetInfo(ctx, req)
	if err != nil {
		fmt.Printf("Node get info failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node get info completed successfully\n")
	}
	return nil
}

// whenICallNodeGetCapabilities - Test case for node get capabilities
func (f *feature) whenICallNodeGetCapabilities() error {
	req := new(csi.NodeGetCapabilitiesRequest)

	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodeGetCapabilities(ctx, req)
	if err != nil {
		fmt.Printf("Node get capabilities failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node get capabilities completed successfully\n")
	}
	return nil
}

func FeatureContext(s *godog.ScenarioContext) {
	f := &feature{}
	s.Step(`^a CSI service$`, f.aCSIService)
	s.Step(`^a basic block volume request name "([^"]*)" arrayId "([^"]*)" protocol "([^"]*)" size "(\d+)"$`, f.aBasicBlockVolumeRequest)
	s.Step(`^a basic block volume request with volume content source with name "([^"]*)" arrayId "([^"]*)" protocol "([^"]*)" size "([^"]*)"$`, f.aBasicBlockVolumeRequestWithVolumeContentSource)
	s.Step(`^I call CreateVolume$`, f.iCallCreateVolume)
	s.Step(`^when I call DeleteVolume$`, f.whenICallDeleteVolume)
	s.Step(`^When I call DeleteAllCreatedVolumes$`, f.whenICallDeleteAllCreatedVolumes)
	s.Step(`^there are no errors$`, f.thereAreNoErrors)
	s.Step(`^when I call PublishVolume$`, f.whenICallPublishVolume)
	s.Step(`^when I call UnpublishVolume$`, f.whenICallUnpublishVolume)
	s.Step(`^the error message should contain "([^"]*)"$`, f.theErrorMessageShouldContain)
	s.Step(`^a create snapshot request "([^"]*)"$`, f.aCreateSnapshotRequest)
	s.Step(`^I call CreateSnapshot$`, f.iCallCreateSnapshot)
	s.Step(`^a delete snapshot request$`, f.aDeleteSnapshotRequest)
	s.Step(`^a delete snapshot request "([^"]*)"$`, f.aDeleteSnapshotRequestWithID)
	s.Step(`^I call DeleteSnapshot$`, f.iCallDeleteSnapshot)
	s.Step(`^I call validate volume capabilities with protocol "([^"]*)" with same access mode`, f.iCallValidateVolumeCapabilitiesWithSameAccessMode)
	s.Step(`^I call validate volume capabilities with protocol "([^"]*)" with different access mode$`, f.iCallValidateVolumeCapabilitiesWithDifferentAccessMode)
	s.Step(`^I call validate volume capabilities with protocol "([^"]*)" with volume ID "([^"]*)"$`, f.iCallValidateVolumeCapabilitiesWithVolumeID)
	s.Step(`^I call Controller Get Capabilities$`, f.iCallControllerGetCapabilities)
	s.Step(`^I call Controller Expand Volume "([^"]*)"$`, f.iCallControllerExpandVolume)
	s.Step(`^I call Controller Expand Volume "([^"]*)" with volume "([^"]*)"$`, f.iCallControllerExpandVolumeWithVolume)
	s.Step(`^when I call NodePublishVolume fsType "([^"]*)" readonly "([^"]*)"$`, f.whenICallNodePublishVolume)
	s.Step(`^when I call NodeUnPublishVolume$`, f.whenICallNodeUnPublishVolume)
	s.Step(`^when I call NodeStageVolume fsType "([^"]*)"$`, f.whenICallNodeStageVolume)
	s.Step(`^when I call NodeUnstageVolume$`, f.whenICallNodeUnstageVolume)
	s.Step(`^When I call NodeGetInfo$`, f.whenICallNodeGetInfo)
	s.Step(`^When I call NodeGetCapabilities$`, f.whenICallNodeGetCapabilities)
}
