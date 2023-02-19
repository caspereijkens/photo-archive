package main

import (
	"crypto/sha1"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/minio/minio-go"
	uuid "github.com/satori/go.uuid"
	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB
var sessionStore = make(map[string]int)
var publicTpl *template.Template

type Post struct {
	Id          int
	ImageURL    string
	Year        int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Edited      bool
	UserId      int
	Title       string
	Description string
	Tags        []string
}

func init() {
	var err error
	db, err = sql.Open("postgres", "postgres://casper:password@db/joepeijkens?sslmode=disable")
	if err != nil {
		panic(err)
	}
	if err = db.Ping(); err != nil {
		panic(err)
	}
	log.Println("You connected to your database.")
	publicTpl = template.Must(template.ParseGlob("public/html/*"))
}

func archiveHandler(w http.ResponseWriter, req *http.Request) {
	type data struct {
		Posts    []Post
		LoggedIn bool
		First    bool
		Last     bool
		PrevURL  string
		NextURL  string
	}
	_, loggedIn := getLoginStatus(req)
	limit, offset, year, tag, err := queryURL(req)
	if err != nil {
		http.Error(w, "invalid limit and/or offset", http.StatusForbidden)
	}
	posts, first, last, err := listPosts(limit, offset, year, tag)
	if err != nil {
		http.Error(w, "error retrieving posts from the database", http.StatusInternalServerError)
	}
	prevURL, nextURL := createNavigationURLs(limit, offset, year, tag)
	d := data{
		Posts:    posts,
		LoggedIn: loggedIn,
		First:    first,
		Last:     last,
		PrevURL:  prevURL,
		NextURL:  nextURL,
	}
	err = publicTpl.ExecuteTemplate(w, "archive.gohtml", d)
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
	err := publicTpl.ExecuteTemplate(w, "login.gohtml", nil)
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
	err := publicTpl.ExecuteTemplate(w, "register.gohtml", nil)
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
	err := publicTpl.ExecuteTemplate(w, "upload.gohtml", d)
	if err != nil {
		http.Error(w, "error templating page", http.StatusInternalServerError)
	}
}

func createNavigationURLs(limit, offset, year int, tag string) (string, string) {
	prevOffset, nextOffset := navigateOffsets(limit, offset)

	var filterProperties string
	if year > 0 {
		filterProperties = "&year=" + strconv.Itoa(year)
	}
	if tag != "" {
		filterProperties += "&tag=" + url.QueryEscape(tag)
	}
	prevURL := "/archive?limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(prevOffset) + filterProperties
	nextURL := "/archive?limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(nextOffset) + filterProperties

	return prevURL, nextURL
}

func updateHandler(w http.ResponseWriter, req *http.Request) {
	type data struct {
		Post        Post
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
	post, err := getPost(postId)
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
		err = updatePost(post)
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
	err = publicTpl.ExecuteTemplate(w, "update.gohtml", d)
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
		err = deletePost(postId, *userId)
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
	endpoint := "nginx:9000"
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
		Posts    []Post
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
	err = publicTpl.ExecuteTemplate(w, "index.gohtml", d)
	if err != nil {
		http.Error(w, "error templating page", http.StatusInternalServerError)
	}
}

func contactHandler(w http.ResponseWriter, req *http.Request) {
	err := publicTpl.ExecuteTemplate(w, "contactinfo.gohtml", nil)
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

func verifyRegistration(w http.ResponseWriter, req *http.Request) (*string, *string, []byte, *string, error) {
	name := req.PostFormValue("name")
	email := req.PostFormValue("email")
	role := req.PostFormValue("role")

	_, err := mail.ParseAddress(email)
	if err != nil {
		http.Error(w, "Email is not of correct format.", http.StatusForbidden)
		return nil, nil, nil, nil, err
	}
	if role != "admin" && role != "user" {
		http.Error(w, "Role does not exist.", http.StatusForbidden)
		return nil, nil, nil, nil, errors.New("role does not exist")
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.PostFormValue("password")), bcrypt.MinCost)
	if err != nil {
		http.Error(w, "Password could not be encrypted.", http.StatusForbidden)
		return nil, nil, nil, nil, err
	}
	err = bcrypt.CompareHashAndPassword(hashedPassword, []byte(req.PostFormValue("repassword")))
	if err != nil {
		http.Error(w, "Entered passwords do not match.", http.StatusForbidden)
		return nil, nil, nil, nil, err
	}
	return &name, &email, hashedPassword, &role, err
}

func createUser(w http.ResponseWriter, name *string, email *string, hashedPassword []byte, role *string) error {
	_, err := db.Exec(`
		INSERT INTO users 
		(NAME,EMAIL,HASHEDPASSWORD,ROLE) 
		VALUES ($1, $2, $3, $4);`, *name, *email, string(hashedPassword), *role)
	if err != nil {
		http.Error(w, "User could not be created.", http.StatusForbidden)
	}
	return err
}

func getUserIdAndHashedPassword(email string) (*int, []byte, error) {
	var userId int
	var registeredHashedPassword []byte
	err := db.QueryRow("SELECT id, hashedpassword FROM users WHERE email=$1;", email).Scan(&userId, &registeredHashedPassword)
	if err != nil {
		return nil, nil, err
	}
	if registeredHashedPassword == nil {
		return nil, nil, errors.New("password not found")
	}
	return &userId, registeredHashedPassword, nil
}

func createPost(minioUrl string, year int, userId int) (*int, error) {
	var postId int
	err := db.QueryRow(`
		INSERT INTO posts (MINIO_URL, YEAR, USER_ID) VALUES ($1, $2, $3) RETURNING ID;`,
		minioUrl, year, userId).Scan(&postId)
	if err != nil {
		return nil, err
	}
	return &postId, nil
}

func updatePost(post Post) error {
	_, err := db.Exec(`
		UPDATE posts 
		SET UPDATED_AT=NOW(), 
			EDITED=TRUE, 
			TITLE=$1, 
			DESCRIPTION=$2,
			YEAR=$3 
		WHERE ID=$4 and USER_ID=$5;
		`,
		post.Title, post.Description, post.Year, post.Id, post.UserId)
	return err
}

func updateTags(postId *int, tags []string) error {
	db.Exec("DELETE FROM tagmap WHERE post_id = $1;", *postId)
	err := createTags(postId, tags)
	return err
}

func login(email string, password []byte) (*int, error) {
	userId, registeredHashedPassword, err := getUserIdAndHashedPassword(email)
	if err != nil {
		return nil, err
	}
	err = bcrypt.CompareHashAndPassword(registeredHashedPassword, password)
	if err != nil {
		return nil, err
	}
	return userId, nil
}

func createSession(w http.ResponseWriter, email string, password []byte) error {
	userId, err := login(email, password)
	if err != nil {
		return err
	}
	sessionID := uuid.NewV4().String()
	sessionStore[sessionID] = *userId
	cookie := &http.Cookie{
		Name:  "session",
		Value: sessionID,
	}
	http.SetCookie(w, cookie)
	return nil
}

func getLoginStatus(req *http.Request) (*int, bool) {
	cookie, err := req.Cookie("session")
	if err != nil {
		return nil, false
	}
	sessionId := cookie.Value
	userId, ok := sessionStore[sessionId]
	if !ok {
		return nil, false
	}
	return &userId, true
}

func deleteSession(req *http.Request) *http.Cookie {
	cookie, err := req.Cookie("session")
	if err != nil {
		return nil
	}
	sessionId := cookie.Value
	delete(sessionStore, sessionId)
	cookie = &http.Cookie{
		Name:   "session",
		Value:  "",
		MaxAge: -1,
	}
	return cookie
}

func deletePost(postId int, userId int) error {
	_, err := db.Exec("DELETE FROM tagmap WHERE post_id=$1;", postId)
	if err != nil {
		return err
	}
	_, err = db.Exec("DELETE FROM posts WHERE ID=$1 AND USER_ID=$2;", postId, userId)
	return err
}

func listPosts(limit, offset int, year int, tag string) ([]Post, bool, bool, error) {
	var first bool
	var last bool
	var tags []sql.NullString
	posts := make([]Post, 0)
	rows, err := queryArchive(tag, year, limit, offset)
	if err != nil {
		log.Println(err)
		return posts, false, false, err
	}
	defer rows.Close()

	for rows.Next() {
		post := Post{}
		err := rows.Scan(&post.Id, &post.ImageURL, &post.Year, &post.CreatedAt, &post.UpdatedAt, &post.Edited, &post.UserId, &post.Title, &post.Description, pq.Array(&tags))
		if err != nil {
			return posts, false, false, err
		}
		for _, nullString := range tags {
			if !nullString.Valid {
				continue
			}
			post.Tags = append(post.Tags, nullString.String)
		}
		posts = append(posts, post)
	}
	err = rows.Err()
	if err != nil {
		return posts, false, false, err
	}
	if len(posts) <= limit {
		last = true
	}
	if len(posts) > limit {
		posts = posts[:len(posts)-1]
	}
	if offset == 0 {
		first = true
	}
	return posts, first, last, nil
}

func listTagReps() ([]Post, error) {
	var tag string
	posts := make([]Post, 0)
	rows, err := db.Query(`
		SELECT
		posts.id as post_id,
		posts.minio_url as file,
		tags.name AS tag
		FROM
		(
			SELECT tag_id, MAX(post_id) AS last_post_id
			FROM tagmap
			GROUP BY tag_id
		) AS latest_post
		JOIN tags ON tags.id = latest_post.tag_id
		JOIN posts ON posts.id = latest_post.last_post_id
		ORDER BY tags.name, posts.created_at DESC;
		`)
	if err != nil {
		log.Println(err)
		return posts, err
	}
	defer rows.Close()
	for rows.Next() {
		post := Post{}
		err := rows.Scan(&post.Id, &post.ImageURL, &tag)
		if err != nil {
			return posts, err
		}
		post.Tags = []string{tag}
		posts = append(posts, post)
	}
	err = rows.Err()
	return posts, err
}

func queryURL(req *http.Request) (int, int, int, string, error) {
	var limit int = 12
	var offset int = 0
	var tag string
	var err error
	var year int

	q := req.URL.Query()

	if reqLimit, ok := q["limit"]; ok {
		limit, err = strconv.Atoi(reqLimit[0])
		if err != nil {
			return limit, offset, year, tag, err
		}
		limit = roundToNearestLimit(limit)
	}

	if reqOffset, ok := q["offset"]; ok {
		offset, err = strconv.Atoi(reqOffset[0])
		if err != nil {
			return limit, offset, year, tag, err
		}
		offset = floorDivision(offset, limit)
	}

	if reqYear, ok := q["year"]; ok {
		year, err = strconv.Atoi(reqYear[0])
		if err != nil {
			return limit, offset, year, tag, err
		}
	}
	if reqTags, ok := q["tag"]; ok {
		tag = reqTags[0]
	}

	return limit, offset, year, tag, nil
}

func getPost(postId int) (Post, error) {
	post := Post{}
	var tags []sql.NullString
	err := db.QueryRow(`
		SELECT posts.*, array_agg(tags.name) AS tags
		FROM posts
		LEFT JOIN tagmap ON posts.id = tagmap.post_id
		LEFT JOIN tags ON tagmap.tag_id = tags.id
		WHERE posts.id=$1
		GROUP BY posts.id;
	`, postId).Scan(&post.Id, &post.ImageURL, &post.Year, &post.CreatedAt, &post.UpdatedAt, &post.Edited, &post.UserId, &post.Title, &post.Description, pq.Array(&tags))
	if err != nil {
		return post, err
	}
	for _, nullString := range tags {
		if !nullString.Valid {
			continue
		}
		post.Tags = append(post.Tags, nullString.String)
	}
	return post, nil
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
		minioUrl := objectName // more efficient to store without localhostq
		postId, err := createPost(minioUrl, year, *userId)
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

func parseTags(tags string) []string {
	return strings.Split(tags, ",")
}

func createTags(postId *int, tags []string) error {
	var tagId int
	for _, tag := range cleanTags(tags) {
		err := db.QueryRow(`
			INSERT INTO tags (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name = $1 RETURNING id;
			`, tag).Scan(&tagId)
		if err != nil {
			return err
		}
		_, err = db.Exec(`
			INSERT INTO tagmap (post_id, tag_id)
			VALUES ($1, $2)
			ON CONFLICT (post_id, tag_id)
			DO UPDATE SET post_id = EXCLUDED.post_id;
		`, *postId, tagId)
		if err != nil {
			return err
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
	endpoint := "nginx:9000"
	accessKeyID := "minioadmin"
	secretAccessKey := "minioadmin"
	useSSL := false
	minioClient, err := minio.New(endpoint, accessKeyID, secretAccessKey, useSSL)
	return minioClient, err
}

func roundToNearestLimit(n int) int {
	// Create an array of the possible rounding values
	roundingValues := []int{12, 24, 48}

	// Initialize the result as the first rounding value
	result := roundingValues[0]

	// Find the nearest rounding value
	for _, val := range roundingValues {
		if abs(val-n) < abs(result-n) {
			result = val
		}
	}

	return result
}

func navigateOffsets(limit, offset int) (int, int) {
	offset = floorDivision(offset, limit)
	nextOffset := floorDivision(offset+limit, limit)
	prevOffset := floorDivision(offset-limit, limit)
	return prevOffset, nextOffset
}

func floorDivision(dividend, divisor int) int {
	quotient := int(math.Max(float64(dividend), 0)) / divisor

	return quotient * divisor
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func cleanTags(tags []string) []string {
	cleanTags := []string{}
	for _, str := range tags {
		if str != "" {
			cleanTags = append(cleanTags, strings.ToLower(str))
		}
	}
	return cleanTags
}

func queryArchive(tag string, year, limit, offset int) (*sql.Rows, error) {
	var rows *sql.Rows
	var err error
	log.Println("Hello from 1")
	if year == 0 {
		if tag == "" {
			rows, err = db.Query(`
				SELECT p.*, array_agg(t.name) AS tags
				FROM posts p
				LEFT JOIN tagmap tm ON p.id = tm.post_id
				LEFT JOIN tags t ON tm.tag_id = t.id
				GROUP BY p.id
				ORDER BY p.updated_at DESC
				LIMIT $1
				OFFSET $2;
				`, limit+1, offset)
		} else {
			rows, err = db.Query(`
				SELECT p.*, array_agg(t.name) AS tags
				FROM posts p
				LEFT JOIN tagmap tm ON p.id = tm.post_id
				LEFT JOIN tags t ON tm.tag_id = t.id
				GROUP BY p.id
				HAVING count(CASE WHEN t.name = $1 THEN 1 ELSE NULL END) >= 1
				ORDER BY p.updated_at DESC
				LIMIT $2
				OFFSET $3;
				`, tag, limit+1, offset)
		}
	} else {
		if tag == "" {
			rows, err = db.Query(`
				SELECT p.*, array_agg(t.name) AS tags
				FROM posts p
				LEFT JOIN tagmap tm ON p.id = tm.post_id
				LEFT JOIN tags t ON tm.tag_id = t.id
				WHERE p.year = $1
				GROUP BY p.id
				ORDER BY p.updated_at DESC
				LIMIT $2
				OFFSET $3;
				`, year, limit+1, offset)
		} else {
			rows, err = db.Query(`
				SELECT p.*, array_agg(t.name) AS tags
				FROM posts p
				LEFT JOIN tagmap tm ON p.id = tm.post_id
				LEFT JOIN tags t ON tm.tag_id = t.id
				WHERE p.year = $1
				GROUP BY p.id
				HAVING count(CASE WHEN t.name = '$2' THEN 1 ELSE NULL END) >= 1
				ORDER BY p.updated_at DESC
				LIMIT $3
				OFFSET $4;
				`, year, tag, limit+1, offset)
		}
	}
	log.Println(err)

	return rows, err
}

// func connectionCounter() {
// 	var count int
// 	err := db.QueryRow("SELECT COUNT(*) FROM pg_stat_activity WHERE state != 'idle'").Scan(&count)
// 	if err != nil {
// 		panic(err)
// 	}
// 	log.Println("Number of concurrent connections:", count)
// }
