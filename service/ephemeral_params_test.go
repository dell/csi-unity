// Copyright © 2026 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//      http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeEphemeralCreateVolumeParams(t *testing.T) {
	t.Parallel()

	input := map[string]string{
		"arrayID":                "attacker-array-upper",
		"arrayId":                "attacker-array",
		"nasServer":              "attacker-nas",
		"storagePool":            "attacker-pool",
		"protocol":               "NFS",
		"thinProvisioned":        "true",
		"isDataReductionEnabled": "true",
		"tieringPolicy":          "Auto-Tier",
		"size":                   "1Gi",
	}

	got := sanitizeEphemeralCreateVolumeParams(input)

	assert.Equal(t, "1Gi", got["size"])
	assert.NotContains(t, got, "arrayID")
	assert.NotContains(t, got, "arrayId")
	assert.NotContains(t, got, "nasServer")
	assert.Equal(t, "attacker-pool", got["storagePool"])
	assert.Equal(t, "NFS", got["protocol"])
	assert.NotContains(t, got, "thinProvisioned")
	assert.NotContains(t, got, "isDataReductionEnabled")
	assert.NotContains(t, got, "tieringPolicy")
}

func TestCopyAndValidateEphemeralCreateVolumeParams(t *testing.T) {
	t.Parallel()

	input := map[string]string{
		"arrayID":   "array-upper",
		"arrayId":   "array-lower",
		"nasServer": "nas-1",
		"protocol":  "NFS",
	}

	gotParams, hasArrayID, hasNasServer := copyAndValidateEphemeralCreateVolumeParams(input)

	assert.Equal(t, input, gotParams)
	assert.True(t, hasArrayID)
	assert.True(t, hasNasServer)
}
