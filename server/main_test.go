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
	"project/server/config"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/lib/pq"
	"github.com/minio/minio-go"
	uuid "github.com/satori/go.uuid"
	"golang.org/x/crypto/bcrypt"
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

func TestListLastPostPerTag(t *testing.T) {
	email, password := createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	userId, _ := login(email, password)
	tags := []string{"tag1", "tag1", "tag2", "tag2", "tag3", "tag3", "tag4", "tag4"}
	defer config.DB.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")
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
	post, err := getPost(1)
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

func TestLogin(t *testing.T) {
	email, password := createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	postId, _ := createPost("image", 2017, 1)
	err := createTags(postId, inputTags)
	if err != nil {
		t.Error(("Tags should have been created."))
	}
	defer config.DB.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")

	query := `
		SELECT array_agg(tags.name) AS tags
		FROM tagmap
		INNER JOIN tags ON tagmap.tag_id = tags.id
		WHERE tagmap.post_id=1;
		`
	err = config.DB.QueryRow(query).Scan(pq.Array(&outputTags))
	if err != nil {
		t.Error(("Tags should have been listed."))
	}
	if len(inputTags) != len(outputTags) {
		t.Errorf("Number of tags listed is incorrect %d!=%d.", len(inputTags), len(outputTags))
	}
}

func TestParseTags(t *testing.T) {
	// define test cases
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"case1", "tag1, tag2, tag3", []string{"tag1", "tag2", "tag3"}},
		{"case2", "TaG1, TaG2", []string{"tag1", "tag2"}},
		{"case3", "tag1", []string{"tag1"}},
		{"case4", "", []string{""}},
		{"case5", " tag1", []string{"tag1"}},
		{"case6", " tag1 ", []string{"tag1"}},
		{"case7", "tag1 ", []string{"tag1"}},
	}

	// iterate over test cases and run assertions
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTags(tt.input)

			// check length of resulting array
			if len(got) != len(tt.want) {
				t.Errorf("parseTags() = %v, want %v", got, tt.want)
			}

			// check contents of resulting array
			for i := 0; i < len(got); i++ {
				if got[i] != tt.want[i] {
					t.Errorf("parseTags() = %v, want %v", got, tt.want)
				}
			}

			// check that the resulting array has the same type as the expected result
			if reflect.TypeOf(got) != reflect.TypeOf(tt.want) {
				t.Errorf("parseTags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeletePost(t *testing.T) {
	createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
		rows, _ := config.DB.Query("SELECT COUNT(*) FROM posts;")
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
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")
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

func TestListYears(t *testing.T) {
	createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")
	tag1 := []string{"tag1"}
	tag2 := []string{"tag2"}
	tag12 := []string{"tag1", "tag2"}
	tag3 := []string{"tag3"}

	postId1, _ := createPost("image1", 2022, 1)
	createTags(postId1, tag1)

	postId3, _ := createPost("image3", 2023, 1)
	createTags(postId3, tag1)

	postId2, _ := createPost("image2", 1999, 1)
	createTags(postId2, tag2)

	postId4, _ := createPost("image4", 1995, 1)
	createTags(postId4, tag2)

	postId5, _ := createPost("image5", 2015, 1)
	createTags(postId5, tag12)

	postId6, _ := createPost("image6", 2000, 1)
	createTags(postId6, tag12)

	postId7, _ := createPost("image7", 2017, 1)
	createTags(postId7, tag12)

	postId8, _ := createPost("image8", 2011, 1)
	createTags(postId8, tag3)

	expectedYears1 := []int{2000, 2015, 2017, 2022, 2023}
	expectedYears2 := []int{1995, 1999, 2000, 2015, 2017}
	expectedYears12 := []int{1995, 1999, 2000, 2015, 2017, 2022, 2023}
	expectedYearsAll := []int{1995, 1999, 2000, 2011, 2015, 2017, 2022, 2023}

	years, err := listYears(tag1)
	if err != nil {
		t.Errorf("Error listing years: %v", err)
	}
	if !sameContents(years, expectedYears1) {
		t.Errorf("Error listing years with tag 1: %v", err)
	}

	years, err = listYears(tag2)
	if err != nil {
		t.Errorf("Error listing years: %v", err)
	}
	if !sameContents(years, expectedYears2) {
		t.Errorf("Error listing years with tag 2: %v", err)
	}

	years, err = listYears(tag12)
	if err != nil {
		t.Errorf("Error listing years: %v", err)
	}
	if !sameContents(years, expectedYears12) {
		t.Errorf("Error listing years with tags 1 and 2: %v", err)
	}

	years, err = listYears([]string{""})
	if err != nil {
		t.Errorf("Error listing years: %v", err)
	}
	if !sameContents(years, expectedYearsAll) {
		t.Errorf("Error listing years without tag: %v", err)
	}

}

func TestUpdatePost(t *testing.T) {
	createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	postId, _ := createPost("image", 2022, 1)
	createTags(postId, []string{"test-tag"})
	defer config.DB.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")
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
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
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

func TestCreateNavigationProperties(t *testing.T) {
	limit, offset, year := 10, 20, 2022
	tag := "example"

	props, prevProps, nextProps := createProperties(limit, offset, year, tag)

	// Check that the returned strings contain the correct values
	if props != "limit=10&offset=20&year=2022&tag=example" {
		t.Errorf("Incorrect prevProperties value: %s", prevProps)
	}
	if prevProps != "limit=10&offset=10&year=2022&tag=example" {
		t.Errorf("Incorrect prevProperties value: %s", prevProps)
	}
	if nextProps != "limit=10&offset=30&year=2022&tag=example" {
		t.Errorf("Incorrect nextProperties value: %s", nextProps)
	}

	// Test the function without a tag
	tag = ""
	props, prevProps, nextProps = createProperties(limit, offset, year, tag)
	if props != "limit=10&offset=20&year=2022" {
		t.Errorf("Incorrect prevProperties value: %s", prevProps)
	}
	if prevProps != "limit=10&offset=10&year=2022" {
		t.Errorf("Incorrect prevProperties value without tag: %s", prevProps)
	}
	if nextProps != "limit=10&offset=30&year=2022" {
		t.Errorf("Incorrect nextProperties value without tag: %s", nextProps)
	}

	// Test the function without a year or tag
	year, tag = 0, ""
	props, prevProps, nextProps = createProperties(limit, offset, year, tag)
	if props != "limit=10&offset=20" {
		t.Errorf("Incorrect prevProperties value: %s", prevProps)
	}
	if prevProps != "limit=10&offset=10" {
		t.Errorf("Incorrect prevProperties value without year or tag: %s", prevProps)
	}
	if nextProps != "limit=10&offset=30" {
		t.Errorf("Incorrect nextProperties value without year or tag: %s", nextProps)
	}
}

func TestQueryURL(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		expectedLimit  int
		expectedOffset int
		expectedYear   int
		expectedTag    string
		expectedErr    error
	}{
		{
			name:           "default parameters",
			query:          "",
			expectedLimit:  12,
			expectedOffset: 0,
			expectedYear:   0,
			expectedTag:    "",
			expectedErr:    nil,
		},
		{
			name:           "custom parameters",
			query:          "limit=20&offset=10&year=2022&tag=test",
			expectedLimit:  24,
			expectedOffset: 0,
			expectedYear:   2022,
			expectedTag:    "test",
			expectedErr:    nil,
		},
		{
			name:           "invalid limit parameter",
			query:          "limit=abc&year=2022&tag=test",
			expectedLimit:  12,
			expectedOffset: 0,
			expectedYear:   2022,
			expectedTag:    "test",
			expectedErr:    strconv.ErrSyntax,
		},
		{
			name:           "invalid offset parameter",
			query:          "offset=xyz&year=2022&tag=test",
			expectedLimit:  12,
			expectedOffset: 0,
			expectedYear:   2022,
			expectedTag:    "test",
			expectedErr:    strconv.ErrSyntax,
		},
		{
			name:           "multiple tags",
			query:          "tag=testoverride&offset=xyz&year=2022&tag=test",
			expectedLimit:  12,
			expectedOffset: 0,
			expectedYear:   2022,
			expectedTag:    "testoverride",
			expectedErr:    strconv.ErrSyntax,
		},
		{
			name:           "multiple years",
			query:          "year=2011&offset=xyz&year=2022&tag=test",
			expectedLimit:  12,
			expectedOffset: 0,
			expectedYear:   2011,
			expectedTag:    "test",
			expectedErr:    strconv.ErrSyntax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create a sample request with query parameters
			req := &http.Request{
				URL: &url.URL{
					RawQuery: tt.query,
				},
			}

			// call the queryURL function to extract query parameters
			limit, offset, year, tag, _ := queryURL(req)

			// check if the extracted parameters are correct
			if limit != tt.expectedLimit {
				t.Errorf("queryURL returned an incorrect limit: expected %d, got %d", tt.expectedLimit, limit)
			}
			if offset != tt.expectedOffset {
				t.Errorf("queryURL returned an incorrect offset: expected %d, got %d", tt.expectedOffset, offset)
			}
			if year != tt.expectedYear {
				t.Errorf("queryURL returned an incorrect year: expected %d, got %d", tt.expectedYear, year)
			}
			if tag != tt.expectedTag {
				t.Errorf("queryURL returned an incorrect tag: expected %s, got %s", tt.expectedTag, tag)
			}
		})
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

func sameContents(slice1, slice2 []int) bool {
	if len(slice1) != len(slice2) {
		return false
	}

	// Make a copy of slice1 and sort it
	sortedSlice1 := make([]int, len(slice1))
	copy(sortedSlice1, slice1)
	sort.Ints(sortedSlice1)

	// Make a copy of slice2 and sort it
	sortedSlice2 := make([]int, len(slice2))
	copy(sortedSlice2, slice2)
	sort.Ints(sortedSlice2)

	// Compare the sorted slices
	for i, val := range sortedSlice1 {
		if val != sortedSlice2[i] {
			return false
		}
	}

	return true
}
