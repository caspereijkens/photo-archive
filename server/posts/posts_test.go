package posts

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"project/server/config"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func TestGetPost(t *testing.T) {
	createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	for i := 0; i <= 3; i++ {
		imageName := fmt.Sprintf("image-%d", i)
		CreatePost(imageName, 2022, 1)
		post, _ := GetPost(i + 1)
		if post.ImageURL != imageName {
			t.Errorf("Test failed because title '%s' was expected.", imageName)
		}
	}
}

func TestUpdatePost(t *testing.T) {
	createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	CreatePost("image", 2022, 1)
	post, _ := GetPost(1)
	postCreatedAt := post.CreatedAt
	for i := 0; i <= 5; i++ {
		imageName := fmt.Sprintf("image-%d", i)
		post.ImageURL = imageName
		UpdatePost(post)
		updatedPost, _ := GetPost(1)
		postUpdatedAt := updatedPost.UpdatedAt
		if !postUpdatedAt.After(postCreatedAt) {
			t.Error("Post was not updated.")
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
		posts, first, last, err := ListPosts(12, 0, year, "")
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
		postId, err := CreatePost(imageName, 2022, 1)
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
		posts, _, _, _ := ListPosts(12, 0, year, tagName)
		if len(posts) != (testCount + 1 - i) {
			t.Error("Test failed because tag filter is not working properly.")
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
		postId, err := CreatePost(c.Image, c.Year, c.UserId)
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

func TestListLastPostPerTag(t *testing.T) {
	createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	userId := 1
	tags := []string{"tag1", "tag1", "tag2", "tag2", "tag3", "tag3", "tag4", "tag4"}
	defer config.DB.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")
	for i, tag := range tags {
		postId, _ := CreatePost(fmt.Sprintf("image-%d", i), 2022, userId)
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
		CreatePost("image", 2022, 1)
		DeletePost(c.PostId, c.UserId)
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

func TestCreateTags(t *testing.T) {
	var outputTags []string
	inputTags := []string{"kermis", "lolly", "draaimolen", "roze"}
	createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	postId, _ := CreatePost("image", 2017, 1)
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

func TestListYears(t *testing.T) {
	createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")
	tag1 := []string{"tag1"}
	tag2 := []string{"tag2"}
	tag12 := []string{"tag1", "tag2"}
	tag3 := []string{"tag3"}

	postId1, _ := CreatePost("image1", 2022, 1)
	createTags(postId1, tag1)

	postId3, _ := CreatePost("image3", 2023, 1)
	createTags(postId3, tag1)

	postId2, _ := CreatePost("image2", 1999, 1)
	createTags(postId2, tag2)

	postId4, _ := CreatePost("image4", 1995, 1)
	createTags(postId4, tag2)

	postId5, _ := CreatePost("image5", 2015, 1)
	createTags(postId5, tag12)

	postId6, _ := CreatePost("image6", 2000, 1)
	createTags(postId6, tag12)

	postId7, _ := CreatePost("image7", 2017, 1)
	createTags(postId7, tag12)

	postId8, _ := CreatePost("image8", 2011, 1)
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

func TestUpdateTags(t *testing.T) {
	createTestUser()
	defer config.DB.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE;")
	postId, _ := CreatePost("image", 2022, 1)
	createTags(postId, []string{"test-tag"})
	defer config.DB.Exec("TRUNCATE TABLE tags RESTART IDENTITY CASCADE;")
	defer config.DB.Exec("TRUNCATE TABLE tagmap RESTART IDENTITY CASCADE;")
	inputTags := []string{"edited-test-tag", "extra-test-tag"}
	err := UpdateTags(postId, inputTags)
	if err != nil {
		t.Errorf("Error updating post: %v", err)
	}
	post, _ := GetPost(*postId)
	for _, tag := range inputTags {
		if !contains(post.Tags, tag) {
			t.Error("Tags of post were not properly updated.")
		}
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
