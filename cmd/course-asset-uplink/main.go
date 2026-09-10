package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/example/course-asset-uplink/internal/courseassets"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	bucket := os.Getenv("COURSE_ASSET_BUCKET")
	if bucket == "" {
		bucket = "course-assets"
	}
	issuer := courseassets.NewUploadIssuer(courseassets.NewInfraiClient(apiKey), bucket)
	if err := issuer.Prepare(context.Background()); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("POST /course-assets/upload-url", func(w http.ResponseWriter, r *http.Request) {
		var input courseassets.UploadRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request"})
			return
		}
		grant, err := issuer.Issue(r.Context(), input)
		if err != nil {
			status := http.StatusBadGateway
			switch {
			case errors.Is(err, courseassets.ErrDeadlinePassed), errors.Is(err, courseassets.ErrAssetTooLarge), errors.Is(err, courseassets.ErrInvalidInput):
				status = http.StatusUnprocessableEntity
			default:
				var apiErr *courseassets.APIError
				if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
					status = apiErr.HTTPStatus
				}
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, grant)
	})

	log.Println("course asset uplink listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}
