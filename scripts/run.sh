#!/bin/bash

# Copyright © 2020-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#      http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

#!/usr/bin/env bash
#
#  Copyright © 2019 Dell Inc. or its subsidiaries. All Rights Reserved.
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

echo "Endpoint ${CSI_ENDPOINT}"
if [[ -S ${CSI_ENDPOINT} ]]; then
    rm -rf ${CSI_ENDPOINT}
    echo "Removed endpoint $CSI_ENDPOINT"
    ls -l ${CSI_ENDPOINT}
elif [[ ${CSI_ENDPOINT} == unix://* ]]; then
    ENDPOINT=${CSI_ENDPOINT/unix:\/\//}
    ls -l ${ENDPOINT}
    if [[ -S ${ENDPOINT} ]]; then
        rm -rf ${ENDPOINT}
        echo "Removed endpoint $ENDPOINT"
    fi
fi
exec /csi-unity "$@"
