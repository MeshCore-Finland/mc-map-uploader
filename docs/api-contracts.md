# API contracts

## MQTT input

The uploader connects to an MQTT broker as an MQTT 3.1.1 client with a clean session. It subscribes at QoS 0 to `mqtt.status_filter` and `mqtt.packets_filter` in `config.yaml` (the example uses `meshcore/+/+/status` and `meshcore/+/+/packets`). It never publishes. The MQTT account needs read permission for those filters only. Disconnection or a full local queue loses messages; there is no replay request. Filters select subscriptions; message parsing still requires the documented `meshcore/<region-code>/<key>/<kind>` topic shape and the map allowlist.

The topic shape is `meshcore/<region-code>/<64-hex-observer-key>/<kind>`, where `kind` is `status` or `packets`. Region codes are matched exactly against the allowlist; they may be longer than three letters. Payloads are UTF-8 JSON, at most 16 KiB. `status` provides radio frequency, bandwidth, spreading factor, and coding rate, either under `params` or at the top level. Accepted names are `freq`/`frequency`/`radioFreq`, `bw`/`bandwidth`/`radioBw`, `sf`/`spreadingFactor`/`radioSf`, and `cr`/`codingRate`/`radioCr`; the `radio` string format also supports `869.525 MHz BW125 SF11 CR5` or `869.525,125,11,5`. Frequency is normalized to MHz and bandwidth to kHz. A complete valid status is cached for 24 hours per observer.

`map.allowed_iata` is required and contains uppercase letters-only codes of any length. A message outside that list is discarded before it can update status state or create an upload job. The uploader fails startup if the list is absent or malformed.

`map.geofence_polygons` is optional. Each polygon is a YAML list of `[latitude, longitude]` points, with at least three points. Multiple polygons form a union, so disconnected areas can be included. When configured, signed advert coordinates must fall inside at least one polygon before an upload job is created. This filters the advertised node's location, not the observer's location. The boundary is operator-supplied in YAML; the uploader contains no country-specific coordinates. Omit the setting to accept any valid advertised location. Polygon edges are included; holes and antimeridian-crossing polygons are not supported.

Advert logs use `packet_id`, the first 16 lowercase hex characters of MeshCore's SHA-256 content hash (ADVERT payload type followed by immutable payload bytes). This identity stays the same across route changes. Every recognized ADVERT with parseable raw packet bytes logs `advert received` and then `advert processing complete` when filtering ends, with either a drop reason or `outcome: queued`. A per-sighting `processing_id` joins those two intake lines, even when the same packet is heard repeatedly. HTTP upload logs are a separate lifecycle joined by `packet_id`; response logs include `attempt`, `http_status`, and `response_code`. Dry runs have no HTTP fields. Malformed JSON or packet text that cannot be identified as an ADVERT is ignored without an advert lifecycle log.

After a valid advert passes decoding, the uploader ignores another sighting with the same `packet_id` for one minute. Once an advert is handled, it records that node's signed advert timestamp for 72 hours. During that period it skips adverts from the same node with timestamps less than 3,600 seconds newer than the last handled timestamp. A new signed advert whose timestamp is at least 3,600 seconds newer is eligible for processing, even within the 72-hour period. Pending uploads also block another job for that node. These records are process-local and reset on restart. Dry run simulates a handled result, so the same timing rules apply without sending a map request.

`packets` provides a MeshCore packet as hexadecimal text in `raw`, `packet`, `payload`, or `data`. The packet must be an ADVERT for a `REPEATER`, `ROOM`, or `SENSOR`; its Ed25519 advert signature must verify. Uploadable adverts must include the location flag and coordinates other than the pair `(0, 0)`. The uploader ignores other packets and adverts. The packet is associated with the radio settings last seen from the same observer key.

The uploader subscribes only to the configured `status` and `packets` topics.

## Map request

POST to `https://map.meshcore.io/api/v1/uploader/node` with `Content-Type: application/json`:

```json
{"data":"{\"params\":{\"freq\":869.525,\"cr\":5,\"sf\":11,\"bw\":125},\"links\":[\"meshcore://<raw-packet-hex>\"]}","signature":"<hex-ed25519-signature>","publicKey":"<hex-ed25519-public-key>"}
```

`data` is a JSON string. The signature is Ed25519 over SHA-256 of the exact UTF-8 bytes of that string. The uploader reads a persistent 32-byte Ed25519 seed or 64-byte MeshCore expanded private key from `map.key_file`; normal startup never creates or modifies it. The map author's [reference uploader](https://github.com/recrof/map.meshcore.io-uploader/blob/main/index.mjs) uses this request contract. Most observed repeaters may already be on the map: a successful response counts as a handled refresh, and `ERR_ADVERT_DUPLICATE` counts as a handled recent duplicate. Neither is logged as an upload failure. `NODES_INSERTED` also counts as handled. Successful responses and `ERR_ADVERT_*`/`ERR_COORDS_*` terminal responses update the per-node handled timestamp described above. Permanent HTTP 4xx responses finish without immediate retry; HTTP 408/429, 5xx, and network errors retry within the configured attempt limit. Responses are limited to 64 KiB. This signing key establishes a durable identity for entries created by this uploader; refreshing an existing entry does not transfer its removal rights to this key.

The key file contains one line of 64 hex characters for a 32-byte seed or 128 hex characters for a 64-byte MeshCore expanded private key. Uppercase hex is accepted. `keygen` creates a seed file with mode 0600; `identity` derives the public key without printing the secret.

## Operational interface

Configuration is a single strict YAML file; see [the example](example-config.yaml). The MQTT password and map key are read from separate 0600 files. The service reads MQTT messages, sends map requests, and writes structured JSON logs to stdout. At startup, it logs its effective MQTT and map settings, excluding the MQTT password and signing key contents. Geofence settings are logged as polygon and point counts plus a SHA-256 digest of the coordinates, rather than printing every point. Process liveness and MQTT subscription logs are the operational signals. `map.dry_run` must be explicitly true or false; true sends no map request.
