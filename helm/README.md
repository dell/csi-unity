# Dell EMC Unity Helm Chart for Kubernetes

For detailed installation instructions, please check the doc directory

The general outline is:

    1. Satisfy the pre-requsites outlined in the Release and Installation Notes in the doc directory.

    2. Copy the `csi-unity/values.yaml` to a file  `myvalues.yaml` in this directory and fill in various installation parameters.

    3. Invoke the `install.unity` shell script which deploys the helm chart in csi-unity.