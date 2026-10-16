{{- define "k8s-fm.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "k8s-fm.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{ include "k8s-fm.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "k8s-fm.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "k8s-fm.serviceAccountName" -}}
{{- default (include "k8s-fm.fullname" .) .Values.serviceAccount.name -}}
{{- end -}}

{{- define "k8s-fm.secretName" -}}
{{- if .Values.spotify.existingSecret -}}
{{- .Values.spotify.existingSecret -}}
{{- else -}}
{{- printf "%s-spotify" (include "k8s-fm.fullname" .) -}}
{{- end -}}
{{- end -}}
