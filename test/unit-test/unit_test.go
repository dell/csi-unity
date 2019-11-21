/*
Copyright (c) 2019 Dell EMC Corporation
All Rights Reserved
*/
package unit_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DATA-DOG/godog"
	"github.com/container-storage-interface/spec/lib/go/csi"
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
	listSnapshotsRequest              *csi.ListSnapshotsRequest
	listSnapshotsResponse             *csi.ListSnapshotsResponse
	listVolumesRequest                *csi.ListVolumesRequest
	listVolumesResponse               *csi.ListVolumesResponse
	capability                        *csi.VolumeCapability
	capabilities                      []*csi.VolumeCapability
	validateVolumeCapabilitiesRequest *csi.ValidateVolumeCapabilitiesRequest
	getCapacityRequest                *csi.GetCapacityRequest
	controllerGetCapabilitiesRequest  *csi.ControllerGetCapabilitiesRequest
	controllerExpandVolumeRequest     *csi.ControllerExpandVolumeRequest
	nodePublishVolumeRequest          *csi.NodePublishVolumeRequest
	nodeUnpublishVolumeRequest        *csi.NodeUnpublishVolumeRequest
	nodeStageVolumeRequest            *csi.NodeStageVolumeRequest
	nodeUnstageVolumeRequest          *csi.NodeUnstageVolumeRequest
	volID                             string
	volIDList                         []string
	maxRetryCount                     int
	nodeId                            string
}

//addError method appends an error to the error list
func (f *feature) addError(err error) {
	f.errs = append(make([]error, 0), err)
}

//thereAreNoErrors method verifies if there are is any error that has been added to the error list during scenario execution
func (f *feature) thereAreNoErrors() error {
	if len(f.errs) == 0 {
		return nil
	}
	return f.errs[0]
}

//aCSIService method is used to initialize/reset variables and errors before a test scenario begins
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

//aBasicBlockVolumeRequest method is used to build a Create volume request
func (f *feature) aBasicBlockVolumeRequest(volumeName string, size int) error {
	f.createVolumeRequest = nil
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagepool"] = os.Getenv("STORAGE_POOL")
	params["thinProvisioned"] = "true"
	params["isDataReductionEnabled"] = "false"
	params["tieringPolicy"] = "0"
	params["description"] = "CSI Volume Unit Test"
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

//aBasicBlockVolumeRequestWithParameters method is used to build a Create volume request with parameters
func (f *feature) aBasicBlockVolumeRequestWithParameters(volumeName string, size int, storagepool, thinProvisioned, isDataReductionEnabled, tieringPolicy string) error {
	f.createVolumeRequest = nil
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	if storagepool == "id"{
		params["storagepool"] = os.Getenv("STORAGE_POOL")
	}else if storagepool == "name"{
		params["storagepool"] = os.Getenv("STORAGE_POOL_NAME")
	}else {
		params["storagepool"] = "xyz"
	}
	params["thinProvisioned"] = thinProvisioned
	params["isDataReductionEnabled"] = isDataReductionEnabled
	params["tieringPolicy"] = tieringPolicy
	params["description"] = "CSI Volume Unit Test"
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

//iChangeVolumeCapabilityAccessmode is a method to change volume capabilities access mode
func (f *feature) iChangeVolumeCapabilityAccessmode() error {
	f.createVolumeRequest.VolumeCapabilities[0].AccessMode.Mode = 4
	return nil
}

//iCallCreateVolume - Test case to create volume
func (f *feature) iCallCreateVolume() error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	volResp, err := client.CreateVolume(ctx, f.createVolumeRequest)
	if err != nil {
		fmt.Printf("CreateVolume %s:\n", err.Error())
		f.volID = "NoID"
		f.addError(err)
	} else {
		fmt.Printf("CreateVolume %s (%s) %s\n", volResp.GetVolume().VolumeContext["Name"],
			volResp.GetVolume().VolumeId, volResp.GetVolume().VolumeContext["CreationTime"])
		f.volID = volResp.GetVolume().VolumeId
		f.volIDList = append(f.volIDList, volResp.GetVolume().VolumeId)
	}
	f.createVolumeResponse = volResp
	return nil
}

//whenICallDeleteVolume - Test case to delete volume
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

//whenICallPublishVolume - Test case to Publish volume to the given host
func (f *feature) whenICallPublishVolume() error {
	req := new(csi.ControllerPublishVolumeRequest)
	req.VolumeId = f.volID
	req.NodeId = os.Getenv("X_CSI_UNITY_NODENAME")
	f.nodeId = req.NodeId
	fmt.Printf("req.NodeId %s\n", req.NodeId)
	req.Readonly = false
	req.VolumeCapability = f.capability

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerPublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("ControllerPublishVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerPublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

//whenICallPublishVolumeWithParam - Test case to Publish volume to the given host with readonly as parameter
func (f *feature) whenICallPublishVolumeWithParam(hostName, readonly string) error {
	req := new(csi.ControllerPublishVolumeRequest)
	req.VolumeId = f.volID
	req.NodeId = hostName
	f.nodeId = req.NodeId
	fmt.Printf("req.NodeId %s\n", req.NodeId)
	read, _ := strconv.ParseBool(readonly)
	req.Readonly = read
	req.VolumeCapability = f.capability

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerPublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("ControllerPublishVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerPublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

//whenICallPublishVolumeWithVolumeId - Test case to Publish volume to the given host with volumeID as parameter
func (f *feature) whenICallPublishVolumeWithVolumeId(volId string) error {
	req := new(csi.ControllerPublishVolumeRequest)
	req.VolumeId = volId
	req.NodeId = os.Getenv("X_CSI_UNITY_NODENAME")
	f.nodeId = req.NodeId
	fmt.Printf("req.NodeId %s\n", req.NodeId)
	req.Readonly = false
	req.VolumeCapability = f.capability

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerPublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("ControllerPublishVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerPublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

//whenICallUnpublishVolume - Test case to unpublish volume
func (f *feature) whenICallUnpublishVolume() error {
	req := new(csi.ControllerUnpublishVolumeRequest)
	req.VolumeId = f.volID
	req.NodeId = f.nodeId
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerUnpublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("ControllerUnpublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerUnpublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

//iCallUnpublishVolumeWithVolumeId - Test case to unpublish volume with volume ID as parameter
func (f *feature) iCallUnpublishVolumeWithVolumeId(volId string) error {
	req := new(csi.ControllerUnpublishVolumeRequest)
	req.VolumeId = volId
	req.NodeId = f.nodeId
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerUnpublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("ControllerUnpublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerUnpublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

//theErrorMessageShouldContain is verification method to evaluate expected errors
func (f *feature) theErrorMessageShouldContain(expected string) error {
	// If expected is none, we expect no error, any error received is unexpected
	if expected == "none" {
		if len(f.errs) == 0 {
			return nil
		} else {
			err := f.errs[0]
			f.errs = make([]error, 0)
			return fmt.Errorf("Unexpected error(s): %s", err)
		}
	}
	// We expect an error...
	if len(f.errs) == 0 {
		return errors.New("there were no errors but we expected: " + expected)
	}
	err0 := f.errs[0]
	f.errs = make([]error, 0)
	if !strings.Contains(err0.Error(), expected) {
		return errors.New(fmt.Sprintf("Error %s does not contain the expected message: %s", err0.Error(), expected))
	}
	return nil
}

//aCreateSnapshotRequest method is used to build a Create Snapshot request
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

//iCallCreateSnapshot - Test case to create snapshot
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

//aDeleteSnapshotRequest method is used to build a Delete Snapshot request
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

//aDeleteSnapshotRequestWithID method is used to build a Delete Snapshot request with ID
func (f *feature) aDeleteSnapshotRequestWithID(snap_id string) error {
	f.deleteSnapshotRequest = nil
	req := new(csi.DeleteSnapshotRequest)
	req.SnapshotId = snap_id
	f.deleteSnapshotRequest = req
	return nil
}

//iCallDeleteSnapshot - Test case to delete snapshot
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

//aListSnapshotsRequest method is used to build a List Snapshots request
func (f *feature) aListSnapshotsRequest(startToken string, maxEntries int32, sourceVolumeId, snapshotId string) error {
	f.listSnapshotsRequest = nil
	req := new(csi.ListSnapshotsRequest)
	req.MaxEntries = maxEntries
	req.StartingToken = startToken
	req.SourceVolumeId = sourceVolumeId
	req.SnapshotId = snapshotId
	f.listSnapshotsRequest = req
	return nil
}

//iCallListSnapshots - Test case to list snapshots
func (f *feature) iCallListSnapshots() error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	listSnapsResponse, err := client.ListSnapshots(ctx, f.listSnapshotsRequest)
	if err != nil {
		fmt.Printf("List Snapshots: %s\n", err.Error())
		f.addError(err)
	}
	if listSnapsResponse != nil {
		fmt.Printf("No. of Snapshots retrieved: %d\nList Snapshots Response next token: %s\n", len(listSnapsResponse.Entries), listSnapsResponse.NextToken)
	}
	f.listSnapshotsResponse = listSnapsResponse
	return nil
}

//aListVolumesRequest method is used to build a List Volumes request
func (f *feature) aListVolumesRequest(maxEntries int32, startingToken string) error {
	f.listVolumesRequest = nil
	req := new(csi.ListVolumesRequest)
	req.MaxEntries = maxEntries
	req.StartingToken = startingToken
	f.listVolumesRequest = req
	return nil
}

//iCallListVolumes - Test case to list volumes
func (f *feature) iCallListVolumes() error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	listVolsResponse, err := client.ListVolumes(ctx, f.listVolumesRequest)
	if err != nil {
		fmt.Printf("List Volumes %s:\n", err.Error())
		f.addError(err)
	}
	if listVolsResponse != nil {
		fmt.Printf("No. of Volumes retrieved: %d\nList Volumes Response next token: %s\n", len(listVolsResponse.Entries), listVolsResponse.NextToken)
	}
	f.listVolumesResponse = listVolsResponse
	return nil
}

//iCallValidateVolumeCapabilitiesWithSameAccessMode - Test case to validate volume capabilities
func (f *feature) iCallValidateVolumeCapabilitiesWithSameAccessMode() error {
	f.validateVolumeCapabilitiesRequest = nil
	req := new(csi.ValidateVolumeCapabilitiesRequest)
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

//iCallValidateVolumeCapabilitiesWithDifferentAccessMode - Test case to validate volume capabilities
func (f *feature) iCallValidateVolumeCapabilitiesWithDifferentAccessMode() error {
	f.validateVolumeCapabilitiesRequest = nil
	req := new(csi.ValidateVolumeCapabilitiesRequest)
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

//iCallValidateVolumeCapabilitiesWithVolumeID - Test case to validate volume capabilities with volume Id as parameter
func (f *feature) iCallValidateVolumeCapabilitiesWithVolumeID(volID string) error {
	f.validateVolumeCapabilitiesRequest = nil
	req := new(csi.ValidateVolumeCapabilitiesRequest)
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

//iCallGetCapacityWithPool - Test case to get capacity with storage pool as parameter
func (f *feature) iCallGetCapacityWithPool(pool string) error {
	f.getCapacityRequest = nil
	req := new(csi.GetCapacityRequest)
	params := make(map[string]string)
	if pool == "id"{
		params["storagepool"] = os.Getenv("STORAGE_POOL")
	}else if pool == "name"{
		params["storagepool"] = os.Getenv("STORAGE_POOL_NAME")
	}else {
		params["storagepool"] = "xyz"
	}
	req.Parameters = params
	f.getCapacityRequest = req

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.GetCapacity(ctx, req)
	if err != nil {
		fmt.Printf("Get Capacity failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Get Capacity completed successfully\n")
	}
	return nil
}

//iCallControllerGetCapabilities - Test case for controller get capabilities
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

//iCallControllerExpandVolume - Test case for controller expand volume
func (f *feature) iCallControllerExpandVolume(new_size int) error {
	f.controllerExpandVolumeRequest = nil
	req := new(csi.ControllerExpandVolumeRequest)
	req.VolumeId = f.volID
	capRange := new(csi.CapacityRange)
	capRange.RequiredBytes = int64(new_size * 1024 * 1024 * 1024)
	capRange.LimitBytes = int64(new_size * 1024 * 1024 * 1024)
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

//iCallControllerExpandVolume - Test case for controller expand volume with volume id as parameter
func (f *feature) iCallControllerExpandVolumeWithVolume(new_size int, volID string) error {
	f.controllerExpandVolumeRequest = nil
	req := new(csi.ControllerExpandVolumeRequest)
	req.VolumeId = volID
	capRange := new(csi.CapacityRange)
	capRange.RequiredBytes = int64(new_size * 1024 * 1024 * 1024)
	capRange.LimitBytes = int64(new_size * 1024 * 1024 * 1024)
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

//whenICallNodePublishVolume - Test case for node publish volume
func (f *feature) whenICallNodePublishVolume(fsType, readonly string) error {
	f.nodePublishVolumeRequest = nil
	req := new(csi.NodePublishVolumeRequest)
	if f.createVolumeResponse != nil {
		req.VolumeId = f.volID
	} else {
		req.VolumeId = ""
	}
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

//whenICallNodePublishVolumeWithTargetPath - Test case for node publish volume with target path
func (f *feature) whenICallNodePublishVolumeWithTargetPath(target_path,fsType string) error {
	f.nodePublishVolumeRequest = nil
	req := new(csi.NodePublishVolumeRequest)
	if f.createVolumeResponse != nil {
		req.VolumeId = f.volID
	} else {
		req.VolumeId = ""
	}
	req.TargetPath = target_path
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
	req.Readonly = false
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

//whenICallNodePublishVolumeWithoutAccessmode - Test case for node publish volume without access mode
func (f *feature) whenICallNodePublishVolumeWithoutAccessmode(fsType string) error {
	f.nodePublishVolumeRequest = nil
	req := new(csi.NodePublishVolumeRequest)
	if f.createVolumeResponse != nil {
		req.VolumeId = f.volID
	} else {
		req.VolumeId = ""
	}
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mount.FsType = fsType
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	capability.AccessMode = nil
	req.VolumeCapability = capability
	req.TargetPath = os.Getenv("X_CSI_PUBLISH_TARGET_PATH")
	req.Readonly = false
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

//whenICallNodeUnPublishVolume - Test case for node unpublish volume
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

//whenICallNodeStageVolume - Test case for node stage volume
func (f *feature) whenICallNodeStageVolume() error {
	f.nodeStageVolumeRequest = nil
	req := new(csi.NodeStageVolumeRequest)
	req.VolumeId = f.volID
	if f.createVolumeResponse == nil {
		req.VolumeId = "NoID"
	}
	req.StagingTargetPath = os.Getenv("X_CSI_STAGING_TARGET_PATH")
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
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

//whenICallNodeStageVolumeWithTargetPath - Test case for node stage volume with target path as parameter
func (f *feature) whenICallNodeStageVolumeWithTargetPath(target_path string) error {
	f.nodeStageVolumeRequest = nil
	req := new(csi.NodeStageVolumeRequest)
	req.VolumeId = f.volID
	if f.createVolumeResponse == nil {
		req.VolumeId = "NoID"
	}
	req.StagingTargetPath = target_path
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
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

//whenICallNodeUnstageVolume - Test case for node unstage volume
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

//whenICallNodeGetInfo - Test case for node get info
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

//whenICallNodeGetCapabilities - Test case for node get capabilities
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

//whenICallGetPluginCapabilities - Test case to get plugin capabilities
func (f *feature) whenICallGetPluginCapabilities() error {
	req := new(csi.GetPluginCapabilitiesRequest)

	ctx := context.Background()
	client := csi.NewIdentityClient(grpcClient)
	_, err := client.GetPluginCapabilities(ctx, req)
	if err != nil {
		fmt.Printf("Get plugin capabilities failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Get plugin capabilities completed successfully\n")
	}
	return nil
}

//whenICallGetPluginInfo - Test case to get plugin info
func (f *feature) whenICallGetPluginInfo() error {
	req := new(csi.GetPluginInfoRequest)

	ctx := context.Background()
	client := csi.NewIdentityClient(grpcClient)
	_, err := client.GetPluginInfo(ctx, req)
	if err != nil {
		fmt.Printf("Get plugin info failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Get Plugin info completed successfully\n")
	}
	return nil
}

//aCSIServiceWithoutCSIUnityEndpoint - Test case to call probe without CSI Unity endpoint
func (f *feature) aCSIServiceWithoutCSIUnityEndpoint() error {
	stop()
	time.Sleep(10 * time.Second)
	os.Setenv("X_CSI_UNITY_ENDPOINT", "")
	os.Setenv("X_CSI_MODE", "controller")
	ctx := context.Background()
	grpcClient, stop = startServer(ctx)
	time.Sleep(5 * time.Second)
	client := csi.NewIdentityClient(grpcClient)
	probeResp, err := client.Probe(ctx, &csi.ProbeRequest{})
	if err != nil {
		fmt.Printf("Controller Probe failed with error: %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Controller Probe passed: %s\n", probeResp.Ready)
	}
	stop()
	time.Sleep(10 * time.Second)
	os.Setenv("X_CSI_MODE", "node")
	ctx = context.Background()
	grpcClient, stop = startServer(ctx)
	time.Sleep(10 * time.Second)
	client = csi.NewIdentityClient(grpcClient)
	probeResp, err = client.Probe(ctx, &csi.ProbeRequest{})
	if err != nil {
		fmt.Printf("Node Probe failed with error: %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node Probe passed: %s\n", probeResp.Ready)
	}

	os.Setenv("X_CSI_UNITY_ENDPOINT", "https://10.247.55.82")
	os.Setenv("X_CSI_MODE", "")
	return nil
}

//aCSIServiceWithCSIUnityPassword - Test case to call probe with CSI Unity password as parameter
func (f *feature) aCSIServiceWithCSIUnityPassword(password string) error {
	stop()
	time.Sleep(10 * time.Second)
	os.Setenv("X_CSI_UNITY_PASSWORD", password)
	os.Setenv("X_CSI_MODE", "controller")
	ctx := context.Background()
	grpcClient, stop = startServer(ctx)
	time.Sleep(10 * time.Second)
	client := csi.NewIdentityClient(grpcClient)
	probeResp, err := client.Probe(ctx, &csi.ProbeRequest{})
	if err != nil {
		fmt.Printf("Controller Probe failed with error: %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Controller Probe passed: %s\n", probeResp.Ready)
	}
	stop()
	time.Sleep(10 * time.Second)
	os.Setenv("X_CSI_MODE", "node")
	ctx = context.Background()
	grpcClient, stop = startServer(ctx)
	time.Sleep(10 * time.Second)
	client = csi.NewIdentityClient(grpcClient)
	probeResp, err = client.Probe(ctx, &csi.ProbeRequest{})
	if err != nil {
		fmt.Printf("Node Probe failed with error: %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node Probe passed: %s\n", probeResp.Ready)
	}

	os.Setenv("X_CSI_UNITY_PASSWORD", "Password123!")
	os.Setenv("X_CSI_MODE", "")
	return nil
}

//whenICallNodeStageVolumeWithoutProbe - Test case to call node stage volume without probe
func (f *feature) whenICallNodeStageVolumeWithoutProbe() error {
	f.nodeStageVolumeRequest = nil
	req := new(csi.NodeStageVolumeRequest)
	req.VolumeId = "vol"
	req.StagingTargetPath = "path"
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	req.VolumeCapability = capability
	f.nodeStageVolumeRequest = req

	stop()
	time.Sleep(10 * time.Second)
	os.Setenv("X_CSI_UNITY_ENDPOINT", "")
	ctx := context.Background()
	grpcClient, stop = startServer(ctx)
	time.Sleep(10 * time.Second)
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodeStageVolume(ctx, req)
	if err != nil {
		fmt.Printf("Node Stage failed with error: %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node stage passed\n")
	}
	return nil
}

//whenICallNodePublishVolumeWithoutProbe - Test case to call node publish volume without probe
func (f *feature) whenICallNodePublishVolumeWithoutProbe() error {
	f.nodePublishVolumeRequest = nil
	req := new(csi.NodePublishVolumeRequest)
	req.VolumeId = "vol"
	req.TargetPath = "path"
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	req.VolumeCapability = capability
	req.Readonly = false
	f.nodePublishVolumeRequest = req

	ctx := context.Background()
	time.Sleep(10 * time.Second)
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodePublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("Node publish failed with error: %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node publish passed\n")
	}
	return nil
}

//whenICallNodeUnstageVolumeWithoutProbe - Test case to call node unstage volume without probe
func (f *feature) whenICallNodeUnstageVolumeWithoutProbe() error {
	f.nodeUnstageVolumeRequest = nil
	req := new(csi.NodeUnstageVolumeRequest)
	req.VolumeId = "vol"
	req.StagingTargetPath = "path"
	f.nodeUnstageVolumeRequest = req

	ctx := context.Background()
	time.Sleep(10 * time.Second)
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodeUnstageVolume(ctx, req)
	if err != nil {
		fmt.Printf("Node unstage failed with error: %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node unstage passed\n")
	}
	os.Setenv("X_CSI_UNITY_ENDPOINT", "https://10.247.55.82")
	return nil
}

//whenICallNodeGetInfoWithoutProbe - Test case to call node get info without probe
func (f *feature) whenICallNodeGetInfoWithoutProbe() error {
	stop()
	time.Sleep(10 * time.Second)
	os.Setenv("X_CSI_UNITY_ENDPOINT", "")
	ctx := context.Background()
	grpcClient, stop = startServer(ctx)
	time.Sleep(5 * time.Second)
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodeGetInfo(ctx, &csi.NodeGetInfoRequest{})
	if err != nil {
		fmt.Printf("Node get info failed with error: %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node get info passed\n")
	}

	os.Setenv("X_CSI_UNITY_ENDPOINT", "https://10.247.55.82")
	return nil
}

//whenICallNodeGetInfoHostname - Test case to call node get info with hostname
func (f *feature) whenICallNodeGetInfoHostname(hostname string) error {
	stop()
	time.Sleep(10 * time.Second)
	os.Setenv("X_CSI_UNITY_NODENAME", hostname)
	ctx := context.Background()
	grpcClient, stop = startServer(ctx)
	time.Sleep(5 * time.Second)
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodeGetInfo(ctx, &csi.NodeGetInfoRequest{})
	if err != nil {
		fmt.Printf("Node get info failed with error: %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Node get info passed\n")
	}

	os.Setenv("X_CSI_UNITY_NODENAME", "lgloc183")
	return nil
}

func FeatureContext(s *godog.Suite) {
	f := &feature{}
	s.Step(`^a CSI service$`, f.aCSIService)
	s.Step(`^a CSI service without CSI Unity Endpoint$`, f.aCSIServiceWithoutCSIUnityEndpoint)
	s.Step(`^a CSI service with CSI Unity Password "([^"]*)"$`, f.aCSIServiceWithCSIUnityPassword)
	s.Step(`^a basic block volume request "([^"]*)" "(\d+)"$`, f.aBasicBlockVolumeRequest)
	s.Step(`^I change volume capability accessmode$`, f.iChangeVolumeCapabilityAccessmode)
	s.Step(`^a basic block volume request with volumeName "([^"]*)" size "([^"]*)" storagepool "([^"]*)" thinProvisioned "([^"]*)" isDataReductionEnabled "([^"]*)" tieringPolicy "([^"]*)"$`, f.aBasicBlockVolumeRequestWithParameters)
	s.Step(`^I call CreateVolume$`, f.iCallCreateVolume)
	s.Step(`^when I call DeleteVolume$`, f.whenICallDeleteVolume)
	s.Step(`^there are no errors$`, f.thereAreNoErrors)
	s.Step(`^when I call PublishVolume$`, f.whenICallPublishVolume)
	s.Step(`^when I call PublishVolume with host "([^"]*)" readonly "([^"]*)"$`, f.whenICallPublishVolumeWithParam)
	s.Step(`^when I call PublishVolume with volumeId "([^"]*)"$`, f.whenICallPublishVolumeWithVolumeId)
	s.Step(`^when I call UnpublishVolume$`, f.whenICallUnpublishVolume)
	s.Step(`^I call UnpublishVolume with volumeId "([^"]*)"$`, f.iCallUnpublishVolumeWithVolumeId)
	s.Step(`^the error message should contain "([^"]*)"$`, f.theErrorMessageShouldContain)
	s.Step(`^a create snapshot request "([^"]*)"$`, f.aCreateSnapshotRequest)
	s.Step(`^I call CreateSnapshot$`, f.iCallCreateSnapshot)
	s.Step(`^a delete snapshot request$`, f.aDeleteSnapshotRequest)
	s.Step(`^a delete snapshot request "([^"]*)"$`, f.aDeleteSnapshotRequestWithID)
	s.Step(`^I call DeleteSnapshot$`, f.iCallDeleteSnapshot)
	s.Step(`^a list snapshots request with startToken "([^"]*)" maxEntries "([^"]*)" sourceVolumeId "([^"]*)" snapshotId "([^"]*)"$`, f.aListSnapshotsRequest)
	s.Step(`^I call list snapshots$`, f.iCallListSnapshots)
	s.Step(`^a list volumes request with maxEntries "([^"]*)" startToken "([^"]*)"$`, f.aListVolumesRequest)
	s.Step(`^I call list volumes$`, f.iCallListVolumes)
	s.Step(`^I call validate volume capabilities with same access mode`, f.iCallValidateVolumeCapabilitiesWithSameAccessMode)
	s.Step(`^I call validate volume capabilities with different access mode$`, f.iCallValidateVolumeCapabilitiesWithDifferentAccessMode)
	s.Step(`^I call validate volume capabilities with volume ID "([^"]*)"$`, f.iCallValidateVolumeCapabilitiesWithVolumeID)
	s.Step(`^I call Get Capacity with storage pool "([^"]*)"$`, f.iCallGetCapacityWithPool)
	s.Step(`^I call Controller Get Capabilities$`, f.iCallControllerGetCapabilities)
	s.Step(`^I call Controller Expand Volume "([^"]*)"$`, f.iCallControllerExpandVolume)
	s.Step(`^I call Controller Expand Volume "([^"]*)" with volume "([^"]*)"$`, f.iCallControllerExpandVolumeWithVolume)
	s.Step(`^when I call NodePublishVolume fsType "([^"]*)" readonly "([^"]*)"$`, f.whenICallNodePublishVolume)
	s.Step(`^when I call NodePublishVolume without probe$`, f.whenICallNodePublishVolumeWithoutProbe)
	s.Step(`^when I call NodePublishVolume targetpath "([^"]*)" fsType "([^"]*)"$`, f.whenICallNodePublishVolumeWithTargetPath)
	s.Step(`^when I call NodeUnPublishVolume$`, f.whenICallNodeUnPublishVolume)
	s.Step(`^when I call NodeStageVolume$`, f.whenICallNodeStageVolume)
	s.Step(`^when I call NodeStageVolume with StagingTargetPath "([^"]*)"$`, f.whenICallNodeStageVolumeWithTargetPath)
	s.Step(`^when I call NodeStageVolume without probe$`, f.whenICallNodeStageVolumeWithoutProbe)
	s.Step(`^when I call NodeUnstageVolume$`, f.whenICallNodeUnstageVolume)
	s.Step(`^when I call NodeUnstageVolume without probe$`, f.whenICallNodeUnstageVolumeWithoutProbe)
	s.Step(`^When I call NodeGetInfo$`, f.whenICallNodeGetInfo)
	s.Step(`^When I call NodeGetInfo without probe$`, f.whenICallNodeGetInfoWithoutProbe)
	s.Step(`^When I call NodeGetInfo hostname "([^"]*)"$`, f.whenICallNodeGetInfoHostname)
	s.Step(`^When I call NodeGetCapabilities$`, f.whenICallNodeGetCapabilities)
	s.Step(`^when I call NodePublishVolume without accessmode and fsType "([^"]*)"$`, f.whenICallNodePublishVolumeWithoutAccessmode)
	s.Step(`^When I call GetPluginCapabilities$`, f.whenICallGetPluginCapabilities)
	s.Step(`^When I call GetPluginInfo$`, f.whenICallGetPluginInfo)
}