{{- define "kgateway-ui.fullname" -}}{{ printf "%s-kgateway-ui" .Release.Name | trunc 63 | trimSuffix "-" }}{{- end }}
{{- define "kgateway-ui.labels" -}}
app.kubernetes.io/name: kgateway-ui
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}
{{- define "kgateway-ui.selector" -}}
app.kubernetes.io/name: kgateway-ui
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
{{- define "kgateway-ui.sa" -}}
{{- if and (not .Values.serviceAccount.create) (not .Values.serviceAccount.name) -}}
{{- fail "serviceAccount.name is required when serviceAccount.create is false" -}}
{{- end -}}
{{ default (include "kgateway-ui.fullname" .) .Values.serviceAccount.name }}
{{- end }}
