package storage

import (
	"fmt"
	"path"

	"github.com/google/uuid"
)

// InputKey builds the object key for an uploaded input of a job:
// uploads/{job_id}/{ordinal}-{filename}.
func InputKey(jobID uuid.UUID, ordinal int, filename string) string {
	return fmt.Sprintf("uploads/%s/%d-%s", jobID, ordinal, path.Base(filename))
}

// OutputKey builds the object key for a converted output of a job:
// results/{job_id}/{ordinal}-{filename}.
func OutputKey(jobID uuid.UUID, ordinal int, filename string) string {
	return fmt.Sprintf("results/%s/%d-%s", jobID, ordinal, path.Base(filename))
}

// AttemptOutputKey builds the staging key for one attempt output:
// attempts/{attempt_id}/out/{ordinal}.
func AttemptOutputKey(attemptID uuid.UUID, ordinal int) string {
	return fmt.Sprintf("attempts/%s/out/%d", attemptID, ordinal)
}

// AcceptedOutputKey builds the published key for one accepted output:
// results/{job_id}/accepted/{attempt_id}/{ordinal}-{sha256}.
func AcceptedOutputKey(jobID, attemptID uuid.UUID, ordinal int, sha256hex string) string {
	return fmt.Sprintf("results/%s/accepted/%s/%d-%s", jobID, attemptID, ordinal, sha256hex)
}
