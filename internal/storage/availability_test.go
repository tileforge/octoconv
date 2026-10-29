package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestObjectExistsDistinguishesMissingFromFailure(t *testing.T) {
	for _, status := range []int{200, 404, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "0")
				w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
				w.WriteHeader(status)
			}))
			defer server.Close()
			mc, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("key", "secret", ""), Region: "us-east-1"})
			if err != nil {
				t.Fatal(err)
			}
			client := &Client{mc: mc, bucket: "test-bucket"}
			exists, err := client.ObjectExists(context.Background(), "uploads/source")
			if status == 200 && (!exists || err != nil) {
				t.Fatalf("exists %v err %v", exists, err)
			}
			if status == 404 && (exists || err != nil) {
				t.Fatalf("missing: exists %v err %v", exists, err)
			}
			if status == 403 && (exists || err == nil) {
				t.Fatalf("failure misclassified: exists %v err %v", exists, err)
			}
		})
	}
}
