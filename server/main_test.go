package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"project/server/config"
	"project/server/posts"
	"strings"
	"testing"

	"github.com/lib/pq"
	"github.com/minio/minio-go"
	uuid "github.com/satori/go.uuid"
)

func TestIndexHandler(t *testing.T) {

	type Test struct {
		Description    string
		Target         string
		ExpectedStatus int
	}
	cases := []Test{
		{"happy flow 1", "/", http.StatusOK},
		{"happy flow 2", "/archive?year=2022&limit=50", http.StatusOK},
		{"happy flow 3", "/archive?year=2022&limit=50&tag=kermis", http.StatusOK},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.Target, nil)
		w := httptest.NewRecorder()
		archiveHandler(w, req)
		if w.Code != c.ExpectedStatus {
			t.Errorf("Test '%s' failed because http response is '%d' instead of %d", c.Description, w.Code, c.ExpectedStatus)
		}
		res := w.Result()
		defer res.Body.Close()
		_, err := io.ReadAll(res.Body)
		// This of course should test the logic of the handlerFunc instead of testing te basic functionality
		if err != nil {
			t.Error("Failed to write to body.")
		}

	}
}

func TestLoginHandler(t *testing.T) {
	email, password := createTestUser()

	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	type Test struct {
		Description    string
		Email          string
		Password       string
		Login          bool
		ExpectedError  bool
		ExpectedStatus int
	}
	cases := []Test{
		{"happy flow", email, string(password), false, false, http.StatusSeeOther},
		{"wrong password", email, "wrongPassword", false, true, http.StatusForbidden},
		{"user does not exist", "wrong_email", string(password), false, true, http.StatusForbidden},
		{"already logged in", email, string(password), true, false, http.StatusSeeOther},
	}
	for _, c := range cases {
		form := url.Values{}
		form.Set("email", c.Email)
		form.Add("password", c.Password)
		body := form.Encode()
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if c.Login {
			userId, _ := login(email, password)
			sessionID := uuid.NewV4().String()
			sessionStore[sessionID] = *userId
			cookie := &http.Cookie{
				Name:  "session",
				Value: sessionID,
			}
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		loginHandler(w, req)
		if !c.Login {
			cookie := w.Header().Get("Set-Cookie")
			if (cookie == "") != c.ExpectedError {
				t.Errorf("Test '%s' failed because the cookie was not set properly.", c.Description)
			}
		}
		if w.Code != c.ExpectedStatus {
			t.Errorf("For test '%s' http response is not %d", c.Description, c.ExpectedStatus)
		}
	}
}

func TestLogoutHandler(t *testing.T) {
	email, password := createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	type Test struct {
		Description    string
		Login          bool
		ExpectedError  bool
		ExpectedStatus int
	}
	cases := []Test{
		{"happy flow", true, false, http.StatusSeeOther},
		{"not logged in", false, true, http.StatusSeeOther},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/logout", nil)
		if c.Login {
			userId, _ := login(email, password)
			sessionID := uuid.NewV4().String()
			sessionStore[sessionID] = *userId // necessary?
			cookie := &http.Cookie{
				Name:  "session",
				Value: sessionID,
			}
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		logoutHandler(w, req)
		cookie := w.Header().Get("Set-Cookie")
		if (cookie != "session=; Max-Age=0") != c.ExpectedError {
			t.Errorf("Test '%s' failed because the cookie was not set properly.", c.Description)
		}
		if w.Code != c.ExpectedStatus {
			t.Errorf("For test '%s' http response is not %d", c.Description, c.ExpectedStatus)
		}
	}
}

func TestRegisterHandler(t *testing.T) {
	// Testing overall handler functionality, so expected total behavior.
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	name := "Test User"
	type Test struct {
		Description    string
		Name           string
		Email          string
		Password       string
		Repassword     string
		Role           string
		Login          bool
		ExpectedStatus int
	}
	cases := []Test{
		{"happy flow 1", name, "test@icloud.com", "Password123", "Password123", "user", false, http.StatusSeeOther},
		{"happy flow 2", name, "admin_user_test@icloud.com", "Password123", "Password123", "admin", false, http.StatusSeeOther},
		{"password mismatch", name, "test@icloud.com", "Password123", "WrongPassword123", "user", false, http.StatusForbidden},
		{"invalid email", name, "test_icloud.com", "Password123", "Password123", "user", false, http.StatusForbidden},
		{"invalid role", name, "test@icloud.com", "Password123", "Password123", "non-existent role", false, http.StatusForbidden},
		{"duplicate entry", name, "test@icloud.com", "Password123", "Password123", "user", false, http.StatusForbidden},
		{"already logged in", name, "test@icloud.com", "Password123", "Password123", "user", true, http.StatusSeeOther},
	}

	for _, c := range cases {
		form := url.Values{}
		form.Set("name", c.Name)
		form.Add("email", c.Email)
		form.Add("password", c.Password)
		form.Add("repassword", c.Repassword)
		form.Add("role", c.Role)
		body := form.Encode()
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if c.Login {
			userId, _ := login(c.Email, []byte(c.Password))
			sessionID := uuid.NewV4().String()
			sessionStore[sessionID] = *userId
			cookie := &http.Cookie{
				Name:  "session",
				Value: sessionID,
			}
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		registerHandler(w, req)
		if w.Code != c.ExpectedStatus {
			t.Errorf("For test '%s' http response is not %d", c.Description, c.ExpectedStatus)
		}
	}
}

func TestUploadHandler(t *testing.T) {
	// Arrange
	email, password := createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	userId, _ := login(email, password)
	sessionID := uuid.NewV4().String()
	sessionStore[sessionID] = *userId
	cookie := &http.Cookie{Name: "session", Value: sessionID}

	// create a multipart form
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, _ := w.CreateFormFile("file", filepath.Join("test_data", "martian.jpg"))
	src, _ := os.Open(filepath.Join("test_data", "martian.jpg"))
	src.Seek(0, 0)
	io.Copy(fw, src)
	w.Close()

	type Test struct {
		Description    string
		Login          bool
		CorruptFile    bool
		ExpectedStatus int
	}

	cases := []Test{
		{"happy flow", true, false, http.StatusSeeOther},
		{"not authenticated", false, false, http.StatusSeeOther},
		{"corrupted file", true, true, http.StatusInternalServerError},
	}
	// create a new request
	for _, c := range cases {
		if c.CorruptFile {
			var b bytes.Buffer
			w := multipart.NewWriter(&b)
			fw, _ := w.CreateFormFile("file", filepath.Join("test_data", "martian.jpg"))
			src, _ := os.Open(filepath.Join("test_data", "martian.jpg"))
			src.Seek(0, 0)
			io.Copy(fw, src)
			b.WriteString("corrupted")
			w.Close()
		}
		req, _ := http.NewRequest("POST", "/upload", &b)
		req.Header.Add("Content-Type", w.FormDataContentType())

		// Add PostForm values
		form := url.Values{}
		form.Add("year", "2022")
		req.PostForm = form
		wr := httptest.NewRecorder()
		if c.Login {
			req.AddCookie(cookie)
		}

		// call the function
		uploadHandler(wr, req)
		if wr.Code != c.ExpectedStatus {
			t.Errorf("Test '%s' failed because status %d was expected.", c.Description, c.ExpectedStatus)
		}

	}

	// TODO test if redirected to right url
}

func TestUpdateHandler(t *testing.T) {
	email, password := createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	userId, _ := login(email, password)
	sessionID := uuid.NewV4().String()
	sessionStore[sessionID] = *userId
	cookie := &http.Cookie{Name: "session", Value: sessionID}

	postId, _ := createPost("image", 2022, 1)
	createTags(postId, []string{"test-tag"})
	defer config.DB.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")

	// updatePost
	form := url.Values{}
	form.Set("id", "1")
	form.Add("year", "2021")
	form.Add("title", "New Title")
	form.Add("description", "New Description")
	form.Add("tags", "new-test-tag,extra-test-tag")
	req := httptest.NewRequest(http.MethodPost, "/update/1", nil)
	req.PostForm = form
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	updateHandler(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("Post was not successfully updated.")
	}
}

func listYears(tags []string) ([]int, error) {
	var year int
	var years []int
	var filter string
	var rows *sql.Rows

	if len(tags) == 0 {
		return nil, fmt.Errorf("tags are empty")
	} else if len(tags) == 1 && tags[0] == "" {
		filter = ""
	} else {
		filter = `
			JOIN tagmap ON posts.id = tagmap.post_id 
			WHERE tagmap.tag_id IN (
				SELECT id FROM tags WHERE name IN ('` + strings.Join(tags, "','") + `')
			)
		`
	}

	query := fmt.Sprintf(`
        SELECT DISTINCT posts.year 
        FROM posts 
		%s
        ORDER BY posts.year ASC;
    `, filter)
	// Prepare the query
	stmt, err := config.DB.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	// Execute the query
	rows, err = stmt.Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Iterate over the results
	for rows.Next() {
		err := rows.Scan(&year)
		if err != nil {
			return nil, err
		}
		years = append(years, year)
	}

	// Check for any errors during iteration
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return years, nil
}

func listTagReps() ([]posts.Post, error) {
	var tags []string
	postSlice := make([]posts.Post, 0)
	rows, err := config.DB.Query(`
		SELECT
			MAX(posts.id) AS post_id,
			posts.minio_url AS file,
			ARRAY_AGG(tags.name) AS tags
		FROM
			(
				SELECT tag_id, MAX(post_id) AS last_post_id
				FROM tagmap
				GROUP BY tag_id
			) AS latest_post
			JOIN tags ON tags.id = latest_post.tag_id
			JOIN posts ON posts.id = latest_post.last_post_id
		GROUP BY
			posts.minio_url
		ORDER BY
			tags, MAX(posts.created_at) DESC;
		`)
	if err != nil {
		log.Println(err)
		return postSlice, err
	}
	defer rows.Close()
	for rows.Next() {
		post := posts.Post{}
		err := rows.Scan(&post.Id, &post.ImageURL, pq.Array(&tags))
		if err != nil {
			return postSlice, err
		}
		post.Tags = tags
		postSlice = append(postSlice, post)
	}
	err = rows.Err()
	return postSlice, err
}

func parseTags(tags string) []string {
	tagList := strings.Split(tags, ",")

	for i := range tagList {
		tagList[i] = strings.ToLower(strings.TrimSpace(tagList[i]))
	}

	return tagList
}

func createTags(postId *int, tags []string) error {
	var tagId int
	for _, tag := range cleanTags(tags) {
		err := config.DB.QueryRow(`
			INSERT INTO tags (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name = $1 RETURNING id;
			`, tag).Scan(&tagId)
		if err != nil {
			return err
		}
		_, err = config.DB.Exec(`
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

func TestDeleteHandler(t *testing.T) {
	email, password := createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")

	createPost("test", 2022, 1)

	type Test struct {
		Descripion     string
		Login          bool
		Target         string
		ExpectedStatus int
	}

	cases := []Test{
		{"Not authenticated", false, "/delete/1", http.StatusSeeOther},
		{"Happy flow", true, "/delete/1", http.StatusSeeOther},
		{"Already removed", true, "/delete/1", http.StatusSeeOther},
		{"Does not exist", true, "/delete/5", http.StatusSeeOther},
		{"Invalid post id", true, "/delete/corrupt", http.StatusForbidden},
	}

	for _, c := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, c.Target, nil)
		if c.Login {
			userId, _ := login(email, password)
			sessionID := uuid.NewV4().String()
			sessionStore[sessionID] = *userId // necessary?
			cookie := &http.Cookie{
				Name:  "session",
				Value: sessionID,
			}
			req.AddCookie(cookie)
		}
		deleteHandler(w, req)
		if w.Code != c.ExpectedStatus {
			t.Errorf("Test '%s' failed because a different status code was expected (%d != %d)", c.Descripion, c.ExpectedStatus, w.Code)
		}
	}

}

func TestStoreFiles(t *testing.T) {
	// Arrange
	email, password := createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	userId, _ := login(email, password)
	sessionID := uuid.NewV4().String()
	sessionStore[sessionID] = *userId
	cookie := &http.Cookie{Name: "session", Value: sessionID}

	// create a multipart form
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, _ := w.CreateFormFile("file", filepath.Join("test_data", "martian.jpg"))
	src, _ := os.Open(filepath.Join("test_data", "martian.jpg"))
	srcHash := computeHashSum(src)
	src.Seek(0, 0)
	io.Copy(fw, src)

	w.Close()

	// create a new request
	req, _ := http.NewRequest("POST", "/upload", &b)
	req.Header.Add("Content-Type", w.FormDataContentType())

	// Add PostForm values
	form := url.Values{}
	form.Add("year", "2022")
	form.Add("tags", "kermis,tilburg,kei")
	req.PostForm = form
	req.AddCookie(cookie)
	// call the function
	if err := storeFiles(req); err != nil {
		t.Fatalf("error writing files to local storage: %v", err)
	}
	defer config.DB.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")

	// Start MinIO client
	minioClient, err := newMinIO()
	if err != nil {
		t.Fatalf("error starting MinIO client: %v", err)
	}
	objectName := fmt.Sprintf("2022/%x.jpg", srcHash)
	bucket := "download"
	defer minioClient.RemoveObject(bucket, objectName)
	// check if the file was written to minio storage
	dstInfo, err := minioClient.GetObjectACL(bucket, objectName)
	if err != nil {
		t.Fatalf("error retrieving object from MinIO: %v", err)
	}
	if dstInfo.Size == 0 {
		t.Fatal("Stored object has no size")
	}
	// Check if object can be loaded
	dst, err := minioClient.GetObject(bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("error retrieving object from MinIO: %v", err)
	}
	defer dst.Close()

	// check if the file hash is the same
	dstHash := computeHashSum(dst)
	if srcHash != dstHash {
		t.Fatal("error the file was written to local storage with loss")
	}

	// check if a post was written to the db
	post, err := GetPost(1)
	if err != nil {
		t.Fatalf("error retrieving post from database: %v", err)
	}
	if post.ImageURL == "" {
		t.Fatal("error as the post was not written to the database")
	}
	// check if tags were written to db
	var outputTags []string
	err = config.DB.QueryRow(`
		SELECT array_agg(tags.name) AS tags
		FROM tagmap
		INNER JOIN tags ON tagmap.tag_id = tags.id
		WHERE tagmap.post_id = 1;
		`).Scan(pq.Array(&outputTags))
	if err != nil {
		t.Error(("Tags should have been listed."))
	}
	if len(outputTags) != 3 {
		t.Errorf("Number of tags listed is incorrect %d!=%d.", 3, len(outputTags))
	}
}
