# Run with Docker Compose

The example configuration uses an MQTT broker at `127.0.0.1:1883`. The Compose file uses host networking so the container can reach a broker bound to the host loopback address. Set `mqtt.address` to your broker's address in `config.yaml`. The container publishes no ports.

From the uploader repository directory, keep `config.yaml` and `secrets/` beside `compose.yaml`. They are ignored by Git and excluded from the Docker build context. The signing key must be a persistent, backed-up identity. Set `map.dry_run: false` only when you intend to upload.

```sh
chmod 700 secrets
chmod 600 secrets/mqtt-password secrets/map-signing.key
./deploy.sh
docker compose run --rm --no-deps uploader validate /app/config.yaml
docker compose logs -f --tail=100 uploader
```

The Compose file defaults to UID/GID `1000:1000`. Set `MC_UID` and `MC_GID` to the owner IDs of the 0600 secret files when they differ, for example `MC_UID=$(id -u) MC_GID=$(id -g) docker compose up -d --build`. These IDs are deployment settings; MQTT credentials and the signing key stay in files.

Stop with `docker compose stop uploader`; restart with `docker compose up -d`. After code changes, run `./deploy.sh`. Check `docker compose ps` and logs for `subscribed to MQTT broker`, `advert received`, `advert processing complete`, and `map HTTP response`. The process reconnects to the MQTT broker.

The container filesystem and both mounts are read-only. The uploader writes no runtime files. Docker stores stdout logs on the host with three 10 MB rotated files. Nonblocking log delivery may drop lines if logging falls behind.

The Compose service uses `restart: unless-stopped`, so Docker starts it again after a host reboot once the Docker daemon is running. The example MQTT filters subscribe to all region topics; only codes listed under `map.allowed_iata` are processed. The MQTT account must have read access to the subscribed topic filters.
