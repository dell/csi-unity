Feature: CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.

  Scenario: Controller get capabilities, create, validate capabilities and delete basic volume
    Given a CSI service
    When I call Controller Get Capabilities
    Then there are no errors
    And a basic block volume request name "gditest-vol1" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    Then there are no errors
    When I call validate volume capabilities with protocol "FC" with same access mode
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create, validate capabilities and delete basic volume
    Given a CSI service
    And a basic block volume request name "gditest-vol2" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    Then there are no errors
    When I call validate volume capabilities with protocol "FC" with different access mode
    Then the error message should contain "Unsupported capability"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create, expand and delete basic volume
    Given a CSI service
    And a basic block volume request name "gditest-vol3" arrayId "apm00175023135" protocol "FC" size "2"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "3"
    Then there are no errors
    And a basic block volume request name "gditest-vol3" arrayId "apm00175023135" protocol "FC" size "3"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Controller expand volume with smaller new size
    Given a CSI service
    And a basic block volume request name "gditest-vol4" arrayId "apm00175023135" protocol "FC" size "3"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "2"
    Then the error message should contain "requested new capacity smaller than existing capacity"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Idempotent create and delete basic volume
    Given a CSI service
    And a basic block volume request name "gditest-vol5" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    And I call CreateVolume
    And when I call DeleteVolume
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume from snapshot of thin volume
    Given a CSI service
    And a basic block volume request name "gditest-vol6" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_volforsnap"
    When I call CreateSnapshot
    And there are no errors
    Given a basic block volume request with volume content source with name "gditest-vol7" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors
    Given a delete snapshot request
    When I call DeleteSnapshot
    Then there are no errors
    And When I call DeleteAllCreatedVolumes
    Then there are no errors

  Scenario: Create, publish, unpublish, and delete basic volume with idempotency check for publish and unpublish
    Given a CSI service
    And a basic block volume request name "gditest-vol8" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call PublishVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create and delete basic 264000G volume
    Given a CSI service
    And a basic block volume request name "gditest-vol9" arrayId "apm00175023135" protocol "FC" size "264000"
    When I call CreateVolume
    Then the error message should contain "The system could not create the LUNs because specified size is too big."
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create and delete basic 96G volume
    Given a CSI service
    And a basic block volume request name "gditest-vol10" arrayId "apm00175023135" protocol "FC" size "96"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create volume, create snapshot, delete snapshot and delete volume
    Given a CSI service
    And a basic block volume request name "gditest-vol11" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_integration1"
    When I call CreateSnapshot
    And there are no errors
    Given a delete snapshot request
    And I call DeleteSnapshot
    And there are no errors
    And when I call DeleteVolume
    And there are no errors

  Scenario: Create volume, idempotent create snapshot, idempotent delete snapshot delete volume
    Given a CSI service
    And a basic block volume request name "gditest-vol12" arrayId "apm00175023135" protocol "FC" size "5"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_integration1"
    When I call CreateSnapshot
    And there are no errors
    Given a create snapshot request "snap_integration1"
    When I call CreateSnapshot
    And there are no errors
    Given a delete snapshot request
    And I call DeleteSnapshot
    And there are no errors
    Given a delete snapshot request
    And I call DeleteSnapshot
    And there are no errors
    And when I call DeleteVolume
    And there are no errors

  Scenario: Node stage, publish, unpublish and unstage volume with idempotency
    Given a CSI service
    And a basic block volume request name "gditest-vol13" arrayId "apm00175023135" protocol "FC" size "5"
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

  Scenario: Node stage, publish, unpublish and unstage volume for iSCSI
    Given a CSI service
    And a basic block volume request name "gditest-vol14" arrayId "apm00175023135" protocol "iSCSI" size "5"
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
    And a basic block volume request name "gditest-vol15" arrayId "apm00175023135" protocol "NFS" size "5"
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