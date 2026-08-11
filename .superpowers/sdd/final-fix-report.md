# Pilotserver phase-1 whole-branch final fix report

Date: 2026-08-12

## Scope and result

All three Critical and all seven Important findings from the final whole-branch
review were addressed.

## Critical fixes

1. Device JWT authentication
   - Device requests now read `identity` (with legacy `dongle_id` fallback),
     load that device's stored PEM public key, and verify only RS256 or ES256.
   - HS256 remains available for admin JWT and server-side upload signing, but
     is rejected for device authentication.
   - `/v1/me`, upload URL authorization, route authorization, and Athena
     WebSocket authorization all use the asymmetric verifier.
   - Pairing accepts RSA and ECDSA device public keys.

2. DragonPilot upload URL compatibility
   - Added `GET /v1.4/{dongleID}/upload_url/?path=...`.
   - Preserved `POST /v1.1/devices/{dongleID}/upload_url/`.
   - Response contains the uploader-compatible `url` and `headers` fields.

3. Closed pairing
   - Added required `PILOTSERVER_PAIRING_TOKEN` configuration (minimum 8 bytes).
   - `/v2/pilotauth/` now rejects requests whose `register_token` does not
     exactly match the configured token.

## Important fixes

4. Removed the default admin password. `PILOTSERVER_ADMIN_PASSWORD` is required
   and must contain at least 8 bytes.

5. Added admin-authenticated route browsing:
   - `GET /admin/api/devices/{dongleID}/routes`
   - `GET /admin/api/devices/{dongleID}/routes/{route}/segments`
   - `GET /admin/api/devices/{dongleID}/routes/{route}/files/{path...}`
   The admin UI now uses these Bearer-authenticated endpoints and downloads
   files through authenticated fetch requests.

6. Updated `docs/ota.md` and `docs/dragonpilot-fork-urls.md` to state that
   phase-1 OTA HTTP does not implement Git protocol. A fork must patch its
   updater for HTTP artifacts or point its Git remote at hosted Git.

7. Uploads are written to a temporary file in the destination directory and
   committed with `os.Rename`. Error paths remove only the temporary file and
   do not delete a previously good destination file.

8. Added a 512 MiB request limit with `http.MaxBytesReader`; oversized uploads
   return HTTP 413. Content-Length is rejected early when available.

9. SSH tunnel creation now waits up to two seconds for the matching JSON-RPC
   response. A device JSON-RPC error or timeout fails the admin SSH request.
   Successful tunnel creation emits an audit log containing dongle, port, and
   UTC time.

10. Configuration rejects non-loopback `PILOTSERVER_LISTEN` hosts unless
    `PILOTSERVER_ALLOW_NON_LOOPBACK=1`.

## TDD and verification evidence

Observed expected red tests before implementation:

- `go test ./internal/auth`: failed because `VerifyDeviceJWT` did not exist.
- `go test ./internal/config`: failed because missing credentials and
  non-loopback listening were accepted.
- `go test ./internal/api`: failed because `PairingToken` did not exist.
- `go test ./internal/upload`: v1.4 GET returned 404 and asymmetric device JWT
  was rejected by the old HS256 verifier.

Final fresh verification:

```text
go test ./...
ok pilotserver/cmd/pilotserver
ok pilotserver/internal/adminapi
ok pilotserver/internal/api
ok pilotserver/internal/athena
ok pilotserver/internal/auth
ok pilotserver/internal/billing
ok pilotserver/internal/config
ok pilotserver/internal/ota
ok pilotserver/internal/store
ok pilotserver/internal/upload
```

Build evidence:

```text
go build -o bin/pilotserver ./cmd/pilotserver
exit status 0
```

## Remaining operational notes

- Pairing still returns a short-lived server-issued `access_token` for
  compatibility/testing, but protected device endpoints require a
  device-private-key-signed RS256/ES256 JWT.
- Existing deployments must set both newly enforced secrets and explicitly
  opt in before binding pilotserver to a non-loopback interface.
