package uploader

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"
)

func signedPacket(t *testing.T, kind byte) []byte {
	app := make([]byte, 9)
	app[0] = 0x80 | 0x10 | kind
	binary.LittleEndian.PutUint32(app[1:5], uint32(60_000_000))
	binary.LittleEndian.PutUint32(app[5:9], uint32(24_000_000))
	app = append(app, []byte("test node")...)
	return signedPacketWithApp(t, app)
}

func signedPacketWithApp(t *testing.T, app []byte) []byte {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	stamp := make([]byte, 4)
	binary.LittleEndian.PutUint32(stamp, 12345)
	signed := append(append(append([]byte{}, public...), stamp...), app...)
	sig := ed25519.Sign(private, signed)
	packet := []byte{0x11, 0} // flood route, ADVERT payload, empty path
	packet = append(packet, public...)
	packet = append(packet, stamp...)
	packet = append(packet, sig...)
	packet = append(packet, app...)
	return packet
}

func TestDecodeAdvertRequiresUsableCoordinates(t *testing.T) {
	missing := signedPacketWithApp(t, []byte{0x82})
	if _, err := DecodeAdvert(missing); !errors.Is(err, ErrMissingCoordinates) {
		t.Fatalf("missing coordinates: got %v", err)
	}
	zero := signedPacketWithApp(t, append([]byte{0x12}, make([]byte, 8)...))
	if _, err := DecodeAdvert(zero); !errors.Is(err, ErrMissingCoordinates) {
		t.Fatalf("zero coordinates: got %v", err)
	}
	latitudeOnly := make([]byte, 9)
	latitudeOnly[0] = 0x12
	binary.LittleEndian.PutUint32(latitudeOnly[1:5], uint32(60_000_000))
	if _, err := DecodeAdvert(signedPacketWithApp(t, latitudeOnly)); err != nil {
		t.Fatalf("valid coordinate pair with zero longitude rejected: %v", err)
	}
}

func TestDecodeAdvert(t *testing.T) {
	packet := signedPacket(t, 2)
	advert, err := DecodeAdvert(packet)
	if err != nil {
		t.Fatal(err)
	}
	if advert.Type != "REPEATER" || advert.Timestamp != 12345 || advert.Name != "test node" || advert.RawHex != hex.EncodeToString(packet) {
		t.Fatalf("unexpected advert: %+v", advert)
	}
	identity := sha256.Sum256(append([]byte{4}, packet[2:]...))
	if advert.PacketID != hex.EncodeToString(identity[:8]) {
		t.Fatalf("packet ID %q does not match MeshCore content hash", advert.PacketID)
	}
	routed := append([]byte{packet[0], 1, 0xE4}, packet[2:]...)
	routedAdvert, err := DecodeAdvert(routed)
	if err != nil || routedAdvert.PacketID != advert.PacketID {
		t.Fatalf("path changed packet identity: %+v, %v", routedAdvert, err)
	}
	packet[len(packet)-1] ^= 1
	if _, err := DecodeAdvert(packet); err == nil {
		t.Fatal("changed signed data was accepted")
	}
	chat, err := DecodeAdvert(signedPacket(t, 1))
	if !errors.Is(err, ErrNotUploadable) || chat.Type != "CHAT" || chat.Name != "test node" {
		t.Fatalf("CHAT advert should retain its decoded name before rejection, got %+v, %v", chat, err)
	}
}

func TestRadioAndPacketInputs(t *testing.T) {
	radio, err := ParseRadio([]byte(`{"params":{"frequency":869525000,"bandwidth":125000,"spreadingFactor":11,"codingRate":5}}`))
	if err != nil {
		t.Fatal(err)
	}
	if radio.Freq != 869.525 || radio.BW != 125 || radio.SF != 11 || radio.CR != 5 {
		t.Fatalf("unexpected radio: %+v", radio)
	}
	radio, err = ParseRadio([]byte(`{"radio":"869.525 MHz BW125 SF11 CR5"}`))
	if err != nil || !radio.Valid() || radio.Freq != 869.525 {
		t.Fatalf("radio string parsing failed: %+v %v", radio, err)
	}
	packet := signedPacket(t, 4)
	decoded, err := ParsePacket([]byte(`{"raw":"` + hex.EncodeToString(packet) + `"}`))
	if err != nil || hex.EncodeToString(decoded) != hex.EncodeToString(packet) {
		t.Fatalf("packet parsing failed: %v", err)
	}
	if _, err := ParseRadio([]byte(`{"freq":0}`)); err == nil {
		t.Fatal("invalid radio accepted")
	}
}
