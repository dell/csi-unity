#!/bin/bash
#
# Copyright (c) 2020 Dell Inc., or its subsidiaries. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#  http://www.apache.org/licenses/LICENSE-2.0

# verify-csi-unity method
function verify-csi-unity() {
  verify_k8s_versions "1.24" "1.29"
  verify_openshift_versions "4.13" "4.14"
  verify_namespace "${NS}"
  verify_required_secrets "${RELEASE}-creds"
  verify_optional_secrets "${RELEASE}-certs"
  verify_alpha_snap_resources
  verify_unity_protocol_installation
  verify_snap_requirements  
  verify_helm_3
  verify_helm_values_version "${DRIVER_VERSION}"
}


function verify_unity_protocol_installation() {
if [ ${NODE_VERIFY} -eq 0 ]; then
    return
  fi

  log smart_step "Verifying sshpass installation.."
  SSHPASS=$(which sshpass)
  if [ -z "$SSHPASS" ]; then
   found_warning "sshpass is not installed. It is mandatory to have ssh pass software for multi node kubernetes setup."
  fi
  
   
  log smart_step "Verifying iSCSI installation" "$1"

  error=0
  for node in $MINION_NODES; do
    # check if the iSCSI client is installed
    echo
    echo -n "Enter the ${NODEUSER} password of ${node}: "
    read -s nodepassword
    echo
    echo "$nodepassword" > protocheckfile
    chmod 0400 protocheckfile
    unset nodepassword
    run_command sshpass -f protocheckfile ssh -o StrictHostKeyChecking=no ${NODEUSER}@"${node}" "cat /etc/iscsi/initiatorname.iscsi" > /dev/null 2>&1
    rv=$?
    if [ $rv -ne 0 ]; then
      error=1
      found_warning "iSCSI client is either not found on node: $node or not able to verify"
    fi
    run_command sshpass -f protocheckfile ssh -o StrictHostKeyChecking=no ${NODEUSER}@"${node}" "pgrep iscsid" > /dev/null 2>&1
    rv1=$?
    if [ $rv1 -ne 0 ]; then
      error=1
      found_warning "iscsid service is either not running on node: $node or not able to verify"
    fi
    rm -f protocheckfile
  done
  check_error error
}
