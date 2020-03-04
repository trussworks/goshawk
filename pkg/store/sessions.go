package simplestore

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/us-dod-saber/culper/api"
)

// CreateSession creates a new session. It errors if a valid session already exists.
func (s SimpleStore) CreateSession(accountID int, sessionKey string, sessionIndex sql.NullString, expirationDuration time.Duration) error {
	expirationDate := time.Now().UTC().Add(expirationDuration)

	createQuery := `INSERT INTO sessions (session_key, account_id, session_index, expiration_date)
		VALUES ($1, $2, $3, $4)`

	_, createErr := s.db.Exec(createQuery, sessionKey, accountID, sessionIndex, expirationDate)
	if createErr != nil {

		return fmt.Errorf("Unexpectedly failed to create a session: %w", createErr)
	}

	return nil
}

// FetchPossiblyExpiredSession returns a session row by account ID regardless of wether it is expired
// This is potentially dangerous, it is only intended to be used during the new login flow, never to check
// on a valid session for authentication purposes.
func (s SimpleStore) FetchPossiblyExpiredSession(accountID int) (api.Session, error) {
	fetchQuery := `SELECT * FROM sessions WHERE account_id = $1`

	session := api.Session{}
	selectErr := s.db.Get(&session, fetchQuery, accountID)
	if selectErr != nil {
		if selectErr == sql.ErrNoRows {
			return api.Session{}, sql.ErrNoRows
		}
		return api.Session{}, fmt.Errorf("Failed to fetch a session row: %w", selectErr)
	}

	return session, nil

}

// DeleteSession removes a session record from the db
func (s SimpleStore) DeleteSession(sessionKey string) error {
	deleteQuery := "DELETE FROM sessions WHERE session_key = $1"

	sqlResult, deleteErr := s.db.Exec(deleteQuery, sessionKey)
	if deleteErr != nil {
		return fmt.Errorf("Failed to delete session: %w", deleteErr)
	}

	rowsAffected, _ := sqlResult.RowsAffected()
	if rowsAffected == 0 {
		return api.ErrValidSessionNotFound
	}

	return nil
}

type sessionAccountRow struct {
	api.Session
	api.Account
}

// ExtendAndFetchSessionAccount fetches an account and session data from the db
// On success it returns the account and the session
// On failure, it can return ErrValidSessionNotFound, ErrSessionExpired, or an unexpected error
func (s SimpleStore) ExtendAndFetchSessionAccount(sessionKey string, expirationDuration time.Duration) (api.Account, api.Session, error) {

	expirationDate := time.Now().UTC().Add(expirationDuration)

	// We update the session expiration date to be $DURATION from now and fetch the account and the session.
	fetchQuery := `UPDATE sessions
					SET expiration_date = $1
				FROM accounts
				WHERE
					sessions.account_id = accounts.id
					AND sessions.session_key = $2
					AND sessions.expiration_date > $3
				RETURNING
					sessions.session_key, sessions.account_id, sessions.expiration_date, sessions.session_index,
					accounts.id, accounts.form_version, accounts.form_type, accounts.username,
					accounts.email, accounts.external_id, accounts.status`

	row := sessionAccountRow{}
	selectErr := s.db.Get(&row, fetchQuery, expirationDate, sessionKey, time.Now().UTC())
	if selectErr != nil {
		if selectErr != sql.ErrNoRows {
			return api.Account{}, api.Session{}, fmt.Errorf("Unexpected error looking for valid session: %w", selectErr)
		}

		// If the above query returns no rows, either the session is expired, or it does not exist.
		// To determine which and return an appropriate error, we do a second query to see if it exists
		existsQuery := `SELECT sessions.* FROM sessions, accounts WHERE sessions.account_id = accounts.id AND sessions.session_key = $1`

		session := api.Session{}
		selectAgainErr := s.db.Get(&session, existsQuery, sessionKey)
		if selectAgainErr != nil {
			if selectAgainErr == sql.ErrNoRows {
				return api.Account{}, api.Session{}, api.ErrValidSessionNotFound
			}
			return api.Account{}, api.Session{}, fmt.Errorf("Unexpected error fetching single invalid session: %w", selectAgainErr)
		}

		// quick sanity check:
		if session.ExpirationDate.After(time.Now()) {
			errors.New(fmt.Sprintf("For some reason, this session we could not find was not actually expired: %s", session.SessionKey))
		}
		// The session must have been expired, not deleted.
		return api.Account{}, api.Session{}, api.ErrSessionExpired
	}

	// time.Times come back from the db with no tz info, so let's set it to UTC to be safe and consistent.
	row.Session.ExpirationDate = row.Session.ExpirationDate.UTC()

	return row.Account, row.Session, nil
}