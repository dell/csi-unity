# Dell Unity Helm Chart for Kubernetes

For detailed installation instructions, please check the `dell-csi-helm-installer` directory

The general outline is:

    1. Satisfy the pre-requsites outlined in the Release and Installation Notes in the doc directory.

    2. Create a Kubernetes secret with the Unity credentials using the template in secret.yaml.

    3. Make a copy of the `csi-unity/values.yaml` to the location of your choice(say csi-unity/myvalues.yaml) and fill in various installation parameters.

    4. Run the helm install command, first using the --dry-run flag to confirm various parameters are as desired.
       Once the parameters are validated, run the command without the --dry-run flag.
       Note: The below example assumes that the user is at repo root helm folder i.e csi-unity/helm.
              
       Syntax: helm install --dry-run --values <myvalues.yaml location> --namespace <namespace> <name of secret> <helmPath>
       <name of secret> - unity in case of unity-creds and unity-certs-0 secrets.
       <helmPath> - Path of the helm directory.
       <namespace> - namespace of the driver installation. 
       e.g: helm install --dry-run --values ./csi-unity/myvalues.yaml --namespace unity unity ./csi-unity

    5. Or Invoke the `dell-csi-helm-installer/csi-install.sh` shell script which deploys the helm chart for CSI Unity driver.
