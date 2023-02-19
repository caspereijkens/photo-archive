package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lib/pq"
	"github.com/minio/minio-go"
	uuid "github.com/satori/go.uuid"
	"golang.org/x/crypto/bcrypt"
)

// func init() {
// 	var err error
// 	// db, err = sql.Open("postgres", os.Getenv("POSTGRES_DATA_SOURCE_NAME"))
// 	db, err = sql.Open("postgres", "postgres://postgres:password@localhost/joepeijkens?sslmode=disable")
// 	if err != nil {
// 		panic(err)
// 	}
// 	if err = db.Ping(); err != nil {
// 		panic(err)
// 	}
// 	log.Println("You connected to your database.")
// 	publicTpl = template.Must(template.ParseGlob("../public/html/*"))
// }

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

	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	userId, _ := login(email, password)
	sessionID := uuid.NewV4().String()
	sessionStore[sessionID] = *userId
	cookie := &http.Cookie{Name: "session", Value: sessionID}

	postId, _ := createPost("image", 2022, 1)
	createTags(postId, []string{"test-tag"})
	defer db.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer db.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")

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

func TestDeleteHandler(t *testing.T) {
	email, password := createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")

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

func TestListLastPostPerTag(t *testing.T) {
	email, password := createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	userId, _ := login(email, password)
	tags := []string{"tag1", "tag1", "tag2", "tag2", "tag3", "tag3", "tag4", "tag4"}
	defer db.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer db.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")
	for i, tag := range tags {
		postId, _ := createPost(fmt.Sprintf("image-%d", i), 2022, *userId)
		createTags(postId, []string{tag})

	}

	posts, err := listTagReps()

	if err != nil {
		t.Error("Error querying for last post per tag")
	}

	if len(posts) != len(tags)/2 {
		t.Error("Not every tag is represented here")
	}

	for i, post := range posts {
		if post.Id != (2 * (i + 1)) {
			t.Error("This is not the last post")
		}

	}

}

func TestStoreFiles(t *testing.T) {
	// Arrange
	email, password := createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer db.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer db.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")

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
	post, err := getPost(1)
	if err != nil {
		t.Fatalf("error retrieving post from database: %v", err)
	}
	if post.ImageURL == "" {
		t.Fatal("error as the post was not written to the database")
	}
	// check if tags were written to db
	var outputTags []string
	err = db.QueryRow(`
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

func TestLogin(t *testing.T) {
	email, password := createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	type Test struct {
		Description   string
		Email         string
		Password      []byte
		ExpectedError bool
	}
	cases := []Test{
		{"happy flow", email, password, false},
		{"wrong password", email, []byte("wrongPassword"), true},
		{"user does not exist", "wrong_email", password, true},
	}
	for _, c := range cases {
		userId, err := login(c.Email, c.Password)
		if (err != nil) != c.ExpectedError {
			t.Errorf("Test '%s' failed because an error was expected.", c.Description)
		}
		if (userId == nil) != c.ExpectedError {
			t.Errorf("Test '%s' failed because an error was expected.", c.Description)
		}
	}
}

func TestDeleteSession(t *testing.T) {
	email, password := createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	userId, _ := login(email, password)
	sessionId := uuid.NewV4().String()
	sessionStore[sessionId] = *userId
	cookie := &http.Cookie{
		Name:  "session",
		Value: sessionId,
	}
	req.AddCookie(cookie)
	cookie = deleteSession(req)
	if (cookie.Value != "") || (cookie.MaxAge != -1) {
		t.Error("Test failed because the cookie is invalid.")
	}
	_, ok := sessionStore[sessionId]
	if ok {
		t.Error("Test failed because the session Id was not removed from the sessions table.")
	}
}

func TestCreateSession(t *testing.T) {
	email, password := createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	type Test struct {
		Description   string
		Email         string
		Password      []byte
		ExpectedError bool
	}
	cases := []Test{
		{"happy flow", email, password, false},
		{"wrong password", email, []byte("wrongPassword"), true},
		{"user does not exist", "wrong_email", password, true},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		err := createSession(w, c.Email, c.Password)
		if (err != nil) != c.ExpectedError {
			t.Errorf("Test '%s' failed because an error was expected.", c.Description)
		}
		cookie := w.Header().Get("Set-Cookie")
		if (cookie == "") != c.ExpectedError {
			t.Errorf("Test '%s' failed because the cookie was not set properly.", c.Description)
		}
	}
}

func TestGetLoginStatus(t *testing.T) {
	email, password := createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	userId, _ := login(email, password)
	sessionID := uuid.NewV4().String()
	sessionStore[sessionID] = *userId

	type Test struct {
		Description   string
		Cookie        *http.Cookie
		ExpectedLogin bool
	}

	cases := []Test{
		{"happy flow", &http.Cookie{Name: "session", Value: sessionID}, true},
		{"invalid cookie name", &http.Cookie{Name: "invalid", Value: sessionID}, false},
		{"invalid sessionID", &http.Cookie{Name: "session", Value: "wrong-id"}, false},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		req.AddCookie(c.Cookie)
		userId, loggedIn := getLoginStatus(req)
		if loggedIn != c.ExpectedLogin {
			t.Errorf("Test '%s' failed because a different login status was expected.", c.Description)
		}
		if (userId != nil) != c.ExpectedLogin {
			t.Errorf("Test '%s' failed because a different userId was expected.", c.Description)
		}
	}
}

func TestGetUserIdAndHashedPassword(t *testing.T) {
	email, password := createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	type Test struct {
		Description   string
		Email         string
		ExpectedError bool
	}
	cases := []Test{
		{"happy flow", email, false},
		{"email not registered", "wrong_email@icloud.com", true},
	}
	for _, c := range cases {
		userId, hashedPassword, err := getUserIdAndHashedPassword(c.Email)
		if (err != nil) != c.ExpectedError {
			t.Fatalf("Test '%s' failed because an error was expected", c.Description)
		}
		if (hashedPassword == nil) != c.ExpectedError {
			t.Errorf("Test '%s' failed because returned password is not nil.", c.Description)
		}
		if (userId == nil) != c.ExpectedError {
			t.Errorf("Test '%s' failed because returned password is not nil.", c.Description)
		}
		if hashedPassword != nil {
			err = bcrypt.CompareHashAndPassword(hashedPassword, password)
			if err != nil {
				t.Errorf("Test '%s' failed because passwords do not match.", c.Description)
			}
		}
	}
}

func TestVerifyRegistration(t *testing.T) {
	validPassword := "Password123"
	validEmail := "test@icloud.com"
	invalidEmail := "test_icloud.com"
	invalidPassword := ""
	validRole := "user"
	invalidRole := "invalidrole"
	name := "Martin"

	type Test struct {
		TestDescription string
		Name            string
		Email           string
		Password        string
		Repassword      string
		Role            string
		ExpectedError   bool
	}
	cases := []Test{
		{"happy flow", name, validEmail, validPassword, validPassword, validRole, false},
		{"password mismatch", name, validEmail, validPassword, invalidPassword, validRole, true},
		{"invalid email", name, invalidEmail, validPassword, validPassword, validRole, true},
		{"invalid role", name, validEmail, validPassword, validPassword, invalidRole, true},
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
		w := httptest.NewRecorder()
		name, email, hashedPassword, role, err := verifyRegistration(w, req)
		if (err != nil) != c.ExpectedError {
			t.Fatalf("Test '%s' failed because no error was thrown.", c.TestDescription)
		}
		if (email == nil) != c.ExpectedError {
			t.Fatalf("Test '%s' failed because the incorrect email was registered.", c.TestDescription)
		}
		if (role == nil) != c.ExpectedError {
			t.Fatalf("Test '%s' failed because the incorrect role was registered.", c.TestDescription)

		}
		if (hashedPassword == nil) != c.ExpectedError {
			t.Fatalf("Test '%s' failed because the password was incorrectly encrypted.", c.TestDescription)
		}
		if (name == nil) != c.ExpectedError {
			t.Fatalf("Test '%s' failed because the incorrect name was registered.", c.TestDescription)

		}
	}
}

func TestCreateUser(t *testing.T) {
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	hashedPassword := []byte("Password123")
	email := "test@icloud.com"
	name := "Test User"
	role := "user"
	w := httptest.NewRecorder()

	type Test struct {
		TestDescription string
		Name            string
		Email           string
		HashedPassword  []byte
		Role            string
		ExpectedError   bool
	}
	cases := []Test{
		{"happy flow", name, email, hashedPassword, role, false},
		{"duplicate entry", name, email, hashedPassword, role, true},
		{"invalid role", name, email, hashedPassword, "invalid role", true},
	}
	for _, c := range cases {
		err := createUser(w, &c.Name, &c.Email, c.HashedPassword, &c.Role)
		if (err != nil) != c.ExpectedError {
			t.Fatalf("Test '%s' failed because no error was thrown.", c.TestDescription)
		}
	}

}

func TestCreatePost(t *testing.T) {
	createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	userId := 1
	year := 2022
	type Test struct {
		Description   string
		Image         string
		Year          int
		UserId        int
		ExpectedError bool
	}
	cases := []Test{
		{"happy flow", "image", year, userId, false},
		{"duplicate entry", "image", year, userId, true},
	}
	for _, c := range cases {
		postId, err := createPost(c.Image, c.Year, c.UserId)
		if (err != nil) != c.ExpectedError {
			t.Errorf("Test '%s' failed because a different error was expected.", c.Description)
		}
		if err == nil {
			if (*postId != 1) != c.ExpectedError {
				t.Errorf("Test '%s' failed because a different post id was expected.", c.Description)
			}
		}
	}
}

func TestCreateTags(t *testing.T) {
	var outputTags []string
	inputTags := []string{"kermis", "lolly", "draaimolen", "roze"}
	createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	postId, _ := createPost("image", 2017, 1)
	err := createTags(postId, inputTags)
	if err != nil {
		t.Error(("Tags should have been created."))
	}
	defer db.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer db.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")

	query := `
		SELECT array_agg(tags.name) AS tags
		FROM tagmap
		INNER JOIN tags ON tagmap.tag_id = tags.id
		WHERE tagmap.post_id=1;
		`
	err = db.QueryRow(query).Scan(pq.Array(&outputTags))
	if err != nil {
		t.Error(("Tags should have been listed."))
	}
	if len(inputTags) != len(outputTags) {
		t.Errorf("Number of tags listed is incorrect %d!=%d.", len(inputTags), len(outputTags))
	}
}

func TestParseTags(t *testing.T) {
	inputTags := "1,2,3,4,5,6,7,8,9,10"
	outputTags := parseTags(inputTags)

	if len(outputTags) != 10 {
		t.Error("error parsing tags")
	}
}

func TestDeletePost(t *testing.T) {
	createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	var count int
	type Test struct {
		Description   string
		PostId        int
		UserId        int
		ExpectedError bool
	}
	cases := []Test{
		{"happy flow", 1, 1, false},
		{"post does not exist", 1, 1, true},
		{"not ownded by user", 3, 2, true},
	}
	for _, c := range cases {
		createPost("image", 2022, 1)
		deletePost(c.PostId, c.UserId)
		rows, _ := db.Query("SELECT COUNT(*) FROM posts;")
		defer rows.Close()
		for rows.Next() {
			rows.Scan(&count)
		}
		if (count > 0) != c.ExpectedError {
			t.Errorf("Test '%s' failed because a different error was expected.", c.Description)
		}
	}
}

func TestFloorOffset(t *testing.T) {
	type Test struct {
		Limit          int
		Offset         int
		ExpectedOffset int
	}
	cases := []Test{
		{12, 0, 0},
		{12, 12, 12},
		{12, 24, 24},
		{12, 1, 0},
		{12, 13, 12},
		{24, 0, 0},
		{24, 1, 0},
		{24, 24, 24},
		{24, 25, 24},
		{24, 48, 48},
		{48, 0, 0},
		{48, 1, 0},
		{48, 48, 48},
		{48, 96, 96},
		{48, -1, 0},
		{48, -1000, 0},
		{12, -1000, 0},
		{24, -1000, 0},
	}
	for _, c := range cases {
		offset := floorDivision(c.Offset, c.Limit)
		if offset != c.ExpectedOffset {
			t.Errorf("%d (actual) != %d (expected)", offset, c.ExpectedOffset)
		}
	}
}

func TestGetNavigationOffsets(t *testing.T) {
	type Test struct {
		Limit              int
		Offset             int
		ExpectedPrevOffset int
		ExpectedNextOffset int
	}
	cases := []Test{
		{12, 0, 0, 12},
		{12, 1, 0, 12},
		{12, -1000, 0, 12},
		{12, 12, 0, 24},
		{12, 24, 12, 36},
		{24, 0, 0, 24},
		{24, -1000, 0, 24},
		{24, 23, 0, 24},
		{24, 24, 0, 48},
		{24, 48, 24, 72},
		{48, 0, 0, 48},
		{48, 19, 0, 48},
		{48, 48, 0, 96},
		{48, 96, 48, 144},
	}
	for _, c := range cases {
		prevOffset, nextOffset := navigateOffsets(c.Limit, c.Offset)
		if prevOffset != c.ExpectedPrevOffset {
			t.Errorf("%d (actual) != %d (expected)", prevOffset, c.ExpectedPrevOffset)
		}
		if nextOffset != c.ExpectedNextOffset {
			t.Errorf("%d (actual) != %d (expected)", nextOffset, c.ExpectedNextOffset)
		}
	}
}

func TestListPost(t *testing.T) {
	createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	defer db.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer db.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")
	year := 2022
	tags := []string{}
	testCount := 3
	for i := 0; i <= testCount; i++ {
		// TODO this is using a *http.Request now!
		posts, first, last, err := listPosts(12, 0, year, "")
		if err != nil {
			t.Error("Test failed because posts could not be listed.")
		}
		if len(posts) != i {
			t.Errorf("Test failed because %d rows were expected.", i)
		}
		if !last {
			t.Error("Test failed because end of query should be reached.")
		}
		if !first {
			t.Error("Test failed because this should be the beginning of the query.")
		}
		imageName := fmt.Sprintf("image-%d", i)
		postId, err := createPost(imageName, 2022, 1)
		if err != nil {
			t.Error("Test failed because post could not be created.")
		}
		tags = append(tags, fmt.Sprintf("tag-%d", i))
		err = createTags(postId, tags)
		if err != nil {
			t.Error("Test failed because tags could not be created.")
		}

	}
	for i := 0; i <= testCount; i++ {
		tagName := fmt.Sprintf("tag-%d", i)
		posts, _, _, _ := listPosts(12, 0, year, tagName)
		if len(posts) != (testCount + 1 - i) {
			t.Error("Test failed because tag filter is not working properly.")
		}

	}

}

func TestUpdatePost(t *testing.T) {
	createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	createPost("image", 2022, 1)
	post, _ := getPost(1)
	postCreatedAt := post.CreatedAt
	for i := 0; i <= 5; i++ {
		imageName := fmt.Sprintf("image-%d", i)
		post.ImageURL = imageName
		updatePost(post)
		updatedPost, _ := getPost(1)
		postUpdatedAt := updatedPost.UpdatedAt
		if !postUpdatedAt.After(postCreatedAt) {
			t.Error("Post was not updated.")
		}
	}
}

func TestUpdateTags(t *testing.T) {
	createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	postId, _ := createPost("image", 2022, 1)
	createTags(postId, []string{"test-tag"})
	defer db.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer db.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")
	inputTags := []string{"edited-test-tag", "extra-test-tag"}
	err := updateTags(postId, inputTags)
	if err != nil {
		t.Errorf("Error updating post: %v", err)
	}
	post, _ := getPost(*postId)
	for _, tag := range inputTags {
		if !contains(post.Tags, tag) {
			t.Error("Tags of post were not properly updated.")
		}
	}

}

func TestGetPost(t *testing.T) {
	createTestUser()
	defer db.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	for i := 0; i <= 3; i++ {
		imageName := fmt.Sprintf("image-%d", i)
		createPost(imageName, 2022, 1)
		post, _ := getPost(i + 1)
		if post.ImageURL != imageName {
			t.Errorf("Test failed because title '%s' was expected.", imageName)
		}
	}
}

// Registers a new user
func createTestUser() (string, []byte) {
	w := httptest.NewRecorder()
	name := "Test User"
	email := "test@icloud.com"
	password := []byte("Password123")
	hashedPassword, _ := bcrypt.GenerateFromPassword(password, bcrypt.MinCost)
	role := "user"
	createUser(w, &name, &email, hashedPassword, &role)
	return email, password
}

func TestRoundToNearest(t *testing.T) {
	type Test struct {
		InputData int
		Expected  int
	}
	cases := []Test{
		{0, 12},
		{-5, 12},
		{100, 48},
		{51, 48},
		{30, 24},
	}
	for _, c := range cases {
		output := roundToNearestLimit(c.InputData)
		if output != c.Expected {
			t.Errorf("mismatch! %d != %d", output, c.Expected)
		}
	}
}

func TestParseFilter(t *testing.T) {
	tests := []struct {
		year     int
		tag      string
		expected string
	}{
		{0, "", ""},
		{0, "tag1", "WHERE tags.name='tag1'"},
		{2022, "", "WHERE posts.year=2022"},
		{2022, "tag1", "WHERE posts.year=2022 AND tags.name='tag1'"},
	}

	for _, test := range tests {
		result := parseArchiveQuery(test.year, test.tag)
		if result != test.expected {
			t.Errorf("parseFilter(%d, %s) = %s, expected %s", test.year, test.tag, result, test.expected)
		}
	}
}

func TestCreateNavigationProperties(t *testing.T) {
	limit, offset, year := 10, 20, 2022
	tag := "example"

	prevProps, nextProps := createNavigationURLs(limit, offset, year, tag)

	// Check that the returned strings contain the correct values
	if prevProps != "limit=10&offset=10&year=2022&tag=example" {
		t.Errorf("Incorrect prevProperties value: %s", prevProps)
	}
	if nextProps != "limit=10&offset=30&year=2022&tag=example" {
		t.Errorf("Incorrect nextProperties value: %s", nextProps)
	}

	// Test the function without a tag
	tag = ""
	prevProps, nextProps = createNavigationURLs(limit, offset, year, tag)
	if prevProps != "limit=10&offset=10&year=2022" {
		t.Errorf("Incorrect prevProperties value without tag: %s", prevProps)
	}
	if nextProps != "limit=10&offset=30&year=2022" {
		t.Errorf("Incorrect nextProperties value without tag: %s", nextProps)
	}

	// Test the function without a year or tag
	year, tag = 0, ""
	prevProps, nextProps = createNavigationURLs(limit, offset, year, tag)
	if prevProps != "limit=10&offset=10" {
		t.Errorf("Incorrect prevProperties value without year or tag: %s", prevProps)
	}
	if nextProps != "limit=10&offset=30" {
		t.Errorf("Incorrect nextProperties value without year or tag: %s", nextProps)
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
