package service

import (
	"github.com/dell/gounity"
	"golang.org/x/net/context"
)

var MockRequireProbeErr error
var MockGetUnityErr error
var MockGetUnity *gounity.Client
var MockCallAPIResp *gounity.Volume
var MockVolAPIResp gounity.VolumeInterface
var MockFsAPIResp gounity.FilesystemInterface
var MockCallFsAPIResp *gounity.Filesystem

type TestConfig struct {
	Service *service
}
type VolumeMock struct{}

var TestConf *TestConfig

func (vm VolumeMock) InterfaceAssignment() gounity.VolumeInterface {
	return MockVolAPIResp
}

func MockRequireProbe(ctx context.Context, s *service, arrayID string) error {
	return MockRequireProbeErr
}

func MockGetUnityClient(ctx context.Context, s *service, arrayID string) (*gounity.Client, error) {
	return MockGetUnity, MockGetUnityErr
}

func MockCallVolAPI(client *gounity.Client) *gounity.Volume {
	return MockCallAPIResp
}

func MockCallFsAPI(client *gounity.Client) *gounity.Filesystem {
	return MockCallFsAPIResp
}

func MockTransport(vol gounity.VolumeAssign) gounity.VolumeInterface {
	return MockVolAPIResp
}
func MockTransportFs(f gounity.FilesystemAssign) gounity.FilesystemInterface {
	return MockFsAPIResp
}
