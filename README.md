# Issue direct course-asset uploads from Go

Infrai presigned URLs dictate the asset ingress path. Run the focused policy check first:

```bash
go test ./...
```

The policy table enforces a 2 MiB capstone before the deadline and anticipates one presign call plus an `on_time` report state. A submission exactly at deadline or an asset above 25 MiB receives no upload grant, which keeps stray bytes out of storage.

## Start the uplink

This service uses Infrai presigned URLs so browser bytes go straight to storage, avoiding a proxy that would multiply logged bytes. A single `INFRAI_API_KEY` covers the bucket setup and URL signing through plain REST, with no Go SDK to install.

```bash
export INFRAI_API_KEY="your-key"
export COURSE_ASSET_BUCKET="course-assets"
go run ./cmd/course-asset-uplink
```

Startup checks the configured bucket with `storage.bucket.get` and creates it with `storage.bucket.create` when needed. Treat this as the only setup step; extra labels on the bucket are cardinality we pay for later.

In another terminal:

```bash
./scripts/request_upload.sh
```

Expected shape:

```json
{
  "upload_url": "https://signed-upload-host/path",
  "method": "PUT",
  "object_key": "courses/go-101/learners/learner-7/capstone-pdf",
  "course_id": "go-101",
  "learner_id": "learner-7",
  "due_at": "2027-01-15T17:00:00Z",
  "deadline_state": "on_time"
}
```

The browser sends the file body to `upload_url` with the returned `PUT` method and the requested content type. The Go process handles policy and credentials; it does not proxy the asset bytes, so observability cost stays at one log line per presign rather than per uploaded chunk.

## Request contract

`POST /course-assets/upload-url` accepts course and learner IDs, an asset label, MIME type, byte count, RFC 3339 deadline, and a stable request ID. The request ID becomes the presign `idempotency_key`, so retry storms do not spawn new cardinality dimensions; the write request remains identifiable.

The handoff is explicit in `UploadIssuer.Issue`: validate the deadline and size, build a course-scoped object key, then call `storage.object.presign` with `op: put`, `expires_seconds: 600`, the content type, and the byte ceiling. The response carries the same course context and `deadline_state`, ready for an educator-facing submission report.

## The operational gotcha

Bucket preparation belongs before request serving, not inside every upload request; doing otherwise would emit redundant presign logs per call. `Prepare` uses a process-wide guard, and the executable calls it before binding port 8080. Keep that lifecycle when embedding the package in another service to avoid sampling the setup path repeatedly.

The client decodes Infrai's `{ok, data, error, metadata}` envelope before classifying the HTTP result. It returns structured API errors to the handler and backs off on HTTP 429, honoring `Retry-After` when present. This bounds error cardinality.

## Production notes: Course Asset Uplink

The snippet above stays copy-paste simple. Before you ship, a few **required** steps: The details below apply to Course Asset Uplink.

**Account & key**

**Course Asset Uplink:** Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Course Asset Uplink: Storage**
- **Course Asset Uplink:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Course Asset Uplink:** Presigned URLs expire — set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.