package objects

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"project/server/config"

	"github.com/minio/minio-go"
)

func ImageHandler(w http.ResponseWriter, req *http.Request) {
	// Extract the filename from the URL path
	filename := req.URL.Path[len("/blob/"):]

	minioClient, err := config.NewMinIO()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set the response header to indicate the content type
	contentType := mime.TypeByExtension(filepath.Ext(filename))
	w.Header().Set("Content-Type", contentType)

	// Get the object from Minio and stream it to the response body
	object, err := minioClient.GetObject("download", filename, minio.GetObjectOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer object.Close()

	if _, err := io.Copy(w, object); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
