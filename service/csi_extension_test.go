/*
Copyright (c) 2021 Dell EMC Corporation
All Rights Reserved
*/

package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/dell-csi-extensions/podmon"
	"github.com/dell/gounity"
	"github.com/dell/gounity/types"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"testing"
)

type testCaseSpec struct {
	setup            func()
	cleanup          func()
	request          *podmon.ValidateVolumeHostConnectivityRequest
	expectedResponse *podmon.ValidateVolumeHostConnectivityResponse
	expectError      error
	skip             bool
}

func TestValidateVolumeHostConnectivityBasic(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	var testCases map[string]testCaseSpec
	testCases = map[string]testCaseSpec{
		"No Op": {
			request: &podmon.ValidateVolumeHostConnectivityRequest{},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Messages: []string{"ValidateVolumeHostConnectivity is implemented"},
			},
		},

		"NodeID is invalid": {
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				ArrayId:   "array1",
				NodeId:    "",
				VolumeIds: []string{"volume1"},
			},
			expectError: status.Errorf(codes.InvalidArgument, "The NodeID is a required field"),
		},

		"No Default Array": {
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectError: status.Errorf(codes.Aborted, "Could not find default array"),
		},

		"Default Array, No Host Connections": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: false,
				Messages:  []string{},
			},
		},
	}

	runTestCases(t, ctx, testCases)
}

func TestValidateVolumeHostConnectivityHost(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	var testCases map[string]testCaseSpec
	testCases = map[string]testCaseSpec{
		"Default Array, Get Host fails": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostId = mockGetHostId
				mockGetHostErr = fmt.Errorf("induced GetHost failure")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: false,
				Messages:  []string{"Host lookup failed. Error: induced GetHost failure"},
			},
			expectError: nil,
		},

		"Default Array, Get Host fails with NotFound code": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostId = mockGetHostId
				mockGetHostErr = status.Errorf(codes.NotFound, "Host was not found")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: false,
				Messages:  []string{"Array array1 does have any host with name 'worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju'"},
			},
			expectError: nil,
		},

		"Default Array, Get Unity fails": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostId = mockGetHostId
				mockGetUnityErr = fmt.Errorf("induced GetUnity failure")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: false,
				Messages:  []string{"Unable to get unity client for topology validation: induced GetUnity failure"},
			},
			expectError: nil,
		},

		"Default Array, Probe fails": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				mockRequireProbeErr = fmt.Errorf("induced RequireProbe error")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				ArrayId: "array1",
				NodeId:  "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectError: fmt.Errorf("induced RequireProbe error"),
		},
	}

	runTestCases(t, ctx, testCases)
}

func TestValidateVolumeHostConnectivityVolumeIds(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	var testCases map[string]testCaseSpec
	testCases = map[string]testCaseSpec{
		"No Array, Volumes And NodeID": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{"vol1-iscsi-array1-lun1"},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages:  []string{"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
			},
		},

		"No Array, Volumes And short NodeID": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{"vol1-iscsi-array1-lun1"},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages:  []string{"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
			},
		},

		"No Array, Multiple Arrays": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{"vol1-iscsi-array1-lun1", "vol1-iscsi-array2-lun1", "vol1-iscsi-array3-lun1"},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
				},
			},
		},

		"No Array, Bad VolumeID": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				mockGetArrayFromVolumeFail = "vol1-invalid"
				mockGetArrayIdErr = errors.New("invalid volume context id or no default array found in the csi-unity driver configuration")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{"vol1-invalid"},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages:  []string{"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
			},
		},
	}

	runTestCases(t, ctx, testCases)
}

func TestValidateVolumeHostConnectivityISCSI(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	var testCases map[string]testCaseSpec
	testCases = map[string]testCaseSpec{
		"Default Array, Good iSCSI HostConnections": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages:  []string{"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
			},
		},

		"ArrayID Passed In, Good iSCSI HostConnections": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				ArrayId: "array1",
				NodeId:  "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages:  []string{"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
			},
		},

		"Default Array, Bad iSCSI HostConnections": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 1, 0)
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockBadInitiator = mockHost.HostContent.IscsiInitiators[0].Id
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: false,
				Messages:  []string{},
			},
		},

		"Default Array, One Bad iSCSI HostConnection": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 2, 0)
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockBadInitiator = mockHost.HostContent.IscsiInitiators[0].Id
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages:  []string{"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
			},
		},

		"Default Array, iSCSI FindHostInitiatorById fails": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 1, 0)
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockFindInitatorFail = mockHost.HostContent.IscsiInitiators[0].Id
				mockFindHostInitiatorErr = fmt.Errorf("induced FindHostInitiatorById error")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: false,
				Messages:  []string{},
			},
		},
	}

	runTestCases(t, ctx, testCases)
}

func TestValidateVolumeHostConnectivityFC(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	var testCases map[string]testCaseSpec
	testCases = map[string]testCaseSpec{

		"Default Array, Good FC HostConnections": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 0, 1)
					return mockHost, nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages:  []string{"FC Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
			},
		},

		"Default Array, Bad FC HostConnections": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 0, 1)
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockBadInitiator = mockHost.HostContent.FcInitiators[0].Id
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: false,
				Messages:  []string{},
			},
		},

		"Default Array, One Bad FC HostConnection": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 0, 2)
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockBadInitiator = mockHost.HostContent.FcInitiators[0].Id
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages:  []string{"FC Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
			},
		},

		"Default Array, FC FindHostInitiatorById fails": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 0, 1)
				GetHostId = func(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockFindInitatorFail = mockHost.HostContent.FcInitiators[0].Id
				mockFindHostInitiatorErr = fmt.Errorf("induced FindHostInitiatorById error")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: false,
				Messages:  []string{},
			},
		},
	}

	runTestCases(t, ctx, testCases)
}

func runTestCases(t *testing.T, ctx context.Context, testCases map[string]testCaseSpec) {
	for name, tc := range testCases {
		if tc.skip {
			t.Logf("Skipping test %s", name)
			continue
		}

		t.Logf("Attempting test %s", name)

		// Set up the default mocks before each test
		defaultMocks()

		if tc.setup != nil {
			tc.setup()
		}

		res, err := testConf.service.ValidateVolumeHostConnectivity(ctx, tc.request)

		if tc.expectedResponse != nil {
			assert.NotNil(t, res, name)
			if res != nil {
				assert.Equal(t, tc.expectedResponse.Connected, res.Connected, name)
				assert.Equal(t, len(tc.expectedResponse.Messages), len(res.Messages), name)
				for _, msg := range res.Messages {
					assert.Contains(t, tc.expectedResponse.Messages, msg, name)
				}
			}
		}

		if tc.expectError != nil {
			assert.NotNil(t, err, name)
			assert.Equal(t, tc.expectError, err)
		}

		if tc.expectError == nil && err != nil {
			assert.Errorf(t, err, fmt.Sprintf("Expected test case '%s' to not return error, but it did", name))
		}

		if tc.cleanup != nil {
			tc.cleanup()
		}
	}
}

var mockGetArrayFromVolumeFail string
var mockGetArrayIdErr error
var mockRequireProbeErr error
var mockGetHost *types.Host
var mockGetHostErr error
var mockGetUnity *gounity.Client
var mockGetUnityErr error
var mockFindHostInitiatorErr error
var mockBadInitiator string
var mockFindInitatorFail string

func defaultMocks() {
	GetHostId = mockGetHostId
	RequireProbe = mockRequireProbe
	GetUnityClient = mockGetUnityClient
	GetArrayIdFromVolumeContext = mockGetArrayIdFromVolumeContext
	FindHostInitiatorById = mockFindHostInitiatorById
	mockGetArrayFromVolumeFail = ""
	mockGetArrayIdErr = nil
	mockRequireProbeErr = nil
	mockGetHost = nil
	mockGetHostErr = nil
	mockGetUnity = nil
	mockGetUnityErr = nil
	mockFindHostInitiatorErr = nil
	mockBadInitiator = ""
	mockFindInitatorFail = ""
}

func mockGetArrayIdFromVolumeContext(_ *service, contextVolId string) (string, error) {
	if mockGetArrayFromVolumeFail == contextVolId {
		return "", mockGetArrayIdErr
	}
	components := strings.Split(contextVolId, "-")
	return components[len(components)-2], mockGetArrayIdErr
}

func mockRequireProbe(s *service, ctx context.Context, arrayId string) error {
	return mockRequireProbeErr
}

func mockGetHostId(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
	return mockGetHost, mockGetHostErr
}

func mockGetUnityClient(s *service, ctx context.Context, arrayId string) (*gounity.Client, error) {
	return mockGetUnity, mockGetUnityErr
}

func mockStorage(ctx context.Context) *StorageArrayConfig {
	client, _ := gounity.NewClient(ctx)
	return &StorageArrayConfig{
		ArrayId:        "array1",
		Username:       "username",
		Password:       "pass",
		RestGateway:    "http://some.host.com",
		Insecure:       false,
		IsDefaultArray: true,
		IsProbeSuccess: false,
		IsHostAdded:    false,
		UnityClient:    client,
	}
}

func mockAHost(name string, iScsiPorts, fcPorts int) *types.Host {
	mockISCSI := make([]types.Initiators, 0)
	for i := 0; i < iScsiPorts; i++ {
		mockISCSI = append(mockISCSI, types.Initiators{Id: fmt.Sprintf("mock_%s_iscsi_initiator_%d", name, i)})
	}
	mockFC := make([]types.Initiators, 0)
	for i := 0; i < fcPorts; i++ {
		mockFC = append(mockFC, types.Initiators{Id: fmt.Sprintf("mock_%s_fc_initiator_%d", name, i)})
	}
	mockHost := &types.Host{
		HostContent: types.HostContent{
			ID:              name,
			Name:            name,
			Description:     fmt.Sprintf("Mock test host %s", name),
			FcInitiators:    mockFC,
			IscsiInitiators: mockISCSI,
			IpPorts:         nil,
			Address:         fmt.Sprintf("%s.host.name", name),
		},
	}
	return mockHost
}

func mockAnInitiator(id string) *types.HostInitiator {
	var descriptions []string

	if id == mockFindInitatorFail {
		return nil
	}

	if id == mockBadInitiator {
		descriptions = []string{"ALRT_INITIATOR_NO_LOGGED_IN_PATH"}
	} else {
		descriptions = []string{"ALRT_COMPONENT_OK"}
	}

	return &types.HostInitiator{
		HostInitiatorContent: types.HostInitiatorContent{
			Id: id,
			Health: types.HealthContent{
				DescriptionIDs: descriptions,
			},
			Type:        0,
			InitiatorId: id,
			IsIgnored:   false,
			Paths:       nil,
		},
	}
}

func mockFindHostInitiatorById(unity *gounity.Client, ctx context.Context, wwnOrIqn string) (*types.HostInitiator, error) {
	return mockAnInitiator(wwnOrIqn), mockFindHostInitiatorErr
}
