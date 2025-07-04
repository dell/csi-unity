/*
 Copyright © 2019-2025 Dell Inc. or its subsidiaries. All Rights Reserved.

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

package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dell/dell-csi-extensions/podmon"
	"google.golang.org/grpc"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/container-storage-interface/spec/lib/go/csi"
	constants "github.com/dell/csi-unity/common"
	"github.com/dell/csi-unity/core"
	"github.com/dell/csi-unity/k8sutils"
	"github.com/dell/csi-unity/service/csiutils"
	"github.com/dell/csi-unity/service/logging"
	"github.com/dell/gobrick"
	"github.com/dell/gocsi"
	csictx "github.com/dell/gocsi/context"
	"github.com/dell/goiscsi"
	"github.com/dell/gounity"
	"github.com/dell/gounity/util"
	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"
)

const (
	// Name is the name of the Unity CSI.

	// VendorVersion is the version of this Unity CSI.
	VendorVersion = "0.0.0"

	// TCPDialTimeout - Tcp dial default timeout in Milliseconds
	TCPDialTimeout = 1000

	// IScsiPort - iSCSI port used
	IScsiPort = "3260"
)

// Name - name of the service
var Name string

// DriverConfig - Driver config
var DriverConfig string

// DriverSecret - Driver secret
var DriverSecret string

// To maintain runid for Non debug mode. Note: CSI will not generate runid if CSI_DEBUG=false
var runid int64

// Manifest is the SP's manifest.
var Manifest = map[string]string{
	"url":    "http://github.com/dell/csi-unity",
	"semver": core.SemVer,
	"commit": core.CommitSha32,
	"formed": core.CommitTime.Format(time.RFC1123),
}

// StorageArrayList - To parse the secret yaml file
type StorageArrayList struct {
	StorageArrayList []StorageArrayConfig `yaml:"storageArrayList"`
}

// StorageArrayConfig - Storage array configuration
type StorageArrayConfig struct {
	ArrayID                   string `yaml:"arrayId"`
	Username                  string `yaml:"username"`
	Password                  string `yaml:"password"`
	Endpoint                  string `yaml:"endpoint"`
	IsDefault                 *bool  `yaml:"isDefault,omitempty"`
	SkipCertificateValidation *bool  `yaml:"skipCertificateValidation,omitempty"`
	IsProbeSuccess            bool
	IsHostAdded               bool
	IsHostAdditionFailed      bool
	IsDefaultArray            bool
	UnityClient               gounity.UnityClient
	mu                        sync.Mutex // Mutex to protect shared variables
}

// Service is a CSI SP and idempotency.Provider.
type Service interface {
	csi.ControllerServer
	csi.IdentityServer
	csi.NodeServer
	BeforeServe(context.Context, *gocsi.StoragePlugin, net.Listener) error
	RegisterAdditionalServers(*grpc.Server)
}

// Opts defines service configuration options.
type Opts struct {
	NodeName                      string
	LongNodeName                  string
	Chroot                        string
	Thick                         bool
	AutoProbe                     bool
	AllowRWOMultiPodAccess        bool
	PvtMountDir                   string
	Debug                         bool
	SyncNodeInfoTimeInterval      int64
	EnvEphemeralStagingTargetPath string
	KubeConfigPath                string
	MaxVolumesPerNode             int64
	LogLevel                      string
	TenantName                    string
	IsVolumeHealthMonitorEnabled  bool
	allowedNetworks               []string
}

type service struct {
	opts           Opts
	arrays         *sync.Map
	mode           string
	iscsiClient    goiscsi.ISCSIinterface
	fcConnector    fcConnector // gobrick connectors
	iscsiConnector iSCSIConnector
}

type iSCSIConnector interface {
	ConnectVolume(ctx context.Context, info gobrick.ISCSIVolumeInfo) (gobrick.Device, error)
	DisconnectVolumeByDeviceName(ctx context.Context, name string) error
	GetInitiatorName(ctx context.Context) ([]string, error)
}

type fcConnector interface {
	ConnectVolume(ctx context.Context, info gobrick.FCVolumeInfo) (gobrick.Device, error)
	DisconnectVolumeByDeviceName(ctx context.Context, name string) error
	GetInitiatorPorts(ctx context.Context) ([]string, error)
}

// New returns a new CSI Service.
func New() Service {
	return &service{}
}

// To display the StorageArrayConfig content
func (s *StorageArrayConfig) String() string {
	return fmt.Sprintf("ArrayID: %s, Username: %s, Endpoint: %s, SkipCertificateValidation: %v, IsDefaultArray:%v, IsProbeSuccess:%v, IsHostAdded:%v",
		s.ArrayID, s.Username, s.Endpoint, s.SkipCertificateValidation, s.IsDefaultArray, s.IsProbeSuccess, s.IsHostAdded)
}

// BeforeServe allows the SP to participate in the startup
// sequence. This function is invoked directly before the
// gRPC server is created, giving the callback the ability to
// modify the SP's interceptors, server options, or prevent the
// server from starting by returning a non-nil error.
func (s *service) BeforeServe(
	ctx context.Context, _ *gocsi.StoragePlugin, _ net.Listener,
) error {
	ctx, log := setRunIDContext(ctx, "start")
	var err error
	defer func() {
		fields := map[string]interface{}{
			"nodename":  s.opts.NodeName,
			"autoprobe": s.opts.AutoProbe,
			"mode":      s.mode,
		}
		log.WithFields(fields).Infof("configured %s", Name)
	}()

	// Get the SP's operating mode.
	s.mode = csictx.Getenv(ctx, gocsi.EnvVarMode)
	log.Info("Driver Mode:", s.mode)

	opts := Opts{}
	if name, ok := csictx.LookupEnv(ctx, gocsi.EnvVarDebug); ok {
		opts.Debug, _ = strconv.ParseBool(name)
	}

	if name, ok := csictx.LookupEnv(ctx, EnvNodeName); ok {
		log.Infof("%s: %s", EnvNodeName, name)
		opts.LongNodeName = name
		var shortHostName string
		// check its ip or not
		ipFormat := regexp.MustCompile(`(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)){3}`)
		if ipFormat.MatchString(name) {
			shortHostName = name
		} else {
			shortHostName = strings.Split(name, ".")[0]
		}
		opts.NodeName = shortHostName
	}

	if kubeConfigPath, ok := csictx.LookupEnv(ctx, EnvKubeConfigPath); ok {
		opts.KubeConfigPath = kubeConfigPath
	}

	// pb parses an environment variable into a boolean value. If an error
	// is encountered, default is set to false, and error is logged
	pb := func(n string) bool {
		if v, ok := csictx.LookupEnv(ctx, n); ok {
			b, err := strconv.ParseBool(v)
			if err != nil {
				log.WithField(n, v).Warn(
					"invalid boolean value. defaulting to false")
				return false
			}
			return b
		}
		return false
	}

	opts.MaxVolumesPerNode = 0 // With 0 there is no limit on Volumes per node

	if syncNodeInfoTimeInterval, ok := csictx.LookupEnv(ctx, SyncNodeInfoTimeInterval); ok {
		opts.SyncNodeInfoTimeInterval, err = strconv.ParseInt(syncNodeInfoTimeInterval, 10, 64)
		log.Debugf("SyncNodeInfoTimeInterval %d", opts.SyncNodeInfoTimeInterval)
		if err != nil {
			log.Warn("Failed to parse syncNodeInfoTimeInterval provided by user, hence reverting to default value")
			opts.SyncNodeInfoTimeInterval = 15
		}
	}

	opts.AllowRWOMultiPodAccess = pb(EnvAllowRWOMultiPodAccess)
	if opts.AllowRWOMultiPodAccess {
		log.Warn("AllowRWOMultiPodAccess has been set to true. PVCs will now be accessible by multiple pods on the same node.")
	}

	opts.AutoProbe = pb(EnvAutoProbe)
	if volumeHealthMonitor, ok := csictx.LookupEnv(ctx, EnvIsVolumeHealthMonitorEnabled); ok {
		if volumeHealthMonitor == "true" {
			opts.IsVolumeHealthMonitorEnabled = true
		}
	} else {
		opts.IsVolumeHealthMonitorEnabled = false
	}

	// Global mount directory will be used to node unstage volumes mounted via CSI-Unity v1.0 or v1.1
	if pvtmountDir, ok := csictx.LookupEnv(ctx, EnvPvtMountDir); ok {
		opts.PvtMountDir = pvtmountDir
	}

	if ephemeralStagePath, ok := csictx.LookupEnv(ctx, EnvEphemeralStagingPath); ok {
		opts.EnvEphemeralStagingTargetPath = ephemeralStagePath
	}

	var networksList []string
	if allNetworks, ok := csictx.LookupEnv(ctx, EnvAllowedNetworks); ok {
		allNetworks = strings.TrimSpace(allNetworks)
		if allNetworks == "" {
			networksList = []string{}
		} else {
			networksList = strings.Split(allNetworks, ",")
		}
	}
	opts.allowedNetworks = networksList

	// setup the iscsi client
	iscsiOpts := make(map[string]string, 0)
	if chroot, ok := csictx.LookupEnv(ctx, EnvISCSIChroot); ok {
		iscsiOpts[goiscsi.ChrootDirectory] = chroot
		opts.Chroot = chroot
	}
	s.iscsiClient = goiscsi.NewLinuxISCSI(iscsiOpts)

	s.opts = opts
	// Update the storage array list
	runid := fmt.Sprintf("config-%d", 0)
	ctx, log = setRunIDContext(ctx, runid)
	s.arrays = new(sync.Map)
	err = s.syncDriverSecret(ctx)
	if err != nil {
		return err
	}
	syncNodeInfoChan = make(chan bool)
	// Dynamically load the config
	go s.loadDynamicConfig(ctx, DriverSecret, DriverConfig)

	// Add node information to hosts
	if s.mode == "node" {
		// Get Host Name
		if s.opts.NodeName == "" {
			return status.Error(codes.InvalidArgument, "'Node Name' has not been configured. Set environment variable X_CSI_UNITY_NODENAME")
		}

		go s.syncNodeInfoRoutine(ctx)
		syncNodeInfoChan <- true
	}

	return nil
}

// RegisterAdditionalServers registers any additional grpc services that use the CSI socket.
func (s *service) RegisterAdditionalServers(server *grpc.Server) {
	_, log := setRunIDContext(context.Background(), "RegisterAdditionalServers")
	log.Info("Registering additional GRPC servers")
	podmon.RegisterPodmonServer(server, s)
}

// Get storage array from sync Map
func (s *service) getStorageArray(arrayID string) *StorageArrayConfig {
	if a, ok := s.arrays.Load(arrayID); ok {
		return a.(*StorageArrayConfig)
	}
	return nil
}

// Returns the size of arrays
func (s *service) getStorageArrayLength() (length int) {
	length = 0
	s.arrays.Range(func(_, _ interface{}) bool {
		length++
		return true
	})
	return
}

// Get storage array list from sync Map
func (s *service) getStorageArrayList() []*StorageArrayConfig {
	list := make([]*StorageArrayConfig, 0)
	s.arrays.Range(func(_ interface{}, value interface{}) bool {
		list = append(list, value.(*StorageArrayConfig))
		return true
	})
	return list
}

// To get the UnityClient for a specific array
func (s *service) getUnityClient(ctx context.Context, arrayID string) (gounity.UnityClient, error) {
	_, _, rid := GetRunidLog(ctx)
	if s.getStorageArrayLength() == 0 {
		return nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "Invalid driver csi-driver configuration provided. At least one array should present or invalid yaml format. "))
	}

	array := s.getStorageArray(arrayID)
	if array != nil && array.UnityClient != nil {
		return array.UnityClient, nil
	}
	return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Unity client not found for array %s", arrayID))
}

// return volumeid from csi volume context
func getVolumeIDFromVolumeContext(contextVolID string) string {
	if contextVolID == "" {
		return ""
	}
	tokens := strings.Split(contextVolID, "-")
	if len(tokens) == 1 {
		// Only one token found, which means volume created using csi-unity v1.0 and v1.1
		return tokens[0]
	} else if len(tokens) >= 4 {
		return tokens[len(tokens)-1]
	}
	return ""
}

// @Below method is unused. So remove.
func (s *service) getArrayIDFromVolumeContext(contextVolID string) (string, error) {
	if contextVolID == "" {
		return "", errors.New("volume context id should not be empty ")
	}
	tokens := strings.Split(contextVolID, "-")
	if len(tokens) == 1 {
		// Only one token found, which means volume created using csi-unity v1.0 and v1.1. So return default array
		for _, array := range s.getStorageArrayList() {
			if array.IsDefaultArray {
				return array.ArrayID, nil
			}
		}
		return "", errors.New("no default array found in the csi-unity driver configuration")
	} else if len(tokens) >= 4 {
		return tokens[len(tokens)-2], nil
	}
	return "", errors.New("invalid volume context id or no default array found in the csi-unity driver configuration")
}

var watcher *fsnotify.Watcher

func (s *service) loadDynamicConfig(ctx context.Context, secretFile, configFile string) error {
	i := 1
	runid := fmt.Sprintf("config-%d", i)
	ctx, log := setRunIDContext(ctx, runid)

	log.Info("Dynamic config load goroutine invoked")

	// Dynamic update of config
	vc := viper.New()
	vc.AutomaticEnv()
	vc.SetConfigFile(configFile)
	if err := vc.ReadInConfig(); err != nil {
		log.Warnf("unable to read driver config params from file '%s'. Using defaults.", configFile)
	}

	s.syncDriverConfig(ctx, vc)

	// Watch for changes to driver config params file
	vc.WatchConfig()
	vc.OnConfigChange(func(_ fsnotify.Event) {
		log.Infof("Driver config params file changed")
		s.syncDriverConfig(ctx, vc)
	})

	// Dynamic update of secret
	watcher, _ := fsnotify.NewWatcher()
	defer watcher.Close()

	parentFolder, _ := filepath.Abs(filepath.Dir(secretFile))
	log.Debug("Secret folder:", parentFolder)
	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Create == fsnotify.Create && event.Name == parentFolder+"/..data" {
					log.Warnf("****************Driver secret file modified. Loading the secret file:%s****************", event.Name)
					err := s.syncDriverSecret(ctx)
					if err != nil {
						log.Debug("Driver configuration array length:", s.getStorageArrayLength())
						log.Error("Invalid configuration in secret.yaml. Error:", err)
						// return
					}
					if s.mode == "node" {
						syncNodeInfoChan <- true
					}
					i++
				}
				runid = fmt.Sprintf("config-%d", i)
				ctx, log = setRunIDContext(ctx, runid)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error("Driver secret load error:", err)
			}
		}
	}()
	err := watcher.Add(parentFolder)
	if err != nil {
		log.Error("Unable to add file watcher for folder ", parentFolder)
		return err
	}
	<-done
	return nil
}

// return protocol from csi volume context
func (s *service) getProtocolFromVolumeContext(contextVolID string) (string, error) {
	if contextVolID == "" {
		return "", errors.New("volume context id should not be empty ")
	}
	tokens := strings.Split(contextVolID, "-")
	if len(tokens) == 1 {
		// Only one token found, which means volume created using csi-unity v1.0 and v1.1. So return Unknown protocol
		return ProtocolUnknown, nil
	} else if len(tokens) >= 4 {
		return tokens[len(tokens)-3], nil
	}
	return "", errors.New("invalid volume context id")
}

var (
	syncMutex sync.Mutex
	readFile  = os.ReadFile
)

// syncDriverSecret - Reads credentials from secrets and initialize all arrays.
func (s *service) syncDriverSecret(ctx context.Context) error {
	ctx, log, _ := GetRunidLog(ctx)
	log.Info("*************Synchronizing driver secret**************")
	syncMutex.Lock()
	defer syncMutex.Unlock()
	s.arrays.Range(func(key interface{}, _ interface{}) bool {
		s.arrays.Delete(key)
		return true
	})
	secretBytes, err := readFile(filepath.Clean(DriverSecret))
	if err != nil {
		return fmt.Errorf("File ('%s') error: %v", DriverSecret, err)
	}

	if string(secretBytes) != "" {

		log.Debugf("Trying to parse DriverSecret %s as yaml", DriverSecret)
		secretConfig := new(StorageArrayList)
		yamlErr := yaml.Unmarshal(secretBytes, secretConfig)
		if yamlErr != nil {
			log.Errorf("Couldnt parse DriverSecret %s  as yaml", DriverSecret)
			return fmt.Errorf("Unable to parse the DriverSecret as yaml [%v]", yamlErr)
		}

		if len(secretConfig.StorageArrayList) == 0 {
			return errors.New("Arrays details are not provided in unity-creds secret")
		}

		s.arrays.Range(func(key interface{}, _ interface{}) bool {
			s.arrays.Delete(key)
			return true
		})

		var noOfDefaultArrays int
		for i := range secretConfig.StorageArrayList {
			secret := &secretConfig.StorageArrayList[i] // Get a pointer to avoid copying the struct
			if secret.ArrayID == "" {
				return fmt.Errorf("invalid value for ArrayID at index [%d]", i)
			}
			if secret.Username == "" {
				return fmt.Errorf("invalid value for Username at index [%d]", i)
			}
			if secret.Password == "" {
				return fmt.Errorf("invalid value for Password at index [%d]", i)
			}
			if secret.SkipCertificateValidation == nil {
				return fmt.Errorf("invalid value for SkipCertificateValidation at index [%d]", i)
			}
			if secret.Endpoint == "" {
				return fmt.Errorf("invalid value for Endpoint at index [%d]", i)
			}
			if secret.IsDefault == nil {
				return fmt.Errorf("invalid value for IsDefault at index [%d]", i)
			}

			endpoint := secret.Endpoint
			insecure := *secret.SkipCertificateValidation
			secret.IsDefaultArray = *secret.IsDefault
			secret.ArrayID = strings.ToLower(secret.ArrayID)

			unityClient, err := gounity.NewClientWithArgs(ctx, endpoint, insecure)
			if err != nil {
				return fmt.Errorf("unable to initialize the Unity client [%v]", err)
			}
			err = unityClient.Authenticate(ctx, &gounity.ConfigConnect{
				Endpoint: endpoint,
				Username: secret.Username,
				Password: secret.Password,
				Insecure: insecure,
			})
			if err != nil {
				log.Errorf("unable to authenticate [%v]", err)
			}
			secret.UnityClient = unityClient

			copyStorage := StorageArrayConfig{
				ArrayID:                   secret.ArrayID,
				Username:                  secret.Username,
				Password:                  secret.Password,
				Endpoint:                  secret.Endpoint,
				IsDefault:                 secret.IsDefault,
				SkipCertificateValidation: secret.SkipCertificateValidation,
				IsProbeSuccess:            secret.IsProbeSuccess,
				IsHostAdded:               secret.IsHostAdded,
				IsHostAdditionFailed:      secret.IsHostAdditionFailed,
				IsDefaultArray:            secret.IsDefaultArray,
				UnityClient:               secret.UnityClient,
			}

			_, ok := s.arrays.Load(secret.ArrayID)

			if ok {
				return fmt.Errorf("Duplicate ArrayID [%s] found in storageArrayList parameter", secret.ArrayID)
			}
			s.arrays.Store(secret.ArrayID, &copyStorage)

			fields := logrus.Fields{
				"ArrayId":                   secret.ArrayID,
				"username":                  secret.Username,
				"password":                  "*******",
				"Endpoint":                  endpoint,
				"SkipCertificateValidation": insecure,
				"IsDefault":                 *secret.IsDefault,
			}
			logrus.WithFields(fields).Infof("configured %s", Name)

			if secret.IsDefaultArray {
				noOfDefaultArrays++
			}

			if noOfDefaultArrays > 1 {
				return fmt.Errorf("'isDefault' parameter is true in multiple places ArrayId: %s. 'isDefaultArray' parameter should present only once in the storageArrayList", secret.ArrayID)
			}
		}
	} else {
		return errors.New("Arrays details are not provided in unity-creds secret")
	}

	return nil
}

// syncDriverSecret - Reads config map and update configuration parameters
func (s *service) syncDriverConfig(ctx context.Context, v *viper.Viper) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Info("*************Synchronizing driver config**************")

	if v.IsSet(constants.ParamCSILogLevel) {
		inputLogLevel := v.GetString(constants.ParamCSILogLevel)

		if inputLogLevel == "" {
			// setting default log level to Info if input is invalid
			s.opts.LogLevel = "Info"
		}

		if s.opts.LogLevel != inputLogLevel {
			s.opts.LogLevel = inputLogLevel
			logging.ChangeLogLevel(s.opts.LogLevel)
			// Change log level on gounity
			util.ChangeLogLevel(s.opts.LogLevel)
			// Change log level on gocsi
			// set X_CSI_LOG_LEVEL so that gocsi doesn't overwrite the loglevel set by us
			_ = os.Setenv(gocsi.EnvVarLogLevel, s.opts.LogLevel)
			log.Warnf("Log level changed to: %s", s.opts.LogLevel)
		}
	}

	if v.IsSet(constants.ParamAllowRWOMultiPodAccess) {
		s.opts.AllowRWOMultiPodAccess = v.GetBool(constants.ParamAllowRWOMultiPodAccess)
		if s.opts.AllowRWOMultiPodAccess {
			log.Warn("AllowRWOMultiPodAccess has been set to true. PVCs will now be accessible by multiple pods on the same node.")
		}
	}

	if v.IsSet(constants.ParamMaxUnityVolumesPerNode) {
		s.opts.MaxVolumesPerNode = v.GetInt64(constants.ParamMaxUnityVolumesPerNode)
	}

	if v.IsSet(constants.ParamTenantName) {
		s.opts.TenantName = v.GetString(constants.ParamTenantName)
	}

	if v.IsSet(constants.ParamSyncNodeInfoTimeInterval) {
		s.opts.SyncNodeInfoTimeInterval = v.GetInt64(constants.ParamSyncNodeInfoTimeInterval)
	}
}

// Set arraysId in log messages and re-initialize the context
func setArrayIDContext(ctx context.Context, arrayID string) (context.Context, *logrus.Entry) {
	return setLogFieldsInContext(ctx, arrayID, logging.ARRAYID)
}

// Set arraysId in log messages and re-initialize the context
func setRunIDContext(ctx context.Context, runID string) (context.Context, *logrus.Entry) {
	return setLogFieldsInContext(ctx, runID, logging.RUNID)
}

var logMutex sync.Mutex

// Common method to get log and context
func setLogFieldsInContext(ctx context.Context, logID string, logType string) (context.Context, *logrus.Entry) {
	logMutex.Lock()
	defer logMutex.Unlock()

	fields := logrus.Fields{}
	fields, ok := ctx.Value(logging.LogFields).(logrus.Fields)
	if !ok {
		fields = logrus.Fields{}
	}
	if fields == nil {
		fields = logrus.Fields{}
	}
	fields[logType] = logID
	ulog, ok := ctx.Value(logging.UnityLogger).(*logrus.Entry)
	if !ok {
		ulog = logging.GetLogger().WithFields(fields)
	}
	ulog = ulog.WithFields(fields)
	ctx = context.WithValue(ctx, logging.UnityLogger, ulog)
	ctx = context.WithValue(ctx, logging.LogFields, fields)
	return ctx, ulog
}

var (
	syncNodeLogCount   int32
	syncConfigLogCount int32
)

// Increment run id log
func incrementLogID(ctx context.Context, runidPrefix string) (context.Context, *logrus.Entry) {
	if runidPrefix == "node" {
		newID := atomic.AddInt32(&syncNodeLogCount, 1) - 1
		runid := fmt.Sprintf("%s-%d", runidPrefix, newID)
		return setRunIDContext(ctx, runid)
	} else if runidPrefix == "config" {
		newID := atomic.AddInt32(&syncConfigLogCount, 1) - 1
		runid := fmt.Sprintf("%s-%d", runidPrefix, newID)
		return setRunIDContext(ctx, runid)
	}
	return nil, nil
}

// GetRunidLog - Get run id of log
func GetRunidLog(ctx context.Context) (context.Context, *logrus.Entry, string) {
	var rid string
	fields := logrus.Fields{}
	if ctx == nil {
		return ctx, logging.GetLogger().WithFields(fields), rid
	}

	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		reqid, ok := headers[csictx.RequestIDKey]
		if ok && len(reqid) > 0 {
			rid = reqid[0]
		} else {
			atomic.AddInt64(&runid, 1)
			rid = fmt.Sprintf("%d", runid)
		}
	}

	fields, _ = ctx.Value(logging.LogFields).(logrus.Fields)
	if fields == nil {
		fields = logrus.Fields{}
	}

	if ok {
		fields[logging.RUNID] = rid
	}

	logMutex.Lock()
	defer logMutex.Unlock()
	l := logging.GetLogger()
	log := l.WithFields(fields)
	ctx = context.WithValue(ctx, logging.UnityLogger, log)
	ctx = context.WithValue(ctx, logging.LogFields, fields)
	return ctx, log, rid
}

func getLogFields(ctx context.Context) logrus.Fields {
	fields := logrus.Fields{}
	if ctx == nil {
		return fields
	}
	fields, ok := ctx.Value(logging.LogFields).(logrus.Fields)
	if !ok {
		fields = logrus.Fields{}
	}

	csiReqID, ok := ctx.Value(csictx.RequestIDKey).(string)
	if !ok {
		return fields
	}
	fields[logging.RUNID] = csiReqID
	return fields
}

func (s *service) initISCSIConnector(chroot string) {
	if s.iscsiConnector == nil {
		setupGobrick(s)
		s.iscsiConnector = gobrick.NewISCSIConnector(
			gobrick.ISCSIConnectorParams{Chroot: chroot})
	}
}

func (s *service) initFCConnector(chroot string) {
	if s.fcConnector == nil {
		setupGobrick(s)
		s.fcConnector = gobrick.NewFCConnector(
			gobrick.FCConnectorParams{Chroot: chroot})
	}
}

func setupGobrick(_ *service) {
	gobrick.SetLogger(&customLogger{})
	gobrick.SetTracer(&emptyTracer{})
}

type emptyTracer struct{}

func (dl *emptyTracer) Trace(_ context.Context, _ string, _ ...interface{}) {
}

type customLogger struct{}

func (lg *customLogger) Info(ctx context.Context, format string, args ...interface{}) {
	log := logging.GetLogger()
	log.WithFields(getLogFields(ctx)).Infof(format, args...)
}

func (lg *customLogger) Debug(ctx context.Context, format string, args ...interface{}) {
	log := logging.GetLogger()
	log.WithFields(getLogFields(ctx)).Debugf(format, args...)
}

func (lg *customLogger) Error(ctx context.Context, format string, args ...interface{}) {
	log := logging.GetLogger()
	log.WithFields(getLogFields(ctx)).Errorf(format, args...)
}

func (s *service) requireProbe(ctx context.Context, arrayID string) error {
	rid, log := logging.GetRunidAndLogger(ctx)
	if !s.opts.AutoProbe {
		return status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Controller Service has not been probed"))
	}
	log.Debug("Probing controller service automatically")
	if err := s.controllerProbe(ctx, arrayID); err != nil {
		return status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "failed to probe/init plugin: %s", err.Error()))
	}
	return nil
}

func singleArrayProbe(ctx context.Context, probeType string, array *StorageArrayConfig) error {
	rid, log := logging.GetRunidAndLogger(ctx)
	ctx, log = setArrayIDContext(ctx, array.ArrayID)

	err := array.UnityClient.BasicSystemInfo(ctx, &gounity.ConfigConnect{
		Endpoint: array.Endpoint,
		Username: array.Username,
		Password: array.Password,
		Insecure: *array.SkipCertificateValidation,
	})
	if err != nil {
		log.Errorf("Unity probe failed for array %s error: %v", array.ArrayID, err)
		if e, ok := status.FromError(err); ok {
			if e.Code() == codes.Unauthenticated {
				array.mu.Lock()
				array.IsProbeSuccess = false
				array.mu.Unlock()
				return status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Unable Get basic system info from Unity. Error: %s", err.Error()))
			}
		}
		array.mu.Lock()
		array.IsProbeSuccess = false
		array.mu.Unlock()
		return status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "Unable Get basic system info from Unity. Verify hostname/IP Address of unity. Error: %s", err.Error()))
	}

	array.mu.Lock()
	array.IsProbeSuccess = true
	array.mu.Unlock()
	log.Debugf("%s Probe Success", probeType)
	return nil
}

func (s *service) probe(ctx context.Context, probeType string, arrayID string) error {
	rid, log := logging.GetRunidAndLogger(ctx)
	log.Debugf("Inside %s Probe", probeType)
	if arrayID != "" {
		if array := s.getStorageArray(arrayID); array != nil {
			return singleArrayProbe(ctx, probeType, array)
		}
	} else {
		log.Debug("Probing all arrays")
		atleastOneArraySuccess := false
		for _, array := range s.getStorageArrayList() {
			err := singleArrayProbe(ctx, probeType, array)
			if err == nil {
				atleastOneArraySuccess = true
				break
			}
			log.Errorf("Probe failed for array %s error:%v", array, err)

		}

		if !atleastOneArraySuccess {
			return status.Error(codes.FailedPrecondition, csiutils.GetMessageWithRunID(rid, "All unity arrays are not working. Could not proceed further"))
		}
	}
	log.Infof("%s Probe Success", probeType)
	return nil
}

func (s *service) validateAndGetResourceDetails(ctx context.Context, resourceContextID string, resourceType resourceType) (resourceID, protocol, arrayID string, unity gounity.UnityClient, err error) {
	ctx, _, rid := GetRunidLog(ctx)
	if s.getStorageArrayLength() == 0 {
		return "", "", "", nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "Invalid driver csi-driver configuration provided. At least one array should present or invalid yaml format. "))
	}
	resourceID = getVolumeIDFromVolumeContext(resourceContextID)
	if resourceID == "" {
		return "", "", "", nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "%sId can't be empty.", resourceType))
	}
	arrayID, err = s.getArrayIDFromVolumeContext(resourceContextID)
	if err != nil {
		return "", "", "", nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "[%s] [%s] error:[%v]", resourceType, resourceID, err))
	}

	protocol, err = s.getProtocolFromVolumeContext(resourceContextID)
	if err != nil {
		return "", "", "", nil, status.Error(codes.InvalidArgument, csiutils.GetMessageWithRunID(rid, "[%s] [%s] error:[%v]", resourceType, resourceID, err))
	}

	unity, err = s.getUnityClient(ctx, arrayID)
	if err != nil {
		return "", "", "", nil, err
	}
	return
}

func (s *service) GetNodeLabels(ctx context.Context) (map[string]string, error) {
	ctx, log, rid := GetRunidLog(ctx)
	k8sclientset, err := k8sutils.CreateKubeClientSet(s.opts.KubeConfigPath)
	if err != nil {
		return nil, status.Error(codes.Internal, csiutils.GetMessageWithRunID(rid, "init client failed with error: %v", err))
	}
	// access the API to fetch node object
	node, err := k8sclientset.CoreV1().Nodes().Get(context.TODO(), s.opts.LongNodeName, v1.GetOptions{})
	if err != nil {
		return nil, status.Error(codes.Internal, csiutils.GetMessageWithRunID(rid, "Unable to fetch the node labels. Error: %v", err))
	}
	log.Debugf("Node labels: %v\n", node.Labels)
	return node.Labels, nil
}
