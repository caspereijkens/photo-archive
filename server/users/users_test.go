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
