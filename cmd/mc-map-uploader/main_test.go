package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"mc-map-uploader/internal/config"
	"mc-map-uploader/internal/mqtt"
	"mc-map-uploader/internal/uploader"
)

func TestStartupConfigLogOmitsSecrets(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	logStartupConfig(logger, "/app/config.yaml", config.Runtime{
		MQTT:     mqtt.Config{Address: "127.0.0.1:1883", Username: "uploader", Password: "secret-password", ClientID: "mc-map-uploader", StatusFilter: "meshcore/+/+/status", PacketsFilter: "meshcore/+/+/packets"},
		Uploader: uploader.Config{APIURL: "https://map.meshcore.io/api/v1/uploader/node", KeyFile: "/app/secrets/map-signing.key", AllowedIATA: "HEL", DryRun: false, HTTPTimeout: 10 * time.Second, QueueSize: 250, Attempts: 3, RetryDelay: 5 * time.Second},
	})
	if bytes.Contains(output.Bytes(), []byte("secret-password")) {
		t.Fatal("MQTT password leaked into startup log")
	}
	var record struct {
		MQTT map[string]any `json:"mqtt"`
		Map  map[string]any `json:"map"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.MQTT["status_filter"] != "meshcore/+/+/status" || record.Map["dry_run"] != false || record.Map["upload_queue"] != float64(250) {
		t.Fatalf("startup settings missing: %s", output.String())
	}
}
