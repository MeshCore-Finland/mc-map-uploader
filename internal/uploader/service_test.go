package uploader

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mc-map-uploader/internal/mqtt"
)

func TestIATAAllowlist(t *testing.T) {
	for _, value := range []string{"", "hel", "HEL1", "HEL,", "HEL, hel"} {
		if _, err := parseAllowlist(value); err == nil {
			t.Fatalf("invalid allowlist %q accepted", value)
		}
	}
	allowed, err := parseAllowlist("HEL, REGIONX")
	if err != nil || len(allowed) != 2 {
		t.Fatalf("valid allowlist rejected: %v", err)
	}
	keyFile := filepath.Join(t.TempDir(), "map.key")
	if err := CreateKeyFile(keyFile); err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{APIURL: "https://map.meshcore.io/api/v1/uploader/node", KeyFile: keyFile, AllowedIATA: "HEL", GeofencePolygons: []Polygon{{{59.8, 23.8}, {59.8, 24.2}, {60.2, 24.2}, {60.2, 23.8}}}, DryRun: true, HTTPTimeout: time.Second, QueueSize: 2, Attempts: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	observer := strings.Repeat("a", 64)
	status := []byte(`{"params":{"freq":869.525,"bw":125,"sf":11,"cr":5}}`)
	service.Handle(mqtt.Message{Topic: "meshcore/STO/" + observer + "/status", Payload: status})
	if len(service.status) != 0 {
		t.Fatal("disallowed IATA seeded status")
	}
	service.Handle(mqtt.Message{Topic: "meshcore/HEL/" + observer + "/status", Payload: status})
	if len(service.status) != 1 {
		t.Fatal("allowed IATA status not accepted")
	}
	for _, app := range [][]byte{{0x82}, {0x12, 0, 0, 0, 0, 0, 0, 0, 0}} {
		packet := []byte(`{"raw":"` + hex.EncodeToString(signedPacketWithApp(t, app)) + `"}`)
		service.Handle(mqtt.Message{Topic: "meshcore/HEL/" + observer + "/packets", Payload: packet})
		if len(service.jobs) != 0 {
			t.Fatal("advert without usable coordinates created upload job")
		}
	}
	outside := make([]byte, 9)
	outside[0] = 0x12
	binary.LittleEndian.PutUint32(outside[1:5], uint32(59_000_000))
	binary.LittleEndian.PutUint32(outside[5:9], uint32(24_000_000))
	service.Handle(mqtt.Message{Topic: "meshcore/HEL/" + observer + "/packets", Payload: []byte(`{"raw":"` + hex.EncodeToString(signedPacketWithApp(t, outside)) + `"}`)})
	if len(service.jobs) != 0 {
		t.Fatal("advert outside configured geofence created upload job")
	}
	packet := []byte(`{"raw":"` + hex.EncodeToString(signedPacket(t, 2)) + `"}`)
	service.Handle(mqtt.Message{Topic: "meshcore/STO/" + observer + "/packets", Payload: packet})
	if len(service.jobs) != 0 {
		t.Fatal("disallowed IATA created upload job")
	}
	service.Handle(mqtt.Message{Topic: "meshcore/HEL/" + observer + "/packets", Payload: packet})
	if len(service.jobs) != 1 {
		t.Fatal("allowed IATA did not create upload job")
	}
}
