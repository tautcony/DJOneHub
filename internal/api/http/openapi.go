package httpapi

import "strings"

func openAPIDocument() map[string]any {
	paths := make(map[string]any)
	for _, spec := range routeRegistry() {
		operations, ok := paths[spec.Pattern].(map[string]any)
		if !ok {
			operations = make(map[string]any)
			paths[spec.Pattern] = operations
		}
		operations[openAPIMethod(spec.Method)] = openAPIOperationForRoute(spec)
	}
	return map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": "DJOneHub API", "version": "1.0.0"},
		"paths":   paths,
		"components": map[string]any{
			"schemas": map[string]any{
				"Error": map[string]any{
					"type":     "object",
					"required": []string{"code", "message", "retryable"},
					"properties": map[string]any{
						"code":      map[string]any{"type": "string"},
						"message":   map[string]any{"type": "string"},
						"retryable": map[string]any{"type": "boolean"},
						"details":   map[string]any{"type": "object", "additionalProperties": true},
					},
				},
				"ErrorEnvelope": map[string]any{
					"type": "object", "required": []string{"error"},
					"properties": map[string]any{"error": map[string]any{"$ref": "#/components/schemas/Error"}},
				},
				"OperationAccepted": map[string]any{
					"type": "object", "required": []string{"operation_id"},
					"properties": map[string]any{"operation_id": map[string]any{"type": "string"}},
				},
			},
			"responses": map[string]any{
				"Error": map[string]any{
					"description": "Structured API error",
					"content": map[string]any{"application/json": map[string]any{
						"schema": map[string]any{"$ref": "#/components/schemas/ErrorEnvelope"},
					}},
				},
			},
		},
	}
}

func openAPIOperationForRoute(spec RouteSpec) map[string]any {
	operation := make(map[string]any, len(spec.Operation)+1)
	for key, value := range spec.Operation {
		operation[key] = value
	}
	var parameters []any
	for _, part := range strings.Split(spec.Pattern, "/") {
		if !strings.HasPrefix(part, "{") || !strings.HasSuffix(part, "}") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
		kind := "string"
		if name == "trace_id" || name == "sequence" {
			kind = "integer"
		}
		parameters = append(parameters, map[string]any{
			"name": name, "in": "path", "required": true, "schema": map[string]any{"type": kind},
		})
	}
	if len(parameters) > 0 {
		operation["parameters"] = parameters
	}
	return operation
}

func responses(description string) map[string]any {
	return map[string]any{
		"200": map[string]any{"description": description},
		"401": map[string]any{"$ref": "#/components/responses/Error"},
		"404": map[string]any{"$ref": "#/components/responses/Error"},
		"422": map[string]any{"$ref": "#/components/responses/Error"},
		"503": map[string]any{"$ref": "#/components/responses/Error"},
	}
}
