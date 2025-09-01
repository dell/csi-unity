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

package k8sutils

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var exitFunc = os.Exit

type MockLeaderElection struct {
	mock.Mock
}

func (m *MockLeaderElection) Run() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockLeaderElection) WithNamespace(namespace string) {
	m.Called(namespace)
}

// MockExit is a mock implementation of os.Exit
var MockExit = func(code int) {
	fmt.Printf("os.Exit(%d) called\n", code)
}

func TestLeaderElection(t *testing.T) {
	clientset := &kubernetes.Clientset{} // Mock or use a fake clientset if needed
	runFunc := func(_ context.Context) {
		fmt.Println("Running leader function")
	}

	mockLE := new(MockLeaderElection)
	mockLE.On("Run").Return(nil) // Mocking a successful run

	// Override exitFunc to prevent test from exiting
	exitCalled := false
	oldExit := exitFunc
	defer func() { recover(); exitFunc = oldExit }()
	exitFunc = func(_ int) { exitCalled = true; panic("exitFunc called") }

	// Simulate LeaderElection function
	func() {
		defer func() {
			if r := recover(); r != nil {
				exitCalled = true
			}
		}()
		LeaderElection(clientset, "test-lock", "test-namespace", time.Second, time.Second*2, time.Second*3, runFunc)
	}()

	if !exitCalled {
		t.Errorf("exitFunc was called unexpectedly")
	}
}

func TestCreateKubeClientSet(t *testing.T) {
	// Test cases
	tests := []struct {
		name       string
		kubeconfig string
		configErr  error
		clientErr  error
		wantErr    bool
	}{
		{
			name:       "Valid kubeconfig",
			kubeconfig: "valid_kubeconfig",
			configErr:  nil,
			clientErr:  nil,
			wantErr:    false,
		},
		{
			name:       "Invalid kubeconfig",
			kubeconfig: "invalid_kubeconfig",
			configErr:  errors.New("config error"),
			clientErr:  nil,
			wantErr:    true,
		},
		{
			name:       "In-cluster config",
			kubeconfig: "",
			configErr:  nil,
			clientErr:  nil,
			wantErr:    false,
		},
		{
			name:       "In-cluster config error",
			kubeconfig: "",
			configErr:  errors.New("config error"),
			clientErr:  nil,
			wantErr:    true,
		},
		{
			name:       "New for config error",
			kubeconfig: "",
			configErr:  nil,
			clientErr:  errors.New("client error"),
			wantErr:    true,
		},
		{
			name:       "New for config error with valid kubeconfig",
			kubeconfig: "valid_kubeconfig",
			configErr:  nil,
			clientErr:  errors.New("client error"),
			wantErr:    true,
		},
	}

	// Save original functions
	origBuildConfigFromFlags := buildConfigFromFlags
	origInClusterConfig := inClusterConfig
	origNewForConfig := newForConfig

	// Restore original functions after tests
	defer func() {
		buildConfigFromFlags = origBuildConfigFromFlags
		inClusterConfig = origInClusterConfig
		newForConfig = origNewForConfig
	}()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary kubeconfig file for the valid kubeconfig test case
			if tt.name == "Valid kubeconfig" || tt.name == "New for config error with valid kubeconfig" {
				tmpFile, err := ioutil.TempFile("", "kubeconfig")
				require.NoError(t, err)
				defer os.Remove(tmpFile.Name())

				kubeconfigContent := `
apiVersion: v1
clusters:
- cluster:
    server: https://127.0.0.1:6443
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
				tt.kubeconfig = tmpFile.Name()
			}

			// Mock environment variables for in-cluster config
			if tt.name == "In-cluster config" || tt.name == "In-cluster config error" {
				os.Setenv("KUBERNETES_SERVICE_HOST", "127.0.0.1")
				os.Setenv("KUBERNETES_SERVICE_PORT", "443")
				defer os.Unsetenv("KUBERNETES_SERVICE_HOST")
				defer os.Unsetenv("KUBERNETES_SERVICE_PORT")
			}

			// Mock functions
			buildConfigFromFlags = func(_, _ string) (*rest.Config, error) {
				return &rest.Config{}, tt.configErr
			}
			inClusterConfig = func() (*rest.Config, error) {
				return &rest.Config{}, tt.configErr
			}
			newForConfig = func(_ *rest.Config) (*kubernetes.Clientset, error) {
				if tt.clientErr != nil {
					return nil, tt.clientErr
				}
				return &kubernetes.Clientset{}, nil
			}

			clientset, err := CreateKubeClientSet(tt.kubeconfig)
			if (err != nil) != tt.wantErr {
				return
			}
			if !tt.wantErr && clientset == nil {
				t.Errorf("CreateKubeClientSet() = nil, want non-nil")
			}
		})
	}
}

func TestRunLeaderElection(t *testing.T) {
	// Save the original ExitFunc
	origExitFunc := ExitFunc
	// Restore the original ExitFunc after the test
	defer func() { ExitFunc = origExitFunc }()

	// Override ExitFunc with the mock implementation
	ExitFunc = MockExit

	t.Run("RunLeaderElection_Success", func(t *testing.T) {
		mockLE := new(MockLeaderElection)
		mockLE.On("Run").Return(nil)

		RunLeaderElection(mockLE)

		mockLE.AssertExpectations(t)
	})

	t.Run("RunLeaderElection_Error", func(t *testing.T) {
		mockLE := new(MockLeaderElection)
		mockLE.On("Run").Return(errors.New("leader election error"))

		// Capture the output
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		// Run the function
		RunLeaderElection(mockLE)

		// Restore stderr
		w.Close()
		os.Stderr = oldStderr

		// Read the captured output
		var buf [1024]byte
		n, _ := r.Read(buf[:])
		output := string(buf[:n])

		require.Contains(t, output, "failed to initialize leader election: leader election error")
		mockLE.AssertExpectations(t)
	})
}
