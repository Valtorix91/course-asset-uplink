package courseassets

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

const MaxAssetBytes int64 = 25 << 20

var safeID = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

var (
	ErrDeadlinePassed = errors.New("course asset deadline has passed")
	ErrAssetTooLarge  = errors.New("course asset exceeds 25 MiB")
	ErrInvalidInput   = errors.New("course_id, learner_id, asset_kind, content_type, and request_id are required")
)

type Storage interface {
	GetBucket(context.Context, string) error
	CreateBucket(context.Context, string) error
	PresignPut(context.Context, string, string, PresignPutRequest) (PresignResult, error)
}

type UploadRequest struct {
	CourseID    string    `json:"course_id"`
	LearnerID   string    `json:"learner_id"`
	AssetKind   string    `json:"asset_kind"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	DueAt       time.Time `json:"due_at"`
	RequestID   string    `json:"request_id"`
}

type UploadGrant struct {
	UploadURL     string    `json:"upload_url"`
	Method        string    `json:"method"`
	ObjectKey     string    `json:"object_key"`
	CourseID      string    `json:"course_id"`
	LearnerID     string    `json:"learner_id"`
	DueAt         time.Time `json:"due_at"`
	DeadlineState string    `json:"deadline_state"`
}

type UploadIssuer struct {
	storage Storage
	bucket  string
	now     func() time.Time
	once    sync.Once
	initErr error
}

func NewUploadIssuer(storage Storage, bucket string) *UploadIssuer {
	return &UploadIssuer{storage: storage, bucket: bucket, now: time.Now}
}

func (u *UploadIssuer) Prepare(ctx context.Context) error {
	u.once.Do(func() {
		if err := u.storage.GetBucket(ctx, u.bucket); err == nil {
			return
		} else {
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 404 {
				u.initErr = err
				return
			}
		}
		u.initErr = u.storage.CreateBucket(ctx, u.bucket)
	})
	return u.initErr
}

func (u *UploadIssuer) Issue(ctx context.Context, in UploadRequest) (UploadGrant, error) {
	if strings.TrimSpace(in.CourseID) == "" || strings.TrimSpace(in.LearnerID) == "" ||
		strings.TrimSpace(in.AssetKind) == "" || strings.TrimSpace(in.ContentType) == "" || strings.TrimSpace(in.RequestID) == "" {
		return UploadGrant{}, ErrInvalidInput
	}
	if in.SizeBytes <= 0 || in.SizeBytes > MaxAssetBytes {
		return UploadGrant{}, ErrAssetTooLarge
	}
	if !u.now().Before(in.DueAt) {
		return UploadGrant{}, ErrDeadlinePassed
	}
	if err := u.Prepare(ctx); err != nil {
		return UploadGrant{}, err
	}

	key := fmt.Sprintf("courses/%s/learners/%s/%s", clean(in.CourseID), clean(in.LearnerID), clean(in.AssetKind))
	result, err := u.storage.PresignPut(ctx, u.bucket, key, PresignPutRequest{
		Op:             "put",
		ExpiresSeconds: 600,
		ContentType:    in.ContentType,
		MaxBytes:       in.SizeBytes,
		IdempotencyKey: in.RequestID,
	})
	if err != nil {
		return UploadGrant{}, err
	}
	return UploadGrant{
		UploadURL: result.URL, Method: "PUT", ObjectKey: key,
		CourseID: in.CourseID, LearnerID: in.LearnerID, DueAt: in.DueAt, DeadlineState: "on_time",
	}, nil
}

func clean(value string) string {
	return strings.Trim(safeID.ReplaceAllString(value, "-"), "-")
}
