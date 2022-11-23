#!/bin/sh
#  Copyright © 2020 Dell Inc. or its subsidiaries. All Rights Reserved.
# 
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#       http://www.apache.org/licenses/LICENSE-2.0
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#
# rescaletest
# This script will rescale helm deployments created as part of the scaletest
# Finally delete all the pvc that were created


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

TARGET=$(expr $REPLICAS \* 1)
echo "Targeting replicas: $REPLICAS"
echo "Targeting pods: $TARGET"

helm upgrade scalevoltest --install --set "name=scalevoltest,namespace=test,replicas=$REPLICAS,storageClass=unity" --namespace "${NAMESPACE}" "${TEST}"

waitOnRunning() {
  if [ "$1" = "" ]; 
    then echo "arg: target" ; 
    exit 2; 
  fi
  WAITINGFOR=$1
  
  RUNNING=$(kubectl get pods -n "${NAMESPACE}" | grep "Running" | wc -l)
  while [ $RUNNING -ne $WAITINGFOR ];
  do
  	  sleep 15
	  RUNNING=$(kubectl get pods -n "${NAMESPACE}" | grep "Running" | wc -l)
	  CREATING=$(kubectl get pods -n "${NAMESPACE}" | grep "ContainerCreating" | wc -l)
	  TERMINATING=$(kubectl get pods -n "${NAMESPACE}" | grep "Terminating" | wc -l)
	  PVCS=$(kubectl get pvc -n "${NAMESPACE}" | wc -l)
	  date
	  date >>log.output
	  echo running $RUNNING creating $CREATING terminating $TERMINATING pvcs $PVCS
	  echo running $RUNNING creating $CREATING terminating $TERMINATING pvcs $PVCS >>log.output
  done
}

waitOnRunning $TARGET

echo "Re scale to $REPLICAS completed successfully"
