{{/*
Expand the name of the chart.
*/}}
{{- define "payment-provider-simulator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "payment-provider-simulator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart name and version as used by the chart label.
*/}}
{{- define "payment-provider-simulator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "payment-provider-simulator.labels" -}}
helm.sh/chart: {{ include "payment-provider-simulator.chart" . }}
{{ include "payment-provider-simulator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels. Every Deployment and Service also pins
app.kubernetes.io/component (api): a Service selecting on these two alone would
select every component a future change adds.
*/}}
{{- define "payment-provider-simulator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "payment-provider-simulator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "payment-provider-simulator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "payment-provider-simulator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the Secret holding DATABASE_URL: the operator's own
(database.existingSecret) or the one this chart creates from database.url.
*/}}
{{- define "payment-provider-simulator.databaseSecretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else }}
{{- include "payment-provider-simulator.fullname" . }}-database
{{- end }}
{{- end }}

{{/*
Image reference. The release job publishes the image as v<version> (and packages
the chart with appVersion <version>), so an empty image.tag means "v" + appVersion.
*/}}
{{- define "payment-provider-simulator.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag | default (printf "v%s" .Chart.AppVersion) }}
{{- end }}

{{/*
MANDATORY: cmd/api exits at boot without DATABASE_URL (real Postgres only, no
in-memory mode). Fails the render naming the values instead of crash-looping.
*/}}
{{- define "payment-provider-simulator.requireDatabase" -}}
{{- if not (or .Values.database.url .Values.database.existingSecret) -}}
{{- fail "payment-provider-simulator requires database.url or database.existingSecret to be set (DATABASE_URL): the service runs on real Postgres only and exits at boot without it." -}}
{{- end -}}
{{- end -}}

{{/*
extraEnv may not bring back what the chart owns: no *_MODE variable (this
service has no mode: Postgres is the only runtime), no *_CONSUMER_GROUP (the
simulator publishes and consumes no events - ADR 0002 A16 - so there is no
consumer to group), and none of the variables the chart renders itself (the
same variable is never set twice).
*/}}
{{- define "payment-provider-simulator.requireCleanExtraEnv" -}}
{{- $owned := list "HTTP_ADDR" "LOG_LEVEL" "DATABASE_URL" "MIGRATIONS_DATABASE_URL" "PROVIDER_TIMEOUT_DELAY" "SHUTDOWN_DRAIN_DELAY" "OTEL_EXPORTER_OTLP_ENDPOINT" "OTEL_SERVICE_NAME" "SERVICE_VERSION" "ENVIRONMENT" -}}
{{- range .Values.extraEnv -}}
{{- $name := toString .name -}}
{{- if hasSuffix "_MODE" $name -}}
{{- fail (printf "extraEnv sets %s: *_MODE variables do not exist here - payment-provider-simulator runs on real Postgres only, with no permissive, off or in-memory mode." $name) -}}
{{- end -}}
{{- if hasSuffix "_CONSUMER_GROUP" $name -}}
{{- fail (printf "extraEnv sets %s: payment-provider-simulator runs no consumer (it publishes and consumes no events, ADR 0002 A16), so there is no group id to set." $name) -}}
{{- end -}}
{{- if has $name $owned -}}
{{- fail (printf "extraEnv sets %s, which the chart renders from its own values: use that value instead." $name) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
SHUTDOWN_DRAIN_DELAY in whole seconds (the binary accepts Go durations; the chart
accepts "Ns", "Nm" and "0" because it needs the number to size the grace
period). Unset = the binary's default, 5s.
*/}}
{{- define "payment-provider-simulator.drainSeconds" -}}
{{- $raw := toString .Values.config.shutdownDrainDelay -}}
{{- if eq $raw "" -}}5
{{- else if eq $raw "0" -}}0
{{- else if regexMatch "^[0-9]+s$" $raw -}}{{ trimSuffix "s" $raw }}
{{- else if regexMatch "^[0-9]+m$" $raw -}}{{ mul 60 (int (trimSuffix "m" $raw)) }}
{{- else -}}
{{- fail (printf "config.shutdownDrainDelay is %q - want whole seconds (\"5s\"), minutes (\"1m\") or \"0\"." $raw) -}}
{{- end -}}
{{- end -}}

{{/*
The graceful shutdown is sequential (cmd/api/main.go): /readyz flips,
SHUTDOWN_DRAIN_DELAY passes, then the HTTP drain bounded at 10s, plus a 10s
buffer. A grace period that does not exceed drain + 20s can SIGKILL an
in-flight authorization mid-drain.
*/}}
{{- define "payment-provider-simulator.requireGracePeriod" -}}
{{- $need := add (int (include "payment-provider-simulator.drainSeconds" .)) 20 -}}
{{- if le (int .Values.terminationGracePeriodSeconds) $need -}}
{{- fail (printf "terminationGracePeriodSeconds is %v - it must exceed %v (SHUTDOWN_DRAIN_DELAY %vs + HTTP drain 10s + 10s buffer): a shorter period SIGKILLs an in-flight request mid-drain." .Values.terminationGracePeriodSeconds $need (include "payment-provider-simulator.drainSeconds" .)) -}}
{{- end -}}
{{- end -}}

{{/*
The port of HTTP_ADDR is the container port the probes and the Service target.
*/}}
{{- define "payment-provider-simulator.requireHttpPort" -}}
{{- $port := trimPrefix ":" (regexFind ":[0-9]+$" (toString .Values.config.httpAddr)) -}}
{{- if ne $port (toString .Values.service.targetPort) -}}
{{- fail (printf "config.httpAddr is %q but service.targetPort is %v: the container port, the probes and the Service must agree." .Values.config.httpAddr .Values.service.targetPort) -}}
{{- end -}}
{{- end -}}

{{/*
Every render-time requirement of the real runtime, in one include. The api
Deployment (always rendered) includes it, so a mis-set release fails
`helm template` / `helm install`.
*/}}
{{- define "payment-provider-simulator.requireRealRuntime" -}}
{{- include "payment-provider-simulator.requireDatabase" . -}}
{{- include "payment-provider-simulator.requireCleanExtraEnv" . -}}
{{- include "payment-provider-simulator.requireGracePeriod" . -}}
{{- include "payment-provider-simulator.requireHttpPort" . -}}
{{- end -}}
