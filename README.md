# Unity CSI

[![Go Report Card](https://goreportcard.com/badge/github.com/dell/csi-unity)](https://goreportcard.com/report/github.com/dell/csi-unity)
[![License](https://img.shields.io/github/license/dell/csi-unity)](https://github.com/dell/csi-unity/blob/master/LICENSE)
[![Docker](https://img.shields.io/docker/pulls/dellemc/csi-unity.svg?logo=docker)](https://hub.docker.com/r/dellemc/csi-unity)
[![Last Release](https://img.shields.io/github/v/release/dell/csi-unity?label=latest&style=flat-square)](https://github.com/dell/csi-unity/releases)

This repo contains [Container Storage Interface(CSI)](<https://github.com/container-storage-interface/>) Unity CSI driver for DellEMC.

## Overview

Unity CSI plugins implement an interface between CSI enabled Container Orchestrator(CO) and Unity Storage Array. It allows static and dynamic provisioning of Unity volumes and attaching them to workloads.

## Support

The CSI Driver for Dell EMC Unity image, which is the built driver code, is available on Dockerhub and is officially supported by Dell EMC.
The source code for CSI Driver for Dell EMC Unity available on Github is unsupported and provided solely under the terms of the license attached to the source code. For clarity, Dell EMC does not provide support for any source code modifications.
For any CSI driver issues, questions or feedback, join the [Dell EMC Container community](<https://www.dell.com/community/Containers/bd-p/Containers/>)

## Introduction
The CSI Driver For Dell EMC Unity conforms to CSI spec 1.1
   * Support for Kubernetes v1.17, v1.18 and v1.19
   * Will add support for other orchestrators over time
   * The CSI specification is documented here: https://github.com/container-storage-interface/spec/tree/release-1.1. The driver uses CSI v1.1.`

## CSI Driver For Dell EMC Unity Capabilities

| Capability | Supported | Not supported |
|------------|-----------| --------------|
|Provisioning | Persistent volumes creation, deletion, mounting, unmounting, expansion | |
|Export, Mount | Mount volume as file system | Raw volumes, Topology|
|Data protection | Creation of snapshots, Create volume from snapshots, Volume Cloning | |
|Types of volumes | Static, Dynamic| |
|Access mode | RWO(FC/iSCSI), RWO/RWX/ROX(NFS) | RWX/ROX(FC/iSCSI)|
|Kubernetes | v1.17, v1.18, v1.19 | V1.16 or previous versions|
|Docker EE | v3.1 | Other versions|
|Installer | Helm v3.x, Operator | |
|OpenShift | v4.3 (except snapshot), v4.4 | Other versions |
|OS | RHEL 7.6, RHEL 7.7, RHEL 7.8, CentOS 7.6, CentOS 7.7, CentOS 7.8 | Ubuntu, other Linux variants|
|Unity | OE 5.0.0, 5.0.1, 5.0.2, 5.0.3 | Previous versions and Later versions|
|Protocol | FC, iSCSI, NFS |  |

## Installation overview

Installation in a Kubernetes cluster should be done using the scripts within the `dell-csi-helm-installer` directory. 

For more information, consult the [README.md](dell-csi-helm-installer/README.md)

The controller section of the Helm chart installs the following components in a Stateful Set:

* CSI Driver for Unity
* Kubernetes Provisioner, which provisions the provisioning volumes
* Kubernetes Attacher, which attaches the volumes to the containers
* Kubernetes Snapshotter, which provides snapshot support
* Kubernetes Resizer, which provides volume expansion support

The node section of the Helm chart installs the following component in a Daemon Set:

* CSI Driver for Unity
* Kubernetes Registrar, which handles the driver registration

### Prerequisites

Before you install CSI Driver for Unity, verify the requirements that are mentioned in this topic are installed and configured.

#### Requirements

* Install Kubernetes
* Configure Docker service
* Install Helm v3
* To use FC protocol, host must be zoned with Unity array
* To use iSCSI and NFS protocol, iSCSI initiator and NFS utility packages need to be installed

## Configure Docker service

The mount propagation in Docker must be configured on all Kubernetes nodes before installing CSI Driver for Unity.

### Procedure

1. Edit the service section of */etc/systemd/system/multi-user.target.wants/docker.service* file as follows:

    ```
    [Service]
    ...
    MountFlags=shared
    ```
    
2. Restart the Docker service with systemctl daemon-reload and

    ```
    systemctl daemon-reload
    systemctl restart docker
    ```

## Install CSI Driver for Unity

Install CSI Driver for Unity using this procedure.

*Before you begin*
 * You must have the downloaded files, including the Helm chart from the source [git repository](https://github.com/dell/csi-unity), ready for this procedure.
 * In the top-level dell-csi-helm-installer directory, there should be two scripts, *csi-install.sh* and *csi-uninstall.sh*. These scripts handle some of the pre and post operations that cannot be performed in the helm chart, such as creating Custom Resource Definitions (CRDs), if needed.
 * Make sure "unity" namespace exists in kubernetes cluster. Use `kubectl create namespace unity` command to create the namespace, if the namespace is not present.
   
   
Procedure

1. Collect information from the Unity Systems like Unique ArrayId, IP address, username  and password. Make a note of the value for these parameters as they must be entered in the secret.json and myvalues.yaml file.

2. Copy the csi-unity/values.yaml into a file named myvalues.yaml in the same directory of csi-install.sh, to customize settings for installation.

3. Edit myvalues.yaml to set the following parameters for your installation:
    
    The following table lists the primary configurable parameters of the Unity driver chart and their default values. More detailed information can be found in the [`values.yaml`](helm/csi-unity/values.yaml) file in this repository.
    
    | Parameter | Description | Required | Default |
    | --------- | ----------- | -------- |-------- |
    | certSecretCount | Represents number of certificate secrets, which user is going to create for ssl authentication. (unity-cert-0..unity-cert-n). Minimum value should be 1 | false | 1 |
    | syncNodeInfoInterval | Time interval to add node info to array. Default 15 minutes. Minimum value should be 1 minute | false | 15 |
    | volumeNamePrefix | String to prepend to any volumes created by the driver | false | csivol |
    | snapNamePrefix | String to prepend to any snapshot created by the driver | false | csi-snap |
    | csiDebug |  To set the debug log policy for CSI driver | false | "false" |
    | imagePullPolicy |  The default pull policy is IfNotPresent which causes the Kubelet to skip pulling an image if it already exists. | false | IfNotPresent |
    | ***Storage Array List*** | Following parameters is a list of parameters to provide multiple storage arrays |
    | storageArrayList[i].name | Name of the storage class to be defined. A suffix of ArrayId and protocol will be added to the name. No suffix will be added to default array. | false | unity |
    | storageArrayList[i].isDefaultArray | To handle the existing volumes created in csi-unity v1.0, 1.1 and 1.1.0.1. The user needs to provide "isDefaultArray": true in secret.json. This entry should be present only for one array and that array will be marked default for existing volumes. | false | "false" |
    | ***Storage Class parameters*** | Following parameters are not present in values.yaml |
    | storageArrayList[i].storageClass.storagePool | Unity Storage Pool CLI ID to use with in the Kubernetes storage class | true | - |
    | storageArrayList[i].storageClass.thinProvisioned | To set volume thinProvisioned | false | "true" |    
    | storageArrayList[i].storageClass.isDataReductionEnabled | To set volume data reduction | false | "false" |
    | storageArrayList[i].storageClass.volumeTieringPolicy | To set volume tiering policy | false | 0 |
    | storageArrayList[i].storageClass.FsType | Block volume related parameter. To set File system type. Possible values are ext3,ext4,xfs. Supported for FC/iSCSI protocol only. | false | ext4 |
    | storageArrayList[i].storageClass.hostIOLimitName | Block volume related parameter.  To set unity host IO limit. Supported for FC/iSCSI protocol only. | false | "" |    
    | storageArrayList[i].storageClass.nasServer | NFS related parameter. NAS Server CLI ID for filesystem creation. | true | "" |
    | storageArrayList[i].storageClass.hostIoSize | NFS related parameter. To set filesystem host IO Size. | false | "8192" |
    | storageArrayList[i].storageClass.reclaimPolicy | What should happen when a volume is removed | false | Delete |
    | ***Snapshot Class parameters*** | Following parameters are not present in values.yaml  |
    | storageArrayList[i] .snapshotClass.retentionDuration | TO set snapshot retention duration. Format:"1:23:52:50" (number of days:hours:minutes:sec)| false | "" |
    
   **Note**: User should provide all boolean values with double quotes. This applicable only for myvalues.yaml. Ex: "true"/"false"
   
   Example *myvalues.yaml*
    
    ```
    csiDebug: "true"
    volumeNamePrefix : csivol
    snapNamePrefix: csi-snap
    imagePullPolicy: Always
    certSecretCount: 1
    syncNodeInfoInterval: 5
    storageClassProtocols:
       - protocol: "FC"
       - protocol: "iSCSI"
       - protocol: "NFS"
    storageArrayList:
       - name: "APM00******1"
         isDefaultArray: "true"
         storageClass:
           storagePool: pool_1
           FsType: ext4
           nasServer: "nas_1"
           thinProvisioned: "true"
           isDataReductionEnabled: true
           hostIOLimitName: "value_from_array"
           tieringPolicy: "2"
         snapshotClass:
           retentionDuration: "2:2:23:45"
       - name: "APM001******2"
         storageClass:
           storagePool: pool_1
           reclaimPolicy: Delete
           hostIoSize: "8192"
           nasServer: "nasserver_2"
    ```

4. Create an empty secret by navigating to helm folder that contains emptysecret.yaml file and running the kubectl create -f emptysecret.yaml command.

5. Prepare the secret.json for driver configuration.
    The following table lists driver configuration parameters for multiple storage arrays.
    
    | Parameter | Description | Required | Default |
    | --------- | ----------- | -------- |-------- |   
    | username | Username for accessing unity system  | true | - |
    | password | Password for accessing unity system  | true | - |
    | restGateway | REST API gateway HTTPS endpoint Unity system| true | - |
    | arrayId | ArrayID for unity system | true | - |
    | insecure | "unityInsecure" determines if the driver is going to validate unisphere certs while connecting to the Unisphere REST API interface If it is set to false, then a secret unity-certs has to be created with a X.509 certificate of CA which signed the Unisphere certificate | true | true |
    | isDefaultArray | An array having isDefaultArray=true is for backward compatibility. This parameter should occur once in the list. | false | false |
    
    Ex: secret.json
    ```json5
       {
         "storageArrayList": [
           {
             "username": "user",
             "password": "password",
             "restGateway": "https://10.1.1.1",
             "arrayId": "APM00******1",
             "insecure": true,
             "isDefaultArray": true
           },
           {
             "username": "user",
             "password": "password",
             "restGateway": "https://10.1.1.2",
             "arrayId": "APM00******2",
             "insecure": true
           }
         ]
       }
    ```
    `kubectl create secret generic unity-creds -n unity --from-file=config=secret.json`

    Use the following command to replace or update the secret
    
    `kubectl create secret generic unity-creds -n unity --from-file=config=secret.json -o yaml --dry-run | kubectl replace -f -`
    
    **Note**: The user needs to validate the JSON syntax and array related key/values while replacing the unity-creds secret.
    The driver will continue to use previous values in case of an error found in the JSON file.
    
    **Note**: "isDefaultArray" parameter in values.yaml and secret.json should match each other. 

6. Setup for snapshots
         
   The Kubernetes Volume Snapshot feature is now beta in Kubernetes v1.17.
           
   * The following section summarizes the changes in the **[beta](<https://kubernetes.io/blog/2019/12/09/kubernetes-1-17-feature-cis-volume-snapshot-beta/>)** release.
     
     In order to use the Kubernetes Volume Snapshot feature, you must ensure the following components have been deployed on your Kubernetes cluster.
        
        * [Install Snapshot Beta CRDs using the following command](<https://kubernetes.io/blog/2019/12/09/kubernetes-1-17-feature-cis-volume-snapshot-beta/#how-do-i-deploy-support-for-volume-snapshots-on-my-kubernetes-cluster>)
          ```shell script
          kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/release-2.0/config/crd/snapshot.storage.k8s.io_volumesnapshotclasses.yaml
          kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/release-2.0/config/crd/snapshot.storage.k8s.io_volumesnapshotcontents.yaml
          kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/release-2.0/config/crd/snapshot.storage.k8s.io_volumesnapshots.yaml
 
        * [Volume snapshot controller](<https://kubernetes.io/blog/2019/12/09/kubernetes-1-17-feature-cis-volume-snapshot-beta/#how-do-i-deploy-support-for-volume-snapshots-on-my-kubernetes-cluster>)                
          ```shell script
          kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/deploy/kubernetes/snapshot-controller/rbac-snapshot-controller.yaml
          kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/deploy/kubernetes/snapshot-controller/setup-snapshot-controller.yaml           
          ```
          After executing these commands, a snapshot-controller pod should be up and running.
        
7. Run the `./csi-install.sh --namespace unity --values ./myvalues.yaml` command to proceed with the installation.

    A successful installation should emit messages that look similar to the following samples:
    ```
    ------------------------------------------------------
    > Installing CSI Driver: csi-unity on 1.18
    ------------------------------------------------------
    ------------------------------------------------------
    > Checking to see if CSI Driver is already installed
    ------------------------------------------------------
    ------------------------------------------------------
    > Verifying Kubernetes and driver configuration
    ------------------------------------------------------
    |- Kubernetes Version: 1.18
    |
    |- Driver: csi-unity
    |
    |- Verifying Kubernetes versions
      |
      |--> Verifying minimum Kubernetes version                         Success
      |
      |--> Verifying maximum Kubernetes version                         Success
    |
    |- Verifying that required namespaces have been created             Success
    |
    |- Verifying that required secrets have been created                Success
    |
    |- Verifying that required secrets have been created                Success
    |
    |- Verifying snapshot support
      |
      |--> Verifying that beta snapshot CRDs are available              Success
      |
      |--> Verifying that beta snapshot controller is available         Success
    |
    |- Verifying helm version                                           Success

    ------------------------------------------------------
    > Verification Complete
    ------------------------------------------------------
    |
    |- Installing Driver                                                Success
      |
      |--> Waiting for statefulset unity-controller to be ready         Success
      |
      |--> Waiting for daemonset unity-node to be ready                 Success
    ------------------------------------------------------
    > Operation complete
    ------------------------------------------------------
    ```
    Results
    At the end of the script statefulset unity-controller and daemonset unity-node will be ready, execute command **kubectl get pods -n unity** to get the status of the pods and you will see the following:
    * unity-controller-0 with 5/5 containers ready, and status displayed as Running.
    * Agent pods with 2/2 containers and the status displayed as Running.

    Finally, the script creates storageclasses such as, "unity". Additional storage classes can be created for different combinations of file system types and Unity storage pools. The script also creates volumesnapshotclass "unity-snapclass".

## Certificate validation for Unisphere REST API calls 

This topic provides details about setting up the certificate validation for the CSI Driver for Dell EMC Unity.
 
Before you begin As part of the CSI driver installation, the CSI driver requires a secret with the name unity-certs-0 to unity-certs-n based on ".Values.certSecretCount" parameter present in the namespace unity.

This secret contains the X509 certificates of the CA which signed the Unisphere SSL certificate in PEM format.
 
If the install script does not find the secret, it creates one empty secret with the name unity-certs-0.

The CSI driver exposes an install parameter in secret.json, which is like storageArrayList[i].insecure, which determines if the driver performs client-side verification of the Unisphere certificates.
 
The storageArrayList[i].insecure parameter set to true by default, and the driver does not verify the Unisphere certificates.

If the storageArrayList[i].insecure set to false, then the secret unity-certs-n must contain the CA certificate for Unisphere.
 
If this secret is empty secret, then the validation of the certificate fails, and the driver fails to start.
 
If the storageArrayList[i].insecure parameter set to false and a previous installation attempt created the empty secret, then this secret must be deleted and re-created using the CA certs.
 
If the Unisphere certificate is self-signed or if you are using an embedded Unisphere, then perform the following steps.

   1. To fetch the certificate, run the following command.
      `openssl s_client -showcerts -connect <Unisphere IP:Port> </dev/null 2>/dev/null | openssl x509 -outform PEM > ca_cert_0.pem`
      Ex. openssl s_client -showcerts -connect 1.1.1.1:443 </dev/null 2>/dev/null | openssl x509 -outform PEM > ca_cert_0.pem
   2. Run the following command to create the cert secret with index '0'
         `kubectl create secret generic unity-certs-0 --from-file=cert-0=ca_cert_0.pem -n unity`
      Use the following command to replace the secret
          `kubectl create secret generic unity-certs-0 -n unity --from-file=cert-0=ca_cert_0.pem -o yaml --dry-run | kubectl replace -f -` 
   3. Repeat step-1 & 2 to create multiple cert secrets with incremental index (ex: unity-certs-1, unity-certs-2, etc)

**Note**: "unity" is the namespace for helm based installation but namespace can be user defined in operator based installation.

**Note**: User can add multiple certificates in the same secret. The certificate file should not exceed more than 1Mb due to kubernetes secret size limitation.

**Note**: Whenever certSecretCount parameter changes in myvalues.yaml user needs to uninstall and install the driver.

## Upgrade CSI Driver for Unity

Preparing myvalues.yaml is the same as explained above.

To upgrade the driver from csi-unity v1.2.1 in k8s 1.16 to csi-unity 1.3 in k8s 1.17:
1. Remove all volume snapshots, volume snapshot content and volume snapshot class objects.
2. Upgrade the Kubernetes version to 1.17 first before upgrading CSI driver.
3. Uninstall existing driver.
4. Uninstall alpha snapshot CRDs.
5. Verify all pre-reqs to install csi-unity v1.3 are fulfilled.
6. Install the driver using installation steps from [here](#install-csi-driver-for-unity)

**Note**: User has to re-create existing custom-storage classes (if any) according to latest (v1.3) format.

## Building the driver image (UBI)
**NOTE** : Only RHEL host can be used to build the driver image.
1. Make sure podman is installed in node.
2. Add the fully-qualified name of the image repository to the [registries.insecure] 
   section of the /etc/containers/registries.conf file. For example:
    ```
	  [registries.insecure]
	  registries = ['myregistry.example.com']
    ```
2. Inside csi-unity directory, execute this command to build the image and this image can be used locally:\
    `make podman-build`
3. Tag the image generated to the desired repository with command:\
    `podman tag IMAGE_NAME:IMAGE_TAG IMAGE_REPO/IMAGE_REPO_NAMESPACE/IMAGE_NAME:IMAGE_TAG`
4. To push the image to the repository, execute command:\
    `podman push IMAGE_REPO/IMAGE_REPO_NAMESPACE/IMAGE_NAME:IMAGE_TAG`

## Test deploying a simple pod with Unity storage
Test the deployment workflow of a simple pod on Unity storage.

1. **Verify Unity system for Host**

    After helm deployment `CSI Driver for Node` will create new Host(s) in the Unity system depending on the number of nodes in kubernetes cluster.
    Verify Unity system for new Hosts and Initiators
    
2. **Creating a volume:**

    Create a file (`pvc.yaml`) with the following content.
    
    **Note**: Use default FC, iSCSI, NFS storage class or create custom storage classes to create volumes. NFS protocol supports ReadWriteOnce, ReadOnlyMany and ReadWriteMany access modes. FC/iSCSI protocol supports ReadWriteOnce access mode only.

    **Note**: Additional 1.5 GB is added to the required size of NFS based volume/pvc. This is due to unity array requirement, which consumes this 1.5 GB for storing metadata. This makes minimum PVC size for NFS protocol via driver as 1.5 GB, which is 3 GB when created directly on the array.

    ```
    apiVersion: v1
    kind: PersistentVolumeClaim
    metadata:
      name: testvolclaim1
    spec:
      accessModes:
      - ReadWriteOnce
      resources:
        requests:
          storage: 5Gi
      storageClassName: unity
    ```

    Execute the following command to create volume
    ```
    kubectl create -f $PWD/pvc.yaml
    ```

    Result: After executing the above command, PVC will be created in the default namespace, and the user can see the pvc by executing `kubectl get pvc`. 
    
    **Note**: Verify unity system for the new volume

3. **Attach the volume to Host**

    To attach a volume to a host, create a new application(Pod) and use the PVC created above in the Pod. This scenario is explained using the Nginx application. Create `nginx.yaml` with the following content.

    ```
    apiVersion: v1
    kind: Pod
    metadata:
      name: nginx-pv-pod
    spec:
      containers:
        - name: task-pv-container
          image: nginx
          ports:
            - containerPort: 80
              name: "http-server"
          volumeMounts:
            - mountPath: "/usr/share/nginx/html"
              name: task-pv-storage
      volumes:
        - name: task-pv-storage
          persistentVolumeClaim:
            claimName: testvolclaim1
    ```

    Execute the following command to mount the volume to kubernetes node
    ```
    kubectl create -f $PWD/nginx.yaml
    ```

    Result: After executing the above command, new nginx pod will be successfully created and started in the default namespace.

    **Note**: Verify unity system for volume to be attached to the Host where the nginx container is running

4. **Create Snapshot**
    The following procedure will create a snapshot of the volume in the container using VolumeSnapshot objects defined in snap.yaml. 
    The following are the contents of snap.yaml.
    
    *snap.yaml*

    ```
    apiVersion: snapshot.storage.k8s.io/v1beta1
    kind: VolumeSnapshot
    metadata:
      name: testvolclaim1-snap1
      namespace: default
    spec:
      volumeSnapshotClassName: unity-snapclass
      source:
        persistentVolumeClaimName: testvolclaim1
    ```
    
    Execute the following command to create snapshot
    ```
    kubectl create -f $PWD/snap.yaml
    ```
    
    The spec.source section contains the volume that will be snapped in the default namespace. For example, if the volume to be snapped is testvolclaim1, then the created snapshot is named testvolclaim1-snap1. Verify the unity system for new snapshot under the lun section.
    
    **Note**:
    
    * User can see the snapshots using `kubectl get volumesnapshot`
    * Notice that this VolumeSnapshot class has a reference to a snapshotClassName:unity-snapclass. The CSI Driver for Unity installation creates this class as its default snapshot class. 
    * You can see its definition using `kubectl get volumesnapshotclasses unity-snapclass -o yaml`.
          
5. **Delete Snapshot**

    Execute the following command to delete the snapshot
    
    ```
    kubectl get volumesnapshot
    kubectl delete volumesnapshot testvolclaim1-snap1
    ```
6.  **To Unattach the volume from Host**

    Delete the Nginx application to unattach the volume from host
    
    `kubectl delete -f nginx.yaml`

7. **To delete the volume**

    ```
    kubectl get pvc
    kubectl delete pvc testvolclaim1
    kubectl get pvc
    ```

8. **Volume Expansion**

    To expand a volume, execute the following command to edit the pvc:
    ```
    kubectl edit pvc pvc-name
    ```
    Then, edit the "storage" field in spec section with required new size:
    ```
    spec:
      accessModes:
      - ReadWriteOnce
      resources:
        requests:
          storage: 10Gi #This field is updated from 5Gi to 10Gi which is required new size
    ```
    **Note**: Make sure the storage class used to create the pvc have allowVolumeExpansion field set to true. The new size cannot be less than the existing size of pvc.

9. **Create Volume Clone**

    Create a file (`clonepvc.yaml`) with the following content.

    ```
    apiVersion: v1
    kind: PersistentVolumeClaim
    metadata:
        name: clone-pvc
    spec:
      accessModes:
      - ReadWriteOnce
      resources:
        requests:
          storage: 5Gi
      dataSource:
        kind: PersistentVolumeClaim
        name: source-pvc
      storageClassName: unity
    ```

    Execute the following command to create volume clone
    ```
    kubectl create -f $PWD/clonepvc.yaml
    ```
    **Note**: Size of clone pvc must be equal to size of source pvc.

    **Note**: For NFS protocol, user cannot expand cloned pvc.

    **Note**: For NFS protocol, deletion of source pvc is not permitted if cloned pvc exists.

10. **Create Volume From Snapshot**

    Create a file (`pvcfromsnap.yaml`) with the following content.

    ```
    apiVersion: v1
    kind: PersistentVolumeClaim
    metadata:
        name: pvcfromsnap
    spec:
      accessModes:
      - ReadWriteOnce
      resources:
        requests:
          storage: 5Gi
      dataSource:
        kind: VolumeSnapshot
        name: source-snapshot
        apiGroup: snapshot.storage.k8s.io
      storageClassName: unity
    ```

    Execute the following command to create volume clone
    ```
    kubectl create -f $PWD/pvcfromsnap.yaml
    ```
    **Note**: Size of created pvc from snapshot must be equal to size of source snapshot.

    **Note**: For NFS protocol, pvc created from snapshot can not be expanded.

    **Note**: For NFS protocol, deletion of source pvc is not permitted if created pvc from snapshot exists.

## Static volume creation (Volume ingestion)

Static provisioning is a feature that is native to Kubernetes and that allows cluster administrators to make existing storage devices available to a cluster.
As a cluster administrator, you must know the details of the storage device, its supported configurations, and mount options.

To make existing storage available to a cluster user, you must manually create the storage device, a PV, and a PVC.

1. Create a volume or select existing volume from Unity Array

2. Create Persistent Volume explained below
```yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: static-pv
  annotations:
    pv.kubernetes.io/provisioned-by: csi-unity.dellemc.com
spec:
  accessModes:
  - ReadWriteOnce
  capacity:
    storage: 5Gi
  csi:
    driver: csi-unity.dellemc.com
    volumeHandle: csivol-vol-name-FC-apm001234567-sv_12
    fsType: xfs
  persistentVolumeReclaimPolicy: Delete
  claimRef:
    namespace: default
    name: myclaim
  storageClassName: unity
```

"volumeHandle" is the critical parameter while creating the PV. "volumeHandle" is defined as four sections.

*\<volume-name\>-\<protocol>-\<arrayid>-\<volume id>*

* volume-name: Name of the volume. Can have any number of "-"
* Possible values for "Protocol" are "FC", "iSCSI" and "NFS"
* arrayid: arrayid defined in lower case  
* volume id: Represents the the LUN cli-id or Filesystem ID (not the resource-id incase of filesystem)

3. Create Persistence Volume Claim
```yaml
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: myclaim
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 5Gi
  storageClassName: unity
```
 
4. Create Pod
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mypod
  namespace: default
spec:
  containers:
    - name: nginx
      image: nginx
      ports:
        - containerPort: 80
          name: "http-server"
      volumeMounts:
        - mountPath: "/usr/share/nginx/html"
          name: myclaim
  volumes:
    - name: myclaim
      persistentVolumeClaim:
        claimName: myclaim
```

## Snapshot ingestion
Snapshot ingestion is a feature that allows cluster administrators to make existing snapshot on array, created by user available to a cluster.

To make existing snapshot available to a cluster user, user must manually create or use existing snapshot in Unisphere for PV.

1. Create a snapshot or identify existing snapshot using Unisphere

2. Create a VolumeSnapshotContent explained below
```yaml
apiVersion: snapshot.storage.k8s.io/v1beta1
kind: VolumeSnapshotContent
metadata:
  name: manual-snapshot-content
spec:
  deletionPolicy: Delete
  driver: csi-unity.dellemc.com
  volumeSnapshotClassName: unity-snapclass
  source:
     snapshotHandle: snap1-FC-apm00175000000-38654806278
  volumeSnapshotRef:
    name: manual-snapshot
    namespace: default
```

**"snapshotHandle"**  is the key parameter that contains four sections.

    1. Snapshot name (unused)
    2. Type of snapshot (unused and if specified it should be FC/iSCSI/NFS)
    3. Arrays id ex: apm00175000000
    4. Snapshot id ex:38654806278
   

3. Create a VolumeSnapshot

```yaml
apiVersion: snapshot.storage.k8s.io/v1beta1
kind: VolumeSnapshot
metadata:
  name: manual-snapshot
spec:
  volumeSnapshotClassName: unity-snapclass
  source:
     volumeSnapshotContentName: manual-snapshot-content
```

4. Ingestion is completed in the above steps and user can perform Clone volume or Create Volume from Snapshot from VolumeSnapshot created from VolumeSnapshotContent.

Ex: Create volume from VolumeSnapshot

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: restore-pvc-from-snap
spec:
  storageClassName: unity
  dataSource:
    name: manual-snapshot
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 5Gi
```

## Dynamically update the unity-creds secrets

Users can dynamically add delete array information from secret. Whenever an update happens the driver updates the "Host" information in an array.
User can update secret using the following command.

    `kubectl create secret generic unity-creds -n unity --from-file=config=secret.json -o yaml --dry-run=client | kubectl replace -f - `

**Note**: Updating unity-certs-x secrets is a manual process, unlike unity-creds. Users have to re-install the driver in case of updating/adding the SSL certificates or changing the certSecretCount parameter.

## Install CSI-Unity driver using dell-csi-operator in OpenShift / upstream Kubernetes
CSI Driver for Dell EMC Unity can also be installed via the new Dell EMC Storage Operator.

The Dell EMC Storage CSI Operator is a Kubernetes Operator, which can be used to install and manage the CSI Drivers provided by Dell EMC for various storage platforms. This operator is available as a community operator for upstream Kubernetes and can be deployed using https://operatorhub.io/operator/dell-csi-operator . It is also available as a community operator for OpenShift clusters and can be deployed using OpenShift Container Platform. Both upstream kubernetes and openshift uses OLM(Operator Lifecycle Manager) as well as manual installation.
 
The operator can also be deployed directly by following the instructions available here - https://github.com/dell/dell-csi-operator
 
There are sample manifests provided, which can be edited to do an easy installation of the driver. Please note that the deployment of the driver using the operator doesn’t use any Helm charts and the installation & configuration parameters will be slightly different from the ones specified via the Helm installer.

Kubernetes Operators make it easy to deploy and manage the entire lifecycle of complex Kubernetes applications. Operators use Custom Resource Definitions (CRD), which represents the application and use custom controllers to manage them.

### Procedure to create new CSI-Unity driver

1. Create namespace
   Run `kubectl create namespace test-unity` to create the a namespace called test-unity. It can be any user-defined name.
   
2. Create *unity-creds*
   
   Create secret mentioned in [Install csi-driver](#install-csi-driver-for-unity) section. The secret should be created in user-defined namespace (test-unity, in this case)

3. Create certificate secrets

   As part of the CSI driver installation, the CSI driver requires a secret with the name unity-certs-0 to unity-certs-n in the user-defined namespace (test-unity, in this case)
   Create certificate procedure explained in the [link](#certificate-validation-for-unisphere-rest-api-calls)
   
   **Note**: *'certSecretCount'* parameter is not required for operator. Based on secret name pattern (unity-certs-*) operator reads all the secrets.
   Secret name suffix should have 0 to N order to read the secrets. Secrets will not be considered, if any number missing in suffix. 
   
   Ex: If unity-certs-0, unity-certs-1, unity-certs-3 are present in the namespace, then only first two secrets are considered for SSL verification.
       
4. Create a CR (Custom Resource) for unity using the sample provided below

Create a new file `csiunity.yaml` by referring the following content. Replace the given sample values according to your environment. You can find may CRDs under deploy/crds folder when you install dell-csi-operator

    ```yaml
    apiVersion: storage.dell.com/v1
    kind: CSIUnity
    metadata:
      name: test-unity
      namespace: test-unity
    spec:
      driver:
        configVersion: v2
        certSecretCount: 1
        replicas: 1
        sideCars:
          -
            name: snapshotter
        snapshotClass:
          -
            name: test-snap
            parameters:
              retentionDuration: ""
        common:
          image: "dellemc/csi-unity:v1.3.0.000R"
          imagePullPolicy: IfNotPresent
          envs:
          - name: X_CSI_UNITY_DEBUG
            value: "true"
        storageClass:
        - name: virt2016****-fc
          default: true
          reclaimPolicy: "Delete"
          parameters:
            storagePool: pool_1
            arrayId: "VIRT2016****"
            protocol: "FC"
        - name: virt2017****-iscsi
          reclaimPolicy: "Delete"
          parameters:
            storagePool: pool_1
            arrayId: "VIRT2017****"
            protocol: "iSCSI"
        snapshotClass:
         - name: test-snap
           parameters: 
             retentionDuration: ""
    ```

5.  Execute the following command to create unity custom resource
    ```kubectl create -f csiunity.yaml```
    The above command will deploy the csi-unity driver in the test-unity namespace.

6. Any deployment error can be found out by loggin the operator pod which is in default namespace (e.g., kubectl logs dell-csi-operator-64c58559f6-cbgv7)
 
7. User can configure the following parameters in CR
       
   The following table lists the primary configurable parameters of the Unity driver chart and their default values.
   
   | Parameter | Description | Required | Default |
   | --------- | ----------- | -------- |-------- |
   | ***Common parameters for node and controller*** |
   | CSI_ENDPOINT | Specifies the HTTP endpoint for Unity. | No | /var/run/csi/csi.sock |
   | X_CSI_DEBUG | To enable debug mode | No | false |
   | GOUNITY_DEBUG | To enable debug mode for gounity library| No | false |
   | ***Controller parameters*** |
   | X_CSI_MODE   | Driver starting mode | No | controller|
   | X_CSI_UNITY_AUTOPROBE | To enable auto probing for driver | No | true |
   | ***Node parameters*** |
   | X_CSI_MODE   | Driver starting mode  | No | node|
   | X_CSI_ISCSI_CHROOT | Path to which the driver will chroot before running any iscsi commands. | No | /noderoot |

### Listing CSI-Unity drivers
  User can query for csi-unity driver using the following commands
  `kubectl get csiunity --all-namespaces`
  `kubectl get pods -n <namespace of unity driver>`

  In addition , user can enter the following command to make sure operator is running

  `kubectl get pods`

  The above command should display a pod whose name starts with dell-csi-operator running on a default namespace.

   To upgrade the driver from csi-unity v1.2.1 in OpenShift 4.3 (Installed using Helm) to csi-unity v1.3 in OpenShift 4.3:
      1. Uninstall the existing csi-unity v1.2.1 driver using Helm's uninstall.unity script.
      2. Install operator using the instructions provided in https://github.com/dell/dell-csi-operator.
      3. Create CR by taking the reference from /deploy/crds/unity_v130_ops_43.yaml.
      4. User can install csi-unity v1.3 in previous namespace (unity) or user can install in the new namespace.
      5. Install csi-unity v1.3 driver using the operator v1.1 by creating the object (E.g., kubectl create -f 
         unity_v130_ops_43.yaml).
      6. Please note that , volumesnapshotclass will not be created as part of this installation and no volume 
         snapshot related operation can be performed on this combination (csi-unity v1.3 and OpenShift 4.3).