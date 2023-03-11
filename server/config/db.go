package config

import (
	"database/sql"
	"log"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func init() {
	var err error
	// DB, err = sql.Open("postgres", "postgres://casper:password@db/joepeijkens?sslmode=disable")
	DB, err = sql.Open("postgres", "postgres://postgres:password@localhost/joepeijkens?sslmode=disable")

	if err != nil {
		panic(err)
	}

	if err = DB.Ping(); err != nil {
		panic(err)
	}
	log.Println("You connected to your database.")
}
