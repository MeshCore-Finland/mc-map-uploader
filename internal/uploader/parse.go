package uploader

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type Radio struct {
	Freq float64 `json:"freq"`
	CR   float64 `json:"cr"`
	SF   float64 `json:"sf"`
	BW   float64 `json:"bw"`
}

func (r Radio) Valid() bool {
	return finite(r.Freq) && finite(r.CR) && finite(r.SF) && finite(r.BW) &&
		r.Freq >= 100 && r.Freq <= 1000 && r.BW > 0 && r.BW <= 1000 &&
		r.SF >= 5 && r.SF <= 12 && r.CR >= 4 && r.CR <= 8
}

func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }

func topicParts(topic string) (observer, kind string, ok bool) {
	parts := strings.Split(topic, "/")
	if len(parts) != 4 || parts[0] != "meshcore" || len(parts[1]) == 0 || len(parts[2]) != 64 {
		return "", "", false
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return "", "", false
	}
	if parts[3] != "status" && parts[3] != "packets" {
		return "", "", false
	}
	return strings.ToLower(parts[2]), parts[3], true
}

func object(payload []byte) map[string]any {
	var value map[string]any
	if len(payload) > 16*1024 || json.Unmarshal(payload, &value) != nil {
		return nil
	}
	return value
}

func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err == nil {
			return f
		}
	}
	return 0
}

func first(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := m[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

var (
	radioCSV  = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]+)?)\s*,\s*([0-9]+(?:\.[0-9]+)?)\s*,\s*([0-9]+)\s*,\s*([0-9]+)\s*$`)
	radioFreq = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*MHz`)
	radioBW   = regexp.MustCompile(`(?i)\bBW\s*([0-9]+(?:\.[0-9]+)?)`)
	radioSF   = regexp.MustCompile(`(?i)\bSF\s*([0-9]+)`)
	radioCR   = regexp.MustCompile(`(?i)\bCR\s*([0-9]+)`)
)

func parseRadioString(value string) Radio {
	if match := radioCSV.FindStringSubmatch(value); match != nil {
		return Radio{Freq: number(match[1]), BW: number(match[2]), SF: number(match[3]), CR: number(match[4])}
	}
	var out Radio
	if match := radioFreq.FindStringSubmatch(value); match != nil {
		out.Freq = number(match[1])
	}
	if match := radioBW.FindStringSubmatch(value); match != nil {
		out.BW = number(match[1])
	}
	if match := radioSF.FindStringSubmatch(value); match != nil {
		out.SF = number(match[1])
	}
	if match := radioCR.FindStringSubmatch(value); match != nil {
		out.CR = number(match[1])
	}
	return out
}

func ParseRadio(payload []byte) (Radio, error) {
	var out Radio
	root := object(payload)
	if root == nil {
		return out, errors.New("invalid status JSON")
	}
	fields := root
	if nested, ok := root["params"].(map[string]any); ok {
		fields = nested
	}
	if text, ok := root["radio"].(string); ok {
		out = parseRadioString(text)
	}
	fieldsRadio := Radio{
		Freq: number(first(fields, "freq", "frequency", "radioFreq")),
		CR:   number(first(fields, "cr", "codingRate", "radioCr")),
		SF:   number(first(fields, "sf", "spreadingFactor", "radioSf")),
		BW:   number(first(fields, "bw", "bandwidth", "radioBw")),
	}
	if first(fields, "freq", "frequency", "radioFreq") != nil {
		out.Freq = fieldsRadio.Freq
	}
	if first(fields, "cr", "codingRate", "radioCr") != nil {
		out.CR = fieldsRadio.CR
	}
	if first(fields, "sf", "spreadingFactor", "radioSf") != nil {
		out.SF = fieldsRadio.SF
	}
	if first(fields, "bw", "bandwidth", "radioBw") != nil {
		out.BW = fieldsRadio.BW
	}
	if out.Freq > 10_000_000 {
		out.Freq /= 1_000_000
	} else if out.Freq > 10_000 {
		out.Freq /= 1_000
	}
	if out.BW > 1_000 {
		out.BW /= 1_000
	}
	out.Freq = math.Round(out.Freq*1000) / 1000
	if !out.Valid() {
		return Radio{}, errors.New("missing or invalid radio parameters")
	}
	return out, nil
}

func ParsePacket(payload []byte) ([]byte, error) {
	if len(payload) > 16*1024 {
		return nil, errors.New("MQTT payload too large")
	}
	root := object(payload)
	if root == nil {
		return nil, errors.New("invalid packet JSON")
	}
	value, ok := first(root, "raw", "packet", "payload", "data").(string)
	if !ok {
		return nil, errors.New("packet hex missing")
	}
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "0x"))
	value = strings.Join(strings.Fields(value), "")
	if len(value) < 2 || len(value) > 1024 || len(value)%2 != 0 {
		return nil, errors.New("invalid packet hex length")
	}
	return hex.DecodeString(value)
}
