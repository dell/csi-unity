/*
Copyright (c) 2019 Dell EMC Corporation
All Rights Reserved
*/
package utils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/gounity/types"
)

//GetVolumeResponseFromVolume Utility method to convert Unity Rest type Volume to CSI standard Volume Response
func GetVolumeResponseFromVolume(volume *types.Volume, arrayId, protocol string, preferredAccessibility []*csi.Topology) *csi.CreateVolumeResponse {
	content := volume.VolumeContent
	return getVolumeResponse(content.Name, protocol, arrayId, content.ResourceId, content.SizeTotal, preferredAccessibility)
}

//GetVolumeResponseFromFilesystem Utility method to convert Unity rest Filesystem response to CSI standard Volume Response
func GetVolumeResponseFromFilesystem(filesystem *types.Filesystem, arrayId, protocol string, preferredAccessibility []*csi.Topology) *csi.CreateVolumeResponse {
	content := filesystem.FileContent
	return getVolumeResponse(content.Name, protocol, arrayId, content.Id, content.SizeTotal, preferredAccessibility)
}

func GetVolumeResponseFromSnapshot(snapshot *types.Snapshot, arrayId, protocol string, preferredAccessibility []*csi.Topology) *csi.CreateVolumeResponse {
	volId := fmt.Sprintf("%s-%s-%s-%s", snapshot.SnapshotContent.Name, protocol, arrayId, snapshot.SnapshotContent.ResourceId)
	VolumeContext := make(map[string]string)
	VolumeContext["protocol"] = protocol
	VolumeContext["arrayId"] = arrayId
	VolumeContext["volumeId"] = snapshot.SnapshotContent.ResourceId

	volumeReq := &csi.Volume{
		VolumeId:           volId,
		CapacityBytes:      int64(snapshot.SnapshotContent.Size),
		VolumeContext:      VolumeContext,
		AccessibleTopology: preferredAccessibility,
	}
	volumeResp := &csi.CreateVolumeResponse{
		Volume: volumeReq,
	}
	return volumeResp
}

func getVolumeResponse(name, protocol, arrayId, resourceId string, size uint64, preferredAccessibility []*csi.Topology) *csi.CreateVolumeResponse {
	volId := fmt.Sprintf("%s-%s-%s-%s", name, protocol, arrayId, resourceId)
	VolumeContext := make(map[string]string)
	VolumeContext["protocol"] = protocol
	VolumeContext["arrayId"] = arrayId
	VolumeContext["volumeId"] = resourceId

	volumeReq := &csi.Volume{
		VolumeId:           volId,
		CapacityBytes:      int64(size),
		VolumeContext:      VolumeContext,
		AccessibleTopology: preferredAccessibility,
	}
	volumeResp := &csi.CreateVolumeResponse{
		Volume: volumeReq,
	}
	return volumeResp
}

func GetMessageWithRunID(runid string, format string, args ...interface{}) string {
	str := fmt.Sprintf(format, args...)
	return fmt.Sprintf(" runid=%s %s", runid, str)
}

func GetFCInitiators(ctx context.Context) ([]string, error) {
	log := GetRunidLogger(ctx)
	portWWNs := make([]string, 0)
	// Read the directory entries for fc_remote_ports
	fcHostsDir := "/sys/class/fc_host"
	hostEntries, err := ioutil.ReadDir(fcHostsDir)
	if err != nil {
		log.Warnf("Cannot read directory: %s : %v", fcHostsDir, err)
		return portWWNs, err
	}

	// Look through the hosts retrieving the port_name
	for _, host := range hostEntries {
		if !strings.HasPrefix(host.Name(), "host") {
			continue
		}
		portPath := fcHostsDir + "/" + host.Name() + "/" + "port_name"
		portName, err := ioutil.ReadFile(filepath.Clean(portPath))
		if err != nil {
			log.Warnf("Error reading file: %s Error: %v", portPath, err)
			continue
		}
		portNameStr := strings.TrimSpace(string(portName))

		nodePath := fcHostsDir + "/" + host.Name() + "/" + "node_name"
		nodeName, err := ioutil.ReadFile(filepath.Clean(nodePath))
		if err != nil {
			log.Warnf("Error reading file: %s Error: %v", nodePath, err)
			continue
		}
		nodeNameStr := strings.TrimSpace(string(nodeName))
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
func GetHostIP() ([]string, error) {
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
			return nil, err
		}
	}
	output := string(cmdOutput.Bytes())
	ips := strings.Split(strings.TrimSpace(output), " ")

	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	var lookup_ips []string
	for _, ip := range ips {
		lookupResp, err := net.LookupAddr(ip)
		if err == nil && strings.Contains(lookupResp[0], hostname) {
			lookup_ips = append(lookup_ips, ip)
		}
	}
	if len(lookup_ips) == 0 {
		lookup_ips = append(lookup_ips, ips[0])
	}
	return lookup_ips, nil
}

//Utility method to convert Unity Rest type Snapshot to CSI standard Snapshot Response
func GetSnapshotResponseFromSnapshot(snap *types.Snapshot, protocol, arrayId string) *csi.CreateSnapshotResponse {
	content := snap.SnapshotContent
	snapId := fmt.Sprintf("%s-%s-%s-%s", content.Name, protocol, arrayId, content.ResourceId)
	var timestamp *timestamp.Timestamp
	if !snap.SnapshotContent.CreationTime.IsZero() {
		timestamp, _ = ptypes.TimestampProto(snap.SnapshotContent.CreationTime)
	}

	snapReq := &csi.Snapshot{
		SizeBytes:      snap.SnapshotContent.Size,
		ReadyToUse:     true,
		SnapshotId:     snapId,
		SourceVolumeId: content.StorageResource.Id,
		CreationTime:   timestamp,
	}

	snapResp := &csi.CreateSnapshotResponse{
		Snapshot: snapReq,
	}

	return snapResp
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

//IpsCompare checks if the given ip is present as IP or FQDN in the given list of host ips
func IpsCompare(ctx context.Context, ip string, hostIps []string) (bool, []string) {
	log := GetRunidLogger(ctx)
	var result = false
	var additionalIps []string

	for _, hostIp := range hostIps {
		if ip == hostIp {
			log.Debug(fmt.Sprintf("Host Ip port %s matched Node IP", hostIp))
			result = true
		} else {
			//If HostIpPort is contains fqdn
			lookupIps, err := net.LookupIP(hostIp)
			if err != nil {
				//Lookup failed and hostIp is considered not to match Ip
				log.Info("Ip Lookup failed: ", err)
				additionalIps = append(additionalIps, hostIp)
			} else if ipListContains(lookupIps, ip) {
				log.Debug(fmt.Sprintf("Host Ip port %v matches Node IP after lookup on %s", lookupIps, hostIp))
				result = true
			} else {
				additionalIps = append(additionalIps, hostIp)
			}
		}
	}
	return result, additionalIps
}

//ipListContains method does contains check operation
func ipListContains(ipArray []net.IP, value string) bool {
	for _, ip := range ipArray {
		if value == ip.String() {
			return true
		}
	}
	return false
}

//GetIPsFromInferfaces - Method to extract ip as string from ipInterface object
func GetIPsFromInferfaces(ctx context.Context, ipInterfaces []types.IPInterfaceEntries) []string {
	ips := make([]string, 0)

	for _, ipInterface := range ipInterfaces {
		ips = append(ips, ipInterface.IPInterfaceContent.IPAddress)
	}
	return ips
}

//IPReachable checks if a given IP is reachable or not
func IPReachable(ctx context.Context, ip, port string, pingTimeout int) bool {
	log := GetRunidLogger(ctx)
	timeout := time.Duration(pingTimeout) * time.Millisecond
	log.Debug("Tcp test on IP", ip)

	_, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%s", ip, port), timeout)

	if err != nil {
		log.Debugf("Interface IP %s is not reachable %v", ip, err)
		return false
	}
	return true
}

//GetWwnFromVolumeContentWwn - Method to process wwn content to extract device wwn of a volume
func GetWwnFromVolumeContentWwn(wwn string) string {
	wwn = strings.ReplaceAll(wwn, ":", "")
	deviceWWN := strings.ToLower(wwn)
	return deviceWWN
}

//GetWwnFromVolumeContentWwn - Method to process wwn content to extract device wwn of a volume
func GetFcPortWwnFromVolumeContentWwn(wwn string) string {
	wwn = GetWwnFromVolumeContentWwn(wwn)
	return wwn[16:32]
}

// parse size for ephemeral volumes
func ParseSize(size string) (int64, error) {
	size = strings.Trim(size, " ")
	patternMap := make(map[string]string)
	patternMap["Mi"] = `[0-9]+[ ]*Mi`
	patternMap["Gi"] = `[0-9]+[ ]*Gi`
	patternMap["Ti"] = `[0-9]+[ ]*Ti`
	patternMap["Pi"] = `[0-9]+[ ]*Pi`
	var unit string
	var value string
	var match bool
	for key, pattern := range patternMap {
		match, _ = regexp.MatchString(pattern, size)
		if match {
			re := regexp.MustCompile("[0-9]+")
			unit = key
			valueList := re.FindAllString(size, -1)
			if len(valueList) > 1 {
				return 0, errors.New("Failed to parse size")
			}
			value = valueList[0]
			break
		}
	}
	if !match {
		return 0, errors.New("Failed to parse size")
	}
	valueInt, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, errors.New("Failed to parse bytes")
	}
	valueMap := make(map[string]int64)
	valueMap["Mi"] = 1048576
	valueMap["Gi"] = 1073741824
	valueMap["Ti"] = 1099511627776
	valueMap["Pi"] = 1125899906842624
	return valueInt * valueMap[unit], nil
}
