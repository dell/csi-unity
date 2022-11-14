/*
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
*/
#!/bin/sh

# scaletest
# This script will kick off a test designed to stress the limits of the driver as well as the array
# It will install a user supplied helm chart with a user supplied number of replicas.
# Each replica will contain a number of volumes
# The test will continue to run until all replicas have been started, volumes created, and mapped
# Then the replicas will be scaled down to 0 and then all the created pvc will be flushed out.

TEST="2volumes"
NAMESPACE="test"
REPLICAS=-1

# Usage information
function usage {
   echo
   echo "`basename ${0}`"
   echo "    -n namespace    - Namespace in which to place the test. Default is: ${NAMESPACE}"
   echo "    -t test         - Test to run. Default is: ${TEST}. The value must point to a Helm Chart"
   echo "    -r replicas     - Number of replicas to create"
   echo "    -p replicas     - Protocol"
   echo
   exit 1
}

# Parse the options passed on the command line
while getopts "n:r:t:" opt; do
  case $opt in
    t)
      TEST="${OPTARG}"
      ;;
    n)
      NAMESPACE="${OPTARG}"
      ;;
	  r)
	    REPLICAS="${OPTARG}"
	    ;;
    \?)
      echo "Invalid option: -$OPTARG" >&2
      usage
      ;;
    :)
      echo "Option -$OPTARG requires an argument." >&2
      usage
      ;;
  esac
done

if [ ${REPLICAS} -eq -1 ]; then 
	echo "No value for number of replicas provided"; 
	usage
fi

validateServiceAccount() {
  # validate that the service account exists
  ACCOUNTS=$(kubectl describe serviceaccount -n "${NAMESPACE}" "unitytest")
  if [ $? -ne 0 ]; then
    echo "Creating Service Account"
    kubectl create -n ${NAMESPACE} -f serviceAccount.yaml
  fi
}

validateServiceAccount

TARGET=$(expr $REPLICAS \* 1)
echo "Targeting replicas: $REPLICAS"
echo "Targeting pods: $TARGET"

helm install scalevoltest --set "name=scalevoltest,replicas=$REPLICAS,storageClass=unity,namespace=${NAMESPACE}" -n ${NAMESPACE} ./${TEST}

waitOnRunning() {
  if [ "$1" = "" ]; 
    then echo "arg: target" ; 
    exit 2; 
  fi
  WAITINGFOR=$1
  
  RUNNING=$(kubectl get pods -n ${NAMESPACE} | grep "Running" | wc -l)

  while [ $RUNNING -ne $WAITINGFOR ];
  do
	  sleep 5
	  RUNNING=$(kubectl get pods -n ${NAMESPACE} | grep "Running" | wc -l)
	  CREATING=$(kubectl get pods -n ${NAMESPACE} | grep "ContainerCreating" | wc -l)
	  PVCS=$(kubectl get pvc -n ${NAMESPACE} | wc -l)
	  date
	  date >>log.output
	  echo running $RUNNING creating $CREATING pvcs $PVCS
	  echo running $RUNNING creating $CREATING pvcs $PVCS >>log.output
  done
}

SECONDS=0
waitOnRunning $TARGET
END=$SECONDS
echo "Time taken to create $REPLICAS replicas with $TEST: $(( (${END} / 60) % 60 ))m $(( ${END} % 60 ))s"
sleep 30

SECONDS=0
# rescale the environment back to 0 replicas
sh rescaletest.sh -n "${NAMESPACE}" -r 0 -t "${TEST}"
END=$SECONDS
echo "Time taken to rescale from $REPLICAS to 0 replicas on test $TEST: $(( (${END} / 60) % 60 ))m $(( ${END} % 60 ))s"

echo "Deleting hem installation under namespace: ${NAMESPACE}"
helm delete -n ${NAMESPACE} scalevoltest

echo "Deleting all pvcs under namespace: ${NAMESPACE}"
kubectl delete pvc --all -n ${NAMESPACE}
