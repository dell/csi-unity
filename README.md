# Unity CSI

This repo contains [Container Storage Interface(CSI)]
(<https://github.com/container-storage-interface/>) Unity CSI driver for DellEMC.

## Overview

Unity CSI plugins implement an interface between CSI enabled Container Orchestrator(CO) and Unity Storage Array. It allows dynamically provisioning Unity volumes and attaching them to workloads.

## Introduction
The CSI Driver For Dell EMC Unity conforms to CSI spec 1.1
   * Support for Kubernetes 1.14 and 1.16
   * Will add support for other orchestrators over time
   * The CSI specification is documented here: https://github.com/container-storage-interface/spec. The driver uses CSI v1.1.

## CSI Driver For Dell EMC Unity Capabilities

| Capability | Supported | Not supported |
|------------|-----------| --------------|
|Provisioning | Persistent volumes creation, deletion, mounting, unmounting, listing | Volume expand |
|Export, Mount | Mount volume as file system | Raw volumes, Topology|
|Data protection | Creation of snapshots, Create volume from snapshots(FC/iSCSI) | Cloning volume, Create volume from snapshots(NFS) |
|Types of volumes | Static, Dynamic| |
|Access mode | RWO(FC/iSCSI), RWO/RWX/ROX(NFS) | RWX/ROX(FC/iSCSI)|
|Kubernetes | v1.14, v1.16 | V1.13 or previous versions|
|Installer | Helm v3.x,v2,x | Operator |
|OpenShift | v4.3 (Helm installation only) | v4.2 |
|OS | RHEL 7.6, RHEL 7.7, CentOS 7.6, CentOS 7.7 | Ubuntu, other Linux variants|
|Unity | OE 5.0 | Previous versions|
|Protocol | FC, iSCSI, NFS |  |

## Installation overview

The Helm chart installs CSI Driver for Unity using a shell script (helm/install.unity). This script installs the CSI driver container image along with the required Kubernetes sidecar containers.

** Note: Linux user should have root privileges to install this CSI Driver.**

The controller section of the Helm chart installs the following components in a Stateful Set in the namespace unity:

* CSI Driver for Unity
* Kubernetes Provisioner, which provisions the provisioning volumes
* Kubernetes Attacher, which attaches the volumes to the containers
* Kubernetes Snapshotter, which provides snapshot support

The node section of the Helm chart installs the following component in a Daemon Set in the namespace unity:

* CSI Driver for Unity
* Kubernetes Registrar, which handles the driver registration

### Prerequisites

Before you install CSI Driver for Unity, verify the requirements that are mentioned in this topic are installed and configured.

#### Requirements

* Install Kubernetes
* Enable the Kubernetes feature gates
* Configure Docker service
* Install Helm v2 with Tiller with a service account or Helm v3
* Deploy Unity using Helm
* To use iSCSI and NFS protocol, iSCSI initiator and NFS utility packages need to be installed

## Enable Kubernetes feature gates

The Kubernetes feature gates must be enabled before installing CSI Driver for Unity.

#### About Enabling Kubernetes feature gates

The Feature Gates section of Kubernetes home page lists the Kubernetes feature gates. The following Kubernetes feature gates must be enabled:

* VolumeSnapshotDataSource

### Procedure

 1. On each master and node of Kubernetes, edit /var/lib/kubelet/config.yaml and append the following lines at the end to set feature-gate settings for the kubelets:
    */var/lib/kubelet/config.yaml*

    ```
    VolumeSnapshotDataSource: true
    ```

2. On the master node, set the feature gate settings of the kube-apiserver.yaml, kube-controllermanager.yaml and kube-scheduler.yaml file as follows:

    */etc/kubernetes/manifests/kube-apiserver.yaml
    /etc/kubernetes/manifests/kube-controller-manager.yaml
    /etc/kubernetes/manifests/kube-scheduler.yaml*

    ```
    - --feature-gates=VolumeSnapshotDataSource=true
    ```

3. On each node (including master), edit the variable **KUBELET_KUBECONFIG_ARGS** of /usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf file as follows:

    ```
    Environment="KUBELET_KUBECONFIG_ARGS=--bootstrap-kubeconfig=/etc/kubernetes/bootstrap-kubelet.conf --kubeconfig=/etc/kubernetes/kubelet.conf --feature-gates=VolumeSnapshotDataSource=true" 
    ```

4. Restart the kublet on all nodes. 

    ```
    systemctl daemon-reload
    systemctl restart kubelet 
    ```

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
 * In the top-level helm directory, there should be two shell scripts, *install.unity* and *uninstall.unity*. These scripts handle some of the pre and post operations that cannot be performed in the helm chart, such as creating Custom Resource Definitions (CRDs), if needed.

Procedure

1. Collect information from the Unity Systems like Unique ArrayId, IP address, username  and password. Make a note of the value for these parameters as they must be entered in the secret.json and myvalues.yaml file.

2. Copy the csi-unity/values.yaml into a file in the same directory as the install.unity named myvalues.yaml, to customize settings for installation.

3. Edit myvalues.yaml to set the following parameters for your installation:
    
    The following table lists the primary configurable parameters of the Unity driver chart and their default values. More detailed information can be found in the [`values.yaml`](helm/csi-unity/values.yaml) file in this repository.
    
    | Parameter | Description | Required | Default |
    | --------- | ----------- | -------- |-------- |
    | certSecretCount | Represents number of certificate secrets, which user is going to create for ssl authentication. (unity-cert-0..unity-cert-n). Value should be between 1 and 10 | false | 1 |
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
    
   Note: User should provide all boolean values with double quotes. This applicable only for myvalues.yaml. Ex: "true"/"false"
   
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

4. Prepare the secret.json for driver configuration.
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
    
    Note: The user needs to validate the JSON syntax and array related key/values while replacing the unity-creds secret.
    The driver will continue to use previous values in case of an error found in the JSON file.
    
4. Run the `sh install.unity` command to proceed with the installation.

    A successful installation should emit messages that look similar to the following samples:
    ```
    sh install.unity 
    Kubernetes version v1.16.8
    Kubernetes master nodes: 10.*.*.*
    Kubernetes minion nodes:
    Verifying the feature gates.
    Installing using helm version 3
    NAME: unity
    LAST DEPLOYED: Thu May 14 05:05:42 2020
    NAMESPACE: unity
    STATUS: deployed
    REVISION: 1
    TEST SUITE: None
    Thu May 14 05:05:53 EDT 2020
    running 2 / 2
    NAME                 READY   STATUS    RESTARTS   AGE
    unity-controller-0   4/4     Running   0          11s
    unity-node-mkbxc     2/2     Running   0          11s
    CSIDrivers:
    NAME    CREATED AT
    unity   2020-05-14T09:05:42Z
    CSINodes:
    NAME       CREATED AT
    <nodename>   2020-04-16T20:59:16Z
    StorageClasses:
    NAME                         PROVISIONER             AGE
    unity (default)              csi-unity.dellemc.com   11s
    unity-iscsi                  csi-unity.dellemc.com   11s
    unity-nfs                    csi-unity.dellemc.com   11s
    unity-<array-id>-fc          csi-unity.dellemc.com   11s
    unity-<array-id>-iscsi       csi-unity.dellemc.com   11s
    unity-<array-id>-nfs         csi-unity.dellemc.com   11s
    ```
    Results
    At the end of the script, the kubectl get pods -n unity is called to GET the status of the pods and you will see the following:
    * unity-controller-0 with 4/4 containers ready, and status displayed as Running.
    * Agent pods with 2/2 containers and the status displayed as Running.

    Finally, the script lists the created storageclasses such as, "unity". Additional storage classes can be created for different combinations of file system types and Unity storage pools. The script also creates volumesnapshotclass "unity-snapclass".

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
         `kubectl create secret generic unity-certs-0 --from-file=cert-0=ca_cert0.pem -n unity`
      Use the following command to replace the secret
          `kubectl create secret generic unity-certs-0 -n unity --from-file=cert-0=ca_cert0.pem -o yaml --dry-run | kubectl replace -f -` 
   3. Repeat step-1 & 2 to create multiple cert secrets with incremental index (ex: unity-certs-1, unity-certs-2, etc)

Note: User can add multiple certificates in the same secret. The certificate file should not exceed more than 1Mb due to kubernetes secret size limitation.

Note: Whenever certSecretCount parameter changes in myvalues.yaml user needs to uninstall and install the driver.

## Upgrade CSI Driver for Unity

Preparing myvalues.yaml is the same as explained above.

**Note** Supported upgrade path is from CSI Driver for Dell EMC Unity v1.1.0.1 to CSI Driver for Dell EMC Unity v1.2. If user is in v1.0 or v1.1, please upgrade to v1.1.0.1 before upgrading to v1.2 to avoid problems.

Delete the unity-creds secret and recreate again using secret.json as explained above.


Execute the following command to not to delete the unity-creds secret by helm

```kubectl annotate secret unity-creds -n unity "helm.sh/resource-policy"=keep```

Make sure unity-certs-* secrets are created properly before upgrading the driver.
 
Run the `sh upgrade.unity` command to proceed with the upgrading process.

**Note**: Upgrading CSI Unity driver is possible within the same version of Helm. (Ex: Helm V2 to Helm V2)

**Note**: Sometimes user might get a warning saying "updates to parameters are forbidden" when we try to upgrade from previous versions. Delete the storage classes and upgrade the driver.
 
A successful upgrade should emit messages that look similar to the following samples:

    ```
    $ ./upgrade.unity 
    Kubernetes version v1.16.8
    Kubernetes master nodes: 10.*.*.*
    Kubernetes minion nodes:
    Verifying the feature gates.
    node-1's password: 
    lifecycle present :2
    Removing lifecycle hooks from daemonset
    daemonset.extensions/unity-node patched
    daemonset.extensions/unity-node patched
    daemonset.extensions/unity-node patched
    warning: Immediate deletion does not wait for confirmation that the running resource has been terminated. The resource may continue to run on the cluster indefinitely.
    pod "unity-node-t1j5h" force deleted
    Thu May 14 05:05:53 EDT 2020
    running 2 / 2
    NAME                 READY   STATUS    RESTARTS   AGE
    unity-controller-0   4/4     Running   0          12s
    unity-node-n14gj     2/2     Running   0          12s
    Upgrading using helm version 3
    Release "unity" has been upgraded. Happy Helming!
    NAME: unity
    LAST DEPLOYED: Thu May 14 05:05:53 2020
    NAMESPACE: unity
    STATUS: deployed
    REVISION: 2
    TEST SUITE: None
    Thu May 14 05:06:02 EDT 2020
    running 2 / 2
    NAME                 READY   STATUS    RESTARTS   AGE
    unity-controller-0   4/4     Running   0          11s
    unity-node-rn6px     2/2     Running   0          11s
    CSIDrivers:
    NAME       CREATED AT
    unity      2020-04-23T09:25:01Z
    CSINodes:
    NAME                   CREATED AT
    <nodename>   2020-04-16T20:59:16Z
    StorageClasses:
    NAME                 PROVISIONER                AGE
    unity (default)              csi-unity.dellemc.com   11s
    unity-iscsi                  csi-unity.dellemc.com   11s
    unity-nfs                    csi-unity.dellemc.com   11s
    unity-<array-id>-fc          csi-unity.dellemc.com   11s
    unity-<array-id>-iscsi       csi-unity.dellemc.com   11s
    unity-<array-id>-nfs         csi-unity.dellemc.com   11s
    ```

    User has to re-create existing custom-storage classes (if any) according to latest (v1.2) format.

## Migrate from Helm 2 to Helm 3
1. Get the latest code from github.com/dell/csi-unity by executing the following command.
    `git clone -b v1.1.0.1 https://github.com/dell/csi-unity.git`
2. Uninstall the CSI Driver for Dell EMC Unity v1.0 or v1.1 using the uninstall.unity script under csi-unity/helm using Helm 2.
3. Go to https://helm.sh/docs/topics/v2_v3_migration/ and follow the instructions to migrate from Helm 2 to Helm 3.
4. Once Helm 3 is ready, install the CSI Driver for Dell EMC Unity v1.1.0.1 using install.unity script under csi-unity/helm.
5. List the pods with the following command (to verify the status)

   `kubectl get pods -n unity`

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

    The following procedure will create a snapshot of the volume in the container using VolumeSnapshot objects defined in snap.yaml. The following are the contents of snap.yaml.
    
    *snap.yaml*

    ```
    apiVersion: snapshot.storage.k8s.io/v1alpha1
    kind: VolumeSnapshot
    metadata:
        name: testvolclaim1-snap1        
    spec:
        snapshotClassName: unity-snapclass
        source:
            name: testvolclaim1
            kind: PersistentVolumeClaim
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
## Static volume creation
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
spec:
  accessModes:
  - ReadWriteOnce
  capacity:
    storage: 5Gi
  csi:
    driver: csi-unity.dellemc.com
    volumeHandle: csivol-vol-name-FC-apm001234567-sv_12
  persistentVolumeReclaimPolicy: Delete
  claimRef:
    namespace: default
    name: myclaim
  storageClassName: unity
```

"volumeHandle" is the critical parameter while creating the PV. "volumeHandle" is defined as four sections.

*\<volume-name\>-\<protocol>-\<arrayid>-\<volume id>*

* volume-name: Name of the volume. Can have any number of "-"
* Possible values for "Protocol" are "FC", "ISCSI" and "NFS"
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


## Dynamically update the unity-creds secrets

Users can dynamically add delete array information from secret. Whenever an update happens the driver updates the "Host" information in an array.
User can update secret using the following command.

    `kubectl create secret generic unity-creds -n unity --from-file=config=secret.json -o yaml --dry-run | kubectl replace -f - `

* Note: * Updating unity-certs-x secrets is a manual process, unlike unity-creds. Users have to re-install the driver in case of updating/adding the SSL certificates or changing the certSecretCount parameter.

## Install CSI-Unity driver using dell-csi-operator in OpenShift
CSI Driver for Dell EMC Unity can also be installed via the new Dell EMC Storage Operator.

Note: Currently, csi-unity v1.1.0.1 is supported using csi-operator. Use helm-v3 to install csi-driver v1.2 for OpenShift 

The Dell EMC Storage CSI Operator is a Kubernetes Operator, which can be used to install and manage the CSI Drivers provided by Dell EMC for various storage platforms. This operator is available as a community operator for upstream Kubernetes and can be deployed using OperatorHub.io. It is also available as a community operator for OpenShift clusters and can be deployed using OpenShift Container Platform. Both these methods of installation use OLM (Operator Lifecycle Manager).
 
The operator can also be deployed directly by following the instructions available here - https://github.com/dell/dell-csi-operator
 
There are sample manifests provided, which can be edited to do an easy installation of the driver. Please note that the deployment of the driver using the operator doesn’t use any Helm charts and the installation & configuration parameters will be slightly different from the ones specified via the Helm installer.

Kubernetes Operators make it easy to deploy and manage the entire lifecycle of complex Kubernetes applications. Operators use Custom Resource Definitions (CRD), which represents the application and use custom controllers to manage them.

### Listing CSI-Unity drivers
User can query for csi-unity driver using the following command
`kubectl get csiunity --all-namespaces`

### Procedure to create new CSI-Unity driver

1. Create namespace
   Run `kubectl create namespace unity` to create the unity namespace.
   
2. Create *unity-creds*
   
   Create a file called unity-creds.yaml with the following content
     ```yaml
    apiVersion: v1
    kind: Secret
    metadata:
      name: unity-creds
      namespace: unity
    type: Opaque
    data:
      # set username to the base64 encoded username
      username: <base64 username>
      # set password to the base64 encoded password
      password: <base64 password>
    ```
   
   Replace the values for the username and password parameters. These values can be optioned using base64 encoding as described in the following example:
   ```
   echo -n "myusername" | base64
   echo -n "mypassword" | base64
   ```
   
   Run `kubectl create -f unity-creds.yaml` command to create the secret
 
3. Create a CR (Custom Resource) for unity using the sample provided below
Create a new file `csiunity.yaml` with the following content.

    ```yaml
    apiVersion: storage.dell.com/v1
    kind: CSIUnity
    metadata:
      name: unity
      namespace: unity
    spec:
      driver:
        configVersion: v1
        replicas: 1
        common:
          image: "dellemc/csi-unity:v1.1.0.000R"
          imagePullPolicy: IfNotPresent
          envs:
          - name: X_CSI_UNITY_DEBUG
            value: "true"
          - name: X_CSI_UNITY_ENDPOINT
            value: "https://<Unisphere URL>"
          - name: X_CSI_UNITY_INSECURE
            value: "true"
        storageClass:
        - name: fc
          default: true
          reclaimPolicy: "Delete"
          parameters:
            storagePool: pool_1
            protocol: "FC"
        - name: iscsi
          reclaimPolicy: "Delete"
          parameters:
            storagePool: pool_1
            protocol: "iSCSI"
        snapshotClass:
          - name: snapshot
            parameters:
              retentionDuration: "1:1:1:1"
    ```

4.  Execute the following command to create unity custom resource
    ```kubectl create -f csiunity.yaml```
    The above command will deploy the csi-unity driver
 
5. User can configure the following parameters in CR
       
   The following table lists the primary configurable parameters of the Unity driver chart and their default values.
   
   | Parameter | Description | Required | Default |
   | --------- | ----------- | -------- |-------- |
   | ***Common parameters for node and controller*** |
   | CSI_ENDPOINT | Specifies the HTTP endpoint for Unity. | No | /var/run/csi/csi.sock |
   | X_CSI_DEBUG | To enable debug mode | No | false |
   | X_CSI_UNITY_ENDPOINT | Must provide a UNITY HTTPS unisphere url. | Yes | |
   | X_CSI_UNITY_INSECURE | Specifies that the Unity's hostname and certificate chain | No | true |
   | GOUNITY_DEBUG | To enable debug mode for gounity library| No | false |
   | ***Controller parameters*** |
   | X_CSI_MODE   | Driver starting mode | No | controller|
   | X_CSI_UNITY_AUTOPROBE | To enable auto probing for driver | No | true |
   | ***Node parameters*** |
   | X_CSI_MODE   | Driver starting mode  | No | node|
   | X_CSI_ISCSI_CHROOT | Path to which the driver will chroot before running any iscsi commands. | No | /noderoot |

## Install csi-unity driver in OpenShift using HELM v3.x

1. Clone the git repository. ( `git clone https://github.com/dell/csi-unity.git`)

2. Change the directory to ./helm

3. Create a namespace "unity" in kubernetes cluster

4. Create unity-cert-0 to unity-cert-n secrets as explained in the previous sections.

5. Create unity-creds secret using the secret.json explained in the previous sections.

6. Create clusterrole (unity-node) with the following yaml

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
 name: unity-node
rules:
 - apiGroups:
     - security.openshift.io
   resourceNames:
     - privileged
   resources:
     - securitycontextconstraints
   verbs:
     - use
```

7. Create clusterrolebinding (unity-node) with the following yaml
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: unity-node
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: unity-node
subjects:
  - kind: ServiceAccount
    name: unity-node
    namespace: unity
```

8. Execute the following command to install the driver.

`helm install unity --values myvalues.yaml --values csi-unity/k8s-1.16-values.yaml -n unity ./csi-unity`

Note: Preparing myvalues.yaml and secret.json is same as explained in the previous sections

## Support
The CSI Driver for Dell EMC Unity image available on Dockerhub is officially supported by Dell EMC.
 
The source code available on Github is unsupported and provided solely under the terms of the license attached to the source code. For clarity, Dell EMC does not provide support for any source code modifications.
 
For any CSI driver setup, configuration issues, questions or feedback, join the Dell EMC Container community at https://www.dell.com/community/Containers/bd-p/Containers
 
For any Dell EMC storage issues, please contact Dell support at: https://www.dell.com/support.
