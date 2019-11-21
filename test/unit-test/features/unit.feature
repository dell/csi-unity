Feature: CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.
  
  Scenario: Create and Delete snapshot without probe
    Given a basic block volume request "probe1" "2"
    When I call CreateVolume
    Then the error message should contain "Controller Service has not been probed"
    And a create snapshot request "snap_test"
    When I call CreateSnapshot
    Then the error message should contain "Controller Service has not been probed"
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then the error message should contain "Controller Service has not been probed"

  Scenario: List snapshots without probe
    Given a list snapshots request with startToken "" maxEntries "50" sourceVolumeId "" snapshotId ""
    When I call list snapshots
    Then the error message should contain "Controller Service has not been probed"
    
  Scenario: List Volumes without probe
    Given a list volumes request with maxEntries "5" startToken "" 
    When I call list volumes
    Then the error message should contain "Controller Service has not been probed"

  Scenario: Create, controller publish, controller unpublish and delete a basic volume
    Given a basic block volume request "probe2" "2"
    When I call CreateVolume
    Then the error message should contain "Controller Service has not been probed"
    And when I call PublishVolume
    Then the error message should contain "Controller Service has not been probed"
    And when I call UnpublishVolume
    Then the error message should contain "Controller Service has not been probed"
    And when I call DeleteVolume
    Then the error message should contain "Controller Service has not been probed"

  Scenario: Validate volume capabilities without probe
    Given a basic block volume request "probe3" "2"
    When I call CreateVolume
    Then the error message should contain "Controller Service has not been probed"
    When I call validate volume capabilities with same access mode
    Then the error message should contain "Controller Service has not been probed"

  Scenario: Get Capacity without probe
    When I call Get Capacity with storage pool "pool"
    Then the error message should contain "Controller Service has not been probed"

  Scenario: Controller expand volume without probe
    Given a basic block volume request "probe4" "2"
    When I call CreateVolume
    Then the error message should contain "Controller Service has not been probed"
    When I call Controller Expand Volume "3"
    Then the error message should contain "Controller Service has not been probed"

  Scenario: Create and Delete snapshot successfully
    Given a CSI service
    And a basic block volume request "snap1" "2"
    When I call CreateVolume
    Then there are no errors
    Given a create snapshot request "csi_snapshot_test"
    When I call CreateSnapshot
    Then there are no errors
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create snapshot with a name that already exists
    Given a CSI service
    And a basic block volume request "snap1_vol" "2"
    When I call CreateVolume
    Then there are no errors
    And a create snapshot request "snap1"
    When I call CreateSnapshot
    Then there are no errors
    When I call CreateSnapshot
    Then there are no errors
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors
  
  Scenario: Create snapshot without giving storage resource name
    Given a CSI service
    And a create snapshot request "snapshot_test1"
    When I call CreateSnapshot
    Then the error message should contain "Storage Resource ID cannot be empty"

  Scenario: Create snapshot with invalid name
    Given a CSI service
    And a basic block volume request "snap_invalid_test" "2"
    When I call CreateVolume
    Then there are no errors
    And a create snapshot request "snap_#$"
    When I call CreateSnapshot
    Then the error message should contain "invalid snapshot name"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Delete snapshot without giving ID
    Given a CSI service
    And a delete snapshot request
    When I call DeleteSnapshot
    Then the error message should contain "snapshot ID is mandatory parameter"

  Scenario: Delete snapshot with incorrect ID
    Given a CSI service
    And a delete snapshot request "snap_not_exist_id"
    When I call DeleteSnapshot
    Then there are no errors

  Scenario: List snapshots successfully
    Given a CSI service
    And a list snapshots request with startToken "" maxEntries "50" sourceVolumeId "" snapshotId ""
    When I call list snapshots
    Then there are no errors

  Scenario: List snapshots with startToken
    Given a CSI service
    And a list snapshots request with startToken "1" maxEntries "50" sourceVolumeId "" snapshotId ""
    When I call list snapshots
    Then there are no errors

  Scenario: List snapshots with incorrect startToken
    Given a CSI service
    And a list snapshots request with startToken "xyz" maxEntries "50" sourceVolumeId "" snapshotId ""
    When I call list snapshots
    Then the error message should contain "Unable to parse StartingToken"

  Scenario: List snapshots with source volume
    Given a CSI service
    And a list snapshots request with startToken "" maxEntries "0" sourceVolumeId "sv_1023" snapshotId ""
    When I call list snapshots
    Then there are no errors

  Scenario: List snapshots with invalid source volume
    Given a CSI service
    And a list snapshots request with startToken "" maxEntries "0" sourceVolumeId "abcd" snapshotId ""
    When I call list snapshots
    Then there are no errors

  Scenario: List snapshots with snapshot id
    Given a CSI service
    And a list snapshots request with startToken "" maxEntries "0" sourceVolumeId "" snapshotId "38654711096"
    When I call list snapshots
    Then there are no errors

  Scenario: List snapshots with invalid snapshot id
    Given a CSI service
    And a list snapshots request with startToken "" maxEntries "0" sourceVolumeId "" snapshotId "abcd"
    When I call list snapshots
    Then there are no errors

  Scenario: List Volumes successfully
    Given a CSI service
    And a list volumes request with maxEntries "10" startToken "" 
    When I call list volumes
    Then there are no errors

  Scenario: List Volumes with startToken
    Given a CSI service
    And a list volumes request with maxEntries "5" startToken "1" 
    When I call list volumes
    Then there are no errors

  Scenario: List Volumes with incorrect start token
    Given a CSI service
    And a list volumes request with maxEntries "5" startToken "xyz"
    When I call list volumes
    Then the error message should contain "Unable to parse StartingToken"

  Scenario: Create and delete basic volume successfully and idempotency test
    Given a CSI service
    And a basic block volume request "unit1" "2"
    When I call CreateVolume
    Then there are no errors
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create an existing volume with same size
    Given a CSI service
    And a basic block volume request "vol_same" "2"
    When I call CreateVolume
    Then there are no errors
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors
  
  Scenario: Create an existing volume with different size
    Given a CSI service
    And a basic block volume request "AAAvol-5" "3"
    When I call CreateVolume
    Then there are no errors
    And a basic block volume request "AAAvol-5" "5"
    When I call CreateVolume
    Then the error message should contain "'Volume name' already exists and size is different"
    And a basic block volume request "AAAvol-5" "3"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without thinProvisioned parameter
    Given a CSI service
    And a basic block volume request with volumeName "param_test1" size "2" storagepool "pool_1" thinProvisioned "" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without isCompressionEnabled parameter
    Given a CSI service
    And a basic block volume request with volumeName "param_test2" size "2" storagepool "pool_1" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without isDataReductionEnabled parameter
    Given a CSI service
    And a basic block volume request with volumeName "param_test3" size "2" storagepool "pool_1" thinProvisioned "true" isDataReductionEnabled "" tieringPolicy "0"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without isDataReductionEnabled parameter
    Given a CSI service
    And a basic block volume request with volumeName "param_test4" size "2" storagepool "pool_1" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy ""
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume with incorrect storage_pool
    Given a CSI service
    And a basic block volume request with volumeName "param_test5" size "2" storagepool "abcd" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then the error message should contain "unable to find the PoolID"

  Scenario: Create a volume without volume name
    Given a CSI service
    And a basic block volume request with volumeName "" size "2" storagepool "abcd" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then the error message should contain "required: Name"

  Scenario: Publish and unpublish a volume to host
    Given a CSI service
    And a basic block volume request "test_publish1" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Publish a volume to host with readonly as true
    Given a CSI service
    And a basic block volume request "test_publish2" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume with host "host" readonly "true"
    Then the error message should contain "Readonly must be false"
    And when I call DeleteVolume
    Then there are no errors 

  Scenario: Publish and unpublish a volume to host without giving hostname
    Given a CSI service
    And a basic block volume request "test_publish3" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume with host "" readonly "false"
    Then the error message should contain "required: NodeID"
    And when I call UnpublishVolume
    Then the error message should contain "Node ID is required"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Publish a volume to host with VolumeCapability_AccessMode other than SINGLE_NODE_WRITER
    Given a CSI service
    And a basic block volume request "test_publish4" "5"
    When I change volume capability accessmode
    When I call CreateVolume
    Then the error message should contain "not supported"
    And when I call PublishVolume
    Then the error message should contain "volume AccessMode is supported only by SINGLE_NODE_WRITER"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Publish and unpublish a volume to host with incorrect hostname
    Given a CSI service
    And a basic block volume request "test_publish5" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume with host "host" readonly "false"
    Then the error message should contain "unable to find host"
    And when I call UnpublishVolume
    Then the error message should contain "unable to find host"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Publish and unpublish a volume to host without giving volume id
    Given a CSI service
    And when I call PublishVolume with volumeId ""
    Then the error message should contain "required: VolumeID"
    When I call UnpublishVolume with volumeId ""
    Then the error message should contain "required: VolumeID"

  Scenario: Publish and unpublish a volume to host with incorrect volume id
    Given a CSI service
    And when I call PublishVolume with volumeId "xyz"
    Then the error message should contain "Unable to find volume"
    When I call UnpublishVolume with volumeId "abcd"
    Then the error message should contain "Unable to find volume"

  Scenario: Publish and unpublish volume idempotency
    Given a CSI service
    And a basic block volume request "test_publish1" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call PublishVolume
    Then there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Publish a volume to a host that is published to another host
    Given a CSI service
    And a basic block volume request "test_publish1" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call PublishVolume with host "lglw7151" readonly "false"
    Then the error message should contain "Volume has been published to a different host already"
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Validate volume capabilities with same access mode
    Given a CSI service
    And a basic block volume request "vvc1" "2"
    When I call CreateVolume
    Then there are no errors
    When I call validate volume capabilities with same access mode
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Validate volume capabilities with different access mode
    Given a CSI service
    And a basic block volume request "vvc1" "2"
    When I call CreateVolume
    Then there are no errors
    When I call validate volume capabilities with different access mode
    Then the error message should contain "Unsupported capability"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Validate volume capabilities with incorrect volume Id
    Given a CSI service
    When I call validate volume capabilities with volume ID "xyz"
    Then the error message should contain "volume not found"

  Scenario: Get Capacity with storage pool name
    Given a CSI service
    And a basic block volume request with volumeName "param_test6" size "2" storagepool "lglad082_AFA" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy "0"
    When I call Get Capacity with storage pool "lglad082_AFA"
    Then there are no errors

  Scenario: Get Capacity with storage pool id
    Given a CSI service
    When I call Get Capacity with storage pool "id"
    Then there are no errors

  Scenario: Get Capacity with incorrect storage pool
    Given a CSI service
    When I call Get Capacity with storage pool "incorrect"
    Then the error message should contain "not found"

  Scenario: Controller get capabilities
    Given a CSI service
    When I call Controller Get Capabilities
    Then there are no errors

  Scenario: Controller expand volume
    Given a CSI service
    And a basic block volume request "expand1" "2"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "3"
    Then there are no errors
    And a basic block volume request "expand1" "3"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Controller expand volume with same new size
    Given a CSI service
    And a basic block volume request "expand2" "2"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "2"
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Controller expand volume with smaller new size
    Given a CSI service
    And a basic block volume request "expand3" "3"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "2"
    Then the error message should contain "requested new capacity smaller than existing capacity"
    And when I call DeleteVolume
    Then there are no errors
    
  Scenario: Controller expand volume with new size as 0
    Given a CSI service
    And a basic block volume request "expand4" "2"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "0"
    Then the error message should contain "required bytes can not be 0 or less"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Controller expand volume with volume that does not exist
    Given a CSI service
    When I call Controller Expand Volume "2" with volume "abcd"
    Then the error message should contain "unable to find the volume"

  Scenario: Controller expand volume without giving volume
    Given a CSI service
    When I call Controller Expand Volume "2" with volume ""
    Then the error message should contain "required"

  Scenario: Node stage, publish, unpublish and unstage volume
    Given a CSI service
    And a basic block volume request "unit_test_publish" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume
    And there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node stage and unstage volume with incorrect volume
    Given a CSI service
    And when I call NodeStageVolume
    Then the error message should contain "Unable to find volume Id"

  Scenario: Node publish volume with readonly as true
    Given a CSI service
    And a basic block volume request "publish_readonly" "5"
    When I call CreateVolume
    And there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "true"
    Then the error message should contain "readonly must be false, because the supported mode only SINGLE_NODE_WRITER"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node publish volume without volume id
    Given a CSI service
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then the error message should contain "required: VolumeID"

  Scenario: Node publish and unpublish volume with volume not present on array
    Given a CSI service
    And a basic block volume request "publish_readonly" "5"
    When I call CreateVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then the error message should contain "not found"
    And when I call NodeUnPublishVolume
    Then the error message should contain "not found"

  Scenario: Node publish volume without controller publish volume
    Given a CSI service
    And a basic block volume request "publish_readonly" "5"
    When I call CreateVolume
    And there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then the error message should contain "not been published to this node"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node unpublish when node publish never hapened
    Given a CSI service
    And a basic block volume request "publish_readonly" "5"
    When I call CreateVolume
    And there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then the error message should contain "not been published to this node"
    And when I call NodeUnPublishVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node Get Info
    Given a CSI service
    And When I call NodeGetInfo
    Then there are no errors

  Scenario: Node Get Capabilities
    Given a CSI service
    And When I call NodeGetCapabilities
    Then there are no errors

  Scenario: Node publish volume without volume capabilities
    Given a CSI service
    And a basic block volume request "nodepublish_test" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodePublishVolume without accessmode and fsType "ext4"
    Then the error message should contain "required: AccessMode"
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node publish volume without node stage and target path not created
    Given a CSI service
    And a basic block volume request "nodepublish_test" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodePublishVolume targetpath "/root/abc" fsType "ext4"
    Then the error message should contain "error getting block device"
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node stage, publish, unpublish and unstage volume idempotency
    Given a CSI service
    And a basic block volume request "unit_test_publish" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume
    And there are no errors
    And when I call NodeStageVolume
    And there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call NodeUnPublishVolume
    Then there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Get Plugin capabilities
    Given a CSI service
    And When I call GetPluginCapabilities
    Then there are no errors

  Scenario: Get Plugin info
    Given a CSI service
    And When I call GetPluginInfo
    Then there are no errors

  Scenario: Node publish with target path not ceated
    Given a CSI service
    And a basic block volume request "unit_test_publish" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume
    And there are no errors
    And when I call NodePublishVolume targetpath "/root/abc" fsType "ext4"
    Then the error message should contain "not pre-created"
    And when I call NodeUnstageVolume
    And there are no errors    
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node stage with target path not ceated
    Given a CSI service
    And a basic block volume request "unit_test_publish" "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume with StagingTargetPath "/root/abc"
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Probe without Unity endpoint
    Given a CSI service without CSI Unity Endpoint
    Then the error message should contain "missing Unity endpoint"

  Scenario: Probe without Unity password
    Given a CSI service with CSI Unity Password ""
    Then the error message should contain "missing Unity password"

  Scenario: Node stage volume without probe
    And when I call NodeStageVolume without probe
    Then the error message should contain "missing Unity endpoint"

  Scenario: Node publish volume without unity endpoint for probe
    And when I call NodePublishVolume without probe
    Then the error message should contain "missing Unity endpoint"

  Scenario: Node unstage volume without unity endpoint for probe
    And when I call NodeUnstageVolume without probe
    Then the error message should contain "missing Unity endpoint"

  Scenario: Node get info without unity endpoint for probe
    Given a CSI service
    And When I call NodeGetInfo without probe
    Then the error message should contain "missing Unity endpoint"

  Scenario: Node get info without hostname
    Given a CSI service
    And When I call NodeGetInfo hostname ""
    Then the error message should contain "'Node Name' has not been configured"

  Scenario: Node get info with incorrect hostname but same network address (hostname here should not be a host on array)
    Given a CSI service
    And When I call NodeGetInfo hostname "host_new"
    Then the error message should contain "The specified host network address already exists"