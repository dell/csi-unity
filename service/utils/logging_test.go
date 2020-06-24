package utils

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestGetRunidLogger(t *testing.T) {
	log := GetLogger()
	ctx := context.Background()
	entry := log.WithField(RUNID, "1111")
	ctx = context.WithValue(ctx, UnityLogger, entry)

	logEntry := GetRunidLogger(ctx)
	logEntry.Message = "Hi This is log test1"
	message, _ := logEntry.String()
	fmt.Println(message)
	assert.True(t, strings.Contains(message, `runid=1111 msg="Hi This is log test1"`), "Log message not found")

	entry = logEntry.WithField(ARRAYID, "arr0000")
	entry.Message = "Hi this is TestSetArrayIdContext"
	message, _ = entry.String()
	assert.True(t, strings.Contains(message, `arrayid=arr0000 runid=1111 msg="Hi this is TestSetArrayIdContext"`), "Log message not found")
}
