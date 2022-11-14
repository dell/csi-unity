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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dell/csi-unity/service/utils"
	"github.com/sirupsen/logrus"
)

type testConfig struct {
	unityConfig  string
	service      *service
	ctx          context.Context
	defaultArray string
}

var testConf *testConfig

func TestMain(m *testing.M) {
	fmt.Println("------------In TestMain--------------")
	os.Setenv("GOUNITY_DEBUG", "true")
	os.Setenv("X_CSI_DEBUG", "true")

	// for this tutorial, we will hard code it to config.txt
	testProp, err := readTestProperties("../test.properties")
	if err != nil {
		panic("The system cannot find the file specified")
	}

	if err != nil {
		fmt.Println(err)
	}
	testConf = &testConfig{}

	testConf.unityConfig = testProp["DRIVER_CONFIG"]
	testConf.service = new(service)
	DriverConfig = testConf.unityConfig
	os.Setenv("X_CSI_UNITY_NODENAME", testProp["X_CSI_UNITY_NODENAME"])
	testConf.service.BeforeServe(context.Background(), nil, nil)
	fmt.Println()

	entry := logrus.WithField(utils.RUNID, "test-1")
	testConf.ctx = context.WithValue(context.Background(), utils.UnityLogger, entry)

	for _, v := range testConf.service.getStorageArrayList() {
		if v.IsDefaultArray {
			testConf.defaultArray = v.ArrayID
			break
		}
	}
	code := m.Run()
	fmt.Println("------------End of TestMain--------------")
	os.Exit(code)
}

func readTestProperties(filename string) (map[string]string, error) {
	// init with some bogus data
	configPropertiesMap := map[string]string{}
	if len(filename) == 0 {
		return nil, errors.New("Error reading properties file " + filename)
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	for {
		line, err := reader.ReadString('\n')

		// check if the line has = sign
		// and process the line. Ignore the rest.
		if equal := strings.Index(line, "="); equal >= 0 {
			if key := strings.TrimSpace(line[:equal]); len(key) > 0 {
				value := ""
				if len(line) > equal {
					value = strings.TrimSpace(line[equal+1:])
				}
				// assign the config map
				configPropertiesMap[key] = value
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return configPropertiesMap, nil
}

func prettyPrintJSON(obj interface{}) string {
	data, _ := json.Marshal(obj)
	return string(data)
}
