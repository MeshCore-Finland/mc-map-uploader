package uploader

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMapRequestSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("wrong request method or content type")
		}
		var body struct{ Data, Signature, PublicKey string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		key, _ := hex.DecodeString(body.PublicKey)
		sig, _ := hex.DecodeString(body.Signature)
		hash := sha256.Sum256([]byte(body.Data))
		if !ed25519.Verify(key, hash[:], sig) {
			t.Errorf("invalid request signature")
		}
		var data struct {
			Params Radio    `json:"params"`
			Links  []string `json:"links"`
		}
		if err := json.Unmarshal([]byte(body.Data), &data); err != nil || len(data.Links) != 1 || data.Links[0] != "meshcore://1234" {
			t.Errorf("wrong signed data: %s", body.Data)
		}
		_, _ = w.Write([]byte(`{"code":"NODES_INSERTED"}`))
	}))
	defer server.Close()
	poster, err := NewPoster(server.URL, time.Second, false, make([]byte, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	accepted, retry, err := poster.Post(context.Background(), Job{
		Advert: Advert{RawHex: "1234"},
		Radio:  Radio{Freq: 869.525, BW: 125, SF: 11, CR: 5},
	}, 1)
	if err != nil || retry || !accepted {
		t.Fatalf("unexpected result: accepted=%t retry=%t err=%v", accepted, retry, err)
	}
}

func TestDuplicateMapResponseSuppressesRepeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"ERR_ADVERT_DUPLICATE"}`))
	}))
	defer server.Close()
	poster, err := NewPoster(server.URL, time.Second, false, make([]byte, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	handled, retry, err := poster.Post(context.Background(), Job{Advert: Advert{RawHex: "1234"}, Radio: Radio{Freq: 869.525, BW: 125, SF: 11, CR: 5}}, 1)
	if !handled || retry || err != nil {
		t.Fatalf("duplicate should be terminal and suppress repeats: %t %t %v", handled, retry, err)
	}
}
