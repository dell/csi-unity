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
