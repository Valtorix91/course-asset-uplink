#!/bin/sh
set -eu

curl --fail-with-body \
  --request POST \
  --header 'Content-Type: application/json' \
  --data '{"course_id":"go-101","learner_id":"learner-7","asset_kind":"capstone.pdf","content_type":"application/pdf","size_bytes":2097152,"due_at":"2027-01-15T17:00:00Z","request_id":"demo-go-101-learner-7-capstone"}' \
  http://localhost:8080/course-assets/upload-url
