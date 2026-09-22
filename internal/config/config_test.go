package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mc-map-uploader/internal/uploader"
)

const validYAML = `mqtt:
  address: 127.0.0.1:1883
  username: uploader
  password_file: mqtt-password
  client_id: mc-map-uploader
  status_filter: meshcore/HEL/+/status
  packets_filter: meshcore/HEL/+/packets
map:
  api_url: https://map.meshcore.io/api/v1/uploader/node
  key_file: map.key
  allowed_iata: [HEL]
  dry_run: true
  http_timeout: 10s
  upload_queue: 250
  upload_attempts: 3
  retry_delay: 5s
`

func TestStrictConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mqtt-password"), []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := uploader.CreateKeyFile(filepath.Join(dir, "map.key")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	check := func(input string) error {
		if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(path)
		return err
	}
	if err := check(validYAML); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if err := check(strings.ReplaceAll(validYAML, "HEL", "REGIONX")); err != nil {
		t.Fatalf("long uppercase region code rejected: %v", err)
	}
	for name, input := range map[string]string{
		"unknown field":     strings.Replace(validYAML, "  username: uploader", "  usernme: uploader", 1),
		"missing dry-run":   strings.Replace(validYAML, "  dry_run: true\n", "", 1),
		"bad region":        strings.Replace(validYAML, "allowed_iata: [HEL]", "allowed_iata: [hel]", 1),
		"mismatched filter": strings.Replace(validYAML, "status_filter: meshcore/HEL/+/status", "status_filter: meshcore/STO/+/status", 1),
		"second document":   validYAML + "---\nmap: {}\n",
		"duplicate field":   strings.Replace(validYAML, "  dry_run: true", "  dry_run: true\n  dry_run: false", 1),
		"bad geofence":      strings.Replace(validYAML, "  dry_run: true", "  geofence_polygons: [[[91, 24], [60, 25], [61, 25]]]\n  dry_run: true", 1),
	} {
		if err := check(input); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
