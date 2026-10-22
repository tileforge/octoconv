package api

import (
	"errors"
	"net/http"
)

var errFormFieldsTooLarge = errors.New("multipart form: non-file data above its bound")

func parseIntakeForm(r *http.Request) error {
	return r.ParseMultipartForm(multipartInMemoryBudget)
}
