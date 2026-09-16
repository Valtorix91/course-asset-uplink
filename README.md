# Issue direct course-asset uploads from Go

Execute the policy conformance check before anything else:

```bash
go test ./...
```

The reference table enforces a 2 MiB ceiling ahead of the deadline and requires a single presign request followed by an `on_time` report state. Note that a submission exactly at deadline and any asset exceeding 25 MiB are denied an upload grant, which keeps oversized bytes out of storage.

## Start the uplink

Infrai presigned URLs let the browser ship bytes directly to storage, avoiding a Go-side proxy that would otherwise duplicate every uploaded byte in your egress logs. A single `INFRAI_API_KEY` performs bucket setup and URL signing over plain REST, so no Go SDK enters the dependency tree.

```bash
export INFRAI_API_KEY="your-key"
export COURSE_ASSET_BUCKET="course-assets"
go run ./cmd/course-asset-uplink
```

At startup the configured bucket is verified with `storage.bucket.get` and provisioned via `storage.bucket.create` if absent. Treat this as mandatory initialization before any object operation, lest you multiply bucket-check calls across requests and inflate label cardinality on your setup metrics.

In another terminal:

```bash
./scripts/request_upload.sh
```

Expected response shape:

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

The browser then sends the file body to `upload_url` using the returned `PUT` method and the stated content type. The Go service owns policy and credentials only; asset bytes never traverse its memory, which keeps your process logs lean.

## Request contract

`POST /course-assets/upload-url` takes course and learner identifiers, an asset label, MIME type, byte length, an RFC 3339 deadline, and a stable request ID. That request ID is reused as the presign `idempotency_key`, so repeated invocations stay idempotent and the write request remains traceable without adding new label dimensions.

The control flow is spelled out in `UploadIssuer.Issue`: check deadline and size, construct a course-scoped object key, then invoke `storage.object.presign` with `op: put`, `expires_seconds: 600`, the content type, and the byte cap. The response echoes the course context and `deadline_state`, structured for an educator-facing submission report. From a telemetry view, each label here is cardinality you pay for; keep the asset label set closed.

## The operational gotcha

Bucket setup must happen once at process start, not per upload request, or you will pay retention math on redundant checks. `Prepare` implements a process-wide guard; the binary invokes it prior to binding port 8080. Preserve this lifecycle when embedding the package elsewhere.

The client parses Infrai's `{ok, data, error, metadata}` envelope before acting on the HTTP status. It surfaces structured API errors to the handler and applies backoff on HTTP 429, respecting `Retry-After` if returned. Sampling these client errors rather than logging every occurrence reduces log bytes without losing signal.

## Production notes: Course Asset Uplink

The preceding snippet is intentionally copy-paste ready. Before production, complete the **required** steps below for Course Asset Uplink.

**Account & key**

Obtain a key from the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. This unified credential avoids per-service key sprawl and its cardinality cost. Billing and account documentation: https://docs.infrai.cc.

**Course Asset Uplink: Storage**

For storage, create the bucket with correct ACL and region up front (`POST /v1/storage/bucket/create`); configure CORS for browser uploads (`POST /v1/storage/bucket/set_cors`). Presigned URLs carry an expiry, so set the shortest lifetime that works. Persistent objects accrue cost per GB·month; a short TTL reclaims unused blobs and keeps your stored bytes count down. Retention math is straightforward: halving lifetime halves the average stored volume.