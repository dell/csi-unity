Feature: CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.

  Scenario: Add node info to array
    Given a CSI service with node
    Given a CSI service with node
    Given a CSI service with node topology
    Then there are no errors

  Scenario: Create and Delete snapshot successfully
    Given a CSI service
    And a basic block volume request name "gdtest-vol1" arrayId "Array1-Id" protocol "FC" size "2"
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
    And a basic block volume request name "gdtest-vol2" arrayId "Array1-Id" protocol "FC" size "2"
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
    And a basic block volume request name "gdtest-vol3" arrayId "Array1-Id" protocol "FC" size "2"
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
    And a basic block volume request name "gdtest-vol4" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create and delete basic volume with default protocol successfully
    Given a CSI service
    And a basic block volume request name "gdtest-vol39" arrayId "Array1-Id" protocol "" size "2"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create volume with 0 size
    Given a CSI service
    And a basic block volume request name "gdtest-zerosizevol" arrayId "Array1-Id" protocol "" size "0"
    When I call CreateVolume
    Then the error message should contain "RequiredBytes should be greater then 0"

  Scenario: Create and delete existing filesystem with same and different size
    Given a CSI service
    And a basic block volume request name "gdtest-vol40" arrayId "Array1-Id" protocol "NFS" size "3"
    When I call CreateVolume
    Then there are no errors
    Given a basic block volume request name "gdtest-vol40" arrayId "Array1-Id" protocol "NFS" size "5"
    When I call CreateVolume
    Then the error message should contain "already exists"
    And a basic block volume request name "gdtest-vol40" arrayId "Array1-Id" protocol "NFS" size "3"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors
  
  Scenario: Create an existing volume with different and same size
    Given a CSI service
    And a basic block volume request name "gdtest-vol6" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    And a basic block volume request name "gdtest-vol6" arrayId "Array1-Id" protocol "FC" size "5"
    When I call CreateVolume
    Then the error message should contain "'Volume name' already exists and size is different"
    And a basic block volume request name "gdtest-vol6" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without thinProvisioned parameter
    Given a CSI service
    And a basic block volume request with volumeName "gdtest-vol7" arrayId "Array1-Id" protocol "FC" size "2" storagepool "id" thinProvisioned "" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without isCompressionEnabled parameter
    Given a CSI service
    And a basic block volume request with volumeName "gdtest-vol8" arrayId "Array1-Id" protocol "FC" size "2" storagepool "id" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without isDataReductionEnabled parameter
    Given a CSI service
    And a basic block volume request with volumeName "gdtest-vol9" arrayId "Array1-Id" protocol "FC" size "2" storagepool "id" thinProvisioned "true" isDataReductionEnabled "" tieringPolicy "0"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume without isDataReductionEnabled parameter
    Given a CSI service
    And a basic block volume request with volumeName "gdtest-vol10" arrayId "Array1-Id" protocol "FC" size "2" storagepool "id" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy ""
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume with incorrect storage_pool
    Given a CSI service
    And a basic block volume request with volumeName "gdtest-vol11" arrayId "Array1-Id" protocol "FC" size "2" storagepool "abcd" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then the error message should contain "Unable to get PoolID"

  Scenario: Create a volume without volume name
    Given a CSI service
    And a basic block volume request with volumeName "" arrayId "Array1-Id" protocol "FC" size "2" storagepool "abcd" thinProvisioned "true" isDataReductionEnabled "false" tieringPolicy "0"
    When I call CreateVolume
    Then the error message should contain "required: Name"

  Scenario: Create a volume from snapshot of thin volume with idempotency
    Given a CSI service
    And a basic block volume request name "gdtest-vol12" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_volforsnap"
    When I call CreateSnapshot
    And there are no errors
    Given a basic block volume request with volume content source as snapshot with name "gdtest-vol13" arrayId "Array1-Id" protocol "FC" size "2"
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

  Scenario: Create a volume from snapshot of filesystem and create snapshot of cloned filesystem
    Given a CSI service
    And a basic block volume request name "gdtest-fssource" arrayId "Array1-Id" protocol "NFS" size "5"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_fs-gdtest-fssource"
    When I call CreateSnapshot
    And there are no errors
    Given a basic block volume request with volume content source as snapshot with name "gdtest-fsclone" arrayId "Array1-Id" protocol "NFS" size "5"
    When I call CreateVolume
    And there are no errors
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    Given a create snapshot request "snap_fs-gdtest-fssource-1"
    When I call CreateSnapshot
    And there are no errors
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    And When I call DeleteAllCreatedVolumes
    Then there are no errors

  Scenario: Create a volume from snaphot for NFS protocol with incompatible size
    Given a CSI service
    And a basic block volume request name "gdtest-fssource-1" arrayId "Array1-Id" protocol "NFS" size "5"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_fs-gdtest-fssource-2"
    When I call CreateSnapshot
    And there are no errors
    Given a basic block volume request with volume content source as snapshot with name "gdtest-fsclone" arrayId "Array1-Id" protocol "NFS" size "8"
    When I call CreateVolume
    Then the error message should contain "size"
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    And When I call DeleteAllCreatedVolumes
    Then there are no errors

  Scenario: Create a volume from snapshot that does not exist
    Given a CSI service
    And a basic block volume request name "gdtest-vol14" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_volforsnap"
    When I call CreateSnapshot
    And there are no errors
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    Given a basic block volume request with volume content source as snapshot with name "gdtest-vol15" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    Then the error message should contain "snapshot not found"
    And When I call DeleteAllCreatedVolumes
    Then there are no errors

  Scenario: Create a volume from snapshot and passing an existing name for new volume 
    Given a CSI service
    And a basic block volume request name "gdtest-vol16" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_volforsnap"
    When I call CreateSnapshot
    And there are no errors
    Given a basic block volume request with volume content source as snapshot with name "gdtest-vol16" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    Then the error message should contain "already exists"
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    And When I call DeleteAllCreatedVolumes
    Then there are no errors

  Scenario: Clone a volume successfully with idempotency
    Given a CSI service
    And a basic block volume request name "gdtest-sourcevol1" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    And there are no errors
    Given a basic block volume request with volume content source as volume with name "gdtest-clonevol" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    Given a basic block volume request name "gdtest-sourcevol1" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    And there are no errors
    Given a basic block volume request with volume content source as volume with name "gdtest-clonevol" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    And When I call DeleteAllCreatedVolumes
    Then there are no errors

  Scenario: Publish and unpublish a volume to host
    Given a CSI service
    And a basic block volume request name "gdtest-vol17" arrayId "Array1-Id" protocol "FC" size "5"
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
    And a basic block volume request name "gdtest-vol18" arrayId "Array1-Id" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume with host "host" readonly "true"
    Then the error message should contain "Readonly must be false"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Publish a volume to host with VolumeCapability_AccessMode other than SINGLE_NODE_WRITER
    Given a CSI service
    And a basic block volume request name "gdtest-vol20" arrayId "Array1-Id" protocol "FC" size "5"
    When I change volume capability accessmode
    When I call CreateVolume
    Then the error message should contain "not supported"
    And when I call PublishVolume
    Then the error message should contain "Access mode MULTI_NODE_SINGLE_WRITER is not supported"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Publish and unpublish a volume to host without giving volume id
    Given a CSI service
    And when I call PublishVolume with volumeId ""
    Then the error message should contain "required: VolumeID"
    When I call UnpublishVolume with volumeId ""
    Then the error message should contain "required: VolumeID"

  Scenario: Publish and unpublish a volume to host with deleted volume
    Given a CSI service
    And a basic block volume request name "gdtest-vol41" arrayId "Array1-Id" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors
    And when I call PublishVolume
    Then the error message should contain "Find volume Failed"
    And when I call UnpublishVolume
    Then there are no errors 

  Scenario: Publish and unpublish a volume to host with deleted filesystem
    Given a CSI service
    And a basic block volume request name "gdtest-vol42" arrayId "Array1-Id" protocol "NFS" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors
    And when I call PublishVolume
    Then the error message should contain "failed"
    And when I call UnpublishVolume
    Then there are no errors 

  Scenario: Publish and unpublish volume idempotency
    Given a CSI service
    And a basic block volume request name "gdtest-vol22" arrayId "Array1-Id" protocol "FC" size "5"
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

  Scenario: Validate volume capabilities with same access mode
    Given a CSI service
    And a basic block volume request name "gdtest-vol24" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call validate volume capabilities with protocol "FC" with same access mode
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Validate volume capabilities with different access mode
    Given a CSI service
    And a basic block volume request name "gdtest-vol25" arrayId "Array1-Id" protocol "FC" size "2"
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
    And a basic block volume request name "gdtest-vol26" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "3"
    Then there are no errors
    And a basic block volume request name "gdtest-vol26" arrayId "Array1-Id" protocol "FC" size "3"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Controller expand volume for NFS protocol
    Given a CSI service
    And a basic block volume request name "gdtest-vol26" arrayId "Array1-Id" protocol "NFS" size "5"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "8"
    Then there are no errors
    And a basic block volume request name "gdtest-vol26" arrayId "Array1-Id" protocol "NFS" size "8"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Controller expand volume for cloned NFS volume
    Given a CSI service
    And a basic block volume request name "gdtest-snapofclonevol" arrayId "Array1-Id" protocol "NFS" size "5"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_volforsnap_1"
    When I call CreateSnapshot
    And there are no errors
    Given a basic block volume request with volume content source as snapshot with name "gdtest-vol13_1" arrayId "Array1-Id" protocol "NFS" size "5"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "8"
    Then the error message should contain "snapshot"
    And when I call DeleteVolume
    Then there are no errors
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    And When I call DeleteAllCreatedVolumes
    Then there are no errors

  Scenario: Controller expand volume with same new size
    Given a CSI service
    And a basic block volume request name "gdtest-vol27" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "2"
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Controller expand volume with smaller new size
    Given a CSI service
    And a basic block volume request name "gdtest-vol28" arrayId "Array1-Id" protocol "FC" size "3"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "2"
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors
    
  Scenario: Controller expand volume with new size as 0
    Given a CSI service
    And a basic block volume request name "gdtest-vol29" arrayId "Array1-Id" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "0"
    Then the error message should contain "Required bytes can not be 0 or less"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Controller expand volume with volume that does not exist
    Given a CSI service
    When I call Controller Expand Volume "2" with volume "abcd"
    Then the error message should contain "Unable to find volume"

  Scenario: Controller expand volume without giving volume
    Given a CSI service
    When I call Controller Expand Volume "2" with volume ""
    Then the error message should contain "required"

  Scenario: Node stage, publish, unpublish and unstage volume
    Given a CSI service
    And a basic block volume request name "gdtest-vol30" arrayId "Array1-Id" protocol "FC" size "5"
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

  Scenario: Node publish volume with readonly as true
    Given a CSI service
    And a basic block volume request name "gdtest-vol31" arrayId "Array1-Id" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume fsType "ext4"
    And there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "true"
    Then there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node publish volume without volume id
    Given a CSI service
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then the error message should contain "required: VolumeID"

  Scenario: Node publish volume without controller publish volume
    Given a CSI service
    And a basic block volume request name "gdtest-vol33" arrayId "Array1-Id" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then the error message should contain "no such file or directory"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node unpublish when node publish never hapened
    Given a CSI service
    And a basic block volume request name "gdtest-vol34" arrayId "Array1-Id" protocol "FC" size "5"
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
    And a basic block volume request name "gdtest-vol35" arrayId "Array1-Id" protocol "FC" size "5"
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
    And a basic block volume request name "gdtest-vol36" arrayId "Array1-Id" protocol "FC" size "5"
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

  Scenario: NodeGetInfo
    Given a CSI service
    And When I call NodeGetInfo
    Then the error message should contain "not added"

  Scenario: Node stage, publish, unpublish and unstage volume for iSCSI
    Given a CSI service
    And a basic block volume request name "gdtest-vol37" arrayId "Array1-Id" protocol "iSCSI" size "5"
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
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors

  Scenario: Node stage, publish, unpublish and unstage volume for NFS with idempotency
    Given a CSI service
    And a basic block volume request name "gdtest-vol38" arrayId "Array1-Id" protocol "NFS" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume fsType ""
    And there are no errors
    And when I call NodeStageVolume fsType ""
    And there are no errors
    And when I call NodePublishVolume fsType "" readonly "false"
    Then there are no errors
    And when I call NodePublishVolume fsType "" readonly "false"
    Then there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node stage, publish, unpublish and unstage volume for NFS with accessmode "ROX"
    Given a CSI service
    And a basic filesystem request name "gdtest-vol43" arrayId "Array1-Id" protocol "NFS" accessMode "ROX" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume fsType ""
    And there are no errors
    And when I call NodeStageVolume fsType ""
    And there are no errors
    And when I call NodePublishVolume fsType "" readonly "false"
    Then there are no errors
    And when I call NodePublishVolume fsType "" readonly "false"
    Then there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node stage, publish, unpublish and unstage volume for NFS with accessmode "RWX"
    Given a CSI service
    And a basic filesystem request name "gdtest-vol44" arrayId "Array1-Id" protocol "NFS" accessMode "RWX" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume fsType ""
    And there are no errors
    And when I call NodeStageVolume fsType ""
    And there are no errors
    And when I call NodePublishVolume fsType "" readonly "false"
    Then there are no errors
    And when I call NodePublishVolume fsType "" readonly "false"
    Then there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors

  Scenario: Ephemeral Inline FC Volume
    Given a CSI service
    And when I call EphemeralNodePublishVolume with volName "gdtest-ephemeralvolfc" fsType "ext4" arrayId "Array1-Id" am "RWO" size "5 Gi" storagePool "id" protocol "FC" nasServer "nas_1" thinProvision "true" dataReduction "true"
    Then there are no errors
    And when I call NodeUnPublishVolume
    Then there are no errors

  Scenario: Ephemeral Inline iSCSI Volume
    Given a CSI service
    And when I call EphemeralNodePublishVolume with volName "gdtest-ephemeralvoliscsi" fsType "ext4" arrayId "Array1-Id" am "RWO" size "5 Gi" storagePool "id" protocol "iSCSI" nasServer "nas_1" thinProvision "true" dataReduction "true"
    Then there are no errors
    And when I call NodeUnPublishVolume
    Then there are no errors

  Scenario: Ephemeral Inline NFS Volume
    Given a CSI service
    And when I call EphemeralNodePublishVolume with volName "gdtest-ephemeralvolnfs" fsType "ext4" arrayId "Array1-Id" am "RWO" size "5 Gi" storagePool "id" protocol "NFS" nasServer "nas_1" thinProvision "true" dataReduction "true"
    Then there are no errors
    And when I call NodeUnPublishVolume
    Then there are no errors

  Scenario: Node stage, publish, expand, unpublish and unstage raw block volume for iSCSI
    Given a CSI service
    And a basic raw block volume request name "gdtest-vol45" arrayId "Array1-Id" protocol "iSCSI" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume fsType ""
    And there are no errors
    And when I call NodePublishVolume fsType "" readonly "false"
    And there are no errors
    When I call Controller Expand Volume "8"
    And there are no errors
    And when I call Node Expand Volume
    And there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Node publish to different target paths with AllowRWOMultiPodAccess true
    Given a CSI service
    And a basic block volume request name "gdtest-vol56" arrayId "Array1-Id" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call NodeStageVolume fsType "ext4"
    And there are no errors
    And when I call NodePublishVolume fsType "ext4" readonly "false"
    Then there are no errors
    And when I call NodePublishVolume targetpath "/root/gdmounts/publishmountpath/mount2" fsType "ext4"
    Then there are no errors
    And when I call NodeUnPublishVolume targetpath "/root/gdmounts/publishmountpath/mount2"
    And there are no errors
    And when I call NodeUnPublishVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors