# Unity CSI

This repo contains [Container Storage Interface(CSI)]
(<https://github.com/container-storage-interface/>) Unity CSI driver for DellEMC.

## Overview

Unity CSI plugins implement an interface between CSI enabled Container Orchestrator(CO) and Unity Storage Array. It allows dynamically provisioning Unity volumes and attaching them to workloads.

## Introduction
The CSI Driver For Dell EMC Unity conforms to CSI spec 1.1
   * Support for Kubernetes 1.14
   * Will add support for other orchestrators over time
   * The CSI specification is documented here: https://github.com/container-storage-interface/spec. The driver uses CSI v1.1.

## CSI Driver For Dell EMC Unity Capabilities

| Capability | Supported | Not supported |
|------------|-----------| --------------|
|Provisioning | Persistent volumes creation, deletion, mounting, unmounting, listing | Volume expand |
|Export, Mount | Mount volume as file system | Raw volumes, Topology|
|Data protection | Creation of snapshots, Create volume from snapshots | Cloning volume |
|Types of volumes | Static, Dynamic| |
|Access mode | Single Node read/write | Multi Node access modes|
|Kubernetes | v1.14 | V1.13 or previous versions|
|OS | RHEL 7.5, 7.6. CentOS 7.6 | Ubuntu, other Linux variants|
|Unity | OE 5.0 | Previous versions|
|Protocol | FC, iSCSI | NFS |

## Installation overview

The Helm chart installs CSI Driver for Unity using a shell script (helm/install.unity). This script installs the CSI driver container image along with the required Kubernetes sidecar containers.

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

* Install Kubernetes.
* Enable the Kubernetes feature gates
* Configure Docker service
* Install Helm and Tiller with a service account
* Deploy Unity using Helm

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

1. Collect information from the Unity System like IP address, username  and password. Make a note of the value for these parameters as they must be entered in the myvalues.yaml file.

2. Copy the csi-unity/values.yaml into a file in the same directory as the install.unity named myvalues.yaml, to customize settings for installation.

3. Edit myvalues.yaml to set the following parameters for your installation:
    
    The following table lists the primary configurable parameters of the Unity driver chart and their default values. More detailed information can be found in the [`values.yaml`](helm/csi-unity/values.yaml) file in this repository.
    
    | Parameter | Description | Required | Default |
    | --------- | ----------- | -------- |-------- |    
    | restGateway | REST API gateway HTTPS endpoint Unity system | true | - |
    | storagePool | Unity Storage Pool CLI ID to use with in the Kubernetes storage class | true | - |
    | unityUsername | Username for accessing unity system <base64 encoded string> | true | - |
    | unityPassword | Password for accessing unity system <base64 encoded string> | true | - |
    | volumeNamePrefix | String to prepend to any volumes created by the driver | false | csivol |
    | snapNamePrefix | String to prepend to any snapshot created by the driver | false | csi-snap |
    | storageClass.name | Name of the storage class to be defined | false | unity |
    | storageClass.isDefault | Whether or not to make this storage class the default | false | true |
    | storageClass.reclaimPolicy | What should happen when a volume is removed | false | Delete |
    | ***Storage Class parameters*** | Following parameters are not present in values.yaml |
    | FsType | To set File system type. Possible values are ext3,ext4,xfs | false | ext4 |
    | volumeThinProvisioned | To set volume thinProvisioned | false | true |    
    | isVolumeDataReductionEnabled | To set volume data reduction | false | false |
    | volumeTieringPolicy | To set volume tiering policy | false | 0 |
    | hostIOLimitName | To set unity host IO limit | false | "" |
    | ***Snapshot Class parameters*** | Following parameters are not present in values.yaml  |
    | snapshotRetentionDuration | TO set snapshot retention duration. Format:"1:23:52:50" (number of days:hours:minutes:sec)| false | "" |
    
    Use the following command to convert username/password to base64 encoded string
    ```
    echo -n 'admin' | base64
    echo -n 'password' | base64 
    ```
    
    Example *myvalues.yaml*
    
    ```
    storagePool: pool_1
    restGateway: "https://<Ip of Unity system>"  
    unityUsername: <Base64 encoded string>  
    unityPassword: <Base64 encoded string>
    images:
       driver: <docker image>
    ```
	
4. Run the `sh install.unity` command to proceed with the installation.

    A successful installation should emit messages that look similar to the following samples:
    ```
    sh install.unity 
    Kubernetes version v1.14.2
    Kubernetes master nodes: 10.*.*.*
    Kubernetes minion nodes:
    Verifying the feature gates.
    NAME:   unity
    LAST DEPLOYED: Wed Aug 14 01:23:35 2019
    NAMESPACE: unity
    STATUS: DEPLOYED
    [....]
    NAME                 READY   STATUS    RESTARTS   AGE
    unity-controller-0   4/4     Running   0          21s
    unity-node-kzv49     2/2     Running   0          21s
    CSIDrivers:
    No resources found.
    CSINodeInfos:
    No resources found.
    StorageClasses:
    NAME              PROVISIONER   AGE
    unity (default)   csi-unity     21s
    ```
    Results
    At the end of the script, the kubectl get pods -n unity is called to GET the status of the pods and you will see the following:
    * unity-controller-0 with 4/4 containers ready, and status displayed as Running.
    * Agent pods with 2/2 containers and the status displayed as Running.

    Finally, the script lists the created storageclasses such as, "unity". Additional storage classes can be created for different combinations of file system types and Unity storage pools. The script also creates volumesnapshotclass "unity-snapclass".

## Test deploying a simple pod with Unity storage
Test the deployment workflow of a simple pod on Unity storage.

1. **Verify Unity system for Host**

    After helm deployment `CSI Driver for Node` will create new Host(s) in the Unity system depending on the number of node in kubernetes cluster.
    Verify Unity system for new Hosts and Initattors
    
2. **Creating a volume:**

    Create a file (`pvc.yaml`) with the following content
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

    Result: After executing the above command PVC will be created in the default namespace, and the user can see the pvc by executing `kubectl get pvc`. 
    Note: Verify unity system for the new volume

3. **Attach the volume to Host**

    To attach a volume to a host, create a new application(Pod) and use the PVC created above in the Pod. This scenario is explained using the Nginx application. Create `nginx.yaml` with the following content.

    ```
    apiVersion: v1
    kind: Pod
    metadata:
      name: ngnix-pv-pod
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
    Note: Verify unity system for volume to be attached to the Host where the nginx container is running

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
    
    Note:
    
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

    Delete the nginx application to unattach the volume from host
    
    `kubectl delete -f nginx.yaml`
7. **To delete the volume**

    ```
    kubectl get pvc
    kubectl delete pvc testvolclaim1
    kubectl get pvc
    ```
## Install CSI-Unity driver using dell-csi-operator in OpenShift
CSI Driver for Dell EMC Unity can also be installed via the new Dell EMC Storage Operator. 
The Dell EMC Storage CSI Operator is a Kubernetes Operator, which can be used to install and manage the CSI Drivers provided by Dell EMC for various storage platforms. This operator is available as a community operator for upstream Kubernetes and can be deployed using OperatorHub.io. It is also available as a community operator for OpenShift clusters and can be deployed using OpenShift Container Platform. Both these methods of installation use OLM (Operator Lifecycle Manager). 
The operator can also be deployed directly by following the instructions available here - https://github.com/dell/dell-csi-operator 
There are sample manifests provided which can be edited to do an easy installation of the driver. Please note that the deployment of the driver using the operator doesn’t use any Helm charts and the installation & configuration parameters will be slightly different from the ones specified via the Helm installer.

Kubernetes Operators make it easy to deploy and manage entire lifecycle of complex Kubernetes applications. Operators use Custom Resource Definitions (CRD) which represents the application and use custom controllers to manage them.

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
            storagepool: pool_1
            protocol: "FC"
        - name: iscsi
          reclaimPolicy: "Delete"
          parameters:
            storagepool: pool_1
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
   | X_CSI_PRIVATE_MOUNT_DIR | Specifies the private directory to which a PVC will be mounted before binding the mount to the target directory. | No | /var/lib/kubelet/plugins/unity.emc.dell.com/disks |
   | X_CSI_ISCSI_CHROOT | Path to which the driver will chroot before running any iscsi commands. | No | /noderoot |
           
## Support
The CSI Driver for Dell EMC Unity image available on Dockerhub is officially supported by Dell EMC.
 
The source code available on Github is unsupported and provided solely under the terms of the license attached to the source code. For clarity, Dell EMC does not provide support for any source code modifications.
 
For any CSI driver setup, configuration issues, questions or feedback, join the Dell EMC Container community athttps://www.dell.com/community/Containers/bd-p/Containers
 
For any Dell EMC storage issues, please contact Dell support at: https://www.dell.com/support.
