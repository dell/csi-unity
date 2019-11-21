/*
Copyright (c) 2019 Dell EMC Corporation
All Rights Reserved
*/
package utils

import (
	"bytes"
	"fmt"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/rexray/gocsi"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/container-storage-interface/spec/lib/go/csi"
	types "github.com/dell/gounity/payloads"
)

//Utility method to convert Unity Rest type Volume to CSI standard Volume Response
func GetVolumeResponseFromVolume(volume *types.Volume) *csi.CreateVolumeResponse {
	content := volume.VolumeContent
	volumeReq := &csi.Volume{
		VolumeId:      content.ResourceId,
		CapacityBytes: int64(content.SizeTotal),
	}

	volumeResp := &csi.CreateVolumeResponse{
		Volume: volumeReq,
	}

	return volumeResp
}

func GetFCInitiators() ([]string, error) {
	log := GetLogger()
	portWWNs := make([]string, 0)
	// Read the directory entries for fc_remote_ports
	fcHostsDir := "/sys/class/fc_host"
	hostEntries, err := ioutil.ReadDir(fcHostsDir)
	if err != nil {
		log.Errorf("Cannot read directory: %s Error: %v", fcHostsDir, err)
		return portWWNs, err
	}

	// Look through the hosts retrieving the port_name
	for _, host := range hostEntries {
		if !strings.HasPrefix(host.Name(), "host") {
			continue
		}
		portPath := fcHostsDir + "/" + host.Name() + "/" + "port_name"
		portName, err := ioutil.ReadFile(portPath)
		if err != nil {
			log.Errorf("Error reading file: %s Error: %v", portPath, err)
			continue
		}
		portNameStr := strings.TrimSpace(string(portName))

		nodePath := fcHostsDir + "/" + host.Name() + "/" + "node_name"
		nodeName, err := ioutil.ReadFile(nodePath)
		if err != nil {
			log.Errorf("Error reading file: %s Error: %v", nodePath, err)
			continue
		}
		nodeNameStr := strings.TrimSpace(string(nodeName))

		log.Debug("portNameStr:", portNameStr)
		log.Debug("nodeNameStr:", nodeNameStr)
		//Ignore first 2 digits
		port := strings.Split(portNameStr, "x")[1]
		node := strings.Split(nodeNameStr, "x")[1]

		portNode := strings.TrimSpace(node + port)
		portNode = strings.Replace(portNode, "\n", "", -1)
		portNode = strings.Replace(portNode, "\r", "", -1)

		var sb strings.Builder
		for pos := range portNode {
			sb.WriteString(string(portNode[pos]))
			if (pos%2 > 0) && (pos != len(portNode)-1) {
				sb.WriteString(":")
			}
		}
		portWWNs = append(portWWNs, strings.ToLower(sb.String()))
	}
	return portWWNs, nil
}

//Utility method to extract Host IP
func GetHostIP() (string, error) {
	cmd := exec.Command("hostname", "-I")
	cmdOutput := &bytes.Buffer{}
	cmd.Stdout = cmdOutput
	err := cmd.Run()
	if err != nil {
		cmd = exec.Command("hostname", "-i")
		cmdOutput = &bytes.Buffer{}
		cmd.Stdout = cmdOutput
		err = cmd.Run()
		if err != nil {
			return "", err
		}
	}

	output := string(cmdOutput.Bytes())
	ip := strings.Split(output, " ")[0]
	return ip, nil
}

//Utility method to convert Unity Rest type Snapshot to CSI standard Snapshot Response
func GetSnapshotResponseFromSnapshot(snap *types.Snapshot) *csi.CreateSnapshotResponse {
	content := snap.SnapshotContent
	var timestamp *timestamp.Timestamp
	if !snap.SnapshotContent.CreationTime.IsZero() {
		timestamp, _ = ptypes.TimestampProto(snap.SnapshotContent.CreationTime)
	}

	snapReq := &csi.Snapshot{
		SizeBytes:      snap.SnapshotContent.Size,
		ReadyToUse:     true,
		SnapshotId:     content.ResourceId,
		SourceVolumeId: content.Lun.Id,
		CreationTime:   timestamp,
	}

	snapResp := &csi.CreateSnapshotResponse{
		Snapshot: snapReq,
	}

	return snapResp
}

var singletonLog *logrus.Logger
var once sync.Once

func GetLogger() *logrus.Logger {
	once.Do(func() {
		singletonLog = logrus.New()
		fmt.Println("csi-unity logger initiated. This should be called only once.")
		var debug bool
		os.Setenv("GOUNITY_DEBUG", "true")
		debugStr := os.Getenv(gocsi.EnvVarDebug)
		debug, _ = strconv.ParseBool(debugStr)
		if debug {
			singletonLog.Level = logrus.DebugLevel
			singletonLog.SetReportCaller(true)
			singletonLog.Formatter = &logrus.TextFormatter{
				CallerPrettyfier: func(f *runtime.Frame) (string, string) {
					filename := strings.Split(f.File, "dell/csi-unity")
					if len(filename) > 1 {
						return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("dell/csi-unity%s:%d", filename[1], f.Line)
					} else {
						return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("%s:%d", f.File, f.Line)
					}
				},
			}
		}
	})

	return singletonLog
}

//ArrayContains method does contains check operation
func ArrayContains(stringArray []string, value string) bool {

	for _, arrayValue := range stringArray {
		if value == arrayValue {
			return true
		}
	}
	return false
}

//ArrayContainsAll method checks if all elements of stringArray1 is present in stringArray2
func ArrayContainsAll(stringArray1 []string, stringArray2 []string) bool {

	for _, arrayElement := range stringArray1 {
		if !ArrayContains(stringArray2, arrayElement) {
			return false
		}
	}
	return true
}

//FindAdditionalWwns returns the set difference stringArray2-stringArray1
func FindAdditionalWwns(stringArray1 []string, stringArray2 []string) []string {
	var differenceSet []string
	for _, element := range stringArray2 {
		if !ArrayContains(stringArray1, element) {
			differenceSet = append(differenceSet, element)
		}
	}
	return differenceSet
}
