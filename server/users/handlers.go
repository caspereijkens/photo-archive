package users

import (
	"net/http"
	"project/server/config"
)

func LoginHandler(w http.ResponseWriter, req *http.Request) {
	_, loggedIn := GetLoginStatus(req)
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

func RegisterHandler(w http.ResponseWriter, req *http.Request) {
	_, loggedIn := GetLoginStatus(req)
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

func LogoutHandler(w http.ResponseWriter, req *http.Request) {
	_, loggedIn := GetLoginStatus(req)
	if !loggedIn {
		http.Redirect(w, req, "/login", http.StatusSeeOther)
		return
	}
	cookie := deleteSession(req)
	http.SetCookie(w, cookie)
	http.Redirect(w, req, "/login", http.StatusSeeOther)
}
