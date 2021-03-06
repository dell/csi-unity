package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/dell/gocsi"
	"os"
	"strings"

	"github.com/dell/csi-unity/k8sutils"
	"github.com/dell/csi-unity/provider"
	"github.com/dell/csi-unity/service"
)

type leaderElection interface {
	Run() error
	WithNamespace(namespace string)
}

// main is ignored when this package is built as a go plug-in.
func main() {
	driverName := flag.String("driver-name", "", "driver name")
	driverConfig := flag.String("driver-config", "", "driver config json file")
	enableLeaderElection := flag.Bool("leader-election", false, "boolean to enable leader election")
	leaderElectionNamespace := flag.String("leader-election-namespace", "", "namespace where leader election lease will be created")

	flag.Parse()

	if *driverName == "" {
		fmt.Fprintf(os.Stderr, "driver-name argument is mandatory")
		os.Exit(1)
	}
	service.Name = *driverName

	if *driverConfig == "" {
		fmt.Fprintf(os.Stderr, "driver-config argument is mandatory")
		os.Exit(1)
	}
	service.DriverConfig = *driverConfig

	kubeconfig := flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	flag.Parse()
	run := func(ctx context.Context) {
		gocsi.Run(ctx, service.Name, "A Unity Container Storage Interface (CSI) Plugin",
			usage, provider.New())
	}
	if !*enableLeaderElection {
		run(context.TODO())
	} else {
		driverName := strings.Replace(service.Name, ".", "-", -1)
		lockName := fmt.Sprintf("driver-%s", driverName)
		k8sclientset, err := k8sutils.CreateKubeClientSet(*kubeconfig)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "failed to initialize leader election: %v", err)
			os.Exit(1)
		}
		// Attempt to become leader and start the driver
		k8sutils.LeaderElection(k8sclientset, lockName, *leaderElectionNamespace, run)
	}
}

const usage = `
    X_CSI_UNITY_NODENAME
        Specifies the name of the node where the Node plugin is running.
        The default value is empty.
`
