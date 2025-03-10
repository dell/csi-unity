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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	constants "github.com/dell/csi-unity/common"
	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/gobrick"
	"github.com/dell/gocsi"
	csictx "github.com/dell/gocsi/context"
	"github.com/dell/gounity"
	gounitymocks "github.com/dell/gounity/mocks"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (m *MockISCSIConnector) NewISCSIConnector(params gobrick.ISCSIConnectorParams) *gobrick.ISCSIConnector {
	args := m.Called(params)
	return args.Get(0).(*gobrick.ISCSIConnector)
}

// Mocking the setupGobrick function
// func setupGobrick(s *service) {
// 	// Mock implementation
// }

func (m *MockFCConnector) NewFCConnector(params gobrick.FCConnectorParams) *gobrick.FCConnector {
	args := m.Called(params)
	return args.Get(0).(*gobrick.FCConnector)
}

// Mocking the setupGobrick function

func TestLoadDynamicConfig(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		secretFile string
		configFile string
		wantErr    bool
	}{
		{
			name:       "secret file modified",
			mode:       "node",
			secretFile: "/path/to/secret.yaml",
			configFile: "/path/to/config.yaml",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &service{
				mode: tt.mode,
			}

			// Create a temporary directory for the test
			tempDir := t.TempDir()
			secretFile := filepath.Join(tempDir, "secret.yaml")
			configFile := filepath.Join(tempDir, "config.yaml")

			// Write initial content to the secret and config files
			err := os.WriteFile(secretFile, []byte("initial secret content"), 0o600)
			if err != nil {
				t.Fatalf("failed to write secret file: %v", err)
			}
			err = os.WriteFile(configFile, []byte("initial config content"), 0o600)
			if err != nil {
				t.Fatalf("failed to write config file: %v", err)
			}

			// Run the loadDynamicConfig function in a separate goroutine
			go func() {
				err := s.loadDynamicConfig(context.Background(), secretFile, configFile)
				if (err != nil) != tt.wantErr {
					t.Errorf("loadDynamicConfig() error = %v, wantErr %v", err, tt.wantErr)
				}
			}()

			// Simulate the creation of the secret file
			newSecretFile := filepath.Join(tempDir, "..data")
			err = os.WriteFile(newSecretFile, []byte("new secret content"), 0o600)
			if err != nil {
				t.Fatalf("failed to write new secret file: %v", err)
			}

			// Allow some time for the goroutine to process the event
			time.Sleep(1 * time.Second)
		})
	}
}

func TestInfo(_ *testing.T) {
	s := customLogger{}
	s.Info(context.Background(), "Node: %s, Value: %s", "node1", "node1")
}

func TestDebug(_ *testing.T) {
	s := customLogger{}
	s.Debug(context.Background(), "Node: %s, Value: %s", "node1", "node1")
}

func TestError(_ *testing.T) {
	s := customLogger{}
	s.Error(context.Background(), "Node: %s, Value: %s", "node1", "node1")
}

func TestRequireProbe(t *testing.T) {
	s := service{}
	err := s.requireProbe(context.Background(), "testarrayid")
	assert.NotNil(t, err)
}

func TestSyncDriverConfig(t *testing.T) {
	ctx := context.Background()
	v := viper.New()

	v.Set(constants.ParamCSILogLevel, "Debug")
	v.Set(constants.ParamAllowRWOMultiPodAccess, true)
	v.Set(constants.ParamCSILogLevel, "")
	v.Set(constants.ParamMaxUnityVolumesPerNode, int64(10))
	v.Set(constants.ParamTenantName, "tenant1")
	v.Set(constants.ParamSyncNodeInfoTimeInterval, int64(30))

	s := &service{}
	s.syncDriverConfig(ctx, v)

	assert.True(t, s.opts.AllowRWOMultiPodAccess)
	assert.Equal(t, int64(10), s.opts.MaxVolumesPerNode)
	assert.Equal(t, "tenant1", s.opts.TenantName)
	assert.Equal(t, int64(30), s.opts.SyncNodeInfoTimeInterval)
}

func TestIncrementLogID(t *testing.T) {
	ctx := context.Background()

	// Test for "node" prefix
	ctx, entry := incrementLogID(ctx, "node")
	assert.NotNil(t, ctx)
	assert.NotNil(t, entry)

	// Test for "config" prefix
	ctx, entry = incrementLogID(ctx, "config")
	assert.NotNil(t, ctx)
	assert.NotNil(t, entry)

	// Test for invalid prefix
	ctx, entry = incrementLogID(ctx, "invalid")
	assert.Nil(t, ctx)
	assert.Nil(t, entry)
}

func TestGetLogFields(t *testing.T) {
	t.Run("NilContext", func(t *testing.T) {
		fields := getLogFields(nil)
		assert.Equal(t, logrus.Fields{}, fields)
	})

	t.Run("NoLogFieldsInContext", func(t *testing.T) {
		ctx := context.Background()
		fields := getLogFields(ctx)
		assert.Equal(t, logrus.Fields{}, fields)
	})

	t.Run("LogFieldsInContext", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), utils.LogFields, logrus.Fields{"key": "value"})
		fields := getLogFields(ctx)
		assert.Equal(t, logrus.Fields{"key": "value"}, fields)
	})

	t.Run("RequestIDInContext", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), csictx.RequestIDKey, "12345")
		fields := getLogFields(ctx)
		assert.Equal(t, logrus.Fields{utils.RUNID: "12345"}, fields)
	})

	t.Run("LogFieldsAndRequestIDInContext", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), utils.LogFields, logrus.Fields{"key": "value"})
		ctx = context.WithValue(ctx, csictx.RequestIDKey, "12345")
		fields := getLogFields(ctx)
		assert.Equal(t, logrus.Fields{"key": "value", utils.RUNID: "12345"}, fields)
	})
}

// Mocking the gobrick package
type MockISCSIConnector struct {
	mock.Mock
}

func TestInitISCSIConnector(t *testing.T) {
	t.Run("ISCSIConnectorIsNil", func(_ *testing.T) {
		s := &service{}
		chroot := "/some/chroot"

		// Mocking the gobrick.NewISCSIConnector function
		mockConnector := new(MockISCSIConnector)
		mockConnector.On("NewISCSIConnector", gobrick.ISCSIConnectorParams{Chroot: chroot}).Return(&gobrick.ISCSIConnector{})

		s.initISCSIConnector(chroot)

		// assert.NotNil(t, s.iscsiConnector)
		// mockConnector.AssertCalled(t, "NewISCSIConnector", gobrick.ISCSIConnectorParams{Chroot: chroot})
	})

	t.Run("ISCSIConnectorIsNotNil", func(_ *testing.T) {
		s := &service{
			iscsiConnector: &gobrick.ISCSIConnector{},
		}
		chroot := "/some/chroot"

		s.initISCSIConnector(chroot)

		// assert.NotNil(t, s.iscsiConnector)
	})
}

func TestInitFCConnector(t *testing.T) {
	t.Run("FCConnectorIsNil", func(_ *testing.T) {
		s := &service{}
		chroot := "/some/chroot"

		// Mocking the gobrick.NewFCConnector function
		mockConnector := new(MockFCConnector)
		mockConnector.On("NewFCConnector", gobrick.FCConnectorParams{Chroot: chroot}).Return(&gobrick.FCConnector{})

		s.initFCConnector(chroot)

		// assert.NotNil(t, s.fcConnector)
		// mockConnector.AssertCalled(t, "NewFCConnector", gobrick.FCConnectorParams{Chroot: chroot})
	})

	t.Run("FCConnectorIsNotNil", func(_ *testing.T) {
		s := &service{
			fcConnector: &gobrick.FCConnector{},
		}
		chroot := "/some/chroot"

		s.initFCConnector(chroot)

		// assert.NotNil(t, s.fcConnector)
	})
}

type MockLogger struct {
	mock.Mock
}

// Fire implements logrus.Hook.
func (m *MockLogger) Fire(*logrus.Entry) error {
	panic("unimplemented")
}

// Levels implements logrus.Hook.
func (m *MockLogger) Levels() []logrus.Level {
	panic("unimplemented")
}

func TestStorageArrayConfig_String(t *testing.T) {
	mockUnity := &gounitymocks.UnityClient{}
	// mockClient.On("AddHost").Return(true)

	config := StorageArrayConfig{
		ArrayID:                   "array123",
		Username:                  "user",
		Password:                  "pass",
		Endpoint:                  "endpoint",
		IsDefault:                 nil,
		SkipCertificateValidation: nil,
		IsProbeSuccess:            true,
		IsHostAdded:               true,
		IsHostAdditionFailed:      false,
		IsDefaultArray:            true,
		UnityClient:               mockUnity,
	}

	expected := "ArrayID: array123, Username: user, Endpoint: endpoint, SkipCertificateValidation: <nil>, IsDefaultArray:true, IsProbeSuccess:true, IsHostAdded:true"
	assert.Equal(t, expected, config.String())
}

// MockUnityClient is a mock implementation of the UnityClient interface
type MockUnityClient struct {
	mock.Mock
}

func (m *MockUnityClient) BasicSystemInfo(ctx context.Context, config *gounity.ConfigConnect) error {
	args := m.Called(ctx, config)
	return args.Error(0)
}

func (m *MockLogger) Info(args ...interface{}) {
	m.Called(args...)
}

func (m *MockLogger) Warnf(format string, args ...interface{}) {
	m.Called(append([]interface{}{format}, args...)...)
}

func (m *MockLogger) Debugf(format string, args ...interface{}) {
	m.Called(append([]interface{}{format}, args...)...)
}

func (m *MockLogger) Errorf(format string, args ...interface{}) {
	m.Called(append([]interface{}{format}, args...)...)
}

func TestSingleArrayProbeSuccess(t *testing.T) {
	mockUnityClient := &gounitymocks.UnityClient{}
	ctx := context.Background()

	array := &StorageArrayConfig{
		ArrayID:                   "array123",
		Username:                  "user",
		Password:                  "pass",
		Endpoint:                  "endpoint",
		SkipCertificateValidation: new(bool),
		UnityClient:               mockUnityClient,
	}

	// Create a real logrus.Logger and use it to create a new entry
	realLogger := logrus.New()
	logger := logrus.NewEntry(realLogger)

	// Mock the logger methods
	mockLogger := new(MockLogger)

	// Mock the GetRunidAndLoggerWrapper function
	originalGetRunidAndLoggerWrapper := utils.GetRunidAndLoggerWrapper
	defer func() { utils.GetRunidAndLoggerWrapper = originalGetRunidAndLoggerWrapper }()
	utils.GetRunidAndLoggerWrapper = func(_ context.Context) (string, *logrus.Entry) {
		return "runid123", logger
	}

	t.Run("Probe Success", func(t *testing.T) {
		mockUnityClient.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
		// mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()

		err := singleArrayProbe(ctx, "probeType", array)
		assert.NoError(t, err)
		assert.True(t, array.IsProbeSuccess)

		mockUnityClient.AssertExpectations(t)
		mockLogger.AssertExpectations(t)
	})
}

func TestSingleArrayProbeFailure(t *testing.T) {
	mockUnityClient := &gounitymocks.UnityClient{}
	ctx := context.Background()

	array := &StorageArrayConfig{
		ArrayID:                   "array123",
		Username:                  "user",
		Password:                  "pass",
		Endpoint:                  "endpoint",
		SkipCertificateValidation: new(bool),
		UnityClient:               mockUnityClient,
	}

	// Create a real logrus.Logger and use it to create a new entry
	realLogger := logrus.New()
	logger := logrus.NewEntry(realLogger)

	// Mock the logger methods
	mockLogger := new(MockLogger)

	// Mock the GetRunidAndLoggerWrapper function
	originalGetRunidAndLoggerWrapper := utils.GetRunidAndLoggerWrapper
	defer func() { utils.GetRunidAndLoggerWrapper = originalGetRunidAndLoggerWrapper }()
	utils.GetRunidAndLoggerWrapper = func(_ context.Context) (string, *logrus.Entry) {
		return "runid123", logger
	}

	t.Run("Probe Failure - Unauthenticated", func(t *testing.T) {
		mockUnityClient.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(status.Error(codes.Unauthenticated, "unauthenticated"))
		// mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()

		err := singleArrayProbe(ctx, "probeType", array)
		assert.Error(t, err)
		assert.False(t, array.IsProbeSuccess)
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))

		mockUnityClient.AssertExpectations(t)
		mockLogger.AssertExpectations(t)
	})
}

func TestSingleArrayProbeFailure1(t *testing.T) {
	mockUnityClient := &gounitymocks.UnityClient{}
	ctx := context.Background()

	array := &StorageArrayConfig{
		ArrayID:                   "array123",
		Username:                  "user",
		Password:                  "pass",
		Endpoint:                  "endpoint",
		SkipCertificateValidation: new(bool),
		UnityClient:               mockUnityClient,
	}

	// Create a real logrus.Logger and use it to create a new entry
	realLogger := logrus.New()
	logger := logrus.NewEntry(realLogger)

	// Mock the logger methods
	mockLogger := new(MockLogger)

	// Mock the GetRunidAndLoggerWrapper function
	originalGetRunidAndLoggerWrapper := utils.GetRunidAndLoggerWrapper
	defer func() { utils.GetRunidAndLoggerWrapper = originalGetRunidAndLoggerWrapper }()
	utils.GetRunidAndLoggerWrapper = func(_ context.Context) (string, *logrus.Entry) {
		return "runid123", logger
	}
	t.Run("Probe Failure - Other Error", func(t *testing.T) {
		mockUnityClient.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(status.Error(codes.Internal, "internal error"))
		// mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()

		err := singleArrayProbe(ctx, "probeType", array)
		assert.Error(t, err)
		assert.False(t, array.IsProbeSuccess)
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))

		mockUnityClient.AssertExpectations(t)
		mockLogger.AssertExpectations(t)
	})
}

func TestRegisterAdditionalServers(_ *testing.T) {
	s := service{}
	server := grpc.NewServer()
	s.RegisterAdditionalServers(server)
}

func TestNew(t *testing.T) {
	s := New()
	assert.NotNil(t, s)
}

func TestGetProtocolFromVolumeContext(t *testing.T) {
	tests := []struct {
		name          string
		contextVolID  string
		expectedProto string
		expectedError string
	}{
		{
			name:          "valid iscsi protocol",
			contextVolID:  "iscsi-1234-5678-91011",
			expectedProto: "1234",
			expectedError: "",
		},
		{
			name:          "empty contextVolID",
			contextVolID:  "",
			expectedProto: "",
			expectedError: "volume context id should not be empty",
		},
		{
			name:          "single token contextVolID",
			contextVolID:  "singleToken",
			expectedProto: ProtocolUnknown,
			expectedError: "",
		},
		{
			name:          "invalid contextVolID",
			contextVolID:  "invalid-context",
			expectedProto: "",
			expectedError: "invalid volume context id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := service{}
			proto, err := s.getProtocolFromVolumeContext(tt.contextVolID)

			if tt.expectedError == "" {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedProto, proto)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			}
		})
	}
}

func TestGetNodeLabels(t *testing.T) {
	s := service{}
	_, err := s.GetNodeLabels(context.Background())
	assert.NotNil(t, err)

	// Create a mock HTTP server for mocking kubernetes API call
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/nodes/test-node" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"metadata": {"labels": {"test-node/testarrayid-iSCSI": "true"}}}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	// Create kubeconfig file
	tmpFile, err := ioutil.TempFile("", "kubeconfig")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	kubeconfigContent := `
apiVersion: v1
clusters:
- cluster:
    server: ` + mockServer.URL + `
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
kind: Config
preferences: {}
users:
- name: test-user
  user:
    token: test-token
`
	err = ioutil.WriteFile(tmpFile.Name(), []byte(kubeconfigContent), 0o600)
	require.NoError(t, err)
	testConf.service.opts.KubeConfigPath = tmpFile.Name()
	testConf.service.opts.LongNodeName = "test-node"

	nodeLabels, err := testConf.service.GetNodeLabels(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, "true", nodeLabels["test-node/testarrayid-iSCSI"])
}

func TestGetUnityClient(t *testing.T) {
	s := &service{
		arrays: &sync.Map{},
	}
	_, err := s.getUnityClient(context.Background(), "arrayID")
	assert.NotNil(t, err)
}

func TestGetArrayIDFromVolumeContext(_ *testing.T) {
	s := &service{}

	tests := []struct {
		contextVolID string
		expectedID   string
		expectedErr  error
	}{
		{"", "", errors.New("volume context id should not be empty")},
		{"vol-123", "array1", nil},
		{"vol-123-456-789", "456", nil},
		{"vol-123-456", "", errors.New("invalid volume context id or no default array found in the csi-unity driver configuration")},
	}

	for _, test := range tests {
		_, _ = s.getArrayIDFromVolumeContext(test.contextVolID)
	}
}

func TestGetArrayIDFromVolumeContext_DefaultArray(t *testing.T) {
	mockUnityClient := &gounitymocks.UnityClient{}
	arrayID := "testarrayid"
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:        arrayID,
		UnityClient:    mockUnityClient,
		IsDefaultArray: true,
	})
	_, err := getArrayIDFromVolumeContext(testConf.service, "volid")
	assert.Equal(t, nil, err)
}

func TestGetRunidLog(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		expectedRID string
	}{
		{
			name:        "Nil context",
			ctx:         nil,
			expectedRID: "",
		},
		{
			name:        "Context with request ID",
			ctx:         metadata.NewIncomingContext(context.Background(), metadata.Pairs(csictx.RequestIDKey, "12345")),
			expectedRID: "12345",
		},
		{
			name:        "Context without request ID",
			ctx:         metadata.NewIncomingContext(context.Background(), metadata.Pairs("some-other-key", "value")),
			expectedRID: "1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, log, rid := GetRunidLog(test.ctx)
			assert.NotNil(t, log)
			assert.Equal(t, test.expectedRID, rid)

			if test.ctx != nil {
				fields, _ := ctx.Value(utils.LogFields).(logrus.Fields)
				assert.NotNil(t, fields)
				if test.expectedRID != "" {
					assert.Equal(t, test.expectedRID, fields[utils.RUNID])
				}
			}
		})
	}
}

func TestTrace(_ *testing.T) {
	dl := emptyTracer{}
	dl.Trace(context.Background(), "test", "test")
}

func TestBeforeServe(t *testing.T) {
	setenv := func() {
		os.Setenv(gocsi.EnvVarDebug, "false")
		os.Setenv(EnvNodeName, "unit-test")
		os.Setenv(EnvKubeConfigPath, "test-kubeconfig")
		os.Setenv(SyncNodeInfoTimeInterval, "15")
		os.Setenv(EnvAllowRWOMultiPodAccess, "true")
		os.Setenv(EnvIsVolumeHealthMonitorEnabled, "true")
		os.Setenv(EnvPvtMountDir, "/var/lib/kubelet/pods")
		os.Setenv(EnvEphemeralStagingPath, "/tmp")
		os.Setenv(EnvAllowedNetworks, "89.0.142.86/24,89.207.132.170/16")
		os.Setenv(EnvISCSIChroot, "/var/lib/kubelet")
	}

	unsetenv := func() {
		os.Unsetenv(gocsi.EnvVarDebug)
		os.Unsetenv(EnvNodeName)
		os.Unsetenv(EnvKubeConfigPath)
		os.Unsetenv(SyncNodeInfoTimeInterval)
		os.Unsetenv(EnvIsVolumeHealthMonitorEnabled)
		os.Unsetenv(EnvPvtMountDir)
		os.Unsetenv(EnvEphemeralStagingPath)
		os.Unsetenv(EnvAllowedNetworks)
		os.Unsetenv(EnvISCSIChroot)
	}

	setenv()
	err := testConf.service.BeforeServe(context.Background(), nil, nil)
	assert.Error(t, err)
	unsetenv()

	// Corner and invalid cases
	setenv = func() {
		os.Setenv(SyncNodeInfoTimeInterval, "invalid")
		os.Setenv(EnvAllowRWOMultiPodAccess, "invalid")
		os.Setenv(EnvAllowedNetworks, "")
		os.Setenv(EnvNodeName, "192.168.1.1")
	}
	unsetenv = func() {
		os.Unsetenv(SyncNodeInfoTimeInterval)
		os.Unsetenv(EnvAllowRWOMultiPodAccess)
		os.Unsetenv(EnvAllowedNetworks)
		os.Unsetenv(EnvNodeName)
	}
	setenv()
	err = testConf.service.BeforeServe(context.Background(), nil, nil)
	assert.Error(t, err)
	unsetenv()
}

func TestSetLogFieldsInContext(t *testing.T) {
	// Create a context without any log fields
	ctx := context.Background()
	logID := "test-log-id"
	logType := "test-log-type"

	// Call the function
	newCtx, ulog := setLogFieldsInContext(ctx, logID, logType)

	// Assert that the context contains the updated log fields
	fields, ok := newCtx.Value(utils.LogFields).(logrus.Fields)
	assert.True(t, ok)
	assert.Equal(t, logID, fields[logType])

	// Assert that the logger contains the updated log fields
	assert.NotNil(t, ulog)
	assert.Equal(t, logID, ulog.Data[logType])

	// Create a context with nil log fields
	ctx = context.WithValue(context.Background(), utils.LogFields, logrus.Fields(nil))

	// Call the function again
	newCtx, ulog = setLogFieldsInContext(ctx, logID, logType)

	// Assert that the context contains the updated log fields
	fields, ok = newCtx.Value(utils.LogFields).(logrus.Fields)
	assert.True(t, ok)
	assert.Equal(t, logID, fields[logType])

	// Assert that the logger contains the updated log fields
	assert.NotNil(t, ulog)
	assert.Equal(t, logID, ulog.Data[logType])
}

func TestProbe(t *testing.T) {
	mockUnityClient := &gounitymocks.UnityClient{}
	arrayID := "testarrayid"
	skipCertificateValidation := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:                   arrayID,
		UnityClient:               mockUnityClient,
		IsDefaultArray:            true,
		SkipCertificateValidation: &skipCertificateValidation,
	})
	testConf.service.mode = "node"
	logger := logrus.NewEntry(logrus.New())
	ctx := context.WithValue(context.Background(), utils.UnityLogger, logger)
	mockUnityClient.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(nil)
	_, err := testConf.service.Probe(ctx, &csi.ProbeRequest{})
	assert.Equal(t, nil, err)

	mockUnityClient.ExpectedCalls = nil
	mockUnityClient.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(errors.New("error"))
	_, err = testConf.service.Probe(ctx, &csi.ProbeRequest{})
	assert.Error(t, err)
}

func TestValidateAndGetResourceDetails(t *testing.T) {
	ctx := context.Background()
	mockUnityClient := &gounitymocks.UnityClient{}
	arrayID := "testarrayid"

	// No Array
	testConf.service.arrays = &sync.Map{}
	_, _, _, _, err := testConf.service.validateAndGetResourceDetails(ctx, "", "")
	assert.Error(t, err)

	// Unable to get protocol
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:     arrayID,
		UnityClient: mockUnityClient,
	})
	_, _, _, _, err = testConf.service.validateAndGetResourceDetails(ctx, "invalid-context", "")
	assert.Error(t, err)

	// No unity client
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:        arrayID,
		IsDefaultArray: true,
		UnityClient:    nil,
	})
	_, _, _, _, err = testConf.service.validateAndGetResourceDetails(ctx, "testresourceContextID ", "testresourcetype")
	assert.Error(t, err)
}

func TestRequireProbeFail(t *testing.T) {
	mockUnityClient := &gounitymocks.UnityClient{}
	arrayID := "testarrayid"
	skipCertificateValidation := true
	testConf.service.arrays.Store(arrayID, &StorageArrayConfig{
		ArrayID:                   arrayID,
		UnityClient:               mockUnityClient,
		IsDefaultArray:            true,
		SkipCertificateValidation: &skipCertificateValidation,
	})
	logger := logrus.NewEntry(logrus.New())
	ctx := context.WithValue(context.Background(), utils.UnityLogger, logger)
	testConf.service.opts.AutoProbe = true
	mockUnityClient.On("BasicSystemInfo", mock.Anything, mock.Anything).Return(errors.New("error"))
	err := testConf.service.requireProbe(ctx, arrayID)
	assert.Error(t, err)
}

func mockReadSecret(_ string) ([]byte, error) {
	driverSecret := os.Getenv("DRIVER_SECRET")
	switch driverSecret {
	case "empty array data":
		return []byte("storageArrayList: []"), nil
	case "invalid array data":
		return []byte("storageArrayList: 1"), nil
	case "valid array data":
		return []byte(`
storageArrayList:
  - arrayId: "testarrayid"
    username: "test-username"
    password: "test-password"
    endpoint: "test-endpoint"
    isDefault: true
    skipCertificateValidation: true
`), nil
	case "empty ArrayID":
		return []byte(`
storageArrayList:
  - arrayId: ""
    username: "test-username"
    password: "test-password"
    endpoint: "test-endpoint"
    isDefault: true
    skipCertificateValidation: true
`), nil
	case "empty Username":
		return []byte(`
storageArrayList:
  - arrayId: "testarrayid"
    username: ""
    password: "test-password"
    endpoint: "test-endpoint"
    isDefault: true
    skipCertificateValidation: true
`), nil
	case "empty Password":
		return []byte(`
storageArrayList:
  - arrayId: "testarrayid"
    username: "test-username"
    password: ""
    endpoint: "test-endpoint"
    isDefault: true
    skipCertificateValidation: true
`), nil
	case "nil SkipCertificateValidation":
		return []byte(`
storageArrayList:
  - arrayId: "testarrayid"
    username: "test-username"
    password: "test-password"
    endpoint: "test-endpoint"
    isDefault: true
    skipCertificateValidation: null
`), nil
	case "empty Endpoint":
		return []byte(`
storageArrayList:
  - arrayId: "testarrayid"
    username: "test-username"
    password: "test-password"
    endpoint: ""
    isDefault: true
    skipCertificateValidation: true
`), nil
	case "nil IsDefault":
		return []byte(`
storageArrayList:
  - arrayId: "testarrayid"
    username: "test-username"
    password: "test-password"
    endpoint: "test-endpoint"
    isDefault: null
    skipCertificateValidation: true
`), nil
	default:
		return []byte(driverSecret), nil
	}
}

func TestSyncDriverSecret(t *testing.T) {
	// Mock Unity client
	mockUnityClient := &gounitymocks.UnityClient{}
	ctx := context.Background()
	readFile = mockReadSecret
	defer func() {
		readFile = os.ReadFile
	}()

	// Test case: Unable to parse the DriverSecret as yaml
	os.Setenv("DRIVER_SECRET", "invalid array data")
	mockUnityClient.On("Authenticate", mock.Anything, mock.Anything).Return(nil)
	err := testConf.service.syncDriverSecret(ctx)
	os.Unsetenv("DRIVER_SECRET")
	assert.Error(t, err)

	// Test case: Arrays details are not provided in unity-creds secret
	os.Setenv("DRIVER_SECRET", "")
	mockUnityClient.On("Authenticate", mock.Anything, mock.Anything).Return(nil)
	err = testConf.service.syncDriverSecret(ctx)
	os.Unsetenv("DRIVER_SECRET")
	assert.Error(t, err)

	// Test case: Arrays is there secret but with empty data
	os.Setenv("DRIVER_SECRET", "empty array data")
	mockUnityClient.On("Authenticate", mock.Anything, mock.Anything).Return(nil)
	err = testConf.service.syncDriverSecret(ctx)
	os.Unsetenv("DRIVER_SECRET")
	assert.Error(t, err)

	// Test case: ArrayID empty
	os.Setenv("DRIVER_SECRET", "empty ArrayID")
	mockUnityClient.On("Authenticate", mock.Anything, mock.Anything).Return(nil)
	err = testConf.service.syncDriverSecret(ctx)
	os.Unsetenv("DRIVER_SECRET")
	assert.Error(t, err)

	// Test case: username empty
	os.Setenv("DRIVER_SECRET", "empty Username")
	mockUnityClient.On("Authenticate", mock.Anything, mock.Anything).Return(nil)
	err = testConf.service.syncDriverSecret(ctx)
	os.Unsetenv("DRIVER_SECRET")
	assert.Error(t, err)

	// Test case: Password empty
	os.Setenv("DRIVER_SECRET", "empty Password")
	mockUnityClient.On("Authenticate", mock.Anything, mock.Anything).Return(nil)
	err = testConf.service.syncDriverSecret(ctx)
	os.Unsetenv("DRIVER_SECRET")
	assert.Error(t, err)

	// Test case: nil SkipCertificateValidation
	os.Setenv("DRIVER_SECRET", "nil SkipCertificateValidation")
	mockUnityClient.On("Authenticate", mock.Anything, mock.Anything).Return(nil)
	err = testConf.service.syncDriverSecret(ctx)
	os.Unsetenv("DRIVER_SECRET")
	assert.Error(t, err)

	// Test case: empty Endpoint
	os.Setenv("DRIVER_SECRET", "empty Endpoint")
	mockUnityClient.On("Authenticate", mock.Anything, mock.Anything).Return(nil)
	err = testConf.service.syncDriverSecret(ctx)
	os.Unsetenv("DRIVER_SECRET")
	assert.Error(t, err)

	// Test case: nil IsDefault
	os.Setenv("DRIVER_SECRET", "nil IsDefault")
	mockUnityClient.On("Authenticate", mock.Anything, mock.Anything).Return(nil)
	err = testConf.service.syncDriverSecret(ctx)
	os.Unsetenv("DRIVER_SECRET")
	assert.Error(t, err)

	// Test case: success case
	os.Setenv("DRIVER_SECRET", "valid array data")
	mockUnityClient.On("Authenticate", mock.Anything, mock.Anything).Return(nil)
	err = testConf.service.syncDriverSecret(ctx)
	// os.Unsetenv("DRIVER_SECRET")
	assert.Equal(t, nil, err)
}
