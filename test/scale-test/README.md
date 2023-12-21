<!--
 Copyright © 2020 Dell Inc. or its subsidiaries. All Rights Reserved.
 
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at
      http://www.apache.org/licenses/LICENSE-2.0
 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
-->
# Helm charts and scripts for scalability tests

## Helm charts
| Name       | Usage |
|------------|-------|
| 2volumes   | Creates 2 volumes per pod/replica
| 10volumes  | Creates 10 volumes per pod/replica
| 20volumes  | Creates 20 volumes per pod/replica
| 30volumes  | Creates 30 volumes per pod/replica
| 50volumes  | Creates 50 volumes per pod/replica
| 100volumes | Creates 100 volumes per pod/replica


## Scripts
| Name           | Usage |
|----------------|-------|
| scaletest.sh   | Script to start the scalability tests
| rescaletest.sh | Script to rescale pods to a different number of replicas


## Scale test prerequisites
* Works only on Helm v3
* Make sure the driver is installed and running fine in the k8s cluster
* The namespace that is to be used for the scale test should be pre-created in the k8s cluster and make sure no objects are present under the namespace.
* The right storage class to be used for the test should be pre-created and specified as StorageClassName in values.yaml under the test folder that is to be run.


## scaletest.sh
The test is run using the script file scaletest.sh in the directory test/scale-tests. The script takes the below arguments.
-r : number of replicas for the statefulset 
-t : the test name to be run [default: 2volumes]
-n : namespace to be used to run the test [default: test]

Examples:

To scale the statefulset to 3 resplicas and then re-scale it back to 0 replicas
./scaletest.sh -r 3 -t 10volumes -n test

To rescale the statefulset to 2 replicas
sh rescaletest.sh -r 0 -t 10volumes -n test







