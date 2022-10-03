# Kubernetes Sanity Script Test

This test runs the Kubernetes sanity test at https://github.com/kubernetes-csi/csi-test.
The test last qualified was v2.5.0.

To run the test, follow these steps:

1. "go get github.com/kubernetees-csi/csi-test"
2. Build and install the executable, csi-sanity,  in a directory in your $PATH.
3. Make sure your env.sh is up to date so the CSI driver can be run.
4. Edit the secrets.yaml to have the correct SYMID, ServiceLevel, SRP, and ApplicationPrefix.
5. Use the start_driver.sh to start the driver.
6. Wait until the driver has fully come up and completed node setup. If you remain attached the logs will print on the screen.
7. Use the script run.sh to start csi-sanity (best if you do this in a separate window.)

## Excluded Tests

The following tests were excluded for the reasons specified:
1. GetCapacity -- the test does not support supplying the Parameters fields that are required (for things like SYMID).
2. An idempotent volume test that attempts to create a volume with a different size as the existing volume. It appears to have a problem;
the new size is over the maximum capability, and we detect that error first and disqualify the request, which I think is valid.
