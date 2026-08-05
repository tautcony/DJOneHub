package httpapi

func openAPIDocument() map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": "DJOneHub API", "version": "1.0.0"},
		"paths": map[string]any{
			"/api/v1/device":                      readPath("single-device status"),
			"/api/v1/device/status":               readPath("single-device status"),
			"/api/v1/device/capabilities":         readPath("device capabilities"),
			"/api/v1/device/actions/rescan":       commandPath("rescan result", false),
			"/api/v1/device/actions/reboot":       commandPath("reboot accepted", true),
			"/api/v1/device/actions/raw-at":       commandPath("raw AT response", false),
			"/api/v1/firmware":                    readPath("firmware and device mode status"),
			"/api/v1/firmware/actions/adb/unlock": commandPath("ADB unlock operation", true),
			"/api/v1/firmware/actions/adb/mode":   commandPath("ADB mode operation", true),
			"/api/v1/firmware/actions/adb/shell/ws": map[string]any{
				"get": map[string]any{"responses": map[string]any{"101": map[string]any{"description": "interactive ADB shell"}}},
			},
			"/api/v1/firmware/actions/usb-id":                      commandPath("USB ID operation", true),
			"/api/v1/firmware/actions/mode":                        commandPath("firmware mode operation", true),
			"/api/v1/firmware/actions/backup":                      commandPath("firmware backup operation", true),
			"/api/v1/firmware/actions/backup/select-directory":     commandPath("firmware backup directory selection", false),
			"/api/v1/firmware/actions/backup/select-edl-directory": commandPath("EDL tool directory selection", false),
			"/api/v1/firmware/actions/select-adb-file":             commandPath("adb executable selection", false),
			"/api/v1/firmware/actions/adb/settings":                commandPath("adb command setting", false),
			"/api/v1/sms/actions/refresh":                          commandPath("SMS list", false),
			"/api/v1/sms/actions/send":                             commandPath("operation accepted", true),
			"/api/v1/sms/actions/clear":                            commandPath("SMS clear result", false),
			"/api/v1/esim":                                         readPath("eSIM profiles"),
			"/api/v1/esim/actions/download":                        commandPath("operation accepted", true),
			"/api/v1/esim/actions/enable":                          commandPath("operation accepted", true),
			"/api/v1/esim/actions/rename":                          commandPath("eSIM rename result", false),
			"/api/v1/esim/actions/delete":                          commandPath("operation accepted", true),
			"/api/v1/esim/health":                                  readPath("eSIM health"),
			"/api/v1/esim/notes":                                   readPath("local profile notes"),
			"/api/v1/network":                                      readPath("network status"),
			"/api/v1/network/actions/mode":                         commandPath("operation accepted", true),
			"/api/v1/network/actions/check":                        commandPath("connectivity result", false),
			"/api/v1/network/actions/traffic":                      readPath("network traffic"),
			"/api/v1/network/traffic/daily":                        readPath("daily network traffic for the active ICCID"),
			"/api/v1/network/traffic/range":                        readPath("daily network traffic range for the active ICCID"),
			"/api/v1/network/diagnostics":                          readPath("network diagnostics"),
			"/api/v1/calls":                                        readPath("call monitor"),
			"/api/v1/calls/actions/reject":                         commandPath("call rejection", false),
			"/api/v1/vowifi":                                       readPath("VoWiFi status"),
			"/api/v1/vowifi/actions/enable":                        commandPath("operation accepted", true),
			"/api/v1/vowifi/actions/disable":                       commandPath("operation accepted", true),
			"/api/v1/vowifi/actions/reconnect":                     commandPath("operation accepted", true),
			"/api/v1/notifications/debug": map[string]any{
				"get":  map[string]any{"responses": responses("notifier debug capabilities")},
				"post": commandPath("published notifier debug events", false)["post"],
			},
			"/api/v1/notifications/permissions":               readPath("notification permission status"),
			"/api/v1/notifications/permissions/request":       commandPath("notification permission request", false),
			"/api/v1/notifications/permissions/open-settings": commandPath("notification settings open", false),
			"/api/v1/notifications/preferences": map[string]any{
				"get": map[string]any{"responses": responses("notification presentation preferences")},
				"put": commandPath("notification presentation preferences", false)["post"],
			},
			"/api/v1/settings/startup": map[string]any{
				"get": map[string]any{"responses": responses("login startup status")},
				"put": commandPath("login startup status", false)["post"],
			},
			"/api/v1/operations/{operation_id}": map[string]any{
				"get": map[string]any{"parameters": []any{map[string]any{
					"name": "operation_id", "in": "path", "required": true, "schema": map[string]any{"type": "string"},
				}}, "responses": responses("operation status")},
			},
			"/api/v1/openapi.json": map[string]any{
				"get": map[string]any{"responses": responses("OpenAPI document")},
			},
			"/api/v1/events/ws": map[string]any{
				"get": map[string]any{"responses": map[string]any{
					"101": map[string]any{"description": "event stream"},
					"401": map[string]any{"$ref": "#/components/responses/Error"},
				}},
			},
		},
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

func readPath(description string) map[string]any {
	return map[string]any{"get": map[string]any{"responses": responses(description)}}
}

func commandPath(description string, async bool) map[string]any {
	status := "200"
	response := description
	if async {
		status = "202"
		response = "operation accepted"
	}
	return map[string]any{"post": map[string]any{"responses": map[string]any{
		status: map[string]any{"description": response},
		"400":  map[string]any{"$ref": "#/components/responses/Error"},
		"422":  map[string]any{"$ref": "#/components/responses/Error"},
		"503":  map[string]any{"$ref": "#/components/responses/Error"},
	}}}
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
