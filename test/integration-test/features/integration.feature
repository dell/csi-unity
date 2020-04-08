Feature: CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.

  Scenario: Controller get capabilities and get pool capacity with id and create, validate capabilities and delete basic volume
    Given a CSI service
    When I call Controller Get Capabilities
    Then there are no errors
    When I call Get Capacity with storage pool "id"
    Then there are no errors
    And a basic block volume request "integration1" "8"
    When I call CreateVolume
    Then there are no errors
    When I call validate volume capabilities with same access mode
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Get pool capacity with id and create, validate capabilities and delete basic volume
    Given a CSI service
    When I call Get Capacity with storage pool "id"
    Then there are no errors
    And a basic block volume request "integration1" "8"
    When I call CreateVolume
    Then there are no errors
    When I call validate volume capabilities with different access mode
    Then the error message should contain "Unsupported capability"
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create, expand and delete basic volume
    Given a CSI service
    And a basic block volume request "expand1" "2"
    When I call CreateVolume
    Then there are no errors
    When I call Controller Expand Volume "3"
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

  Scenario: Idempotent create and delete basic volume
    Given a CSI service
    And a basic block volume request "integration2" "8"
    When I call CreateVolume
    And I call CreateVolume
    And when I call DeleteVolume
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create a volume from snapshot of thin volume
    Given a CSI service
    And a basic block volume request "volforsnap" "2"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_volforsnap"
    When I call CreateSnapshot
    And there are no errors
    Given a basic block volume request with volume content source with name "volfromsnap" size "2"
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
    And a basic block volume request "integration5" "8"
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
    And a basic block volume request "integration4" "264000"
    When I call CreateVolume
    Then the error message should contain "The system could not create the LUNs because specified size is too big."
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create and delete basic 96G volume
    Given a CSI service
    And a basic block volume request "integration3" "96"
    When I call CreateVolume
    Then there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create volume, create snapshot, list volume, list snapshot, delete snapshot and delete volume
    Given a CSI service
    And a basic block volume request "integration1" "8"
    When I call CreateVolume
    And there are no errors
    Given a create snapshot request "snap_integration1"
    When I call CreateSnapshot
    And there are no errors
    Given a list volumes request with maxEntries "5" startToken "" 
    When I call list volumes
    Then there are no errors
    Given a list snapshots request with startToken "" maxEntries "10" sourceVolumeId "" snapshotId ""
    When I call list snapshots
    Then there are no errors
    Given a delete snapshot request
    And I call DeleteSnapshot
    And there are no errors
    And when I call DeleteVolume
    And there are no errors

  Scenario: Create volume, idempotent create snapshot, idempotent delete snapshot delete volume
    Given a CSI service
    And a basic block volume request "integration1" "8"
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
# BUG -> CSIUNITY-350
#    And when I call NodeUnPublishVolume
#    Then there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call NodeUnstageVolume
    And there are no errors
    And when I call UnpublishVolume
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors
	
  Scenario Outline: Scalability test to create volumes, publish, unpublish, delete volumes in parallel
    Given a CSI service
    When I create <numberOfVolumes> volumes in parallel
    And there are no errors
    And I publish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I unpublish <numberOfVolumes> volumes in parallel
    And there are no errors
    And when I delete <numberOfVolumes> volumes in parallel
    Then there are no errors

    Examples:
    | numberOfVolumes |
    | 1               |
    | 2               |
#    | 5               |
#    | 10              |
#    | 20              |
#    | 50              |
#    | 100             |
#    | 200             |

# Bug CSIUNITY-357
#  Scenario Outline: Idempotent create volumes, publish, unpublish, delete volumes in parallel
#    Given a CSI service
#    When I create <numberOfVolumes> volumes in parallel
#    And there are no errors
#    When I create <numberOfVolumes> volumes in parallel
#    And there are no errors
#    And I publish <numberOfVolumes> volumes in parallel 
#    And there are no errors
#    And I publish <numberOfVolumes> volumes in parallel
#    And there are no errors
#    And I unpublish <numberOfVolumes> volumes in parallel
#    And there are no errors
#    And I unpublish <numberOfVolumes> volumes in parallel
#    And there are no errors
#    And when I delete <numberOfVolumes> volumes in parallel
#    And there are no errors
#    And when I delete <numberOfVolumes> volumes in parallel
#    Then there are no errors

#    Examples:
#    | numberOfVolumes |
#    | 1               |
#    | 5               |
#    | 20               |
