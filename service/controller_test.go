/*
 Copyright © 2020 Dell Inc. or its subsidiaries. All Rights Reserved.

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
	"testing"

	"github.com/dell/csi-unity/service/utils"
	"github.com/stretchr/testify/assert"
)

func TestControllerProbe(t *testing.T) {
	DriverConfig = testConf.unityConfig

	//Dynamic update of config
	err := testConf.service.BeforeServe(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("TestBeforeServe failed with error %v", err)
	}
	if testConf.service.getStorageArrayLength() == 0 {
		t.Fatalf("Credentials are empty")
	}
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1111")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	err = testConf.service.probe(ctx, "controller", "")
	assert.True(t, err != nil, "probe failed")
}
