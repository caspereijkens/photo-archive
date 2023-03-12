package users

import (
	"errors"
	"net/http"
	"net/mail"
	"project/server/config"

	uuid "github.com/satori/go.uuid"
	"golang.org/x/crypto/bcrypt"
)

var sessionStore = make(map[string]int)

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
	_, err := config.DB.Exec(`
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
	err := config.DB.QueryRow("SELECT id, hashedpassword FROM users WHERE email=$1;", email).Scan(&userId, &registeredHashedPassword)
	if err != nil {
		return nil, nil, err
	}
	if registeredHashedPassword == nil {
		return nil, nil, errors.New("password not found")
	}
	return &userId, registeredHashedPassword, nil
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
