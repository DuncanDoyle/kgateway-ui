{{- define "kgateway-ui.name" -}}kgateway-ui{{- end }}
{{- define "kgateway-ui.fullname" -}}{{ .Release.Name }}-kgateway-ui{{- end }}
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
{{- define "kgateway-ui.sa" -}}{{ default (include "kgateway-ui.fullname" .) .Values.serviceAccount.name }}{{- end }}
