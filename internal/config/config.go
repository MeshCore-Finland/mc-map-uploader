package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"mc-map-uploader/internal/mqtt"
	"mc-map-uploader/internal/uploader"
)

type fileConfig struct {
	MQTT struct {
		Address       string `yaml:"address"`
		Username      string `yaml:"username"`
		PasswordFile  string `yaml:"password_file"`
		ClientID      string `yaml:"client_id"`
		StatusFilter  string `yaml:"status_filter"`
		PacketsFilter string `yaml:"packets_filter"`
	} `yaml:"mqtt"`
	Map struct {
		APIURL           string             `yaml:"api_url"`
		KeyFile          string             `yaml:"key_file"`
		AllowedIATA      []string           `yaml:"allowed_iata"`
		GeofencePolygons []uploader.Polygon `yaml:"geofence_polygons"`
		DryRun           *bool              `yaml:"dry_run"`
		HTTPTimeout      string             `yaml:"http_timeout"`
		UploadQueue      int                `yaml:"upload_queue"`
		UploadWorkers    int                `yaml:"upload_workers"`
		UploadAttempts   int                `yaml:"upload_attempts"`
		RetryDelay       string             `yaml:"retry_delay"`
	} `yaml:"map"`
}

type Runtime struct {
	MQTT     mqtt.Config
	Uploader uploader.Config
}

func Load(path string) (Runtime, error) {
	var out Runtime
	if path == "" {
		return out, errors.New("config path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if len(data) == 0 || len(data) > 64*1024 {
		return out, errors.New("config.yaml must be 1 to 65536 bytes")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var raw fileConfig
	if err := decoder.Decode(&raw); err != nil {
		return out, fmt.Errorf("invalid YAML config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return out, errors.New("config.yaml must contain exactly one YAML document")
	}
	host, portText, err := net.SplitHostPort(raw.MQTT.Address)
	port, portErr := strconv.Atoi(portText)
	if err != nil || portErr != nil || host == "" || port < 1 || port > 65535 {
		return out, errors.New("mqtt.address must be a host:port TCP address")
	}
	if raw.MQTT.Username == "" || raw.MQTT.ClientID == "" || raw.MQTT.PasswordFile == "" {
		return out, errors.New("mqtt.username, client_id, and password_file are required")
	}
	if strings.ContainsAny(raw.MQTT.ClientID, "\x00\n\r") || len(raw.MQTT.ClientID) > 64 {
		return out, errors.New("mqtt.client_id is invalid")
	}
	allowed, err := uploader.ParseAllowlist(raw.Map.AllowedIATA)
	if err != nil {
		return out, err
	}
	if err := uploader.ValidateGeofence(raw.Map.GeofencePolygons); err != nil {
		return out, err
	}
	if err := checkFilter(raw.MQTT.StatusFilter, "status", allowed); err != nil {
		return out, err
	}
	if err := checkFilter(raw.MQTT.PacketsFilter, "packets", allowed); err != nil {
		return out, err
	}
	if raw.Map.DryRun == nil {
		return out, errors.New("map.dry_run must be explicitly true or false")
	}
	parsedURL, err := url.Parse(raw.Map.APIURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" || parsedURL.User != nil {
		return out, errors.New("map.api_url must be an HTTPS URL without credentials")
	}
	if raw.Map.KeyFile == "" {
		return out, errors.New("map.key_file is required")
	}
	httpTimeout, err := time.ParseDuration(raw.Map.HTTPTimeout)
	if err != nil || httpTimeout < time.Second || httpTimeout > 2*time.Minute {
		return out, errors.New("map.http_timeout must be between 1s and 2m")
	}
	retryDelay, err := time.ParseDuration(raw.Map.RetryDelay)
	if err != nil || retryDelay < time.Second || retryDelay > time.Minute {
		return out, errors.New("map.retry_delay must be between 1s and 1m")
	}
	if raw.Map.UploadQueue < 1 || raw.Map.UploadQueue > 10000 || raw.Map.UploadWorkers < 1 || raw.Map.UploadWorkers > 20 || raw.Map.UploadAttempts < 1 || raw.Map.UploadAttempts > 20 {
		return out, errors.New("map.upload_queue must be 1..10000, upload_workers must be 1..20, and upload_attempts must be 1..20")
	}
	base := filepath.Dir(path)
	password, err := readPassword(resolve(base, raw.MQTT.PasswordFile))
	if err != nil {
		return out, fmt.Errorf("mqtt.password_file: %w", err)
	}
	keyPath := resolve(base, raw.Map.KeyFile)
	if _, err := uploader.ReadKeyFile(keyPath); err != nil {
		return out, fmt.Errorf("map.key_file: %w", err)
	}
	out.MQTT = mqtt.Config{Address: raw.MQTT.Address, Username: raw.MQTT.Username, Password: password, ClientID: raw.MQTT.ClientID, StatusFilter: raw.MQTT.StatusFilter, PacketsFilter: raw.MQTT.PacketsFilter}
	out.Uploader = uploader.Config{APIURL: raw.Map.APIURL, KeyFile: keyPath, AllowedIATA: strings.Join(raw.Map.AllowedIATA, ","), GeofencePolygons: raw.Map.GeofencePolygons, DryRun: *raw.Map.DryRun, HTTPTimeout: httpTimeout, QueueSize: raw.Map.UploadQueue, Workers: raw.Map.UploadWorkers, Attempts: raw.Map.UploadAttempts, RetryDelay: retryDelay}
	return out, nil
}

func resolve(base, name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(base, name)
}

func checkFilter(filter, kind string, allowed map[string]struct{}) error {
	parts := strings.Split(filter, "/")
	if len(parts) != 4 || parts[0] != "meshcore" || parts[3] != kind || parts[2] != "+" {
		return fmt.Errorf("mqtt.%s_filter must be meshcore/<code-or-+>/+/%s", kind, kind)
	}
	if parts[1] != "+" {
		if _, ok := allowed[parts[1]]; !ok {
			return fmt.Errorf("mqtt.%s_filter region must be in map.allowed_iata", kind)
		}
	}
	return nil
}

func readPassword(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("password file must be regular and inaccessible to group and others")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > 4096 {
		return "", errors.New("password file must be 1 to 4096 bytes")
	}
	password := strings.TrimSuffix(string(data), "\n")
	if password == "" || strings.ContainsAny(password, "\x00\n\r") {
		return "", errors.New("password file must contain one nonempty line")
	}
	return password, nil
}
