package uploader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Poster struct {
	url       string
	dryRun    bool
	client    *http.Client
	secret    []byte
	publicHex string
	logger    *slog.Logger
}

func NewPoster(apiURL string, timeout time.Duration, dryRun bool, secret []byte, logger *slog.Logger) (*Poster, error) {
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
		return nil, errors.New("MAP_API_URL must be an HTTP(S) URL without credentials")
	}
	publicHex, err := PublicKeyHex(secret)
	if err != nil {
		return nil, err
	}
	return &Poster{url: apiURL, dryRun: dryRun, client: &http.Client{Timeout: timeout}, secret: append([]byte(nil), secret...), publicHex: publicHex, logger: logger}, nil
}

// Post returns whether this exact advert should be suppressed on future
// observations, and whether a failure is worth retrying immediately.
// The signing identity comes from map.key_file.
func (p *Poster) Post(ctx context.Context, job Job, attempt int) (handled bool, retry bool, err error) {
	if !job.Radio.Valid() {
		return false, false, errors.New("invalid radio parameters")
	}
	if p.dryRun {
		// Simulate a terminal response so repeated sightings do not flood
		// dry-run logs. This state is process-local and never reaches the map.
		return true, false, nil
	}
	data, err := json.Marshal(struct {
		Params Radio    `json:"params"`
		Links  []string `json:"links"`
	}{Params: job.Radio, Links: []string{"meshcore://" + job.Advert.RawHex}})
	if err != nil {
		return false, false, err
	}
	digest := sha256.Sum256(data)
	signature, err := sign(p.secret, digest[:])
	if err != nil {
		return false, false, err
	}
	body, err := json.Marshal(struct {
		Data      string `json:"data"`
		Signature string `json:"signature"`
		PublicKey string `json:"publicKey"`
	}{Data: string(data), Signature: hex.EncodeToString(signature), PublicKey: p.publicHex})
	if err != nil {
		return false, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return false, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return false, true, err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 64*1024+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return false, true, err
	}
	if len(responseBody) > 64*1024 {
		return false, true, errors.New("map response exceeds 64 KiB")
	}
	var message struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(responseBody, &message)
	if p.logger != nil {
		p.logger.Info("map HTTP response", "packet_id", job.Advert.PacketID, "node", job.Advert.PublicKey, "attempt", attempt, "http_status", resp.StatusCode, "response_code", message.Code)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 || message.Code == "NODES_INSERTED" || strings.HasPrefix(message.Code, "ERR_ADVERT_") || strings.HasPrefix(message.Code, "ERR_COORDS_") {
		return true, false, nil
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 && resp.StatusCode != 429 {
		return false, false, fmt.Errorf("permanent map HTTP %d", resp.StatusCode)
	}
	return false, true, fmt.Errorf("map HTTP %d", resp.StatusCode)
}
