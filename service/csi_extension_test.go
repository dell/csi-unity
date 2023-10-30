/*
 Copyright © 2021 Dell Inc. or its subsidiaries. All Rights Reserved.

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
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/dell-csi-extensions/podmon"
	"github.com/dell/gounity"
	"github.com/dell/gounity/types"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type testCaseSpec struct {
	setup            func()
	cleanup          func()
	request          *podmon.ValidateVolumeHostConnectivityRequest
	expectedResponse *podmon.ValidateVolumeHostConnectivityResponse
	expectError      error
	skip             bool
	expectErrorType  string
	expectErrorLike  string
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

	runTestCases(ctx, t, testCases)
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
				GetHostID = mockGetHostID
				mockGetHostErr = fmt.Errorf("induced GetHost failure")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected:     false,
				Messages:      []string{"Host lookup failed. Error: induced GetHost failure"},
				IosInProgress: false,
			},
			expectError: nil,
		},

		"Default Array, Get Host fails with NotFound code": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = mockGetHostID
				mockGetHostErr = status.Errorf(codes.NotFound, "Host was not found")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected:     false,
				Messages:      []string{"Array array1 does have any host with name 'worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju'"},
				IosInProgress: false,
			},
			expectError: nil,
		},

		"Default Array, Get Unity fails": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = mockGetHostID
				mockGetUnityErr = fmt.Errorf("induced GetUnity failure")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected:     false,
				Messages:      []string{"Unable to get unity client for topology validation: induced GetUnity failure"},
				IosInProgress: false,
			},
			expectError: nil,
		},

		"Default Array, Probe fails": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
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

	runTestCases(ctx, t, testCases)
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
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
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
				VolumeIds: []string{"vol1-iscsi-array1-sv_1"},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"sv_1 on array array1 has no IOs",
				},
				IosInProgress: false,
			},
		},

		"No Array, Volumes And short NodeID": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
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
				VolumeIds: []string{"vol1-iscsi-array1-sv_1"},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"sv_1 on array array1 has no IOs",
				},
				IosInProgress: false,
			},
		},

		"No Array, Multiple Volumes": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
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
				VolumeIds: []string{
					"vol1-iscsi-array1-sv_1",
					"vol1-iscsi-array2-sv_2",
					"vol1-iscsi-array3-sv_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
					"sv_1 on array array1 has no IOs",
					"sv_2 on array array2 has IOs",
					"sv_3 on array array3 has IOs",
				},
				IosInProgress: true,
			},
		},

		"No Array, Bad VolumeID": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				mockGetArrayFromVolumeFail = "vol1-invalid"
				mockGetArrayIDErr = errors.New("invalid volume context id or no default array found in the csi-unity driver configuration")
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
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
				},
				IosInProgress: false,
			},
		},
	}

	runTestCases(ctx, t, testCases)
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
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
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
				Connected:     true,
				Messages:      []string{"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
				IosInProgress: false,
			},
		},

		"ArrayID Passed In, Good iSCSI HostConnections": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
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
				Connected:     true,
				Messages:      []string{"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
				IosInProgress: false,
			},
		},

		"Default Array, Bad iSCSI HostConnections": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 1, 0)
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockBadInitiator = mockHost.HostContent.IscsiInitiators[0].ID
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected:     false,
				Messages:      []string{},
				IosInProgress: false,
			},
		},

		"Default Array, One Bad iSCSI HostConnection": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 2, 0)
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockBadInitiator = mockHost.HostContent.IscsiInitiators[0].ID
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected:     true,
				Messages:      []string{"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
				IosInProgress: false,
			},
		},

		"Default Array, iSCSI FindHostInitiatorByID fails": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 1, 0)
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockFindInitatorFail = mockHost.HostContent.IscsiInitiators[0].ID
				mockFindHostInitiatorErr = fmt.Errorf("induced FindHostInitiatorByID error")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected:     false,
				Messages:      []string{},
				IosInProgress: false,
			},
		},
	}

	runTestCases(ctx, t, testCases)
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
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
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
				Connected:     true,
				Messages:      []string{"FC Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
				IosInProgress: false,
			},
		},

		"Default Array, Bad FC HostConnections": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 0, 1)
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockBadInitiator = mockHost.HostContent.FcInitiators[0].ID
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected:     false,
				Messages:      []string{},
				IosInProgress: false,
			},
		},

		"Default Array, One Bad FC HostConnection": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 0, 2)
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockBadInitiator = mockHost.HostContent.FcInitiators[0].ID
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected:     true,
				Messages:      []string{"FC Health is good for array:array1, Health:ALRT_COMPONENT_OK"},
				IosInProgress: false,
			},
		},

		"Default Array, FC FindHostInitiatorByID fails": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				mockHost := mockAHost("name", 0, 1)
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					return mockHost, nil
				}
				mockFindInitatorFail = mockHost.HostContent.FcInitiators[0].ID
				mockFindHostInitiatorErr = fmt.Errorf("induced FindHostInitiatorByID error")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected:     false,
				Messages:      []string{},
				IosInProgress: false,
			},
		},
	}

	runTestCases(ctx, t, testCases)
}

func TestVolumeIOCheck(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	var testCases map[string]testCaseSpec
	testCases = map[string]testCaseSpec{
		"CreateMetricsError": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				mockCreateMetricsCollectionError = fmt.Errorf("MockCreateMetricsCollectionError")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-iscsi-array1-sv_1",
					"vol1-iscsi-array2-sv_2",
					"vol1-iscsi-array3-sv_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
				},
				IosInProgress: false,
			},
			expectError: fmt.Errorf("MockCreateMetricsCollectionError"),
		},
		"GetMetricsError": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				mockGetMetricsCollectionError = fmt.Errorf("MockGetMetricsCollectionError")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-iscsi-array1-sv_1",
					"vol1-iscsi-array2-sv_2",
					"vol1-iscsi-array3-sv_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
				},
				IosInProgress: false,
			},
			expectError: fmt.Errorf("MockGetMetricsCollectionError"),
		},
		"BadMetricData": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				metricsCollectionCache.Store("array1:sp.*.storage.lun.*.currentIOCount", 1)
				mockMetricValueMap = map[string]interface{}{
					"spa": map[string]interface{}{
						"sv_1": "0.0",
						"sv_2": "1.0",
						"sv_3": "1.0",
					},
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-iscsi-array1-sv_1",
					"vol1-iscsi-array2-sv_2",
					"vol1-iscsi-array3-sv_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
				},
				IosInProgress: false,
			},
			expectErrorType: reflect.TypeOf(&strconv.NumError{}).String(),
		},
		"No Stats for LUN": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				// Return mock data that does not include any of
				// the volumes that we're checking against
				mockMetricValueMap = map[string]interface{}{
					"spa": map[string]interface{}{
						"sv_10": "11",
						"sv_20": "22",
						"sv_30": "33",
					},
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-iscsi-array1-sv_1",
					"vol1-iscsi-array2-sv_2",
					"vol1-iscsi-array3-sv_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
					"sv_1 on array array1 has no IOs",
					"sv_2 on array array2 has no IOs",
					"sv_3 on array array3 has no IOs",
				},
				IosInProgress: false,
			},
		},
		"CacheHit, Wrong Collection": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				metricsCollectionCache.Store("array1:sp.*.storage.lun.*.currentIOCount", 1)
				times := 0
				GetMetricsCollection = func(ctx context.Context, s *service, arrayId string, id int) (*types.MetricQueryResult, error) {
					times++
					// Return a different metric once
					if times > 1 {
						return mockGetMetricsCollection(ctx, s, arrayId, id)
					}
					result := types.MetricResult{
						QueryID:   id,
						Path:      "sp.*.storage.filesystemSummary.clientReadBytes",
						Timestamp: time.Now().String(),
						Values: map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "1234",
							},
						},
					}

					results := []types.MetricResultEntry{
						{
							Base:    "MockResultEntry",
							Updated: "",
							Content: result,
						},
					}

					mockResult := &types.MetricQueryResult{
						Base:    "MockResult",
						Updated: time.Now().String(),
						Entries: results,
					}

					return mockResult, nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-iscsi-array1-sv_1",
					"vol1-iscsi-array2-sv_2",
					"vol1-iscsi-array3-sv_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
					"sv_1 on array array1 has no IOs",
					"sv_2 on array array2 has IOs",
					"sv_3 on array array3 has IOs",
				},
				IosInProgress: true,
			},
		},
	}

	runTestCases(ctx, t, testCases)
}

func TestFileSystemIOCheck(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	var testCases map[string]testCaseSpec
	testCases = map[string]testCaseSpec{
		"CreateMetricsError": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				mockCreateMetricsCollectionError = fmt.Errorf("MockCreateMetricsCollectionError")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-nfs-array1-fs_1",
					"vol1-nfs-array2-fs_2",
					"vol1-nfs-array3-fs_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
				},
				IosInProgress: false,
			},
			expectError: fmt.Errorf("MockCreateMetricsCollectionError"),
		},
		"GetMetricsError": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				mockGetMetricsCollectionError = fmt.Errorf("MockGetMetricsCollectionError")
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-nfs-array1-fs_1",
					"vol1-nfs-array2-fs_2",
					"vol1-nfs-array3-fs_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
				},
				IosInProgress: false,
			},
			expectError: fmt.Errorf("MockGetMetricsCollectionError"),
		},
		"BadMetricData": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				metricsCollectionCache.Store("array1:sp.*.storage.filesystem.*.clientReads,sp.*.storage.filesystem.*.clientWrites", 1)
				GetMetricsCollection = func(ctx context.Context, s *service, arrayId string, id int) (*types.MetricQueryResult, error) {
					read := map[string]interface{}{
						"spa": map[string]interface{}{
							"fs_1": "0.0", // Bad data: value is float, expecting integer
							"fs_2": "1.0",
							"fs_3": "1.0",
						},
					}
					write := map[string]interface{}{
						"spa": map[string]interface{}{
							"fs_1": "0.0",
							"fs_2": "1.0",
							"fs_3": "1.0",
						},
					}
					return mockNfsMetricResult(id, read, write), nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-nfs-array1-fs_1",
					"vol1-nfs-array2-fs_2",
					"vol1-nfs-array3-fs_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
				},
				IosInProgress: false,
			},
			expectErrorType: reflect.TypeOf(&strconv.NumError{}).String(),
		},
		"Unexpected Protocol For Volume": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
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
				VolumeIds: []string{
					"vol1-bogus-array1-fs_1",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
				},
				IosInProgress: false,
			},
			expectError: fmt.Errorf("unexpected protocol 'bogus' found in request"),
		},
		"No Stats for LUN": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				// Return mock data that does not include any of
				// the volumes that we're checking against
				GetMetricsCollection = func(ctx context.Context, s *service, arrayId string, id int) (*types.MetricQueryResult, error) {
					read := map[string]interface{}{
						"spa": map[string]interface{}{
							"fs_10": "0",
							"fs_20": "1",
							"fs_30": "1",
						},
					}
					write := map[string]interface{}{
						"spa": map[string]interface{}{
							"fs_10": "0",
							"fs_20": "1",
							"fs_30": "1",
						},
					}
					return mockNfsMetricResult(id, read, write), nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-nfs-array1-fs_1",
					"vol1-nfs-array2-fs_2",
					"vol1-nfs-array3-fs_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
					"fs_1 on array array1 has no IOs",
					"fs_2 on array array2 has no IOs",
					"fs_3 on array array3 has no IOs",
				},
				IosInProgress: false,
			},
		},
		"CacheHit, Wrong Collection": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				metricsCollectionCache.Store("array1:sp.*.storage.filesystem.*.clientReads,sp.*.storage.filesystem.*.clientWrites", 1)
				times := 0
				GetMetricsCollection = func(ctx context.Context, s *service, arrayId string, id int) (*types.MetricQueryResult, error) {
					times++
					// Return a different metric once
					if times > 1 {
						read := map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "0",
								"fs_2": "1",
								"fs_3": "1",
							},
						}
						write := map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "1",
								"fs_2": "2",
								"fs_3": "2",
							},
						}
						return mockNfsMetricResult(id, read, write), nil
					}
					result := types.MetricResult{
						QueryID:   id,
						Path:      "sp.*.storage.lun.*.currentIOs",
						Timestamp: time.Now().String(),
						Values: map[string]interface{}{
							"spa": map[string]interface{}{
								"sv_1": "1234",
							},
						},
					}

					results := []types.MetricResultEntry{
						{
							Base:    "MockResultEntry",
							Updated: "",
							Content: result,
						},
					}

					mockResult := &types.MetricQueryResult{
						Base:    "MockResult",
						Updated: time.Now().String(),
						Entries: results,
					}

					return mockResult, nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-nfs-array1-fs_1",
					"vol1-nfs-array2-fs_2",
					"vol1-nfs-array3-fs_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
					"fs_1 on array array1 has no IOs",
					"fs_2 on array array2 has no IOs",
					"fs_3 on array array3 has no IOs",
				},
				IosInProgress: false,
			},
		},
		"Second GetMetric Fails": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				metricsCollectionCache.Store("array1:sp.*.storage.filesystem.*.clientReads,sp.*.storage.filesystem.*.clientWrites", 1)
				times := 0
				GetMetricsCollection = func(ctx context.Context, s *service, arrayId string, id int) (*types.MetricQueryResult, error) {
					times++
					// Return a different metric once
					if times == 1 {
						read := map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "0",
								"fs_2": "1",
								"fs_3": "1",
							},
						}
						write := map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "1",
								"fs_2": "2",
								"fs_3": "2",
							},
						}
						return mockNfsMetricResult(id, read, write), nil
					}
					// All calls after the first will fail
					return nil, fmt.Errorf("MockGetMetricsCollectionError")
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-nfs-array1-fs_1",
					"vol1-nfs-array2-fs_2",
					"vol1-nfs-array3-fs_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
				},
				IosInProgress: false,
			},
			expectError: fmt.Errorf("MockGetMetricsCollectionError"),
		},
		"Second GetMetric Has Bad Data": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				metricsCollectionCache.Store("array1:sp.*.storage.filesystem.*.clientReads,sp.*.storage.filesystem.*.clientWrites", 1)
				times := 0
				GetMetricsCollection = func(ctx context.Context, s *service, arrayId string, id int) (*types.MetricQueryResult, error) {
					times++
					// Return a different metric once
					if times == 1 {
						read := map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "0",
								"fs_2": "1",
								"fs_3": "1",
							},
						}
						write := map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "1",
								"fs_2": "2",
								"fs_3": "2",
							},
						}
						return mockNfsMetricResult(id, read, write), nil
					}
					badData := map[string]interface{}{
						"spa": map[string]interface{}{
							"fs_1": "0.0", // float, but expected to integer
							"fs_2": "1.0",
							"fs_3": "1.0",
						},
					}
					return mockNfsMetricResult(id, badData, badData), nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-nfs-array1-fs_1",
					"vol1-nfs-array2-fs_2",
					"vol1-nfs-array3-fs_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
				},
				IosInProgress: false,
			},
			expectErrorType: reflect.TypeOf(&strconv.NumError{}).String(),
		},
		"Second GetMetric Missing FileSystem": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				metricsCollectionCache.Store("array1:sp.*.storage.filesystem.*.clientReads,sp.*.storage.filesystem.*.clientWrites", 1)
				times := 0
				GetMetricsCollection = func(ctx context.Context, s *service, arrayId string, id int) (*types.MetricQueryResult, error) {
					times++
					// Return a different metric once
					if times == 1 {
						read := map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "0",
								"fs_2": "1",
								"fs_3": "1",
							},
						}
						write := map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "1",
								"fs_2": "2",
								"fs_3": "2",
							},
						}
						return mockNfsMetricResult(id, read, write), nil
					}
					badData := map[string]interface{}{
						"spa": map[string]interface{}{
							// Data has different file systems than the first mock result
							"fs_10": "3",
							"fs_20": "3",
							"fs_30": "3",
						},
					}
					return mockNfsMetricResult(id, badData, badData), nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-nfs-array1-fs_1",
					"vol1-nfs-array2-fs_2",
					"vol1-nfs-array3-fs_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
				},
				IosInProgress: false,
			},
			expectErrorLike: "unexpected result. Could not find metric value for",
		},
		"FileSystems with IOs": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				times := 0
				GetMetricsCollection = func(ctx context.Context, s *service, arrayId string, id int) (*types.MetricQueryResult, error) {
					times++
					var read, write map[string]interface{}
					if times == 1 {
						// First time zero values
						read = map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "0",
								"fs_2": "0",
								"fs_3": "0",
							},
						}
						write = map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "0",
								"fs_2": "0",
								"fs_3": "0",
							},
						}
						return mockNfsMetricResult(id, read, write), nil
					}
					if times == 2 {
						// After first time, >0 values
						read = map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "1",
								"fs_2": "1",
								"fs_3": "1",
							},
						}
						write = map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "1",
								"fs_2": "1",
								"fs_3": "1",
							},
						}
						// Reset because we have multiple volumes belonging to different arrays
						// and we want to have the two NFS metrics queries for each array to
						// show a diff.
						times = 0
					}
					return mockNfsMetricResult(id, read, write), nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-nfs-array1-fs_1",
					"vol1-nfs-array2-fs_2",
					"vol1-nfs-array3-fs_3",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array3, Health:ALRT_COMPONENT_OK",
					"fs_1 on array array1 has IOs",
					"fs_2 on array array2 has IOs",
					"fs_3 on array array3 has IOs",
				},
				IosInProgress: true,
			},
		},
		"FileSystems with IOs, different SPs": {
			setup: func() {
				testConf.service.opts.AutoProbe = true
				testConf.service.arrays.Store("array1", mockStorage(ctx))
				GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
					mockHost := mockAHost("name", 1, 0)
					return mockHost, nil
				}
				times := 0
				// This simulates getting metrics results where the filesystems are associated with different SPs
				GetMetricsCollection = func(ctx context.Context, s *service, arrayId string, id int) (*types.MetricQueryResult, error) {
					times++
					var read, write map[string]interface{}
					if times == 1 {
						// First time zero values
						read = map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "0",
							},
							"spb": map[string]interface{}{
								"fs_2": "0",
							},
						}
						write = map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "0",
							},
							"spb": map[string]interface{}{
								"fs_2": "0",
							},
						}
						return mockNfsMetricResult(id, read, write), nil
					}
					if times == 2 {
						// After first time, >0 values
						read = map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "1",
							},
							"spb": map[string]interface{}{
								"fs_2": "1",
							},
						}
						write = map[string]interface{}{
							"spa": map[string]interface{}{
								"fs_1": "1",
							},
							"spb": map[string]interface{}{
								"fs_2": "1",
							},
						}
						// Reset because we have multiple volumes belonging to different arrays
						// and we want to have the two NFS metrics queries for each array to
						// show a diff.
						times = 0
					}
					return mockNfsMetricResult(id, read, write), nil
				}
			},
			cleanup: func() {
				testConf.service.arrays.Delete("array1")
			},
			request: &podmon.ValidateVolumeHostConnectivityRequest{
				NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
				// <PV_NAME>-<PROTOCOL>-<ARRAYID>-<LUN_ID>
				VolumeIds: []string{
					"vol1-nfs-array1-fs_1",
					"vol1-nfs-array2-fs_2",
				},
			},
			expectedResponse: &podmon.ValidateVolumeHostConnectivityResponse{
				Connected: true,
				Messages: []string{
					"iSCSI Health is good for array:array1, Health:ALRT_COMPONENT_OK",
					"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
					"fs_1 on array array1 has IOs",
					"fs_2 on array array2 has IOs",
				},
				IosInProgress: true,
			},
		},
	}

	runTestCases(ctx, t, testCases)
}

func TestParallelIOCheck(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	// ====== Test setup ======
	defaultMocks()

	testConf.service.opts.AutoProbe = true
	testConf.service.arrays.Store("array1", mockStorage(ctx))
	GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
		mockHost := mockAHost("name", 1, 0)
		return mockHost, nil
	}

	CreateMetricsCollection = func(ctx context.Context, s *service, arrayId string, metricPaths []string, interval int) (*types.MetricQueryCreateResponse, error) {
		return &types.MetricQueryCreateResponse{
			Base:    "Mock",
			Updated: time.Now().String(),
			Content: types.MetricQueryResponseContent{
				MaximumSamples: 0,
				Expiration:     "mockExpiration",
				Interval:       interval,
				Paths:          metricPaths,
				ID:             2,
			},
		}, nil
	}

	// Fill cache
	metricsCollectionCache.Store("array2", 1)

	GetMetricsCollection = func(ctx context.Context, s *service, arrayId string, id int) (*types.MetricQueryResult, error) {
		if id == 1 {
			log.Infof("FileSystem metrics")
			read := map[string]interface{}{
				"spa": map[string]interface{}{
					"fs_1": "1",
				},
			}
			write := map[string]interface{}{
				"spa": map[string]interface{}{
					"fs_1": "1",
				},
			}
			return mockNfsMetricResult(id, read, write), nil
		}
		if id == 2 {
			log.Infof("Volume Metrics")
			currentIOs := map[string]interface{}{
				"spa": map[string]interface{}{
					"sv_2": "0",
				},
			}
			return mockVolMetricResult(id, currentIOs), nil
		}
		return nil, nil
	}

	// Same request
	request := &podmon.ValidateVolumeHostConnectivityRequest{
		NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
		VolumeIds: []string{
			"vol1-iSCSI-array2-sv_2",
		},
	}

	// A bit longer wait than the default test value
	CollectionWait = 100

	// ===== Run test =====

	// Number of parallel requests
	count := 4

	// Run the same request in a loop to force lock/cache usage
	var wg sync.WaitGroup
	var err error
	res := make([]*podmon.ValidateVolumeHostConnectivityResponse, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(l int) {
			defer wg.Done()
			res[l], err = testConf.service.ValidateVolumeHostConnectivity(ctx, request)
			if err != nil {
				t.Errorf("Did not expect error %v", err)
				t.Fail()
			}
		}(i)
	}

	wg.Wait()

	// ===== Validate results =====
	expected := []string{
		"sv_2 on array array2 has no IOs",
		"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
	}

	for i := 0; i < count; i++ {
		assert.False(t, res[i].IosInProgress, "Expected IosInProgress to be false.")
		assert.True(t, res[i].Connected, "Expected result to have Connected=true")
		for _, msg := range res[i].Messages {
			assert.Contains(t, expected, msg, "Message does not match")
		}
	}
}

func TestMetricsRefresher(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	// ====== Test setup ======
	defaultMocks()

	testConf.service.opts.AutoProbe = true
	testConf.service.arrays.Store("array1", mockStorage(ctx))
	GetHostID = func(ctx context.Context, s *service, arrayId, shortHostname, longHostname string) (*types.Host, error) {
		mockHost := mockAHost("name", 1, 0)
		return mockHost, nil
	}

	CreateMetricsCollection = func(ctx context.Context, s *service, arrayId string, metricPaths []string, interval int) (*types.MetricQueryCreateResponse, error) {
		return &types.MetricQueryCreateResponse{
			Base:    "Mock",
			Updated: time.Now().String(),
			Content: types.MetricQueryResponseContent{
				MaximumSamples: 0,
				Expiration:     "mockExpiration",
				Interval:       interval,
				Paths:          metricPaths,
				ID:             2,
			},
		}, nil
	}

	GetMetricsCollection = func(ctx context.Context, s *service, arrayId string, id int) (*types.MetricQueryResult, error) {
		log.Infof("Volume Metrics")
		currentIOs := map[string]interface{}{
			"spa": map[string]interface{}{
				"sv_2": "0",
			},
		}
		return mockVolMetricResult(id, currentIOs), nil
	}

	request := &podmon.ValidateVolumeHostConnectivityRequest{
		NodeId: "worker-1-rdyqdaifyjtju,worker-1-rdyqdaifyjtju",
		VolumeIds: []string{
			"vol1-iSCSI-array2-sv_2",
		},
	}

	// A bit longer wait than the default test value
	CollectionWait = 100
	// Set the count so that refresh attempts will be tried
	refreshCount.Store(0)
	refreshEnabled = true

	// ===== Run test =====
	res, err := testConf.service.ValidateVolumeHostConnectivity(ctx, request)
	if err != nil {
		t.Errorf("Did not expect error %v", err)
		t.Fail()
	}

	// Wait some time, so that the refreshes can run in the background
	time.Sleep(500 * time.Millisecond)

	// ===== Validate results =====
	expected := []string{
		"sv_2 on array array2 has no IOs",
		"iSCSI Health is good for array:array2, Health:ALRT_COMPONENT_OK",
	}

	assert.False(t, res.IosInProgress, "Expected IosInProgress to be false.")
	assert.True(t, res.Connected, "Expected result to have Connected=true")
	for _, msg := range res.Messages {
		assert.Contains(t, expected, msg, "Message does not match")
	}
}

func runTestCases(ctx context.Context, t *testing.T, testCases map[string]testCaseSpec) {
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
				assert.Equal(t, tc.expectedResponse.IosInProgress, res.IosInProgress, name)
			}
		}

		if tc.expectError != nil {
			assert.NotNil(t, err, name)
			assert.Equal(t, tc.expectError, err)
		}

		if tc.expectError == nil && err != nil {
			assert.Errorf(t, err, fmt.Sprintf("Expected test case '%s' to not return error, but it did", name))
		}

		if tc.expectErrorType != "" {
			assert.NotNil(t, err, name)
			assert.Equal(t, tc.expectErrorType, reflect.TypeOf(err).String())
		}

		if tc.expectErrorLike != "" {
			assert.NotNil(t, err, name)
			assert.True(t, strings.Contains(err.Error(), tc.expectErrorLike),
				fmt.Sprintf("Expected test case '%s' to return error like '%s', but was '%s'", name, tc.expectErrorLike, err.Error()))
		}

		if tc.cleanup != nil {
			tc.cleanup()
		}
	}
}

var (
	mockGetArrayFromVolumeFail       string
	mockGetArrayIDErr                error
	mockRequireProbeErr              error
	mockGetHost                      *types.Host
	mockGetHostErr                   error
	mockGetUnity                     *gounity.Client
	mockGetUnityErr                  error
	mockFindHostInitiatorErr         error
	mockBadInitiator                 string
	mockFindInitatorFail             string
	mockGetMetricsCollectionError    error
	mockCreateMetricsCollectionError error
	mockMetricsCollectionID          int
	mockMetricValueMap               map[string]interface{}
)

func defaultMocks() {
	GetHostID = mockGetHostID
	RequireProbe = mockRequireProbe
	GetUnityClient = mockGetUnityClient
	GetArrayIDFromVolumeContext = mockGetArrayIDFromVolumeContext
	GetProtocolFromVolumeContext = mockGetProtocolFromVolumeContext
	FindHostInitiatorByID = mockFindHostInitiatorByID
	GetMetricsCollection = mockGetMetricsCollection
	CreateMetricsCollection = mockCreateMetricsCollection
	CollectionWait = 10 // msec

	mockGetArrayFromVolumeFail = ""
	mockGetArrayIDErr = nil
	mockRequireProbeErr = nil
	mockGetHost = nil
	mockGetHostErr = nil
	mockGetUnity = nil
	mockGetUnityErr = nil
	mockFindHostInitiatorErr = nil
	mockBadInitiator = ""
	mockFindInitatorFail = ""
	mockMetricsCollectionID = 1
	mockCreateMetricsCollectionError = nil
	mockGetMetricsCollectionError = nil
	metricsCollectionCache.Delete("array1:sp.*.storage.lun.*.currentIOCount")
	metricsCollectionCache.Delete("array1:sp.*.storage.filesystem.*.clientReads,sp.*.storage.filesystem.*.clientWrites")
	mockMetricValueMap = map[string]interface{}{
		"spa": map[string]interface{}{
			"sv_1": "0",
			"sv_2": "1",
			"sv_3": "100",
		},
	}
	// Set a lower refresh interval
	RefreshDuration = 100 * time.Millisecond
	// Effectively disable the refresh by default for testing
	refreshEnabled = false
}

func mockGetArrayIDFromVolumeContext(_ *service, contextVolID string) (string, error) {
	if mockGetArrayFromVolumeFail == contextVolID {
		return "", mockGetArrayIDErr
	}
	components := strings.Split(contextVolID, "-")
	return components[len(components)-2], mockGetArrayIDErr
}

func mockGetProtocolFromVolumeContext(_ *service, contextVolID string) (string, error) {
	tokens := strings.Split(contextVolID, "-")
	if len(tokens) == 1 {
		// Only one token found, which means volume created using csi-unity v1.0 and v1.1. So return Unknown protocol
		return ProtocolUnknown, nil
	} else if len(tokens) >= 4 {
		return tokens[len(tokens)-3], nil
	}
	return "", errors.New("invalid volume context id")
}

func mockRequireProbe(ctx context.Context, s *service, arrayID string) error {
	return mockRequireProbeErr
}

func mockGetHostID(ctx context.Context, s *service, arrayID, shortHostname, longHostname string) (*types.Host, error) {
	return mockGetHost, mockGetHostErr
}

func mockGetUnityClient(ctx context.Context, s *service, arrayID string) (*gounity.Client, error) {
	return mockGetUnity, mockGetUnityErr
}

func mockStorage(ctx context.Context) *StorageArrayConfig {
	client, _ := gounity.NewClient(ctx)
	insecure := true
	return &StorageArrayConfig{
		ArrayID:                   "array1",
		Username:                  "username",
		Password:                  "pass",
		Endpoint:                  "http://some.host.com",
		SkipCertificateValidation: &insecure,
		IsDefaultArray:            true,
		IsProbeSuccess:            false,
		IsHostAdded:               false,
		UnityClient:               client,
	}
}

func mockAHost(name string, iScsiPorts, fcPorts int) *types.Host {
	mockISCSI := make([]types.Initiators, 0)
	for i := 0; i < iScsiPorts; i++ {
		mockISCSI = append(mockISCSI, types.Initiators{ID: fmt.Sprintf("mock_%s_iscsi_initiator_%d", name, i)})
	}
	mockFC := make([]types.Initiators, 0)
	for i := 0; i < fcPorts; i++ {
		mockFC = append(mockFC, types.Initiators{ID: fmt.Sprintf("mock_%s_fc_initiator_%d", name, i)})
	}
	mockHost := &types.Host{
		HostContent: types.HostContent{
			ID:              name,
			Name:            name,
			Description:     fmt.Sprintf("Mock test host %s", name),
			FcInitiators:    mockFC,
			IscsiInitiators: mockISCSI,
			IPPorts:         nil,
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
			ID: id,
			Health: types.HealthContent{
				DescriptionIDs: descriptions,
			},
			Type:        0,
			InitiatorID: id,
			IsIgnored:   false,
			Paths:       nil,
		},
	}
}

func mockFindHostInitiatorByID(ctx context.Context, unity *gounity.Client, wwnOrIqn string) (*types.HostInitiator, error) {
	return mockAnInitiator(wwnOrIqn), mockFindHostInitiatorErr
}

func mockGetMetricsCollection(ctx context.Context, s *service, arrayID string, id int) (*types.MetricQueryResult, error) {
	return mockVolMetricResult(id, mockMetricValueMap), mockGetMetricsCollectionError
}

func mockCreateMetricsCollection(ctx context.Context, s *service, arrayID string, metricPaths []string, interval int) (*types.MetricQueryCreateResponse, error) {
	mockCollection := &types.MetricQueryCreateResponse{
		Base:    "Mock",
		Updated: time.Now().String(),
		Content: types.MetricQueryResponseContent{
			MaximumSamples: 0,
			Expiration:     "mockExpiration",
			Interval:       interval,
			Paths:          metricPaths,
			ID:             mockMetricsCollectionID,
		},
	}
	return mockCollection, mockCreateMetricsCollectionError
}

func mockVolMetricResult(id int, currentIOs map[string]interface{}) *types.MetricQueryResult {
	result := types.MetricResult{
		QueryID:   id,
		Path:      "sp.*.storage.lun.*.currentIOs",
		Timestamp: time.Now().String(),
		Values:    currentIOs,
	}

	results := []types.MetricResultEntry{
		{
			Base:    "MockResultEntry",
			Updated: "",
			Content: result,
		},
	}

	mockResult := &types.MetricQueryResult{
		Base:    "MockResult",
		Updated: time.Now().String(),
		Entries: results,
	}

	return mockResult
}

func mockNfsMetricResult(id int, read, write map[string]interface{}) *types.MetricQueryResult {
	resultR := types.MetricResult{
		QueryID:   id,
		Path:      "sp.*.storage.filesystem.*.clientReads",
		Timestamp: time.Now().String(),
		Values:    read,
	}

	resultW := types.MetricResult{
		QueryID:   id,
		Path:      "sp.*.storage.filesystem.*.clientWrites",
		Timestamp: time.Now().String(),
		Values:    write,
	}

	results := []types.MetricResultEntry{
		{
			Base:    "MockResultEntry",
			Updated: "",
			Content: resultR,
		},
		{
			Base:    "MockResultEntry",
			Updated: "",
			Content: resultW,
		},
	}

	mockResult := &types.MetricQueryResult{
		Base:    "MockResult",
		Updated: time.Now().String(),
		Entries: results,
	}

	return mockResult
}
