package service

import (
	"context"
	"os"
	"testing"

	"github.com/dell/csi-unity/service/utils"
	"github.com/stretchr/testify/assert"
)

// Split TestMkdir into separate tests to test each case
func TestMkdirCreateDir(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1111")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	// Make temp directory to use for testing
	basepath, err := os.MkdirTemp("/tmp", "*")
	defer os.RemoveAll(basepath)
	path := basepath + "/test"

	// Test creating a directory
	created, err := mkdir(ctx, path)
	assert.NoError(t, err)
	assert.True(t, created)
}

func TestMkdirExistingDir(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1111")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	// Make temp directory to use for testing
	basepath, err := os.MkdirTemp("/tmp", "*")
	defer os.RemoveAll(basepath)

	path := basepath + "/test"

	// Pre-create the directory
	err = os.Mkdir(path, 0o755)
	assert.NoError(t, err)

	// Test creating an existing directory
	created, err := mkdir(ctx, path)
	assert.NoError(t, err)
	assert.False(t, created)
}

func TestMkdirExistingFile(t *testing.T) {
	log := utils.GetLogger()
	ctx := context.Background()
	entry := log.WithField(utils.RUNID, "1111")
	ctx = context.WithValue(ctx, utils.UnityLogger, entry)

	// Make temp directory to use for testing
	basepath, err := os.MkdirTemp("/tmp", "*")
	defer os.RemoveAll(basepath)

	path := basepath + "/file"
	file, err := os.Create(path)
	assert.NoError(t, err)
	file.Close()

	// Test creating a directory with an existing file path
	created, err := mkdir(ctx, path)
	assert.Error(t, err)
	assert.False(t, created)
}
