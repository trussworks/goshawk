package dummy

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// returns username
func newTestUserName(t *testing.T, db *sqlx.DB) string {

	randID, err := rand.Int(rand.Reader, big.NewInt(100000))
	if err != nil {
		t.Fatal(err)
	}

	id := uuid.New()
	username := "dummy" + randID.String()

	createQuery := `INSERT INTO users VALUES ($1, $2)`

	_, err = db.Exec(createQuery, id, username)
	if err != nil {
		t.Fatal(err)
	}

	return username
}

func TestFlow(t *testing.T) {

	connStr := dbURLFromEnv()
	db, err := sqlx.Open("postgres", connStr)
	if err != nil {
		t.Fatal(err)
	}

	testUsername := newTestUserName(t, db)

	testServer := httptest.NewServer(setupMux(db))
	defer testServer.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}

	// We shouldn't be able to hit the protected URL while logged in.
	blockedR, err := client.Get(testServer.URL + "/protected")
	if err != nil {
		t.Fatal(err)
	}

	if blockedR.StatusCode != http.StatusUnauthorized {
		t.Fatal("first request should have failed!")
	}

	// Login
	loginResp, err := client.Post(testServer.URL+"/login", "http/txt", strings.NewReader(testUsername))
	if err != nil {
		t.Fatal(err)
	}

	if loginResp.StatusCode != 201 {
		t.Fatal("LoginFailed")
	}

	// Make the protected request again
	allowedR, err := client.Get(testServer.URL + "/protected")
	if err != nil {
		t.Fatal(err)
	}

	if allowedR.StatusCode != 200 {
		t.Fatal("second request should have succeeded!")
	}

	// logout
	logoutResp, err := client.Post(testServer.URL+"/logout", "http/txt", nil)
	if err != nil {
		t.Fatal(err)
	}

	if logoutResp.StatusCode != 204 {
		t.Fatal("Logout Failed.", logoutResp.StatusCode)
	}

	// // Make the protected request a third time, it again should be rejected.
	blockedAgainResp, err := client.Get(testServer.URL + "/protected")
	if err != nil {
		t.Fatal(err)
	}

	if blockedAgainResp.StatusCode != http.StatusUnauthorized {
		t.Fatal("Final request should have failed!")
	}

}
