package main

import (
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"project/server/config"
	"project/server/posts"
	"project/server/users"

	"github.com/minio/minio-go"
)

func styleSheetHandler(w http.ResponseWriter, req *http.Request) {
	http.ServeFile(w, req, "public/styles/style.css")
}

func imageHandler(w http.ResponseWriter, req *http.Request) {
	// Extract the filename from the URL path
	filename := req.URL.Path[len("/blob/"):]

	// Initialize a Minio client
	endpoint := "localhost:9000"
	accessKeyID := "minioadmin"
	secretAccessKey := "minioadmin"
	useSSL := false
	minioClient, err := minio.New(endpoint, accessKeyID, secretAccessKey, useSSL)
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

func contactHandler(w http.ResponseWriter, req *http.Request) {
	err := config.TPL.ExecuteTemplate(w, "contactinfo.gohtml", nil)
	if err != nil {
		http.Error(w, "error templating page", http.StatusInternalServerError)
	}
}

func main() {
	http.HandleFunc("/archive/", posts.ArchiveHandler)
	http.HandleFunc("/login", users.LoginHandler)
	http.HandleFunc("/contact", contactHandler)
	http.HandleFunc("/logout", users.LogoutHandler)
	http.HandleFunc("/", posts.TagRepHandler)
	http.HandleFunc("/register", users.RegisterHandler)
	http.HandleFunc("/upload", posts.UploadHandler)
	http.HandleFunc("/update/", posts.UpdateHandler)
	http.HandleFunc("/blob/", imageHandler)
	http.HandleFunc("/delete/", posts.DeleteHandler)
	http.HandleFunc("/style.css", styleSheetHandler)
	http.Handle("/favicon.ico", http.NotFoundHandler())
	log.Fatal(http.ListenAndServe(":80", nil))
}
