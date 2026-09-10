package courseassets

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingStorage struct {
	presignCalls int
	request      PresignPutRequest
	key          string
}

func (s *recordingStorage) GetBucket(context.Context, string) error    { return nil }
func (s *recordingStorage) CreateBucket(context.Context, string) error { return nil }
func (s *recordingStorage) PresignPut(_ context.Context, _, key string, request PresignPutRequest) (PresignResult, error) {
	s.presignCalls++
	s.request = request
	s.key = key
	return PresignResult{URL: "https://upload.example/signed"}, nil
}

func TestUploadDeadlineDecision(t *testing.T) {
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		dueAt     time.Time
		sizeBytes int64
		wantErr   error
		wantCalls int
	}{
		{name: "before deadline", dueAt: now.Add(time.Hour), sizeBytes: 2 << 20, wantCalls: 1},
		{name: "at deadline", dueAt: now, sizeBytes: 2 << 20, wantErr: ErrDeadlinePassed},
		{name: "over asset limit", dueAt: now.Add(time.Hour), sizeBytes: MaxAssetBytes + 1, wantErr: ErrAssetTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &recordingStorage{}
			issuer := NewUploadIssuer(storage, "course-assets")
			issuer.now = func() time.Time { return now }
			grant, err := issuer.Issue(context.Background(), UploadRequest{
				CourseID: "go-101", LearnerID: "learner-7", AssetKind: "capstone.pdf",
				ContentType: "application/pdf", SizeBytes: tt.sizeBytes, DueAt: tt.dueAt, RequestID: "req-42",
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Issue() error = %v, want %v", err, tt.wantErr)
			}
			if storage.presignCalls != tt.wantCalls {
				t.Fatalf("presign calls = %d, want %d", storage.presignCalls, tt.wantCalls)
			}
			if tt.wantErr == nil {
				if grant.DeadlineState != "on_time" || storage.request.Op != "put" || storage.request.IdempotencyKey != "req-42" {
					t.Fatalf("unexpected grant or presign request: %#v %#v", grant, storage.request)
				}
			}
		})
	}
}
