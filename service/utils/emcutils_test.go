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

package utils

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

var execCommand = exec.Command
var osHostname = os.Hostname

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
	return func(name string, arg ...string) *exec.Cmd {
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
