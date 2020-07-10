Feature: CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.

  Scenario: Create and Delete snapshot successfully
    Given a CSI service
    And a basic block volume request name "gdtest-vol1" arrayId "apm00175023135" protocol "FC" size "2"
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
    And a basic block volume request name "gdtest-vol2" arrayId "apm00175023135" protocol "FC" size "2"
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
    And a basic block volume request name "gdtest-vol3" arrayId "apm00175023135" protocol "FC" size "2"
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
    Then the error message should contain "snapshotId can't be empty"

  Scenario: Delete snapshot with incorrect ID
    Given a CSI service
    And a delete snapshot request "snap_not_exist_id"
    When I call DeleteSnapshot
    Then there are no errors

  Scenario: Create and delete basic volume successfully and idempotency test
    Given a CSI service
    And a basic block volume request name "gdtest-vol4" arrayId "apm00175023135" protocol "FC" size "2"
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
    And a basic block volume request name "gdtest-vol5" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors
  
  Scenario: Create an existing volume with different size
    Given a CSI service
    And a basic block volume request name "gdtest-vol6" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    And a basic block volume request name "gdtest-vol6" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    Then the error message should contain "'Volume name' already exists and size is different"
    And a basic block volume request name "gdtest-vol6" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without thinProvisioned parameter
    Given a CSI service
    And a basic block volume request with volumeName "gdtest-vol7" arrayId "apm00175023135" protocol "FC" size "2" storagepool "pool_1" thinProvisioned "" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without isCompressionEnabled parameter
    Given a CSI service
    And a basic block volume request with volumeName "gdtest-vol8" arrayId "apm00175023135" protocol "FC" size "2" storagepool "pool_1" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without isDataReductionEnabled parameter
    Given a CSI service
    And a basic block volume request with volumeName "gdtest-vol9" arrayId "apm00175023135" protocol "FC" size "2" storagepool "pool_1" thinProvisioned "true" isDataReductionEnabled "" tieringPolicy "0"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without isDataReductionEnabled parameter
    Given a CSI service
    And a basic block volume request with volumeName "gdtest-vol10" arrayId "apm00175023135" protocol "FC" size "2" storagepool "pool_1" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy ""
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume with incorrect storage_pool
    Given a CSI service
    And a basic block volume request with volumeName "gdtest-vol11" arrayId "apm00175023135" protocol "FC" size "2" storagepool "abcd" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then the error message should contain "unable to find the PoolID"

  Scenario: Create a volume without volume name
    Given a CSI service
    And a basic block volume request with volumeName "" arrayId "apm00175023135" protocol "FC" size "2" storagepool "abcd" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then the error message should contain "required: Name"

  Scenario: Create a volume from snapshot of thin volume with idempotency
    Given a CSI service
    And a basic block volume request name "gdtest-vol12" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_volforsnap"
    When I call CreateSnapshot
    And there are no errors
    Given a basic block volume request with volume content source with name "gdtest-vol13" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    And When I call DeleteAllCreatedVolumes
    Then there are no errors

  Scenario: Create a volume from snapshot that does not exist
    Given a CSI service
    And a basic block volume request name "gdtest-vol14" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_volforsnap"
    When I call CreateSnapshot
    And there are no errors
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    Given a basic block volume request with volume content source with name "gdtest-vol15" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    Then the error message should contain "snapshot not found"
    And When I call DeleteAllCreatedVolumes
    Then there are no errors

   Scenario: Create a volume from snapshot and passing an existing name for new volume 
    Given a CSI service
    And a basic block volume request name "gdtest-vol16" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_volforsnap"
    When I call CreateSnapshot
    And there are no errors
    Given a basic block volume request with volume content source with name "gdtest-vol16" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    Then the error message should contain "already exists"
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    And When I call DeleteAllCreatedVolumes
    Then there are no errors

  Scenario: Publish and unpublish a volume to host
    Given a CSI service
    And a basic block volume request name "gdtest-vol17" arrayId "apm00175023135" protocol "FC" size "5"
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
    And a basic block volume request name "gdtest-vol18" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume with host "host" readonly "true"
    Then the error message should contain "Readonly must be false"
    And when I call DeleteVolume
    Then there are no errors 

  Scenario: Publish and unpublish a volume to host without giving hostname
    Given a CSI service
    And a basic block volume request name "gdtest-vol19" arrayId "apm00175023135" protocol "FC" size "5"
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
    And a basic block volume request name "gdtest-vol20" arrayId "apm00175023135" protocol "FC" size "5"
    When I change volume capability accessmode
    When I call CreateVolume
    Then the error message should contain "not supported"
    And when I call PublishVolume
    Then the error message should contain "Access mode MULTI_NODE_SINGLE_WRITER is not supported"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Publish and unpublish a volume to host with incorrect hostname
    Given a CSI service
    And a basic block volume request name "gdtest-vol21" arrayId "apm00175023135" protocol "FC" size "5"
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
    And a basic block volume request name "gdtest-vol22" arrayId "apm00175023135" protocol "FC" size "5"
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
    And a basic block volume request name "gdtest-vol23" arrayId "apm00175023135" protocol "FC" size "5"
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
    And a basic block volume request name "gdtest-vol24" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call validate volume capabilities with protocol "FC" with same access mode
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Validate volume capabilities with different access mode
    Given a CSI service
    And a basic block volume request name "gdtest-vol25" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call validate volume capabilities with protocol "FC" with different access mode
    Then the error message should contain "Unsupported capability"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Validate volume capabilities with incorrect volume Id
    Given a CSI service
    When I call validate volume capabilities with protocol "FC" with volume ID "xyz"
    Then the error message should contain "Volume not found"

  Scenario: Controller get capabilities
    Given a CSI service
    When I call Controller Get Capabilities
    Then there are no errors

  Scenario: Controller expand volume
    Given a CSI service
    And a basic block volume request name "gdtest-vol26" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "3"
    Then there are no errors
    And a basic block volume request name "gdtest-vol26" arrayId "apm00175023135" protocol "FC" size "3"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Controller expand volume with same new size
    Given a CSI service
    And a basic block volume request name "gdtest-vol27" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "2"
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Controller expand volume with smaller new size
    Given a CSI service
    And a basic block volume request name "gdtest-vol28" arrayId "apm00175023135" protocol "FC" size "3"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "2"
    Then the error message should contain "requested new capacity smaller than existing capacity"
    And when I call DeleteVolume
    Then there are no errors
    
  Scenario: Controller expand volume with new size as 0
    Given a CSI service
    And a basic block volume request name "gdtest-vol29" arrayId "apm00175023135" protocol "FC" size "2"
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
    And a basic block volume request name "gdtest-vol30" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume fsType "ext4"
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
    And when I call NodeStageVolume fsType "ext4"
    Then the error message should contain "Unable to find volume Id"

  Scenario: Node publish volume with readonly as true
    Given a CSI service
    And a basic block volume request name "gdtest-vol31" arrayId "apm00175023135" protocol "FC" size "5"
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
    And a basic block volume request name "gdtest-vol32" arrayId "apm00175023135" protocol "FC" size "5"
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
    And a basic block volume request name "gdtest-vol33" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then the error message should contain "no such file or directory"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node unpublish when node publish never hapened
    Given a CSI service
    And a basic block volume request name "gdtest-vol34" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then the error message should contain "no such file or directory"
    And when I call NodeUnPublishVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node Get Capabilities
    Given a CSI service
    And When I call NodeGetCapabilities
    Then there are no errors

  Scenario: Node publish volume without volume capabilities
    Given a CSI service
    And a basic block volume request name "gdtest-vol35" arrayId "apm00175023135" protocol "FC" size "5"
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

  Scenario: Node stage, publish, unpublish and unstage volume idempotency
    Given a CSI service
    And a basic block volume request name "gdtest-vol36" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume fsType "ext4"
    And there are no errors
    And when I call NodeStageVolume fsType "ext4"
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

  Scenario: Node stage, publish, unpublish and unstage volume for iSCSI
    Given a CSI service
    And a basic block volume request name "gdtest-vol37" arrayId "apm00175023135" protocol "iSCSI" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume fsType "ext4"
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

  Scenario: Node stage, publish, unpublish and unstage volume for NFS
    Given a CSI service
    And a basic block volume request name "gdtest-vol38" arrayId "apm00175023135" protocol "NFS" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume fsType ""
    And there are no errors
    And when I call NodePublishVolume fsType "" readonly "false"
    Then there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors