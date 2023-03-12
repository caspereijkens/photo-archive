package main

import (
	"log"
	"net/http"
	"project/server/config"
	"project/server/objects"
	"project/server/posts"
	"project/server/users"
)

func styleSheetHandler(w http.ResponseWriter, req *http.Request) {
	http.ServeFile(w, req, "public/styles/style.css")
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
	// http.HandleFunc("/register", users.RegisterHandler)
	http.HandleFunc("/upload", posts.UploadHandler)
	http.HandleFunc("/update/", posts.UpdateHandler)
	http.HandleFunc("/blob/", objects.ImageHandler)
	http.HandleFunc("/delete/", posts.DeleteHandler)
	http.HandleFunc("/style.css", styleSheetHandler)
	http.Handle("/favicon.ico", http.NotFoundHandler())
	log.Fatal(http.ListenAndServe(":80", nil))
}
