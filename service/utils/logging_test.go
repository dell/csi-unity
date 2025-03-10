/*
 Copyright © 2020-2025 Dell Inc. or its subsidiaries. All Rights Reserved.

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

package utils

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
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

	// Test case where logger is not present in context
	ctx = context.Background() // Reset context
	logEntry = GetRunidLogger(ctx)
	assert.Nil(t, logEntry, "Log entry should be nil when logger is not present in context")
}

func TestChangeLogLevel(t *testing.T) {
	// Ensure singletonLog is initialized before any tests
	GetLogger()

	t.Run("Debug level case", func(t *testing.T) {
		ChangeLogLevel("debug")
		if singletonLog.Level != logrus.DebugLevel {
			t.Errorf("expected DebugLevel, got %v", singletonLog.Level)
		}
	})

	t.Run("Warn level case", func(t *testing.T) {
		ChangeLogLevel("warn")
		if singletonLog.Level != logrus.WarnLevel {
			t.Errorf("expected WarnLevel, got %v", singletonLog.Level)
		}
	})

	t.Run("Warning level case", func(t *testing.T) {
		ChangeLogLevel("warning")
		if singletonLog.Level != logrus.WarnLevel {
			t.Errorf("expected WarnLevel, got %v", singletonLog.Level)
		}
	})

	t.Run("Error level case", func(t *testing.T) {
		ChangeLogLevel("error")
		if singletonLog.Level != logrus.ErrorLevel {
			t.Errorf("expected ErrorLevel, got %v", singletonLog.Level)
		}
	})

	t.Run("Info level case", func(t *testing.T) {
		ChangeLogLevel("info")
		if singletonLog.Level != logrus.InfoLevel {
			t.Errorf("expected InfoLevel, got %v", singletonLog.Level)
		}
	})

	t.Run("Default level case with unknown input", func(t *testing.T) {
		ChangeLogLevel("unknown")
		if singletonLog.Level != logrus.InfoLevel {
			t.Errorf("expected InfoLevel, got %v", singletonLog.Level)
		}
	})
}

func TestGetRunidAndLogger(t *testing.T) {
	log := logrus.New()
	entry := log.WithField(RUNID, "1111")

	tests := []struct {
		name        string
		ctx         context.Context
		expectedRID string
		expectedLog *logrus.Entry
	}{
		{
			name: "Logger and RunID present in context",
			ctx: func() context.Context {
				ctx := context.Background()
				fields := logrus.Fields{RUNID: "1111"}
				ctx = context.WithValue(ctx, LogFields, fields)
				ctx = context.WithValue(ctx, UnityLogger, entry)
				return ctx
			}(),
			expectedRID: "1111",
			expectedLog: entry,
		},
		{
			name: "Logger present but no RunID in context",
			ctx: func() context.Context {
				ctx := context.Background()
				ctx = context.WithValue(ctx, UnityLogger, entry)
				return ctx
			}(),
			expectedRID: "",
			expectedLog: entry,
		},
		{
			name: "RunID present but no Logger in context",
			ctx: func() context.Context {
				ctx := context.Background()
				fields := logrus.Fields{RUNID: "1111"}
				ctx = context.WithValue(ctx, LogFields, fields)
				return ctx
			}(),
			expectedRID: "1111",
			expectedLog: nil,
		},
		{
			name:        "Neither Logger nor RunID present in context",
			ctx:         context.Background(),
			expectedRID: "",
			expectedLog: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rid, logEntry := GetRunidAndLogger(test.ctx)
			assert.Equal(t, test.expectedRID, rid, "RunID should be %v for test %v", test.expectedRID, test.name)
			assert.Equal(t, test.expectedLog, logEntry, "Log entry should be %v for test %v", test.expectedLog, test.name)
		})
	}
}

// resetOnce is a helper function to reset the once variable for testing purposes
func resetOnce() {
	once = sync.Once{}
}

func TestGetLogger(t *testing.T) {
	// Ensure singletonLog is nil before the test
	singletonLog = nil
	resetOnce()

	// Call GetLogger and check if it initializes the logger
	logger := GetLogger()
	assert.NotNil(t, logger, "Logger should not be nil")
	assert.Equal(t, logrus.InfoLevel, logger.Level, "Logger level should be Info")
	assert.True(t, logger.ReportCaller, "Logger should report caller")

	// Call GetLogger again and check if it returns the same instance
	logger2 := GetLogger()
	assert.Equal(t, logger, logger2, "GetLogger should return the same instance")

	// Check if the formatter is set correctly
	formatter, ok := logger.Formatter.(*Formatter)
	assert.True(t, ok, "Logger formatter should be of type *Formatter")
	assert.NotNil(t, formatter.CallerPrettyfier, "CallerPrettyfier should not be nil")

	// Test the CallerPrettyfier function
	frame := &runtime.Frame{
		Function: "TestFunction",
		File:     "dell/csi-unity/testfile.go",
		Line:     42,
	}
	function, file := formatter.CallerPrettyfier(frame)
	assert.Equal(t, "TestFunction()", function, "Function name should be formatted correctly")
	assert.Equal(t, "dell/csi-unity/testfile.go:42", file, "File name should be formatted correctly")

	frame.File = "dell/gounity/testfile.go"
	function, file = formatter.CallerPrettyfier(frame)
	assert.Equal(t, "TestFunction()", function, "Function name should be formatted correctly")
	assert.Equal(t, "dell/gounity/testfile.go:42", file, "File name should be formatted correctly")

	frame.File = "otherpath/testfile.go"
	function, file = formatter.CallerPrettyfier(frame)
	assert.Equal(t, "TestFunction()", function, "Function name should be formatted correctly")
	assert.Equal(t, "otherpath/testfile.go:42", file, "File name should be formatted correctly")
}

func TestFormatter_Format(t *testing.T) {
	formatter := &Formatter{
		LogFormat:       "%time% [%lvl%] %msg% %runid% %arrayid% %intField% %boolField%",
		TimestampFormat: "2006-01-02 15:04:05",
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("%s:%d", f.File, f.Line)
		},
	}

	entry := &logrus.Entry{
		Time:    time.Date(2025, time.February, 27, 15, 0, 0, 0, time.UTC),
		Level:   logrus.InfoLevel,
		Message: "Test message",
		Data: logrus.Fields{
			RUNID:       "1234",
			ARRAYID:     "5678",
			"intField":  42,
			"boolField": true,
		},
		Caller: &runtime.Frame{
			Function: "main.TestFunction",
			File:     "main.go",
			Line:     42,
		},
	}
	entry.Logger = logrus.New()
	entry.Logger.SetReportCaller(true)

	expectedOutput := "2025-02-27 15:00:00 [info] Test message runid=1234 arrayid=5678 42 true func=\"main.TestFunction()\" file=\"main.go:42\"\n"
	output, err := formatter.Format(entry)
	assert.NoError(t, err, "Formatter should not return an error")
	assert.Equal(t, expectedOutput, string(output), "Formatted output should match expected output")
}
