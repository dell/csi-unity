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

package utils

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetRunidLogger(t *testing.T) {
	log := GetLogger()
	ctx := context.Background()
	entry := log.WithField(RUNID, "1111")
	ctx = context.WithValue(ctx, UnityLogger, entry)

	logEntry := GetRunidLogger(ctx)
	logEntry.Message = "Hi This is log test1"
	message, _ := logEntry.String()
	fmt.Println(message)
	assert.True(t, strings.Contains(message, `runid=1111 msg="Hi This is log test1"`), "Log message not found")

	entry = logEntry.WithField(ARRAYID, "arr0000")
	entry.Message = "Hi this is TestSetArrayIdContext"
	message, _ = entry.String()
	assert.True(t, strings.Contains(message, `arrayid=arr0000 runid=1111 msg="Hi this is TestSetArrayIdContext"`), "Log message not found")
}
