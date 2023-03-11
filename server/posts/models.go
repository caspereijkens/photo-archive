package posts

import "time"

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
