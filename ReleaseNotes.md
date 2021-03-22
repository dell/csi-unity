## Release Notes - CSI Unity v1.5.0

## New Features/Changes
- Added support for Kubernetes v1.20
- Added support for OpenShift 4.7 with RHEL and CoreOS worker nodes
- Changed driver base image to UBI 8.x
- Added support for Red Hat Enterprise Linux (RHEL) 8.3
- Added support for SLES 15SP2
- Added support for Docker - UCP 3.3.5

## Fixed Issues
- Raw-Block volume with accessmode RWX can be mounted to multiple nodes.
- PVC creation fails on a cluster with only NFS protocol fixed by adding topology keys for NFS protocol.

## Known Issues

| Issue                                                        | Workaround                                                   |
| ------------------------------------------------------------ | ------------------------------------------------------------ |
| Topology related node labels are not removed automatically.  | Currently, when the driver is uninstalled, topology related node labels are not getting removed automatically. There is an open issue in the Kubernetes to fix this. Until the fix is released we need to manually remove the node labels mentioned here https://github.com/dell/csi-unity#known-issues (Point 1) |
| Dynamic array detection will not work in Topology based environment | Whenever a new array is added or removed, then the driver should be restarted with the below command only if the **topology-based storage classes** are used. Otherwise, the driver will automatically detect the newly added or removed arrays https://github.com/dell//csi-unity#known-issues (Point 2) |
| If source PVC is deleted when cloned PVC exists, then source PVC will be deleted in cluster but on array it will still be present and marked for deletion. | All the cloned pvc should be deleted in order to delete the source pvc from array. |
| PVC creation fails on a fresh cluster with **iSCSI** and **NFS** protocols alone enabled with error **failed to provision volume with StorageClass "unity-iscsi": error generating accessibility requirements: no available topology found**. | This is because iSCSI initiator login takes longer than the node pod startup time. This can be overcome by bouncing the node pods in the cluster using the below command the driver pods with `kubectl get pods -n unity --no-headers=true` |
| On deleting pods sometimes the corresponding 'volumeattachment' will not get removed. This issue is intermittent and happens with one specific protocol (FC, iSCSI or NFS) based storageclasses. This issue occurs across kubernetes versions 1.18 and 1.19 and both versions of OpenShift (4.5/4.6).| On deleting the stale volumeattachment manually, Controller Unpublish gets invoked and then the corresponding PVCs can be deleted.|
|The flag allowRWOMultiPodAccess:false is not applicable for Raw Block volumes and the driver allows creation of multiple pods on the same node with RWO access mode.| Workaround not necessary as this issue doesn't block any usecase |

