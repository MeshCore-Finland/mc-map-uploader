//go:build jscompare

package uploader

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

// Optional parity check against @liamcottle/meshcore.js. It is never part
// of normal Go tests or service runtime.
func TestJavaScriptDecoderParity(t *testing.T) {
	module := os.Getenv("MESHCORE_JS_MODULE")
	if module == "" {
		t.Skip("set MESHCORE_JS_MODULE to the package's absolute JS entry-point path")
	}
	packet := signedPacket(t, 2)
	want, err := DecodeAdvert(packet)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "../../testdata/js_decode.mjs", module, hex.EncodeToString(packet))
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("JavaScript decoder failed: %v", err)
	}
	var got struct {
		PublicKey string `json:"publicKey"`
		Timestamp uint32 `json:"timestamp"`
		Type      string `json:"type"`
		Name      string `json:"name"`
		Verified  bool   `json:"verified"`
	}
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Verified || got.PublicKey != want.PublicKey || got.Timestamp != want.Timestamp || got.Type != want.Type || got.Name != want.Name {
		t.Fatalf("decoder mismatch: Go=%+v JS=%+v", want, got)
	}
}
