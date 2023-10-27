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

package unit_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
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
	getCapacityRequest                *csi.GetCapacityRequest
	getCapacityResponse               *csi.GetCapacityResponse
	capability                        *csi.VolumeCapability
	capabilities                      []*csi.VolumeCapability
	validateVolumeCapabilitiesRequest *csi.ValidateVolumeCapabilitiesRequest
	controllerGetCapabilitiesRequest  *csi.ControllerGetCapabilitiesRequest
	controllerExpandVolumeRequest     *csi.ControllerExpandVolumeRequest
	controllerGetVolumeRequest        *csi.ControllerGetVolumeRequest
	nodePublishVolumeRequest          *csi.NodePublishVolumeRequest
	nodeUnpublishVolumeRequest        *csi.NodeUnpublishVolumeRequest
	nodeStageVolumeRequest            *csi.NodeStageVolumeRequest
	nodeUnstageVolumeRequest          *csi.NodeUnstageVolumeRequest
	volumeContext                     map[string]string
	volID                             string
	volIDList                         []string
	maxRetryCount                     int
	nodeId                            string
	ephemeral                         bool
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
	f.ephemeral = false

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

// aCSIServiceWithNode method is used to add node and initiators on array
func (f *feature) aCSIServiceWithNode() error {
	stop()
	time.Sleep(10 * time.Second)
	os.Setenv("X_CSI_MODE", "node")
	ctx := context.Background()
	grpcClient, stop = startServer(ctx)
	time.Sleep(5 * time.Second)

	ctx = context.Background()
	fmt.Printf("testing Identity Probe\n")
	client := csi.NewIdentityClient(grpcClient)
	probeResp, err := client.Probe(ctx, &csi.ProbeRequest{})
	time.Sleep(120 * time.Second)
	if err != nil {
		fmt.Printf("Probe failed with error: %s:\n", err.Error())
	} else {
		fmt.Printf("Probe passed: %s\n", probeResp.Ready)
	}

	stop()
	time.Sleep(10 * time.Second)
	os.Setenv("X_CSI_MODE", "")
	ctx = context.Background()
	grpcClient, stop = startServer(ctx)
	time.Sleep(5 * time.Second)
	return nil
}

// aCSIServiceWithNodeTopology method is used to add node, initiators and validate topology
func (f *feature) aCSIServiceWithNodeTopology() error {
	stop()
	time.Sleep(10 * time.Second)
	os.Setenv("X_CSI_MODE", "node")
	ctx := context.Background()
	grpcClient, stop = startServer(ctx)
	time.Sleep(5 * time.Second)

	ctx = context.Background()
	fmt.Printf("testing Identity Probe\n")
	client := csi.NewIdentityClient(grpcClient)
	probeResp, err := client.Probe(ctx, &csi.ProbeRequest{})
	time.Sleep(120 * time.Second)
	if err != nil {
		fmt.Printf("Probe failed with error: %s:\n", err.Error())
	} else {
		fmt.Printf("Probe passed: %s\n", probeResp.Ready)
	}

	nodeReq := new(csi.NodeGetInfoRequest)
	nodeClient := csi.NewNodeClient(grpcClient)
	nodeResp, e := nodeClient.NodeGetInfo(ctx, nodeReq)
	if nodeResp.AccessibleTopology.Segments == nil || e != nil {
		fmt.Printf("Erro getting topology segments")
	}
	stop()
	time.Sleep(10 * time.Second)
	os.Setenv("X_CSI_MODE", "")
	ctx = context.Background()
	grpcClient, stop = startServer(ctx)
	time.Sleep(5 * time.Second)
	return nil
}

// aBasicBlockVolumeRequest method is used to build a Create volume request
func (f *feature) aBasicBlockVolumeRequest(volumeName, protocol string, size int) error {
	f.createVolumeRequest = nil
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagePool"] = os.Getenv("STORAGE_POOL")
	params["thinProvisioned"] = "true"
	params["isDataReductionEnabled"] = "false"
	params["tieringPolicy"] = "0"
	params["description"] = "CSI Volume Unit Test"
	params["arrayId"] = os.Getenv("arrayId")
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

// aBasicRawBlockVolumeRequest method is used to build a Create volume request
func (f *feature) aBasicRawBlockVolumeRequest(volumeName, protocol string, size int) error {
	f.createVolumeRequest = nil
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagePool"] = os.Getenv("STORAGE_POOL")
	params["thinProvisioned"] = "true"
	params["isDataReductionEnabled"] = "false"
	params["tieringPolicy"] = "0"
	params["description"] = "CSI Volume Unit Test"
	params["arrayId"] = os.Getenv("arrayId")
	params["protocol"] = protocol
	req.Parameters = params
	req.Name = volumeName
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = int64(size * 1024 * 1024 * 1024)
	req.CapacityRange = capacityRange

	block := new(csi.VolumeCapability_BlockVolume)
	capability := new(csi.VolumeCapability)
	accessType := new(csi.VolumeCapability_Block)
	accessType.Block = block
	capability.AccessType = accessType
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

// aBasicFilesystemRequest method is used to build a Create volume request for filesystem
func (f *feature) aBasicFilesystemRequest(volumeName, protocol, am string, size int) error {
	f.createVolumeRequest = nil
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagePool"] = os.Getenv("STORAGE_POOL")
	params["thinProvisioned"] = "true"
	params["isDataReductionEnabled"] = "false"
	params["tieringPolicy"] = "0"
	params["description"] = "CSI Volume Unit Test"
	params["arrayId"] = os.Getenv("arrayId")
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
	if am == "ROX" {
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	} else if am == "RWX" {
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	} else if am == "RWO" {
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	} else {
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	}
	capability.AccessMode = accessMode
	f.capability = capability
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	f.createVolumeRequest = req
	return nil
}

// aBasicBlockVolumeRequestWithParameters method is used to build a Create volume request with parameters
func (f *feature) aBasicBlockVolumeRequestWithParameters(volumeName, protocol string, size int, storagepool, thinProvisioned, isDataReductionEnabled, tieringPolicy string) error {
	f.createVolumeRequest = nil
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	if storagepool == "id" {
		params["storagePool"] = os.Getenv("STORAGE_POOL")
	} else {
		params["storagePool"] = storagepool
	}
	params["thinProvisioned"] = thinProvisioned
	params["isDataReductionEnabled"] = isDataReductionEnabled
	params["tieringPolicy"] = tieringPolicy
	params["description"] = "CSI Volume Unit Test"
	params["arrayId"] = os.Getenv("arrayId")
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
func (f *feature) aBasicBlockVolumeRequestWithVolumeContentSource(volumeName, protocol string, size int) error {
	f.createVolumeRequest = nil
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagePool"] = os.Getenv("STORAGE_POOL")
	params["arrayId"] = os.Getenv("arrayId")
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
	volumeContentSource_SnapshotSource := new(csi.VolumeContentSource_SnapshotSource)
	volumeContentSource_SnapshotSource.SnapshotId = f.createSnapshotResponse.GetSnapshot().GetSnapshotId()
	volumeContentSource_Snapshot := new(csi.VolumeContentSource_Snapshot)
	volumeContentSource_Snapshot.Snapshot = volumeContentSource_SnapshotSource
	volumeContentSource := new(csi.VolumeContentSource)
	volumeContentSource.Type = volumeContentSource_Snapshot
	req.VolumeContentSource = volumeContentSource
	f.createVolumeRequest = req
	return nil
}

// aBasicBlockVolumeRequest method with volume content source as volume
func (f *feature) aBasicBlockVolumeRequestWithVolumeContentSourceAsVolume(volumeName, protocol string, size int) error {
	f.createVolumeRequest = nil
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagePool"] = os.Getenv("STORAGE_POOL")
	params["arrayId"] = os.Getenv("arrayId")
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
	volumeContentSource_VolumeSource := new(csi.VolumeContentSource_VolumeSource)
	volumeContentSource_VolumeSource.VolumeId = f.createVolumeResponse.GetVolume().GetVolumeId()
	volumeContentSource_Volume := new(csi.VolumeContentSource_Volume)
	volumeContentSource_Volume.Volume = volumeContentSource_VolumeSource
	volumeContentSource := new(csi.VolumeContentSource)
	volumeContentSource.Type = volumeContentSource_Volume
	req.VolumeContentSource = volumeContentSource
	f.createVolumeRequest = req
	return nil
}

// iChangeVolumeCapabilityAccessmode is a method to change volume capabilities access mode
func (f *feature) iChangeVolumeCapabilityAccessmode() error {
	f.createVolumeRequest.VolumeCapabilities[0].AccessMode.Mode = 4
	return nil
}

// iCallCreateVolume - Test case to create volume
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
		f.volumeContext = f.createVolumeRequest.Parameters
	}
	f.createVolumeResponse = volResp
	return nil
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
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	delVolReq := new(csi.DeleteVolumeRequest)
	time.Sleep(SleepTime)
	for i := 0; i < len(f.volIDList); i++ {
		delVolReq.VolumeId = f.volIDList[i]
		var err error

		_, err = client.DeleteVolume(ctx, delVolReq)

		if err != nil {
			fmt.Printf("DeleteVolume %s:\n", err.Error())
			f.addError(err)
		} else {
			fmt.Printf("DeleteVolume %s completed successfully\n", f.volID)
		}
	}
	return nil
}

// whenICallPublishVolume - Test case to Publish volume to the given host
func (f *feature) whenICallPublishVolume() error {
	req := new(csi.ControllerPublishVolumeRequest)
	req.VolumeId = f.volID
	req.NodeId = os.Getenv("X_CSI_UNITY_NODENAME") + "," + os.Getenv("X_CSI_UNITY_LONGNODENAME")
	f.nodeId = req.NodeId
	req.Readonly = false
	req.VolumeCapability = f.capability
	req.VolumeContext = f.volumeContext

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

// whenICallPublishVolumeWithParam - Test case to Publish volume to the given host with readonly as parameter
func (f *feature) whenICallPublishVolumeWithParam(hostName, readonly string) error {
	req := new(csi.ControllerPublishVolumeRequest)
	req.VolumeId = f.volID
	req.NodeId = os.Getenv("X_CSI_UNITY_NODENAME") + "," + os.Getenv("X_CSI_UNITY_LONGNODENAME")
	f.nodeId = req.NodeId
	read, _ := strconv.ParseBool(readonly)
	req.Readonly = read
	req.VolumeCapability = f.capability
	req.VolumeContext = f.volumeContext

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

// whenICallPublishVolumeWithVolumeId - Test case to Publish volume to the given host with volumeID as parameter
func (f *feature) whenICallPublishVolumeWithVolumeId(volId string) error {
	req := new(csi.ControllerPublishVolumeRequest)
	req.VolumeId = volId
	req.NodeId = os.Getenv("X_CSI_UNITY_NODENAME") + "," + os.Getenv("X_CSI_UNITY_LONGNODENAME")
	f.nodeId = req.NodeId
	req.Readonly = false
	req.VolumeCapability = f.capability
	req.VolumeContext = f.volumeContext

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

// whenICallUnpublishVolume - Test case to unpublish volume
func (f *feature) whenICallUnpublishVolume() error {
	req := new(csi.ControllerUnpublishVolumeRequest)
	req.VolumeId = f.volID
	req.NodeId = os.Getenv("X_CSI_UNITY_NODENAME") + "," + os.Getenv("X_CSI_UNITY_LONGNODENAME")
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

// iCallUnpublishVolumeWithVolumeId - Test case to unpublish volume with volume ID as parameter
func (f *feature) iCallUnpublishVolumeWithVolumeId(volId string) error {
	req := new(csi.ControllerUnpublishVolumeRequest)
	req.VolumeId = volId
	req.NodeId = os.Getenv("X_CSI_UNITY_NODENAME") + "," + os.Getenv("X_CSI_UNITY_LONGNODENAME")
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

// theErrorMessageShouldContain is verification method to evaluate expected errors
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
func (f *feature) aDeleteSnapshotRequestWithID(snap_id string) error {
	f.deleteSnapshotRequest = nil
	req := new(csi.DeleteSnapshotRequest)
	req.SnapshotId = snap_id
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

// iCallControllerExpandVolume - Test case for controller expand volume with volume id as parameter
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

// iCallControllerGetVolume - Test case for controller get volume with volume id as parameter
func (f *feature) iCallControllerGetVolumeWithVolume(volID string) error {
	f.controllerGetVolumeRequest = nil
	req := new(csi.ControllerGetVolumeRequest)
	req.VolumeId = volID
	f.controllerGetVolumeRequest = req

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerGetVolume(ctx, req)
	if err != nil {
		fmt.Printf("Controller get volume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("Controller get volume completed successfully\n")
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
	req.StagingTargetPath = path.Join(os.Getenv("X_CSI_STAGING_TARGET_PATH"), f.volID)
	req.TargetPath = path.Join(os.Getenv("X_CSI_PUBLISH_TARGET_PATH"), f.volID)
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mount.FsType = fsType
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = f.capability.AccessMode.Mode
	capability.AccessMode = accessMode
	if fsType == "" {
		req.VolumeCapability = f.capability
	} else {
		req.VolumeCapability = capability
	}
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

// whenICallEphemeralNodePublishVolume - Test case for ephemeral node publish volume
func (f *feature) whenICallEphemeralNodePublishVolume(volName, fsType, am, size, storagePool, protocol, nasServer, thinProvision, dataReduction string) error {
	f.nodePublishVolumeRequest = nil
	req := new(csi.NodePublishVolumeRequest)
	req.VolumeId = volName
	f.volID = req.VolumeId
	f.ephemeral = true
	req.StagingTargetPath = path.Join(os.Getenv("X_CSI_STAGING_TARGET_PATH"), f.volID)
	req.TargetPath = path.Join(os.Getenv("X_CSI_PUBLISH_TARGET_PATH"), f.volID)
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mount.FsType = fsType
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	if am == "ROX" {
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	} else if am == "RWX" {
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	} else if am == "RWO" {
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	} else {
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	}
	capability.AccessMode = accessMode
	capability.AccessMode = accessMode
	req.VolumeCapability = capability
	req.Readonly = false
	params := make(map[string]string)
	params["arrayId"] = os.Getenv("arrayId")
	params["size"] = size
	params["storagePool"] = storagePool
	if storagePool == "id" {
		params["storagePool"] = os.Getenv("STORAGE_POOL")
	}
	params["protocol"] = protocol
	params["nasServer"] = nasServer
	params["thinProvisioned"] = thinProvision
	params["isDataReductionEnabled"] = dataReduction
	params["csi.storage.k8s.io/ephemeral"] = "true"
	req.VolumeContext = params
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

// whenICallNodePublishVolumeWithTargetPath - Test case for node publish volume with target path
func (f *feature) whenICallNodePublishVolumeWithTargetPath(target_path, fsType string) error {
	f.nodePublishVolumeRequest = nil
	req := new(csi.NodePublishVolumeRequest)
	if f.createVolumeResponse != nil || f.ephemeral == true {
		req.VolumeId = f.volID
	} else {
		req.VolumeId = ""
	}
	fmt.Println("========================", req.VolumeId)
	req.StagingTargetPath = path.Join(os.Getenv("X_CSI_STAGING_TARGET_PATH"), f.volID)
	req.TargetPath = target_path
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mount.FsType = fsType
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = f.capability.AccessMode.Mode
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

// whenICallNodePublishVolumeWithoutAccessmode - Test case for node publish volume without access mode
func (f *feature) whenICallNodePublishVolumeWithoutAccessmode(fsType string) error {
	f.nodePublishVolumeRequest = nil
	req := new(csi.NodePublishVolumeRequest)
	if f.createVolumeResponse != nil {
		req.VolumeId = f.volID
	} else {
		req.VolumeId = ""
	}
	req.StagingTargetPath = path.Join(os.Getenv("X_CSI_STAGING_TARGET_PATH"), f.volID)
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mount.FsType = fsType
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	capability.AccessMode = nil
	req.VolumeCapability = capability
	req.TargetPath = path.Join(os.Getenv("X_CSI_PUBLISH_TARGET_PATH"), f.volID)
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

// whenICallNodeUnPublishVolume - Test case for node unpublish volume
func (f *feature) whenICallNodeUnPublishVolume() error {
	f.nodeUnpublishVolumeRequest = nil
	req := new(csi.NodeUnpublishVolumeRequest)
	if f.nodePublishVolumeRequest != nil {
		req.VolumeId = f.nodePublishVolumeRequest.VolumeId
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

// whenICallNodeUnPublishVolumeWithTargetPath - Test case for node unpublish volume with target path
func (f *feature) whenICallNodeUnPublishVolumeWithTargetPath(target_path string) error {
	f.nodeUnpublishVolumeRequest = nil
	req := new(csi.NodeUnpublishVolumeRequest)
	if f.nodePublishVolumeRequest != nil {
		req.VolumeId = f.nodePublishVolumeRequest.VolumeId
	} else {
		req.VolumeId = ""
	}

	req.TargetPath = target_path
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
	req.StagingTargetPath = path.Join(os.Getenv("X_CSI_STAGING_TARGET_PATH"), f.volID)
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mount.FsType = fsType
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = f.capability.AccessMode.Mode
	capability.AccessMode = accessMode
	if fsType == "" {
		req.VolumeCapability = f.capability
	} else {
		req.VolumeCapability = capability
	}
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

// whenICallNodeStageVolumeWithTargetPath - Test case for node stage volume with target path as parameter
func (f *feature) whenICallNodeStageVolumeWithTargetPath(fsType, target_path string) error {
	f.nodeStageVolumeRequest = nil
	req := new(csi.NodeStageVolumeRequest)
	req.VolumeId = f.volID
	if f.createVolumeResponse == nil {
		req.VolumeId = "NoID"
	}
	req.StagingTargetPath = target_path
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mount.FsType = fsType
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = f.capability.AccessMode.Mode
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

// whenICallGetPluginCapabilities - Test case to get plugin capabilities
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

// whenICallGetPluginInfo - Test case to get plugin info
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

// whenICallNodeExpandVolume - Test case to expand volume on node
func (f *feature) whenICallNodeExpandVolume() error {
	nodePublishReq := f.nodePublishVolumeRequest
	if nodePublishReq == nil {
		err := fmt.Errorf("Volume is not stage, nodePublishVolumeRequest not found")
		return err
	}
	err := f.nodeExpandVolume(f.volID, nodePublishReq.TargetPath)
	if err != nil {
		fmt.Printf("NodeExpandVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("NodeExpandVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) nodeExpandVolume(volID, volPath string) error {
	var err error
	req := &csi.NodeExpandVolumeRequest{
		VolumeId:   volID,
		VolumePath: volPath,
	}
	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err = client.NodeExpandVolume(ctx, req)
	return err
}

func (f *feature) whenICallGetCapacity() error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)

	params := make(map[string]string)
	params["storagePool"] = os.Getenv("STORAGE_POOL")
	params["thinProvisioned"] = "true"
	params["isDataReductionEnabled"] = "false"
	params["tieringPolicy"] = "0"
	params["description"] = "CSI Volume Unit Test"
	params["arrayId"] = os.Getenv("arrayId")
	params["nasServer"] = os.Getenv("NAS_SERVER")

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

	f.getCapacityRequest = &csi.GetCapacityRequest{VolumeCapabilities: capabilities, Parameters: params}
	response, err := client.GetCapacity(ctx, f.getCapacityRequest)
	if err != nil {
		fmt.Printf("GetCapacity %s:\n", err.Error())
		f.addError(err)
		return err
	}
	if err == nil {
		fmt.Printf("Maximum Volume Size: %v \n", response.MaximumVolumeSize)
	}
	f.getCapacityResponse = response
	return nil
}

func FeatureContext(s *godog.Suite) {
	f := &feature{}
	s.Step(`^a CSI service$`, f.aCSIService)
	s.Step(`^a CSI service with node$`, f.aCSIServiceWithNode)
	s.Step(`^a CSI service with node topology$`, f.aCSIServiceWithNodeTopology)
	s.Step(`^a basic block volume request name "([^"]*)" protocol "([^"]*)" size "(\d+)"$`, f.aBasicBlockVolumeRequest)
	s.Step(`^a basic raw block volume request name "([^"]*)" protocol "([^"]*)" size "(\d+)"$`, f.aBasicRawBlockVolumeRequest)
	s.Step(`^a basic filesystem request name "([^"]*)" protocol "([^"]*)" accessMode "([^"]*)" size "(\d+)"$`, f.aBasicFilesystemRequest)
	s.Step(`^I change volume capability accessmode$`, f.iChangeVolumeCapabilityAccessmode)
	s.Step(`^a basic block volume request with volumeName "([^"]*)" protocol "([^"]*)" size "([^"]*)" storagepool "([^"]*)" thinProvisioned "([^"]*)" isDataReductionEnabled "([^"]*)" tieringPolicy "([^"]*)"$`, f.aBasicBlockVolumeRequestWithParameters)
	s.Step(`^a basic block volume request with volume content source as snapshot with name "([^"]*)" protocol "([^"]*)" size "([^"]*)"$`, f.aBasicBlockVolumeRequestWithVolumeContentSource)
	s.Step(`^a basic block volume request with volume content source as volume with name "([^"]*)" protocol "([^"]*)" size "([^"]*)"$`, f.aBasicBlockVolumeRequestWithVolumeContentSourceAsVolume)
	s.Step(`^I call CreateVolume$`, f.iCallCreateVolume)
	s.Step(`^when I call DeleteVolume$`, f.whenICallDeleteVolume)
	s.Step(`^When I call DeleteAllCreatedVolumes$`, f.whenICallDeleteAllCreatedVolumes)
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
	s.Step(`^I call validate volume capabilities with protocol "([^"]*)" with same access mode`, f.iCallValidateVolumeCapabilitiesWithSameAccessMode)
	s.Step(`^I call validate volume capabilities with protocol "([^"]*)" with different access mode$`, f.iCallValidateVolumeCapabilitiesWithDifferentAccessMode)
	s.Step(`^I call validate volume capabilities with protocol "([^"]*)" with volume ID "([^"]*)"$`, f.iCallValidateVolumeCapabilitiesWithVolumeID)
	s.Step(`^I call Controller Get Capabilities$`, f.iCallControllerGetCapabilities)
	s.Step(`^I call Controller Expand Volume "([^"]*)"$`, f.iCallControllerExpandVolume)
	s.Step(`^I call Controller Expand Volume "([^"]*)" with volume "([^"]*)"$`, f.iCallControllerExpandVolumeWithVolume)
	s.Step(`^I call Controller Get Volume "([^"]*)" with volume "([^"]*)"$`, f.iCallControllerGetVolumeWithVolume)
	s.Step(`^when I call NodePublishVolume fsType "([^"]*)" readonly "([^"]*)"$`, f.whenICallNodePublishVolume)
	s.Step(`^when I call NodePublishVolume targetpath "([^"]*)" fsType "([^"]*)"$`, f.whenICallNodePublishVolumeWithTargetPath)
	s.Step(`^when I call EphemeralNodePublishVolume with volName "([^"]*)" volName  "([^"]*)" fsType "([^"]*)" am "([^"]*)" size "([^"]*)" storagePool "([^"]*)" protocol "([^"]*)" nasServer "([^"]*)" thinProvision "([^"]*)" dataReduction "([^"]*)"$`, f.whenICallEphemeralNodePublishVolume)
	s.Step(`^when I call NodeUnPublishVolume$`, f.whenICallNodeUnPublishVolume)
	s.Step(`^when I call NodeUnPublishVolume targetpath "([^"]*)"$`, f.whenICallNodeUnPublishVolumeWithTargetPath)
	s.Step(`^when I call NodeStageVolume fsType "([^"]*)"$`, f.whenICallNodeStageVolume)
	s.Step(`^when I call NodeStageVolume fsType "([^"]*)" with StagingTargetPath "([^"]*)"$`, f.whenICallNodeStageVolumeWithTargetPath)
	s.Step(`^when I call NodeUnstageVolume$`, f.whenICallNodeUnstageVolume)
	s.Step(`^When I call NodeGetInfo$`, f.whenICallNodeGetInfo)
	s.Step(`^When I call NodeGetCapabilities$`, f.whenICallNodeGetCapabilities)
	s.Step(`^when I call NodePublishVolume without accessmode and fsType "([^"]*)"$`, f.whenICallNodePublishVolumeWithoutAccessmode)
	s.Step(`^When I call GetPluginCapabilities$`, f.whenICallGetPluginCapabilities)
	s.Step(`^When I call GetPluginInfo$`, f.whenICallGetPluginInfo)
	s.Step(`^when I call Node Expand Volume$`, f.whenICallNodeExpandVolume)
	s.Step(`^I Call GetCapacity$`, f.whenICallGetCapacity)

}
