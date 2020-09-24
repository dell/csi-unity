package service

import (
	"context"
	"github.com/dell/csi-unity/service/utils"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestControllerProbe(t *testing.T) {
	DriverConfig = testConf.unityConfig
	err := testConf.service.syncDriverConfig(context.Background())
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
