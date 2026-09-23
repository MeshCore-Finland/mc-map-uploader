package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"mc-map-uploader/internal/config"
	"mc-map-uploader/internal/mqtt"
	"mc-map-uploader/internal/uploader"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	args := os.Args[1:]
	if len(args) == 2 && args[0] == "keygen" {
		if err := uploader.CreateKeyFile(args[1]); err != nil {
			logger.Error("could not create map signing key", "error", err)
			os.Exit(1)
		}
		logger.Info("map signing key created", "path", args[1])
		return
	}
	if len(args) == 2 && args[0] == "identity" {
		seed, err := uploader.ReadKeyFile(args[1])
		if err != nil {
			logger.Error("could not read map signing key", "error", err)
			os.Exit(1)
		}
		publicKey, err := uploader.PublicKeyHex(seed)
		if err != nil {
			logger.Error("could not derive public key", "error", err)
			os.Exit(1)
		}
		logger.Info("map signing identity", "public_key", publicKey)
		return
	}
	command, path := "run", "config.yaml"
	if len(args) > 0 {
		command = args[0]
	}
	if len(args) > 1 {
		path = args[1]
	}
	if len(args) > 2 || (command != "run" && command != "validate") {
		logger.Error("usage: mc-map-uploader [run|validate [config.yaml] | keygen <key-file> | identity <key-file>]")
		os.Exit(2)
	}
	runtime, err := config.Load(path)
	if err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(2)
	}
	if command == "validate" {
		logger.Info("config valid", "path", path, "dry_run", runtime.Uploader.DryRun)
		return
	}
	service, err := uploader.New(runtime.Uploader, logger)
	if err != nil {
		logger.Error("invalid uploader configuration", "error", err)
		os.Exit(2)
	}
	logStartupConfig(logger, path, runtime)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	messages := make(chan mqtt.Message, 1024)
	go func() {
		err := mqtt.Run(ctx, runtime.MQTT, func(msg mqtt.Message) {
			select {
			case messages <- msg:
			default:
				logger.Warn("MQTT intake full; message dropped")
			}
		}, logger)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("MQTT client stopped", "error", err)
			stop()
		}
	}()
	service.Run(ctx, messages)
	logger.Info("uploader stopped")
}

func logStartupConfig(logger *slog.Logger, path string, runtime config.Runtime) {
	points := 0
	for _, polygon := range runtime.Uploader.GeofencePolygons {
		points += len(polygon)
	}
	boundaryJSON, _ := json.Marshal(runtime.Uploader.GeofencePolygons)
	boundaryHash := sha256.Sum256(boundaryJSON)
	logger.Info("uploader starting",
		"config_path", path,
		slog.Group("mqtt",
			"address", runtime.MQTT.Address,
			"username", runtime.MQTT.Username,
			"client_id", runtime.MQTT.ClientID,
			"status_filter", runtime.MQTT.StatusFilter,
			"packets_filter", runtime.MQTT.PacketsFilter,
		),
		slog.Group("map",
			"api_url", runtime.Uploader.APIURL,
			"key_file", runtime.Uploader.KeyFile,
			"allowed_iata", strings.Split(runtime.Uploader.AllowedIATA, ","),
			"geofence_polygon_count", len(runtime.Uploader.GeofencePolygons),
			"geofence_point_count", points,
			"geofence_sha256", hex.EncodeToString(boundaryHash[:]),
			"dry_run", runtime.Uploader.DryRun,
			"http_timeout", runtime.Uploader.HTTPTimeout.String(),
			"upload_queue", runtime.Uploader.QueueSize,
			"upload_workers", runtime.Uploader.Workers,
			"upload_attempts", runtime.Uploader.Attempts,
			"retry_delay", runtime.Uploader.RetryDelay.String(),
		),
	)
}
