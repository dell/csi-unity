// package service_test

// func test(){

// }
package service_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	common "github.com/dell/csi-unity/common"
	"github.com/dell/csi-unity/service"
	csictx "github.com/dell/gocsi/context"
	"github.com/dell/gounity"
	"github.com/dell/gounity/mocks"
	"github.com/dell/gounity/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

func TestServiceSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ServiceTest Suite")
}

var svc service.Service

var mockVolume *mocks.VolumeInterface

func setenv() {
	svc = service.New()

	testProp, err := readTestProperties("./test_properties.txt")
	if err != nil {
		panic("The system cannot find the file specified")
	}

	if err != nil {
		fmt.Println(err)
	}

	csictx.Setenv(context.Background(), service.EnvAutoProbe, testProp["X_CSI_UNITY_AUTOPROBE"])
	csictx.Setenv(context.Background(), common.ParamCSILogLevel, testProp["CSI_LOG_LEVEL"])
	service.DriverSecret = testProp["DRIVER_SECRET"]
	service.DriverConfig = testProp["DRIVER_CONFIG"]

}

func readTestProperties(filename string) (map[string]string, error) {
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

var _ = Describe("Test CreateVolume()", func() {
	BeforeEach(func() {
		setenv()
		fmt.Println("BeforeEach initialized")
		config := new(service.Opts)
		config.AutoProbe = true
		service.MockRequireProbeErr = nil
		service.MockGetUnityErr = nil
		service.MockGetUnity = returnValue(gounity.NewClientWithArgs(context.Background(), "http://some.host.com", false))

		fmt.Println("MockGetUnityValue ", service.MockGetUnity)
		service.RequireProbeController = service.MockRequireProbe
		service.GetUnityClient = service.MockGetUnityClient

	})
	Describe("Calling CreateVolume", func() {
		When("parameters are correct", func() {
			It("should create volume of iSCSI protocol successfully", func() {
				svc := service.New()
				volName := "NewVol"
				arrayID := "UNISPHERE123"
				protocol := "iSCSI"
				size := 3456789902
				svc.BeforeServe(context.Background(), nil, nil)
				mockVolume = new(mocks.VolumeInterface)
				service.MockCallAPIResp = nil
				service.CallVolAPI = service.MockCallVolAPI
				service.MockVolAPIResp = mockVolume
				service.Transport = service.MockTransport

				response := &(types.IoLimitPolicy{IoLimitPolicyContent: types.IoLimitPolicyContent{ID: "hostIOLimit_1",
					Name: "Autotier"}})

				volResponse := &(types.Volume{VolumeContent: types.VolumeContent{Name: volName,
					ResourceID:             "res_1000",
					Description:            "new vol",
					SizeTotal:              uint64(size),
					SizeUsed:               10000,
					SizeAllocated:          uint64(size) - 1000,
					TieringPolicy:          1,
					IsThinEnabled:          true,
					IsDataReductionEnabled: true}})

				mockVolume.On("InterfaceAssignment").Return(mockVolume)
				mockVolume.On("FindHostIOLimitByName", mock.Anything, "Autotier").Return(response, nil)
				mockVolume.On("FindVolumeByName", mock.Anything, volName).Return(volResponse, nil)
				mockVolume.On("CreateLun", mock.Anything, volName, "pool_7", "new vol", size, 1, mock.Anything, true, true).Return(volResponse, nil)

				resp, err := svc.CreateVolume(context.Background(), CreateVolumeRequest(volName, arrayID, protocol, size))
				Expect(err).To(BeNil())
				Expect(resp).To(Equal(&csi.CreateVolumeResponse{
					Volume: &csi.Volume{
						CapacityBytes: int64(size),
						VolumeId:      "NewVol-iSCSI-unisphere123-res_1000",
						VolumeContext: map[string]string{
							"protocol": "iSCSI",
							"arrayId":  "unisphere123",
							"volumeId": "res_1000",
						},
					},
				}))

			})
			It("should create a filesystem of NFS protocol", func() {
				svc := service.New()
				volName := "New_Fs"
				arrayID := "UNISPHERE123"
				protocol := "NFS"
				size := 3456789902

				mockFilesystem := new(mocks.FilesystemInterface)
				service.CallFSAPI = service.MockCallFsAPI
				svc.BeforeServe(context.Background(), nil, nil)
				service.MockFsAPIResp = mockFilesystem
				service.TransportFs = service.MockTransportFs

				response := &(types.Filesystem{FileContent: types.FileContent{ID: "fs_1000",
					IsThinEnabled:          true,
					IsDataReductionEnabled: true,
					HostIOSize:             int64(size),
					Name:                   "New_fs",
					SizeTotal:              uint64(size),
					Pool: types.Pool{
						ID:   "res_1000",
						Name: "lun_1000",
					},
					Description: "New Filesystem created",
					NASServer: types.Pool{
						ID:   "nas_1",
						Name: "nasserver",
					},
					TieringPolicy: 1,
					Type:          1,
				}})

				mockFilesystem.On("InterfaceAssignment").Return(mockFilesystem)
				mockFilesystem.On("CreateFilesystem", mock.Anything, volName, mock.Anything, "CSI Volume Unit Test", "nas_7", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(response, nil)
				mockFilesystem.On("FindFilesystemByName", mock.Anything, volName).Return(response, nil)

				errResp := "'Filesystem name' already exists and size/NAS server/storage pool is different."

				_, err := svc.CreateVolume(context.Background(), CreateVolumeRequest(volName, arrayID, protocol, size))
				Expect(err.Error()).To(ContainSubstring(errResp))

			})
		})
	})
})

func CreateVolumeRequest(volumeName, arrayId, protocol string, size int) *csi.CreateVolumeRequest {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagePool"] = "pool_7"
	params["thinProvisioned"] = "true"
	params["isDataReductionEnabled"] = "false"
	params["tieringPolicy"] = "0"
	params["description"] = "CSI Volume Unit Test"
	params["arrayId"] = "UNISPHERE123"
	params["protocol"] = protocol
	params["nasServer"] = "nas_7"
	params["hostIOLimitName"] = "Autotier"
	params["hostIoSize"] = "10987"

	req.Parameters = params
	req.Name = volumeName
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = int64(size)
	capacityRange.LimitBytes = int64(size) * 2
	req.CapacityRange = capacityRange
	capability := new(csi.VolumeCapability)
	mount := new(csi.VolumeCapability_MountVolume)
	mountType := new(csi.VolumeCapability_Mount)
	mountType.Mount = mount
	capability.AccessType = mountType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities

	fmt.Printf("Requested parameters to Create Volume = %s", req)
	return req
}

// Return the Value
func returnValue(client *gounity.Client, err error) *gounity.Client {
	return client
}
