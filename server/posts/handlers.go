package posts

import (
	"fmt"
	"log"
	"net/http"
	"project/server/config"
	"project/server/users"
	"strconv"
	"time"
)

func ArchiveHandler(w http.ResponseWriter, req *http.Request) {
	type data struct {
		Posts          []Post
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
	posts, first, last, err := ListPosts(limit, offset, year, tag)
	if err != nil {
		http.Error(w, "error retrieving posts from the database", http.StatusInternalServerError)
	}
	_, loggedIn := users.GetLoginStatus(req)
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

func UploadHandler(w http.ResponseWriter, req *http.Request) {
	type data struct {
		LoggedIn    bool
		CurrentYear int
	}
	_, loggedIn := users.GetLoginStatus(req)
	if !loggedIn {
		http.Redirect(w, req, "/login", http.StatusSeeOther)
		return
	}
	if req.Method == http.MethodPost {
		err := storeFiles(req)
		if err != nil {
			log.Println(err)
			http.Error(w, "error storing file object ", http.StatusInternalServerError)
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

func UpdateHandler(w http.ResponseWriter, req *http.Request) {
	type data struct {
		Post        Post
		LoggedIn    bool
		CurrentYear int
	}
	userId, loggedIn := users.GetLoginStatus(req)
	if !loggedIn {
		http.Redirect(w, req, "/login", http.StatusSeeOther)
		return
	}
	postId, err := strconv.Atoi(req.URL.Path[len("/update/"):])
	if err != nil {
		http.Error(w, "Malformatted post id", http.StatusForbidden)
		return
	}
	post, err := GetPost(postId)
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
		err = UpdatePost(post)
		if err != nil {
			http.Error(w, "Error updating post. Please try again or contact administrator.", http.StatusInternalServerError)
			return
		}
		err = UpdateTags(&postId, parseTags(req.PostFormValue("tags")))
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

func DeleteHandler(w http.ResponseWriter, req *http.Request) {
	userId, loggedIn := users.GetLoginStatus(req)
	if !loggedIn {
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}

	if req.Method == http.MethodPost {
		postId, err := strconv.Atoi(req.URL.Path[len("/delete/"):])
		if err != nil {
			http.Error(w, "requested post id is not valid", http.StatusForbidden)
		}
		err = DeletePost(postId, *userId)
		if err != nil {
			http.Error(w, "requested post could not be deleted", http.StatusForbidden)
		}
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, req, "/", http.StatusForbidden)
}

func TagRepHandler(w http.ResponseWriter, req *http.Request) {
	type data struct {
		Posts    []Post
		LoggedIn bool
	}
	_, loggedIn := users.GetLoginStatus(req)
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
