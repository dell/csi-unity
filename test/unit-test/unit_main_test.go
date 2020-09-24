package unit_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/godog"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/provider"
	"github.com/dell/csi-unity/service"
	"github.com/rexray/gocsi/utils"
	"google.golang.org/grpc"
)

var grpcClient *grpc.ClientConn
var stop func()

//To parse the secret json file
type StorageArrayList struct {
	StorageArrayList []StorageArrayConfig `json:"storageArrayList"`
}

type StorageArrayConfig struct {
	ArrayId string `json:"arrayId"`
}

func TestMain(m *testing.M) {
	os.Setenv("X_CSI_MODE", "")

	file, err := ioutil.ReadFile(os.Getenv("DRIVER_CONFIG"))
	if err != nil {
		panic("Driver Config missing")
	}
	arrayIdList := StorageArrayList{}
	_ = json.Unmarshal([]byte(file), &arrayIdList)
	if len(arrayIdList.StorageArrayList) == 0 {
		panic("Array Info not provided")
	}
	for i := 0; i < len(arrayIdList.StorageArrayList); i++ {
		arrayIdvar := "Array" + strconv.Itoa(i+1) + "-Id"
		os.Setenv(arrayIdvar, arrayIdList.StorageArrayList[i].ArrayId)
	}

	ctx := context.Background()
	fmt.Printf("calling startServer")
	grpcClient, stop = startServer(ctx)
	fmt.Printf("back from startServer")
	time.Sleep(5 * time.Second)

	exitVal := godog.RunWithOptions("godog", func(s *godog.Suite) {
		FeatureContext(s)
	}, godog.Options{
		Format: "pretty",
		Paths:  []string{"features"},
	})
	if st := m.Run(); st > exitVal {
		exitVal = st
	}
	stop()
	os.Exit(exitVal)
}

func TestIdentityGetPluginInfo(t *testing.T) {
	ctx := context.Background()
	fmt.Printf("testing GetPluginInfo\n")
	client := csi.NewIdentityClient(grpcClient)
	info, err := client.GetPluginInfo(ctx, &csi.GetPluginInfoRequest{})
	if err != nil {
		fmt.Printf("GetPluginInfo %s:\n", err.Error())
		t.Error("GetPluginInfo failed")
	} else {
		fmt.Printf("testing GetPluginInfo passed: %s\n", info.GetName())
	}
}

func startServer(ctx context.Context) (*grpc.ClientConn, func()) {
	// Create a new SP instance and serve it with a piped connection.
	sp := provider.New()
	lis, err := utils.GetCSIEndpointListener()
	if err != nil {
		fmt.Printf("couldn't open listener: %s\n", err.Error())
		return nil, nil
	}
	service.Name = os.Getenv("DRIVER_NAME")
	service.DriverConfig = os.Getenv("DRIVER_CONFIG")
	fmt.Printf("lis: %v\n", lis)
	go func() {
		fmt.Printf("starting server\n")
		if err := sp.Serve(ctx, lis); err != nil {
			fmt.Printf("http: Server closed. Error: %v", err)
		}
	}()
	network, addr, err := utils.GetCSIEndpoint()
	if err != nil {
		return nil, nil
	}
	fmt.Printf("network %v addr %v\n", network, addr)

	clientOpts := []grpc.DialOption{
		grpc.WithInsecure(),
	}

	// Create a client for the piped connection.
	fmt.Printf("calling gprc.DialContext, ctx %v, addr %s, clientOpts %v\n", ctx, addr, clientOpts)
	client, err := grpc.DialContext(ctx, "unix:"+addr, clientOpts...)
	if err != nil {
		fmt.Printf("DialContext returned error: %s", err.Error())
	}
	fmt.Printf("grpc.DialContext returned ok\n")

	return client, func() {
		client.Close()
		sp.GracefulStop(ctx)
	}
}
