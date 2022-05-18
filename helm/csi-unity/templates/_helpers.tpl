{{/*
Return the appropriate sidecar images based on k8s version
*/}}
{{- define "csi-unity.attacherImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "24") -}}
      {{- print "k8s.gcr.io/sig-storage/csi-attacher:v3.4.0" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "csi-unity.provisionerImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "24") -}}
      {{- print "k8s.gcr.io/sig-storage/csi-provisioner:v3.1.0" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "csi-unity.snapshotterImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "24") -}}
      {{- print "k8s.gcr.io/sig-storage/csi-snapshotter:v5.0.1" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "csi-unity.resizerImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "24") -}}
      {{- print "k8s.gcr.io/sig-storage/csi-resizer:v1.4.0" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "csi-unity.registrarImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "24") -}}
      {{- print "k8s.gcr.io/sig-storage/csi-node-driver-registrar:v2.5.0" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "csi-unity.healthmonitorImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "24") -}}
      {{- print "gcr.io/k8s-staging-sig-storage/csi-external-health-monitor-controller:v0.4.0" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}