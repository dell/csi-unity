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

package provider

import (
	"testing"

	"github.com/dell/csi-unity/service"
	"github.com/dell/gocsi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	pluginProvider := New()
	require.NotNil(t, pluginProvider, "StoragePluginProvider should not be nil")

	plugin, ok := pluginProvider.(*gocsi.StoragePlugin)
	require.True(t, ok, "Returned provider should be of type *gocsi.StoragePlugin")
	require.NotNil(t, plugin, "*gocsi.StoragePlugin instance should not be nil")

	svc := service.New()

	assert.Equal(t, svc, plugin.Controller, "Controller should be equal to the service")
	assert.Equal(t, svc, plugin.Identity, "Identity should be equal to the service")
	assert.Equal(t, svc, plugin.Node, "Node should be equal to the service")
	assert.NotNil(t, plugin.BeforeServe, "BeforeServe should not be nil")
	assert.NotNil(t, plugin.RegisterAdditionalServers, "RegisterAdditionalServers should not be nil")

	expectedEnvVars := []string{
		gocsi.EnvVarSpecReqValidation + "=true",
		gocsi.EnvVarSerialVolAccess + "=true",
	}
	assert.ElementsMatch(t, expectedEnvVars, plugin.EnvVars, "Environment variables should match the expected values")
}
