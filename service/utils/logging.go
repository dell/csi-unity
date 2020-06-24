package utils

import (
	"context"
	"fmt"
	"github.com/rexray/gocsi"
	"github.com/sirupsen/logrus"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var singletonLog *logrus.Logger
var once sync.Once

const (
	// Default log format will output [INFO]: 2006-01-02T15:04:05Z07:00 - Log message
	defaultLogFormat       = "time=\"%time%\" level=%lvl% %arrayid% %runid% msg=\"%msg%\""
	defaultTimestampFormat = time.RFC3339
)

// Formatter implements logrus.Formatter interface.
type Formatter struct {
	//logrus.TextFormatter
	// Timestamp format
	TimestampFormat string
	// Available standard keys: time, msg, lvl
	// Also can include custom fields but limited to strings.
	// All of fields need to be wrapped inside %% i.e %time% %msg%
	LogFormat string

	CallerPrettyfier func(*runtime.Frame) (function string, file string)
}

// Format building log message.
func (f *Formatter) Format(entry *logrus.Entry) ([]byte, error) {
	output := f.LogFormat
	if output == "" {
		output = defaultLogFormat
	}

	timestampFormat := f.TimestampFormat
	if timestampFormat == "" {
		timestampFormat = defaultTimestampFormat
	}

	output = strings.Replace(output, "%time%", entry.Time.Format(timestampFormat), 1)
	output = strings.Replace(output, "%msg%", entry.Message, 1)
	level := strings.ToUpper(entry.Level.String())
	output = strings.Replace(output, "%lvl%", strings.ToLower(level), 1)

	fields := entry.Data
	x, b := fields[RUNID]
	if b {
		output = strings.Replace(output, "%runid%", fmt.Sprintf("runid=%v", x), 1)
	} else {
		output = strings.Replace(output, "%runid%", "", 1)
	}
	x, b = fields[ARRAYID]

	if b {
		output = strings.Replace(output, "%arrayid%", fmt.Sprintf("arrayid=%v", x), 1)
	} else {
		output = strings.Replace(output, "%arrayid%", "", 1)
	}

	for k, val := range entry.Data {
		switch v := val.(type) {
		case string:
			output = strings.Replace(output, "%"+k+"%", v, 1)
		case int:
			s := strconv.Itoa(v)
			output = strings.Replace(output, "%"+k+"%", s, 1)
		case bool:
			s := strconv.FormatBool(v)
			output = strings.Replace(output, "%"+k+"%", s, 1)
		}
	}

	var funcVal, fileVal string
	if entry.HasCaller() {
		if f.CallerPrettyfier != nil {
			funcVal, fileVal = f.CallerPrettyfier(entry.Caller)
		} else {
			funcVal = entry.Caller.Function
			fileVal = fmt.Sprintf("%s:%d", entry.Caller.File, entry.Caller.Line)
		}

		if funcVal != "" {
			output = fmt.Sprintf("%s func=\"%s\"", output, funcVal)
		}
		if fileVal != "" {
			output = fmt.Sprintf("%s file=\"%s\"", output, fileVal)
		}
	}

	output = fmt.Sprintf("%s\n", output)

	return []byte(output), nil
}

func GetLogger() *logrus.Logger {
	once.Do(func() {
		singletonLog = logrus.New()
		fmt.Println("csi-unity logger initiated. This should be called only once.")
		var debug bool
		debugStr := os.Getenv(gocsi.EnvVarDebug)
		debug, _ = strconv.ParseBool(debugStr)
		if debug {
			singletonLog.Level = logrus.DebugLevel
			singletonLog.SetReportCaller(true)
			singletonLog.Formatter = &Formatter{
				CallerPrettyfier: func(f *runtime.Frame) (string, string) {
					filename1 := strings.Split(f.File, "dell/csi-unity")
					if len(filename1) > 1 {
						return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("dell/csi-unity%s:%d", filename1[1], f.Line)
					}

					filename2 := strings.Split(f.File, "dell/gounity")
					if len(filename2) > 1 {
						return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("dell/gounity%s:%d", filename2[1], f.Line)
					}

					return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("%s:%d", f.File, f.Line)
				},
			}
		} else {
			singletonLog.Formatter = &Formatter{}
		}
	})

	return singletonLog
}

const (
	UnityLogger = "unitylog"
	LogFields   = "fields"
	RUNID       = "runid"
	ARRAYID     = "arrayid"
)

func GetRunidLogger(ctx context.Context) *logrus.Entry {
	tempLog := ctx.Value(UnityLogger)
	if ctx.Value(UnityLogger) != nil && reflect.TypeOf(tempLog) == reflect.TypeOf(&logrus.Entry{}) {
		return ctx.Value(UnityLogger).(*logrus.Entry)
	}
	return nil
}

func GetRunidAndLogger(ctx context.Context) (string, *logrus.Entry) {
	rid := ""
	fields, ok := ctx.Value(LogFields).(logrus.Fields)
	if ok && fields != nil && reflect.TypeOf(fields) == reflect.TypeOf(logrus.Fields{}) {
		if fields[RUNID] != nil {
			rid = fields[RUNID].(string)
		}
	}

	tempLog := ctx.Value(UnityLogger)
	if tempLog != nil && reflect.TypeOf(tempLog) == reflect.TypeOf(&logrus.Entry{}) {
		//rid = fmt.Sprintf("%s", tempLog.(*logrus.Logger).Data[RUNID])
		return rid, tempLog.(*logrus.Entry)
	}
	return rid, nil
}
