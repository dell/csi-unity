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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dell/gocsi"
	"k8s.io/client-go/kubernetes"

	"github.com/dell/csi-unity/k8sutils"
	"github.com/dell/csi-unity/provider"
	"github.com/dell/csi-unity/service"
)

type leaderElection interface {
	Run() error
	WithNamespace(namespace string)
}

var exitFunc = os.Exit

func validateArgs(driverConfigParamsfile *string, driverName *string, driverSecret *string) {
	if *driverConfigParamsfile == "" {
		fmt.Fprintf(os.Stderr, "driver-config argument is mandatory")
		exitFunc(1)
	}
	if *driverName == "" {
		fmt.Fprintf(os.Stderr, "driver-name argument is mandatory")
		exitFunc(1)
	}
	if *driverSecret == "" {
		fmt.Fprintf(os.Stderr, "driver-secret argument is mandatory")
		exitFunc(3)
	}
}

func checkLeaderElectionError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize leader election: %v", err)
		exitFunc(1)
	}
}

func main() {
	mainR(gocsi.Run, func(kubeconfig string) (kubernetes.Interface, error) {
		return k8sutils.CreateKubeClientSet(kubeconfig)
	}, func(clientset kubernetes.Interface, lockName, namespace string, renewDeadline, leaseDuration, retryPeriod time.Duration, run func(ctx context.Context)) {
		k8sutils.LeaderElection(clientset.(*kubernetes.Clientset), lockName, namespace, renewDeadline, leaseDuration, retryPeriod, run)
	})
}

// main is ignored when this package is built as a go plug-in.
func mainR(runFunc func(ctx context.Context, name, desc string, usage string, sp gocsi.StoragePluginProvider), createKubeClientSet func(kubeconfig string) (kubernetes.Interface, error), leaderElection func(clientset kubernetes.Interface, lockName, namespace string, renewDeadline, leaseDuration, retryPeriod time.Duration, run func(ctx context.Context))) {
	driverName := flag.String("driver-name", "", "driver name")
	driverSecret := flag.String("driver-secret", "", "driver secret yaml file")
	driverConfig := flag.String("driver-config", "", "driver config yaml file")
	enableLeaderElection := flag.Bool("leader-election", false, "boolean to enable leader election")
	leaderElectionNamespace := flag.String("leader-election-namespace", "", "namespace where leader election lease will be created")
	leaderElectionLeaseDuration := flag.Duration("leader-election-lease-duration", 15*time.Second, "Duration, in seconds, that non-leader candidates will wait to force acquire leadership")
	leaderElectionRenewDeadline := flag.Duration("leader-election-renew-deadline", 10*time.Second, "Duration, in seconds, that the acting leader will retry refreshing leadership before giving up.")
	leaderElectionRetryPeriod := flag.Duration("leader-election-retry-period", 5*time.Second, "Duration, in seconds, the LeaderElector clients should wait between tries of actions")
	flag.Parse()

	service.Name = *driverName
	service.DriverSecret = *driverSecret
	service.DriverConfig = *driverConfig
	// validate driver name, driver secret and driver config arguments
	validateArgs(driverConfig, driverName, driverSecret)

	// Always set X_CSI_DEBUG to false irrespective of what user has specified
	_ = os.Setenv(gocsi.EnvVarDebug, "false")
	// We always want to enable Request and Response logging (no reason for users to control this)
	_ = os.Setenv(gocsi.EnvVarReqLogging, "true")
	_ = os.Setenv(gocsi.EnvVarRepLogging, "true")

	kubeconfig := flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	flag.Parse()
	run := func(ctx context.Context) {
		runFunc(
			ctx,
			"csi-unity.dellemc.com",
			"An Unity Container Storage Interface (CSI) Plugin",
			usage,
			provider.New())
	}
	if *enableLeaderElection == false {
		run(context.TODO())
	} else {
		driverName := strings.Replace("csi-unity.dellemc.com", ".", "-", -1)
		lockName := fmt.Sprintf("driver-%s", driverName)
		k8sclientset, err := createKubeClientSet(*kubeconfig)
		checkLeaderElectionError(err)
		// Attempt to become leader and start the driver

		leaderElection(k8sclientset, lockName, *leaderElectionNamespace,
			*leaderElectionRenewDeadline, *leaderElectionLeaseDuration, *leaderElectionRetryPeriod, run)
	}
}

const usage = `
    X_CSI_UNITY_NODENAME
        Specifies the name of the node where the Node plugin is running.
        The default value is empty.
`
