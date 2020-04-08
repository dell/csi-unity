#!/usr/bin/env bash

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
