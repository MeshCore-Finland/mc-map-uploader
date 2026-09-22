package uploader

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Advert is the verified subset of a MeshCore ADVERT packet required by the map.
type Advert struct {
	PacketID  string
	PublicKey string
	Timestamp uint32
	Latitude  float64
	Longitude float64
	Type      string
	Name      string
	RawHex    string
}

var ErrNotAdvert = errors.New("not an ADVERT packet")
var ErrNotUploadable = errors.New("advert type is not uploadable")
var ErrMissingCoordinates = errors.New("advert has no usable coordinates")

// DecodeAdvert follows the packet and signed-advert layouts in MeshCore's
// packet format. Only the exact packet bytes are forwarded to the map.
func DecodeAdvert(raw []byte) (Advert, error) {
	var out Advert
	if len(raw) < 2 || len(raw) > 255 {
		return out, errors.New("invalid packet length")
	}
	header := raw[0]
	if header>>6 != 0 {
		return out, errors.New("unsupported packet version")
	}
	if (header>>2)&0x0f != 4 {
		return out, ErrNotAdvert
	}
	offset := 1
	if header&3 == 0 || header&3 == 3 { // transport routing has two uint16 codes
		offset += 4
	}
	if offset >= len(raw) {
		return out, errors.New("missing path length")
	}
	pathByte := raw[offset]
	offset++
	hashSize, hashCount := int(pathByte>>6)+1, int(pathByte&63)
	pathSize := hashSize * hashCount
	if hashSize == 4 || pathSize > 64 || offset+pathSize > len(raw) {
		return out, errors.New("invalid packet path")
	}
	offset += pathSize
	payload := raw[offset:]
	identity := sha256.Sum256(append([]byte{4}, payload...))
	out.PacketID = hex.EncodeToString(identity[:8])
	if len(payload) >= 32 {
		out.PublicKey = hex.EncodeToString(payload[:32])
	}
	if len(payload) < 100 || len(payload) > 184 {
		return out, errors.New("invalid advert payload length")
	}
	key := payload[:32]
	stamp := payload[32:36]
	out.Timestamp = binary.LittleEndian.Uint32(stamp)
	signature := payload[36:100]
	app := payload[100:]
	if len(app) == 0 || len(app) > 32 {
		return out, errors.New("invalid advert app data")
	}
	signed := make([]byte, 0, 36+len(app))
	signed = append(signed, key...)
	signed = append(signed, stamp...)
	signed = append(signed, app...)
	if !ed25519.Verify(ed25519.PublicKey(key), signed, signature) {
		return out, errors.New("invalid advert signature")
	}
	switch app[0] & 0x0f {
	case 1:
		out.Type = "CHAT"
	case 2:
		out.Type = "REPEATER"
	case 3:
		out.Type = "ROOM"
	case 4:
		out.Type = "SENSOR"
	default:
		out.Type = fmt.Sprintf("UNKNOWN_%d", app[0]&0x0f)
	}
	index := 1
	hasCoordinates := app[0]&0x10 != 0
	if hasCoordinates {
		if len(app) < 9 {
			return out, errors.New("truncated advert coordinates")
		}
		latitude := int32(binary.LittleEndian.Uint32(app[1:5]))
		longitude := int32(binary.LittleEndian.Uint32(app[5:9]))
		out.Latitude = float64(latitude) / 1_000_000
		out.Longitude = float64(longitude) / 1_000_000
		index += 8
	}
	if app[0]&0x20 != 0 {
		index += 2
	}
	if app[0]&0x40 != 0 {
		index += 2
	}
	if index > len(app) {
		return out, fmt.Errorf("truncated advert app data")
	}
	if app[0]&0x80 != 0 {
		name := app[index:]
		if !utf8.Valid(name) {
			return out, errors.New("invalid advert name encoding")
		}
		out.Name = strings.TrimSpace(string(name))
	}
	if out.Type != "REPEATER" && out.Type != "ROOM" && out.Type != "SENSOR" {
		return out, ErrNotUploadable
	}
	if !hasCoordinates || (out.Latitude == 0 && out.Longitude == 0) {
		return out, ErrMissingCoordinates
	}
	out.RawHex = hex.EncodeToString(raw)
	return out, nil
}
