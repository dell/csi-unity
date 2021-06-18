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
	"go.uber.org/atomic"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strconv"
	"strings"
	"sync"
	"time"
)

// References to calls to services outside of this function. The reference the real
// implementations by default; they are here so that they can be mocked out.
var GetHostId = getHostId
var RequireProbe = requireProbe
var GetUnityClient = getUnityClient
var GetArrayIdFromVolumeContext = getArrayIdFromVolumeContext
var GetProtocolFromVolumeContext = getProtocolFromVolumeContext
var FindHostInitiatorById = findHostInitiatorById
var GetMetricsCollection = getMetricsCollection
var CreateMetricsCollection = createMetricsCollection

//MetricsCollectionInterval is used for interval to use in the creation of a Unity MetricsCollection
var MetricsCollectionInterval = 5 // seconds
var CollectionWait = (MetricsCollectionInterval + 1) * 1000

var metricsCollectionCache sync.Map
var currentIOCount = []string{
	"sp.*.storage.lun.*.currentIOCount",
}
var fileSystemRWs = []string{
	"sp.*.storage.filesystem.*.clientReads",
	"sp.*.storage.filesystem.*.clientWrites",
}

var cacheRWLock sync.RWMutex
var kickoffOnce sync.Once
var refreshCount atomic.Int32
var refreshEnabled bool

const (
	Iscsi           = "iscsi"
	Fc              = "fc"
	Nfs             = "nfs"
	refreshInterval = 30  // seconds
	maxRefresh      = 300 // seconds
)

var RefreshDuration = refreshInterval * time.Second

// ValidateVolumeHostConnectivity is for validating if there are signs of connectivity to a give Kubernetes node.
func (s *service) ValidateVolumeHostConnectivity(ctx context.Context, req *podmon.ValidateVolumeHostConnectivityRequest) (*podmon.ValidateVolumeHostConnectivityResponse, error) {
	ctx, log, _ := GetRunidLog(ctx)
	log.Infof("ValidateVolumeHostConnectivity called %+v", req)

	kickoffOnce.Do(func() {
		refreshEnabled = true
		go s.refreshMetricsCollections()
	})

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

	// Using the list of volumes in the request, create a map of protocols -> arrays -> volumes
	// Basic algorithm using the above:
	//   FOREACH array IN protocols
	//     FOREACH volumes IN array
	//        hasIOs = anyVolumesHaveIOs(array, protocol, volumes)
	//        IF hasIOs IS TRUE
	//           RETURN result with rep.IosInProgress set to true
	type arrayToVolumes map[string][]string
	protocolToArrays := make(map[string]arrayToVolumes)
	// Go through the list, creating a map of the volume protocols to another
	// map of arrayIds to list of volumes
	for _, requestVolumeId := range req.GetVolumeIds() {
		protocol, getProtoErr := GetProtocolFromVolumeContext(s, requestVolumeId)
		if getProtoErr != nil {
			return rep, getProtoErr
		}

		arrayId, _ := GetArrayIdFromVolumeContext(s, requestVolumeId)
		volumeId := getVolumeIdFromVolumeContext(requestVolumeId)

		// Look up the map of arrays to volumes for this protocol
		a2v, hasProto := protocolToArrays[protocol]
		if !hasProto {
			a2v = make(arrayToVolumes)
			protocolToArrays[protocol] = a2v
		}

		// Look up list of volumes for this array (of a particular protocol)
		volList, hasArray := a2v[arrayId]
		if !hasArray {
			volList = make([]string, 0)
		}
		volList = append(volList, volumeId)
		a2v[arrayId] = volList
	}

	// Go through the protocol to arrayId map and process the check against
	// each array with its volume list.
	for protocol, arrays := range protocolToArrays {
		for arrayId, volumes := range arrays {
			var hasIOs bool
			var checkIOsErr error
			var checkForIOs func(context.Context, *podmon.ValidateVolumeHostConnectivityResponse, string, []string) (bool, error)

			protocol = strings.ToLower(protocol)
			switch protocol {
			case Iscsi:
				fallthrough
			case Fc:
				checkForIOs = s.doesAnyVolumeHaveIO
			case Nfs:
				checkForIOs = s.doesAnyFileSystemHaveIO
			default:
				return rep, fmt.Errorf("unexpected protocol '%s' found in request", protocol)
			}

			hasIOs, checkIOsErr = checkForIOs(ctx, rep, arrayId, volumes)
			if checkIOsErr != nil {
				return rep, checkIOsErr
			}

			if hasIOs {
				rep.IosInProgress = true
			}
		}
	}

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

//doesAnyVolumeHaveIO will determine if any of the given volumes on array has IOs.
func (s *service) doesAnyVolumeHaveIO(ctx context.Context, rep *podmon.ValidateVolumeHostConnectivityResponse, arrayId string, volumeIds []string) (bool, error) {
	ctx, log, _ := GetRunidLog(ctx)

	// Retrieve the latest currentIO metrics for all the array's volumes
	metrics, getErr := s.getMetrics(ctx, arrayId, currentIOCount)
	if getErr != nil {
		return false, getErr
	}

	foundVolumeWithIO := false
	for _, volumeId := range volumeIds {
		// As an example, the results should look like this if printed out as a string:
		// sp.*.storage.lun.*.currentIOCount [spa = map[sv_108:0 sv_18:0 sv_19:0 sv_22:0 sv_23:0 sv_24:0 sv_25:0 sv_26:0]]
		//
		// This translates to the objects in the code below:
		// | metric.Entries                    | entry.Content.Values.key | volumesMetricMap[volumeId] |
		// |-----------------------------------|--------------------------|----------------------------|
		// | sp.*.storage.lun.*.currentIOCount | spa                      | sv_108:0                   |
		for _, entry := range metrics.Entries {
			// CurrentIO metrics are per SP, search through all the SPs
			for spId, value := range entry.Content.Values {
				hasOrNot := "no "
				volumesMetricMap := value.(map[string]interface{})
				// Look up the metric for this volume
				if countStr, exists := volumesMetricMap[volumeId]; exists {
					log.Infof("Array: %s metric: %s SP: %s %s = %v", arrayId, entry.Content.Path, spId, volumeId, countStr)
					count, convErr := strconv.Atoi(countStr.(string))
					if convErr != nil {
						return false, convErr
					}
					if count > 0 {
						hasOrNot = ""
						foundVolumeWithIO = true
					}
				}
				rep.Messages = append(rep.Messages, fmt.Sprintf("%s on array %s has %sIOs", volumeId, arrayId, hasOrNot))
			}
		}
	}

	return foundVolumeWithIO, nil
}

//doesAnyFileSystemHaveIO returns true if any of the file systems in 'fsIds' shows active IOs
func (s *service) doesAnyFileSystemHaveIO(ctx context.Context, rep *podmon.ValidateVolumeHostConnectivityResponse, arrayId string, fsIds []string) (bool, error) {
	ctx, log, _ := GetRunidLog(ctx)

	// Get two samples over the interval period and get a difference between the values
	// found. If any are found to be non-zero, then we return true, otherwise false.
	var (
		first, second             *types.MetricQueryResult
		firstSample, secondSample map[string]int
		getErr, getValueErr       error
	)

	// Retrieve the latest files system read/write metrics
	first, getErr = s.getMetrics(ctx, arrayId, fileSystemRWs)
	if getErr != nil {
		return false, getErr
	}

	time.Sleep(time.Duration(CollectionWait) * time.Millisecond)

	// Retrieve the metrics for a second time
	second, getErr = s.getMetrics(ctx, arrayId, fileSystemRWs)
	if getErr != nil {
		return false, getErr
	}

	foundVolumeWithIO := false
	for _, fsId := range fsIds {
		firstSample, getValueErr = s.getMetricValues(ctx, first, arrayId, fsId)
		if getValueErr != nil {
			return false, getValueErr
		}
		log.Debugf("firstSample = %v", firstSample)

		secondSample, getValueErr = s.getMetricValues(ctx, second, arrayId, fsId)
		if getValueErr != nil {
			return false, getValueErr
		}
		log.Debugf("secondSample = %v", secondSample)

		hasOrNot := "no "
		for metricName, v1 := range firstSample {
			v2, ok := secondSample[metricName]
			if !ok {
				return false, fmt.Errorf("unexpected result. Could not find metric value for %s", metricName)
			}
			// Any case found where the difference between the first
			// and the second queries is non-zero should return true.
			diff := v2 - v1
			if diff > 0 {
				hasOrNot = ""
				foundVolumeWithIO = true
			}
		}
		rep.Messages = append(rep.Messages, fmt.Sprintf("%s on array %s has %sIOs", fsId, arrayId, hasOrNot))
	}
	return foundVolumeWithIO, nil
}

//getMetrics retrieves the specified metrics from the array
func (s *service) getMetrics(ctx context.Context, arrayId string, metrics []string) (*types.MetricQueryResult, error) {
	ctx, log, _ := GetRunidLog(ctx)

	// Synchronize to allow only a single collection per array + metrics
	readLocked := false
	cacheRWLock.RLock()
	readLocked = true
	defer func() {
		if readLocked {
			cacheRWLock.RUnlock()
		}
	}()

	// We cache the metrics collection ID for a give array + metric names
	cacheKey := fmt.Sprintf("%s:%s", arrayId, strings.Join(metrics, ","))
	collectionId, found := metricsCollectionCache.Load(cacheKey)
	if found {
		// Validate that the query works.
		result, getErr := GetMetricsCollection(s, ctx, arrayId, collectionId.(int))
		if getErr == nil {
			// No error on query, but validate that it has the requested metric path
			hasAllPaths := true
			for _, entry := range result.Entries {
				hasMetric := false
				for _, metric := range metrics {
					if entry.Content.Path == metric {
						hasMetric = true
						break
					}
				}
				if !hasMetric {
					hasAllPaths = false
					break
				}
			}
			if hasAllPaths {
				log.Infof("Queried %v metrics collection %v for %s", metrics, collectionId, arrayId)
				// All good, return the results
				return result, nil
			}
			log.Warnf("Stale cache: collection with ID %v doesn't apply to %v.", collectionId, metrics)
		}
	}

	// Unlock read and upgrade to a write lock
	cacheRWLock.RUnlock()
	readLocked = false
	cacheRWLock.Lock()
	defer cacheRWLock.Unlock()

	// If we are here, we have the write lock. Check the cache in case
	// another thread already created the collection while this thread
	// was waiting on the write lock.
	latestCollectionId, foundAgain := metricsCollectionCache.Load(cacheKey)
	if foundAgain && latestCollectionId != collectionId {
		// There was a hit and it was different from what we had above.
		// Return the metrics data based on this latest collection ID.
		log.Infof("Retrieving results from latest collection %v", latestCollectionId)
		return GetMetricsCollection(s, ctx, arrayId, latestCollectionId.(int))
	} else if foundAgain { // but collectionIds match, so a stale entry
		// Clean up a stale cache instance
		metricsCollectionCache.Delete(cacheKey)
	}

	// If we are here, then we are going to be creating a metrics collection.
	// This should be done by a single thread. Subsequent calls to getMetrics
	// should return metrics based on the cached collection ID.
	log.Infof("Attempting to create a %v metrics collection for %s", metrics, arrayId)

	// Create the metrics collection because it doesn't exist
	collection, createErr := CreateMetricsCollection(s, ctx, arrayId, metrics, MetricsCollectionInterval)
	if createErr != nil {
		return nil, createErr
	}

	log.Infof("Metrics collection %d created for %s", collection.Content.Id, arrayId)

	// Wait a bit before trying the first query
	time.Sleep(time.Duration(CollectionWait) * time.Millisecond)

	// Cache the collection Id for subsequent use (above, when there is a cache hit)
	results, getErr := GetMetricsCollection(s, ctx, arrayId, collection.Content.Id)
	if getErr != nil {
		return nil, getErr
	}
	metricsCollectionCache.Store(cacheKey, collection.Content.Id)

	// Reset this counter so that we can continue to "keep-alive" collections for some time.
	refreshCount.Store(0)

	log.Infof("Successfully queried metrics collection %d for %s", collection.Content.Id, arrayId)

	return results, nil
}

//getMetricValues will return a mapping of the metric name to value for a object of the given 'id' on the array.
//Assumes that the value is an integer.
func (s *service) getMetricValues(ctx context.Context, metrics *types.MetricQueryResult, arrayId, id string) (map[string]int, error) {
	ctx, log, _ := GetRunidLog(ctx)

	// As an example, the results should look like this if printed out as a string:
	// sp.*.storage.lun.*.currentIOCount [spa = map[sv_108:0 sv_18:0 sv_19:0 sv_22:0 sv_23:0 sv_24:0 sv_25:0 sv_26:0]]
	//
	// This translates to the objects in the code below:
	// | metric.Entries                    | entry.Content.Values.key | volumesMetricMap[volumeId] |
	// |-----------------------------------|--------------------------|----------------------------|
	// | sp.*.storage.lun.*.currentIOCount | spa                      | sv_108:0                   |
	resultMap := make(map[string]int)
	for _, entry := range metrics.Entries {
		// CurrentIO metrics are per SP, search through all the SPs
		for spId, value := range entry.Content.Values {
			metricMap := value.(map[string]interface{})
			// Look up the metric for this volume
			if countStr, exists := metricMap[id]; exists {
				log.Infof("Array: %s metric: %s SP: %s %s = %v", arrayId, entry.Content.Path, spId, id, countStr)
				count, convErr := strconv.Atoi(countStr.(string))
				if convErr != nil {
					return nil, convErr
				}
				resultMap[spId+":"+entry.Content.Path] = count
			}
		}
	}

	return resultMap, nil
}

// refreshMetricsCollections will iterate over the range of the metricsCollectionCache and call
// GetMetricsCollection, so that we can "keep-alive" the collection. Otherwise, the collection
// will be cleaned up by the array with 1 minute after creation.
func (s *service) refreshMetricsCollections() {
	ctx, log, _ := GetRunidLog(context.Background())
	timeout := 30 * time.Second
	var totalInterval int32
	totalInterval = maxRefresh / refreshInterval

	ticker := time.NewTicker(RefreshDuration)
	for {
		select {
		case <-ticker.C:
			if !refreshEnabled {
				continue
			}
			// Refresh for a certain maximum number of times. The refresh count can be reset
			// at any time, so that the refresh is restarted.
			if refreshCount.Load() < totalInterval {
				metricsCollectionCache.Range(func(cacheKey, collectionId interface{}) bool {
					metricCtx, cancel := context.WithTimeout(ctx, timeout)
					comp := strings.Split(cacheKey.(string), ":")
					log.Infof("Refreshing metric for %v collectionId %v", cacheKey, collectionId)
					_, _ = GetMetricsCollection(s, metricCtx, comp[0], collectionId.(int))
					cancel()
					return true
				})
				refreshCount.Inc()
			}
		}
	}
}

// Below are service calls that are outside of this extension implementation.

func getArrayIdFromVolumeContext(s *service, contextVolId string) (string, error) {
	return s.getArrayIdFromVolumeContext(contextVolId)
}

func getProtocolFromVolumeContext(s *service, contextVolId string) (string, error) {
	return s.getProtocolFromVolumeContext(contextVolId)
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

func getMetricsCollection(s *service, ctx context.Context, arrayId string, id int) (*types.MetricQueryResult, error) {
	return s.getMetricsCollection(ctx, arrayId, id)
}

func createMetricsCollection(s *service, ctx context.Context, arrayId string, metricPaths []string, interval int) (*types.MetricQueryCreateResponse, error) {
	return s.createMetricsCollection(ctx, arrayId, metricPaths, interval)
}
