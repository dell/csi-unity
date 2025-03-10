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
	"fmt"
	"os"
	"time"

	"github.com/kubernetes-csi/csi-lib-utils/leaderelection"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	buildConfigFromFlags = clientcmd.BuildConfigFromFlags
	newForConfig         = kubernetes.NewForConfig
	inClusterConfig      = rest.InClusterConfig
)

// leaderElection interface
type leaderElection interface {
	Run() error
	WithNamespace(namespace string)
}

// ExitFunc is a variable that holds the os.Exit function
var ExitFunc = os.Exit

// RunLeaderElection runs the leader election and handles errors
func RunLeaderElection(le leaderElection) {
	if err := le.Run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to initialize leader election: %v", err)
		ExitFunc(1)
	}
}

// CreateKubeClientSet - Returns kubeclient set
func CreateKubeClientSet(kubeconfig string) (*kubernetes.Clientset, error) {
	var clientset *kubernetes.Clientset
	if kubeconfig != "" {
		// use the current context in kubeconfig
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
		// create the clientset
		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			return nil, err
		}
	} else {
		config, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
		// creates the clientset
		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			return nil, err
		}
	}
	return clientset, nil
}

// LeaderElection - Initialize leader selection
func LeaderElection(clientset *kubernetes.Clientset, lockName string, namespace string,
	leaderElectionRenewDeadline, leaderElectionLeaseDuration, leaderElectionRetryPeriod time.Duration, runFunc func(ctx context.Context),
) {
	log.Info("Starting leader election setup...")

	le := leaderelection.NewLeaderElection(clientset, lockName, runFunc)
	log.Infof("Leader election created with lock name: %s", lockName)

	le.WithNamespace(namespace)
	log.Infof("Namespace set to: %s", namespace)

	le.WithLeaseDuration(leaderElectionLeaseDuration)
	log.Infof("Lease duration set to: %v", leaderElectionLeaseDuration)

	le.WithRenewDeadline(leaderElectionRenewDeadline)
	log.Infof("Renew deadline set to: %v", leaderElectionRenewDeadline)

	le.WithRetryPeriod(leaderElectionRetryPeriod)
	log.Infof("Retry period set to: %v", leaderElectionRetryPeriod)

	log.Info("Running leader election...")
	RunLeaderElection(le)
	log.Info("Leader election completed.")
}
