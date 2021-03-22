/*
Copyright (c) 2021 Dell EMC Corporation
All Rights Reserved
*/

package service

import (
	"context"
	"fmt"
	"github.com/dell/dell-csi-extensions/podmon"
	"github.com/dell/gounity"
	"github.com/dell/gounity/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

// References to calls to services outside of this function. The reference the real
// implementations by default; they are here so that they can be mocked out.
var GetHostId = getHostId
var RequireProbe = requireProbe
var GetUnityClient = getUnityClient
var GetArrayIdFromVolumeContext = getArrayIdFromVolumeContext
var FindHostInitiatorById = findHostInitiatorById

// ValidateVolumeHostConnectivity is for validating if there are signs of connectivity to a give Kubernetes node.
func (s *service) ValidateVolumeHostConnectivity(ctx context.Context, req *podmon.ValidateVolumeHostConnectivityRequest) (*podmon.ValidateVolumeHostConnectivityResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Infof("ValidateVolumeHostConnectivity called %+v", req)

	rep := &podmon.ValidateVolumeHostConnectivityResponse{
		Messages: make([]string, 0),
	}

	if (req.ArrayId == "" || len(req.GetVolumeIds()) == 0) && req.GetNodeId() == "" {
		// This is a nop call just testing the interface is present
		log.Info("ValidateVolumeHostConnectivity is implemented")
		rep.Messages = append(rep.Messages, "ValidateVolumeHostConnectivity is implemented")
		return rep, nil
	}

	if req.GetNodeId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "The NodeID is a required field")
	}

	// Get the arrayIDs to check connectivity
	systemIDs := make(map[string]bool)
	systemID := req.GetArrayId()
	if systemID == "" {
		log.Infof("No arrayId passed in, extracting it using other methods")
		// Try to extract the arrayID from the volumes
		foundOne := s.getArrayIdsFromVolumes(ctx, systemIDs, req.GetVolumeIds())
		// If no arrayIDs found in volumes (possibly because they weren't provided), then try the default array
		if !foundOne {
			// Lookup the default array
			var defaultArray string
			list := s.getStorageArrayList()
			for _, sys := range list {
				if sys.IsDefaultArray {
					defaultArray = sys.ArrayId
					break
				}
			}
			if defaultArray == "" {
				return nil, status.Errorf(codes.Aborted, "Could not find default array")
			}
			log.Infof("Use default array %s", defaultArray)
			systemID = defaultArray
			systemIDs[systemID] = true
		}
	} else { // ArrayID was in the request
		systemIDs[systemID] = true
	}

	// Go through each of the systemIDs
	for systemID := range systemIDs {
		log.Infof("Probe of systemID=%s", systemID)
		// Do a probe of the requested system
		if err := RequireProbe(s, ctx, systemID); err != nil {
			return nil, err
		}

		// First - check if the node is visible from the array
		var checkError error
		checkError = s.checkIfNodeIsConnected(ctx, systemID, req.GetNodeId(), rep)
		if checkError != nil {
			return rep, checkError
		}
	}

	// TODO - Volume IO check

	log.Infof("ValidateVolumeHostConnectivity reply %+v", rep)
	return rep, nil
}

// getArrayIdsFromVolumes iterates the requestVolumeIds list, extracting the arrayId and adding them to 'systemIDs'
// returns true if there was at least one arrayId found
func (s *service) getArrayIdsFromVolumes(ctx context.Context, systemIDs map[string]bool, requestVolumeIds []string) bool {
	ctx, log, _ := GetRunidLog(ctx)
	var err error
	var systemID string
	var foundAtLeastOne bool
	for _, volumeID := range requestVolumeIds {
		// Extract arrayID from the volume ID (if any volumes in the request)
		if systemID, err = GetArrayIdFromVolumeContext(s, volumeID); err != nil {
			log.Warnf("Error getting arrayID for %s - %s", volumeID, err.Error())
		}
		if systemID != "" {
			if _, exists := systemIDs[systemID]; !exists {
				foundAtLeastOne = true
				systemIDs[systemID] = true
				log.Infof("Using systemID from %s, %s", volumeID, systemID)
			}
		} else {
			log.Infof("Could not extract systemID from %s", volumeID)
		}
	}
	return foundAtLeastOne
}

// checkIfNodeIsConnected looks at the 'nodeId' host's initiators to determine if there is connectivity
// to the 'arrayId' array. The 'rep' object will be filled with the results of the check.
func (s *service) checkIfNodeIsConnected(ctx context.Context, arrayId string, nodeId string, rep *podmon.ValidateVolumeHostConnectivityResponse) error {
	ctx, log, _ := GetRunidLog(ctx)
	log.Infof("Checking if array %s is connected to node %s", arrayId, nodeId)
	var message string
	rep.Connected = false

	// Initialize the Unity client to the 'arrayId' array
	ctx, _ = setArrayIdContext(ctx, arrayId)
	unity, err := GetUnityClient(s, ctx, arrayId)
	if err != nil {
		message = fmt.Sprintf("Unable to get unity client for topology validation: %v", err)
		log.Info(message)
		rep.Messages = append(rep.Messages, message)
		return err
	}

	// Look up the 'nodeId' host on the array
	hostnames := strings.Split(nodeId, ",")
	shortName := hostnames[0]
	longName := shortName
	if len(hostnames) > 1 {
		longName = hostnames[1]
	}
	host, err := GetHostId(s, ctx, arrayId, shortName, longName)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			message = fmt.Sprintf("Array %s does have any host with name '%s'", arrayId, nodeId)
		} else {
			message = fmt.Sprintf("Host lookup failed. Error: %v", err)
		}
		log.Infof(message)
		rep.Messages = append(rep.Messages, message)
		rep.Connected = false
		return nil
	}

	// Search in the list of FC initiators (if any)
	fcConnectivity := false
	if host != nil && len(host.HostContent.FcInitiators) != 0 {
		log.Infof("Got FC Initiators, Checking health of initiators:%s", host.HostContent.FcInitiators)
		for _, initiator := range host.HostContent.FcInitiators {
			initiatorID := initiator.Id
			hostInitiator, err := FindHostInitiatorById(unity, ctx, initiatorID)
			if err != nil {
				log.Infof("Unable to get initiators: %s", err)
			}
			if hostInitiator != nil {
				healthContent := hostInitiator.HostInitiatorContent.Health
				if healthContent.DescriptionIDs[0] == componentOkMessage {
					message = fmt.Sprintf("FC Health is good for array:%s, Health:%s", arrayId, healthContent.DescriptionIDs[0])
					log.Infof(message)
					rep.Messages = append(rep.Messages, message)
					rep.Connected = true
					fcConnectivity = true
					break
				} else {
					log.Infof("FC Health is bad for array:%s, Health:%s", arrayId, healthContent.DescriptionIDs[0])
				}
			}
		}
	}

	// Search in the list of iSCSI initiators (if any) and there is no connectivity seen through FC
	if host != nil && len(host.HostContent.IscsiInitiators) != 0 && !fcConnectivity {
		log.Infof("Got iSCSI Initiators, Checking health of initiators:%s", host.HostContent.IscsiInitiators)
		for _, initiator := range host.HostContent.IscsiInitiators {
			initiatorID := initiator.Id
			hostInitiator, err := FindHostInitiatorById(unity, ctx, initiatorID)
			if err != nil {
				log.Infof("Unable to get initiators: %s", err)
			}
			if hostInitiator != nil {
				healthContent := hostInitiator.HostInitiatorContent.Health
				if healthContent.DescriptionIDs[0] == componentOkMessage {
					message = fmt.Sprintf("iSCSI Health is good for array:%s, Health:%s", arrayId, healthContent.DescriptionIDs[0])
					log.Infof(message)
					rep.Messages = append(rep.Messages, message)
					rep.Connected = true
					break
				} else {
					log.Infof("iSCSI Health is bad for array:%s, Health:%s", arrayId, healthContent.DescriptionIDs[0])
				}
			}
		}
	}

	return nil
}

// Below are service calls that are outside of this extension implementation.

func getArrayIdFromVolumeContext(s *service, contextVolId string) (string, error) {
	return s.getArrayIdFromVolumeContext(contextVolId)
}

func requireProbe(s *service, ctx context.Context, arrayId string) error {
	return s.requireProbe(ctx, arrayId)
}

func getHostId(s *service, ctx context.Context, arrayId, shortHostname, longHostname string) (*types.Host, error) {
	return s.getHostId(ctx, arrayId, shortHostname, longHostname)
}

func getUnityClient(s *service, ctx context.Context, arrayId string) (*gounity.Client, error) {
	return s.getUnityClient(ctx, arrayId)
}

func findHostInitiatorById(unity *gounity.Client, ctx context.Context, wwnOrIqn string) (*types.HostInitiator, error) {
	hostAPI := gounity.NewHost(unity)
	return hostAPI.FindHostInitiatorById(ctx, wwnOrIqn)
}
