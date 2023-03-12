package users

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"project/server/config"
	"strings"
	"testing"

	uuid "github.com/satori/go.uuid"
	"golang.org/x/crypto/bcrypt"
)

func TestLogin(t *testing.T) {
	email, password := CreateTestUser()
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
		userId, err := Login(c.Email, c.Password)
		if (err != nil) != c.ExpectedError {
			t.Errorf("Test '%s' failed because an error was expected.", c.Description)
		}
		if (userId == nil) != c.ExpectedError {
			t.Errorf("Test '%s' failed because an error was expected.", c.Description)
		}
	}
}

func TestDeleteSession(t *testing.T) {
	email, password := CreateTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	userId, _ := Login(email, password)
	sessionId := uuid.NewV4().String()
	SessionStore[sessionId] = *userId
	cookie := &http.Cookie{
		Name:  "session",
		Value: sessionId,
	}
	req.AddCookie(cookie)
	cookie = deleteSession(req)
	if (cookie.Value != "") || (cookie.MaxAge != -1) {
		t.Error("Test failed because the cookie is invalid.")
	}
	_, ok := SessionStore[sessionId]
	if ok {
		t.Error("Test failed because the session Id was not removed from the sessions table.")
	}
}

func TestCreateSession(t *testing.T) {
	email, password := CreateTestUser()
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
	email, password := CreateTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	userId, _ := Login(email, password)
	sessionID := uuid.NewV4().String()
	SessionStore[sessionID] = *userId

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
		userId, loggedIn := GetLoginStatus(req)
		if loggedIn != c.ExpectedLogin {
			t.Errorf("Test '%s' failed because a different login status was expected.", c.Description)
		}
		if (userId != nil) != c.ExpectedLogin {
			t.Errorf("Test '%s' failed because a different userId was expected.", c.Description)
		}
	}
}

func TestGetUserIdAndHashedPassword(t *testing.T) {
	email, password := CreateTestUser()
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

func TestLoginHandler(t *testing.T) {
	email, password := CreateTestUser()

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
			userId, _ := Login(email, password)
			sessionID := uuid.NewV4().String()
			SessionStore[sessionID] = *userId
			cookie := &http.Cookie{
				Name:  "session",
				Value: sessionID,
			}
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		LoginHandler(w, req)
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
	email, password := CreateTestUser()
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
			userId, _ := Login(email, password)
			sessionID := uuid.NewV4().String()
			SessionStore[sessionID] = *userId // necessary?
			cookie := &http.Cookie{
				Name:  "session",
				Value: sessionID,
			}
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		LogoutHandler(w, req)
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
			userId, _ := Login(c.Email, []byte(c.Password))
			sessionID := uuid.NewV4().String()
			SessionStore[sessionID] = *userId
			cookie := &http.Cookie{
				Name:  "session",
				Value: sessionID,
			}
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		RegisterHandler(w, req)
		if w.Code != c.ExpectedStatus {
			t.Errorf("For test '%s' http response is not %d", c.Description, c.ExpectedStatus)
		}
	}
}
