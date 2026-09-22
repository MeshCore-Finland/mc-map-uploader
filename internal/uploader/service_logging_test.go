package uploader

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mc-map-uploader/internal/mqtt"
)

func TestAdvertIntakeLogsStartAndCompletion(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "map.key")
	if err := CreateKeyFile(keyFile); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	service, err := New(Config{APIURL: "https://map.meshcore.io/api/v1/uploader/node", KeyFile: keyFile, AllowedIATA: "HEL", GeofencePolygons: []Polygon{{{59.8, 23.8}, {59.8, 24.2}, {60.2, 24.2}, {60.2, 23.8}}}, DryRun: true, HTTPTimeout: time.Second, QueueSize: 1, Attempts: 1}, logger)
	if err != nil {
		t.Fatal(err)
	}
	observer := strings.Repeat("a", 64)
	packetMessage := func(region string, packet []byte) mqtt.Message {
		return mqtt.Message{Topic: "meshcore/" + region + "/" + observer + "/packets", Payload: []byte(`{"raw":"` + hex.EncodeToString(packet) + `"}`)}
	}
	valid := signedPacket(t, 2)
	service.Handle(packetMessage("HEL", valid)) // No observer status.
	service.Handle(mqtt.Message{Topic: "meshcore/HEL/" + observer + "/status", Payload: []byte(`{"params":{"freq":869.525,"bw":125,"sf":11,"cr":5}}`)})
	service.Handle(packetMessage("HEL", signedPacketWithApp(t, []byte{0x82}))) // No coordinates.
	service.Handle(packetMessage("HEL", signedPacket(t, 1)))                   // Named chat advert.
	outside := make([]byte, 9)
	outside[0] = 0x12
	binary.LittleEndian.PutUint32(outside[1:5], uint32(59_000_000))
	binary.LittleEndian.PutUint32(outside[5:9], uint32(24_000_000))
	service.Handle(packetMessage("HEL", signedPacketWithApp(t, outside)))
	service.Handle(packetMessage("STO", valid))
	service.Handle(packetMessage("HEL", valid))              // Queued.
	service.Handle(packetMessage("HEL", valid))              // Packet duplicate.
	service.Handle(packetMessage("HEL", signedPacket(t, 2))) // Queue full.

	wantReasons := []string{"observer_status_unavailable", ErrMissingCoordinates.Error(), ErrNotUploadable.Error(), "outside_geofence", "region_not_allowed", "upload_queued", "packet_duplicate", "upload_queue_full"}
	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	if len(lines) != 2*len(wantReasons) {
		t.Fatalf("got %d log lines, want %d:\n%s", len(lines), 2*len(wantReasons), output.String())
	}
	for i, reason := range wantReasons {
		var start, end map[string]any
		if err := json.Unmarshal(lines[2*i], &start); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(lines[2*i+1], &end); err != nil {
			t.Fatal(err)
		}
		if start["msg"] != "advert received" || end["msg"] != "advert processing complete" || start["processing_id"] == "" || start["processing_id"] != end["processing_id"] || start["packet_id"] != end["packet_id"] || end["reason"] != reason {
			t.Fatalf("bad advert log pair for %q:\n%s\n%s", reason, lines[2*i], lines[2*i+1])
		}
		if reason == ErrNotUploadable.Error() && (start["name"] != "test node" || start["type"] != "CHAT") {
			t.Fatalf("named chat advert lost metadata: %s", lines[2*i])
		}
		if reason == ErrMissingCoordinates.Error() {
			if _, found := start["name"]; found {
				t.Fatalf("unnamed advert logged an empty name: %s", lines[2*i])
			}
		}
	}
}
