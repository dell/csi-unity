/*
 Copyright © 2019-2025 Dell Inc. or its subsidiaries. All Rights Reserved.

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

package csiutils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"bou.ke/monkey"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/service/logging"
	types "github.com/dell/gounity/apitypes"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var (
	execCommand = exec.Command
	osHostname  = os.Hostname
)

// Mock logger
type MockLogger struct{}

// Mock implementations for os.DirEntry
type mockDirEntry struct {
	name string
}

func (l *MockLogger) Debug(_ ...interface{})            {}
func (l *MockLogger) Debugf(_ string, _ ...interface{}) {}
func (l *MockLogger) Info(_ ...interface{})             {}
func (l *MockLogger) Warnf(_ string, _ ...interface{})  {}

func (m mockDirEntry) Name() string               { return m.name }
func (m mockDirEntry) IsDir() bool                { return true }
func (m mockDirEntry) Type() os.FileMode          { return os.ModeDir }
func (m mockDirEntry) Info() (os.FileInfo, error) { return nil, nil }

func TestGetAddresses(t *testing.T) {
	type errorTestCases struct {
		description       string
		addrs             []net.Addr
		networkAddresses  []string
		expectedAddresses []string
		expectedError     string
	}

	for _, scenario := range []errorTestCases{
		{
			description:       "invalid",
			addrs:             []net.Addr{&net.IPNet{IP: net.ParseIP("192.168.2.1"), Mask: net.CIDRMask(24, 32)}},
			networkAddresses:  []string{"192.168.1.1/24"},
			expectedAddresses: []string{},
			expectedError:     fmt.Sprintf("no valid IP address found matching against allowedNetworks %v", []string{"192.168.1.1/24"}),
		},
		{
			description:       "successful",
			addrs:             []net.Addr{&net.IPNet{IP: net.ParseIP("192.168.1.1"), Mask: net.CIDRMask(24, 32)}},
			networkAddresses:  []string{"192.168.1.0/24"},
			expectedAddresses: []string{"192.168.1.1"},
			expectedError:     "",
		},
		{
			description: "multiple networks, multiple addresses-successful",
			addrs: []net.Addr{
				&net.IPNet{IP: net.ParseIP("192.168.1.1"), Mask: net.CIDRMask(24, 32)},
				&net.IPNet{IP: net.ParseIP("192.168.2.1"), Mask: net.CIDRMask(24, 32)},
			},
			networkAddresses:  []string{"192.168.1.0/24", "192.168.2.0/24"},
			expectedAddresses: []string{"192.168.1.1", "192.168.2.1"},
			expectedError:     "",
		},
		{
			description: "multiple networks, one erroneous address",
			addrs: []net.Addr{
				&net.IPNet{IP: net.ParseIP("192.168.1.1"), Mask: net.CIDRMask(24, 32)},
				&net.IPNet{IP: net.ParseIP("192.168.3.1"), Mask: net.CIDRMask(24, 32)},
			},
			networkAddresses:  []string{"192.168.1.0/24", "192.168.2.0/24"},
			expectedAddresses: []string{"192.168.1.1"},
			expectedError:     "",
		},
	} {
		t.Run(scenario.description, func(t *testing.T) {
			addresses, err := GetAddresses(scenario.networkAddresses, scenario.addrs)
			if err != nil {
				assert.EqualError(t, err, scenario.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, scenario.expectedAddresses, addresses)
			}
		})
	}
}

// MockCmd is a helper function to create a mocked exec.Command
func MockCmd(output string, err error) func(name string, arg ...string) *exec.Cmd {
	return func(_ string, _ ...string) *exec.Cmd {
		cmd := exec.Command("echo", output)
		if err != nil {
			cmd = exec.Command("false")
		}
		cmd.Stdout = bytes.NewBufferString(output)
		return cmd
	}
}

// mockHostname is a helper function to mock os.Hostname
func mockHostname(hostname string, err error) func() (string, error) {
	return func() (string, error) {
		return hostname, err
	}
}

func TestGetHostIP(t *testing.T) {
	originalExecCommand := execCommand
	originalHostname := osHostname

	defer func() {
		execCommand = originalExecCommand
		osHostname = originalHostname
	}()

	tests := []struct {
		description    string
		hostnameOutput string
		hostnameError  error
		cmdOutput      string
		cmdError       error
		expectedIps    []string
		expectedError  error
	}{
		{
			description:    "successful hostname -I",
			cmdOutput:      "192.168.1.1\n",
			cmdError:       nil,
			hostnameOutput: "testhost",
			expectedError:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			execCommand = MockCmd(tt.cmdOutput, tt.cmdError)
			osHostname = mockHostname(tt.hostnameOutput, tt.hostnameError)

			_, err := GetHostIP()
			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetNFSClientIP(t *testing.T) {
	_, err := net.InterfaceAddrs()
	if err != nil {
		assert.Error(t, err)
	}

	// Valid allowedNetworks
	allowedNetworks := []string{"89.207.132.170/24", "89.0.142.86/24"}
	_, err = GetNFSClientIP(allowedNetworks)
	if err != nil {
		assert.Error(t, err)
	}

	// Invalid address in allowedNetworks
	allowedNetworks = []string{"invalid_address"}
	_, err = GetNFSClientIP(allowedNetworks)
	if err != nil {
		assert.Error(t, err)
	}
}

func TestGetSnapshotResponseFromSnapshot(t *testing.T) {
	snap := &types.Snapshot{
		SnapshotContent: types.SnapshotContent{
			Name:         "snap1",
			State:        2,
			CreationTime: time.Now(),
			ResourceID:   "snap-123",
			Size:         1024,
			StorageResource: types.StorageResource{
				ID: "vol-123",
			},
		},
	}

	snapResp := GetSnapshotResponseFromSnapshot(snap, "protocol", "array-1")
	assert.Equal(t, snapResp.Snapshot.SnapshotId, fmt.Sprintf("%s-%s-%s-%s", snap.SnapshotContent.Name, "protocol", "array-1", snap.SnapshotContent.ResourceID))
}

func TestArrayContainsAll(t *testing.T) {
	stringArray1 := []string{"array-1"}
	stringArray2 := []string{"array-1"}
	contains := ArrayContainsAll(stringArray1, stringArray2)
	assert.Equal(t, contains, true)

	// Negative case
	stringArray2 = []string{"array-2"}
	contains = ArrayContainsAll(stringArray1, stringArray2)
	assert.Equal(t, contains, false)
}

func TestFindAdditionalWwns(t *testing.T) {
	stringArray1 := []string{"array-1, array-2"}
	stringArray2 := []string{"array-1"}
	differenceSet := FindAdditionalWwns(stringArray1, stringArray2)
	assert.Equal(t, differenceSet, []string{"array-1"})
}

func TestIpListContains(t *testing.T) {
	ipArray := []net.IP{net.ParseIP("192.168.1.10")}
	contains := ipListContains(ipArray, "192.168.1.10")
	assert.Equal(t, contains, true)

	// Negative case
	ipArray = []net.IP{}
	contains = ipListContains(ipArray, "192.168.1.10")
	assert.Equal(t, contains, false)
}

func TestGetIPsFromInferfaces(t *testing.T) {
	// Test case: Empty input
	input := []types.IPInterfaceEntries{}
	expected := []string{}
	result := GetIPsFromInferfaces(context.Background(), input)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected: %v, but got: %v", expected, result)
	}

	// Test case: Single IP
	input = []types.IPInterfaceEntries{
		{
			IPInterfaceContent: types.IPInterfaceContent{
				IPAddress: "1.2.3.4",
			},
		},
	}
	expected = []string{"1.2.3.4"}
	result = GetIPsFromInferfaces(context.Background(), input)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected: %v, but got: %v", expected, result)
	}

	// Test case: Multiple IPs
	input = []types.IPInterfaceEntries{
		{
			IPInterfaceContent: types.IPInterfaceContent{
				IPAddress: "1.2.3.4",
			},
		},
		{
			IPInterfaceContent: types.IPInterfaceContent{
				IPAddress: "5.6.7.8",
			},
		},
	}
	expected = []string{"1.2.3.4", "5.6.7.8"}
	result = GetIPsFromInferfaces(context.Background(), input)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected: %v, but got: %v", expected, result)
	}
}

func TestGetWwnFromVolumeContentWwn(t *testing.T) {
	// Test case: Empty input
	input := ""
	expected := ""
	result := GetWwnFromVolumeContentWwn(input)
	if result != expected {
		t.Errorf("Expected %s, but got %s", expected, result)
	}

	// Test case: Input with colons
	input = "12:34:56:78:90:AB:CD:EF"
	expected = "1234567890abcdef"
	result = GetWwnFromVolumeContentWwn(input)
	if result != expected {
		t.Errorf("Expected %s, but got %s", expected, result)
	}

	// Test case: Input without colons
	input = "1234567890abcdef"
	expected = "1234567890abcdef"
	result = GetWwnFromVolumeContentWwn(input)
	if result != expected {
		t.Errorf("Expected %s, but got %s", expected, result)
	}

	// Test case: Input with uppercase letters
	input = "12:34:56:78:90:AB:CD:EF"
	expected = "1234567890abcdef"
	result = GetWwnFromVolumeContentWwn(input)
	if result != expected {
		t.Errorf("Expected %s, but got %s", expected, result)
	}
}

func TestGetFcPortWwnFromVolumeContentWwn(t *testing.T) {
	wwn := GetFcPortWwnFromVolumeContentWwn("500507680B26FD3A500507680B26FD3BC")
	res := GetWwnFromVolumeContentWwn(wwn)
	assert.Equal(t, res, "500507680b26fd3b")
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		err      error
	}{
		{"100 Mi", 104857600, nil},
		{"100 Gi", 107374182400, nil},
		{"100 Ti", 109951162777600, nil},
		{"100 Pi", 112589990684262400, nil},
		{"100", 0, errors.New("Failed to parse size")},
		{"100 M", 0, errors.New("Failed to parse size")},
		{"100 Mi 100", 0, errors.New("Failed to parse size")},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result, err := ParseSize(test.input)
			if err != nil {
				if test.err == nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if err.Error() != test.err.Error() {
					t.Errorf("Expected error %v, got %v", test.err, err)
				}
				return
			}
			if err == nil && test.err != nil {
				t.Errorf("Expected error %v, got no error", test.err)
				return
			}
			if result != test.expected {
				t.Errorf("Expected result %v, got %v", test.expected, result)
			}
		})
	}
}

func TestGetVolumeResponseFromVolume(t *testing.T) {
	volume := &types.Volume{
		VolumeContent: types.VolumeContent{
			Name:       "test-volume",
			ResourceID: "vol-123",
			SizeTotal:  1024,
		},
	}
	arrayID := "array-1"
	protocol := "iSCSI"
	preferredAccessibility := []*csi.Topology{
		{
			Segments: map[string]string{"topology.kubernetes.io/zone": "zone1"},
		},
	}

	response := GetVolumeResponseFromVolume(volume, arrayID, protocol, preferredAccessibility)

	expectedResponse := getVolumeResponse("test-volume", protocol, arrayID, "vol-123", 1024, preferredAccessibility)
	assert.Equal(t, expectedResponse, response)
}

func TestGetVolumeResponseFromFilesystem(t *testing.T) {
	filesystem := &types.Filesystem{
		FileContent: types.FileContent{
			Name:      "test-filesystem",
			ID:        "fs-123",
			SizeTotal: 2048,
		},
	}
	arrayID := "array-2"
	protocol := "NFS"
	preferredAccessibility := []*csi.Topology{
		{
			Segments: map[string]string{"topology.kubernetes.io/zone": "zone2"},
		},
	}

	response := GetVolumeResponseFromFilesystem(filesystem, arrayID, protocol, preferredAccessibility)

	expectedResponse := getVolumeResponse("test-filesystem", protocol, arrayID, "fs-123", 2048, preferredAccessibility)
	assert.Equal(t, expectedResponse, response)
}

func TestGetVolumeResponseFromSnapshot(t *testing.T) {
	snapshot := &types.Snapshot{
		SnapshotContent: types.SnapshotContent{
			Name:       "test-snapshot",
			ResourceID: "snap-123",
			Size:       4096,
		},
	}
	arrayID := "array-3"
	protocol := "FC"
	preferredAccessibility := []*csi.Topology{
		{
			Segments: map[string]string{"topology.kubernetes.io/zone": "zone3"},
		},
	}

	response := GetVolumeResponseFromSnapshot(snapshot, arrayID, protocol, preferredAccessibility)

	volID := fmt.Sprintf("%s-%s-%s-%s", snapshot.SnapshotContent.Name, protocol, arrayID, snapshot.SnapshotContent.ResourceID)
	expectedVolumeContext := map[string]string{
		"protocol": "FC",
		"arrayId":  "array-3",
		"volumeId": "snap-123",
	}
	expectedVolume := &csi.Volume{
		VolumeId:           volID,
		CapacityBytes:      int64(snapshot.SnapshotContent.Size),
		VolumeContext:      expectedVolumeContext,
		AccessibleTopology: preferredAccessibility,
	}
	expectedResponse := &csi.CreateVolumeResponse{
		Volume: expectedVolume,
	}
	assert.Equal(t, expectedResponse, response)
}

func TestGetMessageWithRunID(t *testing.T) {
	tests := []struct {
		name     string
		runid    string
		format   string
		args     []interface{}
		expected string
	}{
		{
			name:     "Test with no args",
			runid:    "123",
			format:   "This is a test",
			args:     nil,
			expected: " runid=123 This is a test",
		},
		{
			name:     "Test with one arg",
			runid:    "456",
			format:   "Testing with arg: %s",
			args:     []interface{}{"arg1"},
			expected: " runid=456 Testing with arg: arg1",
		},
		{
			name:     "Test with multiple args",
			runid:    "789",
			format:   "Testing with args: %s, %s, %s",
			args:     []interface{}{"arg1", "arg2", "arg3"},
			expected: " runid=789 Testing with args: arg1, arg2, arg3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := GetMessageWithRunID(tc.runid, tc.format, tc.args...)
			if result != tc.expected {
				t.Errorf("Expected: %s, got: %s", tc.expected, result)
			}
		})
	}
}

func TestIpsCompare(t *testing.T) {
	// Mock net.LookupIP
	monkey.Patch(net.LookupIP, func(host string) ([]net.IP, error) {
		if host == "example.com" {
			return []net.IP{net.ParseIP("192.168.1.1")}, nil
		}
		return nil, fmt.Errorf("lookup failed")
	})
	defer monkey.Unpatch(net.LookupIP)

	// Mock GetRunidLogger
	monkey.Patch(logging.GetRunidLogger, func(_ context.Context) *logrus.Entry {
		return logrus.NewEntry(logrus.New())
	})
	defer monkey.Unpatch(logging.GetRunidLogger)

	// Mock ipListContains
	monkey.Patch(ipListContains, func(ips []net.IP, ip string) bool {
		for _, i := range ips {
			if i.String() == ip {
				return true
			}
		}
		return false
	})
	defer monkey.Unpatch(ipListContains)

	ctx := context.Background()

	// Test case: IP matches directly
	result, additionalIps := IpsCompare(ctx, "192.168.1.1", []string{"192.168.1.1", "example.com"})
	assert.True(t, result)
	assert.Empty(t, additionalIps)

	// Test case: IP matches after lookup
	result, additionalIps = IpsCompare(ctx, "192.168.1.1", []string{"example.com"})
	assert.True(t, result)
	assert.Empty(t, additionalIps)

	// Test case: IP does not match
	result, additionalIps = IpsCompare(ctx, "192.168.1.1", []string{"192.168.1.2", "example.org"})
	assert.False(t, result)
	assert.Equal(t, []string{"192.168.1.2", "example.org"}, additionalIps)

	// Test case: Lookup fails
	result, additionalIps = IpsCompare(ctx, "192.168.1.1", []string{"unknown.com"})
	assert.False(t, result)
	assert.Equal(t, []string{"unknown.com"}, additionalIps)
}

func TestGetFCInitiators(t *testing.T) {
	// Mock os.ReadDir
	monkey.Patch(os.ReadDir, func(dirname string) ([]os.DirEntry, error) {
		if dirname == "/sys/class/fc_host" {
			return []os.DirEntry{
				mockDirEntry{name: "host0"},
				mockDirEntry{name: "host1"},
				mockDirEntry{name: "nonhost"},
			}, nil
		}
		return nil, fmt.Errorf("unknown directory: %s", dirname)
	})
	defer monkey.Unpatch(os.ReadDir)

	// Mock os.ReadFile
	monkey.Patch(os.ReadFile, func(filename string) ([]byte, error) {
		switch filename {
		case "/sys/class/fc_host/host0/port_name":
			return []byte("0x500143802426baf4"), nil
		case "/sys/class/fc_host/host0/node_name":
			return []byte("0x500143802426baf5"), nil
		case "/sys/class/fc_host/host1/port_name":
			return nil, fmt.Errorf("error reading port_name")
		case "/sys/class/fc_host/host1/node_name":
			return []byte("0x500143802426baf7"), nil
		default:
			return nil, fmt.Errorf("unknown file: %s", filename)
		}
	})
	defer monkey.Unpatch(os.ReadFile)

	// Mock GetRunidLogger
	monkey.Patch(logging.GetRunidLogger, func(_ context.Context) *logrus.Entry {
		return logrus.NewEntry(logrus.New())
	})
	defer monkey.Unpatch(logging.GetRunidLogger)

	ctx := context.Background()

	// Test case: Directory read error
	monkey.Patch(os.ReadDir, func(_ string) ([]os.DirEntry, error) {
		return nil, fmt.Errorf("cannot read directory")
	})
	wwns, err := GetFCInitiators(ctx)
	assert.Error(t, err)
	assert.Empty(t, wwns)
	monkey.Unpatch(os.ReadDir)

	// Test case: Non-host directory entries and file read errors
	monkey.Patch(os.ReadDir, func(_ string) ([]os.DirEntry, error) {
		return []os.DirEntry{
			mockDirEntry{name: "host0"},
			mockDirEntry{name: "host1"},
			mockDirEntry{name: "nonhost"},
		}, nil
	})
	wwns, err = GetFCInitiators(ctx)
	assert.NoError(t, err)
	assert.Equal(t, []string{"50:01:43:80:24:26:ba:f5:50:01:43:80:24:26:ba:f4"}, wwns)
}

func TestIPReachable(t *testing.T) {
	// Mock net.DialTimeout
	monkey.Patch(net.DialTimeout, func(_, address string, _ time.Duration) (net.Conn, error) {
		if address == "127.0.0.1:80" {
			return nil, nil
		}
		return nil, fmt.Errorf("connection failed")
	})
	defer monkey.Unpatch(net.DialTimeout)

	// Mock GetRunidLogger
	monkey.Patch(logging.GetRunidLogger, func(_ context.Context) *logrus.Entry {
		return logrus.NewEntry(logrus.New())
	})
	defer monkey.Unpatch(logging.GetRunidLogger)

	ctx := context.Background()

	// Test case: IP is reachable
	reachable := IPReachable(ctx, "127.0.0.1", "80", 1000)
	assert.True(t, reachable)

	// Test case: IP is not reachable
	reachable = IPReachable(ctx, "192.168.1.1", "80", 1000)
	assert.False(t, reachable)
}
