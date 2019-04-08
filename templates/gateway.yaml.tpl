type: google.api.Service
config_version: 3
http:
  rules:
{{- range $key, $service := .GetServices -}}
{{- range $key, $method := $service.GetMethods }}
    - selector: {{ $method.GetFullyQualifiedName }}
      post: /{{ replace "." "/" $method.GetFullyQualifiedName }}
      body: "*"
{{- end -}}
{{- end }}
