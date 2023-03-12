package main

import (
	"crypto/sha1"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"project/server/config"
	"project/server/posts"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go"
)

func archiveHandler(w http.ResponseWriter, req *http.Request) {
	type data struct {
		Posts          []posts.Post
		LoggedIn       bool
		First          bool
		Last           bool
		Properties     string
		PrevProperties string
		NextProperties string
		Years          []int
		Tag            string
	}
	limit, offset, year, tag, err := queryURL(req)
	if err != nil {
		http.Error(w, "invalid limit and/or offset", http.StatusForbidden)
	}
	posts, first, last, err := posts.ListPosts(limit, offset, year, tag)
	if err != nil {
		http.Error(w, "error retrieving posts from the database", http.StatusInternalServerError)
	}
	_, loggedIn := getLoginStatus(req)
	years, err := listYears([]string{tag})
	if err != nil {
		http.Error(w, "error retrieving years from the database", http.StatusInternalServerError)
	}
	properties, prevProperties, nextProperties := createProperties(limit, offset, year, tag)
	d := data{
		Posts:          posts,
		LoggedIn:       loggedIn,
		First:          first,
		Last:           last,
		Properties:     properties,
		PrevProperties: prevProperties,
		NextProperties: nextProperties,
		Years:          years,
		Tag:            tag,
	}
	err = config.TPL.ExecuteTemplate(w, "archive.gohtml", d)
	if err != nil {
		http.Error(w, "error loading page", http.StatusInternalServerError)
	}
}

func loginHandler(w http.ResponseWriter, req *http.Request) {
	_, loggedIn := getLoginStatus(req)
	if loggedIn {
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}
	if req.Method == http.MethodPost {
		email := req.FormValue("email")
		err := createSession(w, email, []byte(req.FormValue("password")))
		if err != nil {
			http.Error(w, "Login failed. Please try again.", http.StatusForbidden)
			return
		}
		http.Redirect(w, req, "/upload", http.StatusSeeOther)
	}
	err := config.TPL.ExecuteTemplate(w, "login.gohtml", nil)
	if err != nil {
		http.Error(w, "error templating page", http.StatusInternalServerError)
	}
}

func registerHandler(w http.ResponseWriter, req *http.Request) {
	_, loggedIn := getLoginStatus(req)
	if loggedIn {
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}
	if req.Method == http.MethodPost {
		name, email, hashedPassword, role, err := verifyRegistration(w, req)
		if err != nil {
			http.Error(w, "registration failed because invalid data", http.StatusForbidden)
			return
		}
		err = createUser(w, name, email, hashedPassword, role)
		if err != nil {
			http.Error(w, "error creating user", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/login", http.StatusSeeOther)
		return
	}
	err := config.TPL.ExecuteTemplate(w, "register.gohtml", nil)
	if err != nil {
		http.Error(w, "error templating page", http.StatusInternalServerError)
	}
}

func logoutHandler(w http.ResponseWriter, req *http.Request) {
	_, loggedIn := getLoginStatus(req)
	if !loggedIn {
		http.Redirect(w, req, "/login", http.StatusSeeOther)
		return
	}
	cookie := deleteSession(req)
	http.SetCookie(w, cookie)
	http.Redirect(w, req, "/login", http.StatusSeeOther)
}

func uploadHandler(w http.ResponseWriter, req *http.Request) {
	type data struct {
		LoggedIn    bool
		CurrentYear int
	}
	_, loggedIn := getLoginStatus(req)
	if !loggedIn {
		http.Redirect(w, req, "/login", http.StatusSeeOther)
		return
	}
	if req.Method == http.MethodPost {
		err := storeFiles(req)
		if err != nil {
			http.Error(w, "error storing file object", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, fmt.Sprintf("?year=%s", req.PostFormValue("year")), http.StatusSeeOther)
		return
	}
	d := data{
		LoggedIn:    loggedIn,
		CurrentYear: time.Now().Year(),
	}
	err := config.TPL.ExecuteTemplate(w, "upload.gohtml", d)
	if err != nil {
		http.Error(w, "error templating page", http.StatusInternalServerError)
	}
}

func updateHandler(w http.ResponseWriter, req *http.Request) {
	type data struct {
		Post        posts.Post
		LoggedIn    bool
		CurrentYear int
	}
	userId, loggedIn := getLoginStatus(req)
	if !loggedIn {
		http.Redirect(w, req, "/login", http.StatusSeeOther)
		return
	}
	postId, err := strconv.Atoi(req.URL.Path[len("/update/"):])
	if err != nil {
		http.Error(w, "Malformatted post id", http.StatusForbidden)
		return
	}
	post, err := posts.GetPost(postId)
	if err != nil {
		http.Error(w, "Error loading post. Please try again or contact administrator.", http.StatusInternalServerError)
		return
	}
	if *userId != post.UserId {
		http.Error(w, "Permission denied.", http.StatusForbidden)
		return
	}
	if req.Method == http.MethodPost {
		year, err := strconv.Atoi(req.PostFormValue("year"))
		if err != nil {
			http.Error(w, "Malformatted year", http.StatusForbidden)
		}
		post.Year = year
		post.Title = req.PostFormValue("title")
		post.Description = req.PostFormValue("description")
		err = posts.UpdatePost(post)
		if err != nil {
			http.Error(w, "Error updating post. Please try again or contact administrator.", http.StatusInternalServerError)
			return
		}
		err = updateTags(&postId, parseTags(req.PostFormValue("tags")))
		if err != nil {
			http.Error(w, "Error updating tags. Please try again or contact administrator.", http.StatusInternalServerError)
			return
		}
		// TODO create a landing page that redirects after 2 seconds !
		http.Redirect(w, req, fmt.Sprintf("/archive?year=%s", req.PostFormValue("year")), http.StatusSeeOther) // TODO probably want a view handler for individual posts
		return
	}

	d := data{
		Post:        post,
		LoggedIn:    loggedIn,
		CurrentYear: time.Now().Year(),
	}
	err = config.TPL.ExecuteTemplate(w, "update.gohtml", d)
	if err != nil {
		http.Error(w, "error templating page", http.StatusInternalServerError)
	}
}

func deleteHandler(w http.ResponseWriter, req *http.Request) {
	userId, loggedIn := getLoginStatus(req)
	if !loggedIn {
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}

	if req.Method == http.MethodPost {
		postId, err := strconv.Atoi(req.URL.Path[len("/delete/"):])
		if err != nil {
			http.Error(w, "requested post id is not valid", http.StatusForbidden)
		}
		err = posts.DeletePost(postId, *userId)
		if err != nil {
			http.Error(w, "requested post could not be deleted", http.StatusForbidden)
		}
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, req, "/", http.StatusForbidden)
}

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

func tagRepHandler(w http.ResponseWriter, req *http.Request) {
	type data struct {
		Posts    []posts.Post
		LoggedIn bool
	}
	_, loggedIn := getLoginStatus(req)
	posts, err := listTagReps()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	d := data{
		Posts:    posts,
		LoggedIn: loggedIn,
	}
	err = config.TPL.ExecuteTemplate(w, "index.gohtml", d)
	if err != nil {
		http.Error(w, "error templating page", http.StatusInternalServerError)
	}
}

func contactHandler(w http.ResponseWriter, req *http.Request) {
	err := config.TPL.ExecuteTemplate(w, "contactinfo.gohtml", nil)
	if err != nil {
		http.Error(w, "error templating page", http.StatusInternalServerError)
	}
}

func main() {
	http.HandleFunc("/archive/", archiveHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/contact", contactHandler)
	http.HandleFunc("/logout", logoutHandler)
	http.HandleFunc("/", tagRepHandler)
	http.HandleFunc("/register", registerHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/update/", updateHandler)
	http.HandleFunc("/blob/", imageHandler)
	http.HandleFunc("/delete/", deleteHandler)
	http.HandleFunc("/style.css", styleSheetHandler)
	http.Handle("/favicon.ico", http.NotFoundHandler())
	log.Fatal(http.ListenAndServe(":80", nil))
}

func storeFiles(req *http.Request) error {
	userId, ok := getLoginStatus(req)
	if !ok {
		return fmt.Errorf("request is unauthenticated")
	}
	err := req.ParseMultipartForm(10000000) // max 10 megabytes
	if err != nil {
		return fmt.Errorf("error parsing form data: %v", err)
	}
	minioClient, err := newMinIO()
	if err != nil {
		return fmt.Errorf("error creating minio client: %v", err)
	}
	year, err := strconv.Atoi(req.PostFormValue("year"))
	if err != nil {
		return fmt.Errorf("'year' is not an integer: %v", err)
	}
	tags := parseTags(req.PostFormValue("tags"))
	fileHeaders := req.MultipartForm.File["file"]
	for _, fileHeader := range fileHeaders {
		src, err := fileHeader.Open()
		if err != nil {
			return fmt.Errorf("error opening file: %v", err)
		}
		defer src.Close()
		hashSum := computeHashSum(src)
		ext := strings.Split(fileHeader.Filename, ".")[1]
		objectName := req.PostFormValue("year") + "/" + fmt.Sprintf("%x", hashSum) + "." + ext
		_, err = minioClient.PutObject("download", objectName, src, fileHeader.Size, minio.PutObjectOptions{ContentType: "image/jpeg"})
		if err != nil {
			return fmt.Errorf("error storing file on S3: %v", err)
		}
		minioUrl := objectName
		postId, err := posts.CreatePost(minioUrl, year, *userId)
		if err != nil {
			return fmt.Errorf("error inserting post in database: %v", err)
		}
		err = createTags(postId, tags)
		if err != nil {
			return fmt.Errorf("error creating tags for this post in database: %v", err)
		}
	}
	return nil
}

func computeHashSum(file io.Reader) string {
	h := sha1.New()
	io.Copy(h, io.TeeReader(file, h))
	hashSum := fmt.Sprintf("%x", h.Sum(nil))
	return hashSum
}

func newMinIO() (*minio.Client, error) {
	endpoint := "localhost:9000"
	accessKeyID := "minioadmin"
	secretAccessKey := "minioadmin"
	useSSL := false
	minioClient, err := minio.New(endpoint, accessKeyID, secretAccessKey, useSSL)
	return minioClient, err
}
