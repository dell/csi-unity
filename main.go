package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/rexray/gocsi"
	"os"

	"github.com/dell/csi-unity/provider"
	"github.com/dell/csi-unity/service"
)

// main is ignored when this package is built as a go plug-in.
func main() {
	driverName := flag.String("driver-name", "", "driver name")
	driverConfig := flag.String("driver-config", "", "driver config json file")
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

	gocsi.Run(
		context.Background(),
		service.Name,
		"CSI Plugin for Dell EMC Unity Array.",
		usage,
		provider.New())
}

const usage = `
    X_CSI_UNITY_NODENAME
        Specifies the name of the node where the Node plugin is running.
        The default value is empty.
`
