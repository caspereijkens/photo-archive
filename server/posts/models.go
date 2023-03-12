package posts

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"project/server/config"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

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

func CreatePost(minioUrl string, year int, userId int) (*int, error) {
	var postId int
	err := config.DB.QueryRow(`
		INSERT INTO posts (MINIO_URL, YEAR, USER_ID) VALUES ($1, $2, $3) RETURNING ID;`,
		minioUrl, year, userId).Scan(&postId)
	if err != nil {
		return nil, err
	}
	return &postId, nil
}

func GetPost(postId int) (Post, error) {
	post := Post{}
	var tags []sql.NullString
	err := config.DB.QueryRow(`
		SELECT posts.*, array_agg(tags.name) AS tags
		FROM posts
		LEFT JOIN tagmap ON posts.id = tagmap.post_id
		LEFT JOIN tags ON tagmap.tag_id = tags.id
		WHERE posts.id=$1
		GROUP BY posts.id;
	`, postId).Scan(&post.Id, &post.ImageURL, &post.Year, &post.CreatedAt, &post.UpdatedAt, &post.Edited, &post.UserId, &post.Title, &post.Description, pq.Array(&tags))
	if err != nil {
		return post, err
	}
	for _, nullString := range tags {
		if !nullString.Valid {
			continue
		}
		post.Tags = append(post.Tags, nullString.String)
	}
	return post, nil
}

func UpdatePost(post Post) error {
	_, err := config.DB.Exec(`
		UPDATE posts 
		SET UPDATED_AT=NOW(), 
			EDITED=TRUE, 
			TITLE=$1, 
			DESCRIPTION=$2,
			YEAR=$3 
		WHERE ID=$4 and USER_ID=$5;
		`,
		post.Title, post.Description, post.Year, post.Id, post.UserId)
	return err
}

func DeletePost(postId int, userId int) error {
	_, err := config.DB.Exec("DELETE FROM tagmap WHERE post_id=$1;", postId)
	if err != nil {
		return err
	}
	_, err = config.DB.Exec("DELETE FROM posts WHERE ID=$1 AND USER_ID=$2;", postId, userId)
	return err
}

func ListPosts(limit, offset int, year int, tag string) ([]Post, bool, bool, error) {
	var first bool
	var last bool
	var tags []sql.NullString
	postSlice := make([]Post, 0)
	rows, err := queryArchive(tag, year, limit, offset)
	if err != nil {
		log.Println(err)
		return postSlice, false, false, err
	}
	defer rows.Close()

	for rows.Next() {
		post := Post{}
		err := rows.Scan(&post.Id, &post.ImageURL, &post.Year, &post.CreatedAt, &post.UpdatedAt, &post.Edited, &post.UserId, &post.Title, &post.Description, pq.Array(&tags))
		if err != nil {
			return postSlice, false, false, err
		}
		for _, nullString := range tags {
			if !nullString.Valid {
				continue
			}
			post.Tags = append(post.Tags, nullString.String)
		}
		postSlice = append(postSlice, post)
	}
	err = rows.Err()
	if err != nil {
		return postSlice, false, false, err
	}
	if len(postSlice) <= limit {
		last = true
	}
	if len(postSlice) > limit {
		postSlice = postSlice[:len(postSlice)-1]
	}
	if offset == 0 {
		first = true
	}
	return postSlice, first, last, nil
}

func queryArchive(tag string, year, limit, offset int) (*sql.Rows, error) {
	var rows *sql.Rows
	var err error
	if year == 0 {
		if tag == "" {
			rows, err = config.DB.Query(`
				SELECT p.*, array_agg(t.name) AS tags
				FROM posts p
				LEFT JOIN tagmap tm ON p.id = tm.post_id
				LEFT JOIN tags t ON tm.tag_id = t.id
				GROUP BY p.id
				ORDER BY p.updated_at DESC
				LIMIT $1
				OFFSET $2;
				`, limit+1, offset)
		} else {
			rows, err = config.DB.Query(`
				SELECT p.*, array_agg(t.name) AS tags
				FROM posts p
				LEFT JOIN tagmap tm ON p.id = tm.post_id
				LEFT JOIN tags t ON tm.tag_id = t.id
				GROUP BY p.id
				HAVING count(CASE WHEN t.name = $1 THEN 1 ELSE NULL END) >= 1
				ORDER BY p.updated_at DESC
				LIMIT $2
				OFFSET $3;
				`, tag, limit+1, offset)
		}
	} else {
		if tag == "" {
			rows, err = config.DB.Query(`
				SELECT p.*, array_agg(t.name) AS tags
				FROM posts p
				LEFT JOIN tagmap tm ON p.id = tm.post_id
				LEFT JOIN tags t ON tm.tag_id = t.id
				WHERE p.year = $1
				GROUP BY p.id
				ORDER BY p.updated_at DESC
				LIMIT $2
				OFFSET $3;
				`, year, limit+1, offset)
		} else {
			rows, err = config.DB.Query(`
				SELECT p.*, array_agg(t.name) AS tags
				FROM posts p
				LEFT JOIN tagmap tm ON p.id = tm.post_id
				LEFT JOIN tags t ON tm.tag_id = t.id
				WHERE p.year = $1
				GROUP BY p.id
				HAVING count(CASE WHEN t.name = $2 THEN 1 ELSE NULL END) >= 1
				ORDER BY p.updated_at DESC
				LIMIT $3
				OFFSET $4;
				`, year, tag, limit+1, offset)
		}
	}
	if err != nil {
		log.Printf("Error querying the archive: %v\n", err)
	}

	return rows, err
}

func UpdateTags(postId *int, tags []string) error {
	config.DB.Exec("DELETE FROM tagmap WHERE post_id = $1;", *postId)
	err := createTags(postId, tags)
	return err
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

func cleanTags(tags []string) []string {
	cleanTags := []string{}
	for _, str := range tags {
		if str != "" {
			cleanTags = append(cleanTags, strings.ToLower(str))
		}
	}
	return cleanTags
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

func listTagReps() ([]Post, error) {
	var tags []string
	postSlice := make([]Post, 0)
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
		post := Post{}
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

func createProperties(limit, offset, year int, tag string) (string, string, string) {
	prevOffset, nextOffset := navigateOffsets(limit, offset)

	var filterProperties string
	if year > 0 {
		filterProperties = "&year=" + strconv.Itoa(year)
	}
	if tag != "" {
		filterProperties += "&tag=" + url.QueryEscape(tag)
	}

	properties := "limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(offset) + filterProperties
	prevProperties := "limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(prevOffset) + filterProperties
	nextProperties := "limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(nextOffset) + filterProperties

	return properties, prevProperties, nextProperties
}

func navigateOffsets(limit, offset int) (int, int) {
	offset = floorDivision(offset, limit)
	nextOffset := floorDivision(offset+limit, limit)
	prevOffset := floorDivision(offset-limit, limit)
	return prevOffset, nextOffset
}

func floorDivision(dividend, divisor int) int {
	quotient := int(math.Max(float64(dividend), 0)) / divisor

	return quotient * divisor
}

func queryURL(req *http.Request) (int, int, int, string, error) {
	var limit int = 12
	var limitOverride int
	var offset int
	var offsetOverride int
	var year int
	var yearOverride int
	var tag string
	var err error

	q := req.URL.Query()

	if reqTags, ok := q["tag"]; ok {
		tag = reqTags[0]
	}

	if reqYear, ok := q["year"]; ok {
		yearOverride, err = strconv.Atoi(reqYear[0])
		if err != nil {
			return limit, offset, year, tag, err
		}
		year = yearOverride
	}

	if reqLimit, ok := q["limit"]; ok {
		limitOverride, err = strconv.Atoi(reqLimit[0])
		if err != nil {
			return limit, offset, year, tag, err
		}
		limit = roundToNearestLimit(limitOverride)
	}

	if reqOffset, ok := q["offset"]; ok {
		offsetOverride, err = strconv.Atoi(reqOffset[0])
		if err != nil {
			return limit, offset, year, tag, err
		}
		offset = floorDivision(offsetOverride, limitOverride)
	}

	return limit, offset, year, tag, nil
}

func roundToNearestLimit(n int) int {
	// Create an array of the possible rounding values
	roundingValues := []int{12, 24, 48}

	// Initialize the result as the first rounding value
	result := roundingValues[0]

	// Find the nearest rounding value
	for _, val := range roundingValues {
		if abs(val-n) < abs(result-n) {
			result = val
		}
	}

	return result
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
