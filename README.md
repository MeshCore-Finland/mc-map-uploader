# mc-map-uploader

Standalone Go service that reads MeshCore observer `status` and `packets` topics from an MQTT broker and uploads verified repeater, room, and sensor adverts to the MeshCore map.

NOTE: This software is for MeshCore regional MQTT broker admins who wish to keep the official map updated automatically. It is of no use to MeshCore users or repeater admins.

## Before you live upload

- Start with one region or even one observer and see how it goes
- Configure a boundary polygon to ensure you only upload your own region's nodes
- Test with `dry_run: true` extensively to see why adverts are accepted for upload or dropped
- Play nice, if people don't want you uploading their data, don't

## Quick start

You need Go 1.24 or newer and an MQTT account that can read the configured status and packet topics. From the repository directory:

```sh
go build -o mc-map-uploader ./cmd/mc-map-uploader
install -d -m 700 secrets
./mc-map-uploader keygen ./secrets/map-signing.key
cp docs/example-config.yaml config.yaml
```

Create `secrets/mqtt-password` containing the MQTT password and set its permissions to `0600`. Edit `config.yaml` to set the broker address, username, topic filters, and allowed region codes. The example uses `HEL`; replace or extend it for your deployment. The config and secret files are ignored by Git.

Start with `map.dry_run: true` to check adverts without sending map requests. Then run:

```sh
./mc-map-uploader run config.yaml
```

When the logs show the adverts you expect, set `map.dry_run: false` and restart the uploader to send them to the map. Keep a backup of `secrets/map-signing.key`: it is the persistent signing identity for your uploads.

## Commands

| Command | Purpose |
| --- | --- |
| `./mc-map-uploader` or `./mc-map-uploader run` | Run with `config.yaml` in the current directory. |
| `./mc-map-uploader run <config-file>` | Run with a specified YAML config file. |
| `./mc-map-uploader validate [config-file]` | Check the config and referenced secret files, then exit. Defaults to `config.yaml`. |
| `./mc-map-uploader keygen <key-file>` | Create a new 32-byte Ed25519 signing seed. Refuses to overwrite an existing file. |
| `./mc-map-uploader identity <key-file>` | Print the public key for an existing signing seed or MeshCore expanded private key. |

The key file must contain one line of hex encoding a 32-byte Ed25519 seed or a 64-byte MeshCore expanded private key. It must be readable by the uploader and inaccessible to group and other users. `keygen` creates it with mode `0600`; `identity` prints only its public key.

Configuration is strict YAML: unknown fields and invalid values cause startup to fail. See the [example config](docs/example-config.yaml) for available settings. `map.allowed_iata` accepts uppercase letters-only region codes of any length. An optional `map.geofence_polygons` setting limits uploads by the coordinates in each advert.

## Testing and operation

Run the Go test suite with:

```sh
go test ./...
```

An optional decoder parity test can compare against a local MeshCore JavaScript module:

```sh
MESHCORE_JS_MODULE=/absolute/path/to/meshcore.js-entry.mjs go test -tags jscompare ./internal/uploader
```

The uploader logs structured JSON to stdout. It reconnects to the MQTT broker with a clean QoS 0 session. Missed packets and full-queue work are dropped; runtime state resets on restart. Dry run sends no map requests. See the [API contracts](docs/api-contracts.md) for input, deduplication, and map response behavior.

## Contributing

Issues and PRs welcome but this project WILL stay focused and doing its One Job. It will also stay stateless / memory based (no database etc).

## Documentation

- [API contracts](docs/api-contracts.md)
- [Internal architecture](docs/architecture.md)
- [Docker Compose deployment](docs/docker-compose.md)

Reference implementation: the [map author's companion uploader](https://github.com/recrof/map.meshcore.io-uploader).

Licensed under the [MIT License](LICENSE). This software was written with the help of Codex and GPT-5.6 and GPT-6.
