package session

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	bbolt "go.etcd.io/bbolt"
)

var (
	oidcLogoutBucket       = []byte("oidc_backchannel_logout")
	oidcLogoutReplayBucket = []byte("oidc_backchannel_logout_replay")
)

var ErrLogoutReplay = errors.New("OIDC logout token has already been used")

type OIDCLogout struct {
	Issuer    string
	Subject   string
	SessionID string
	TokenID   string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type RevocationStore struct {
	db *bbolt.DB
}

func NewRevocationStore(db *bbolt.DB) (*RevocationStore, error) {
	if db == nil {
		return nil, errors.New("OIDC logout database is nil")
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(oidcLogoutBucket); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(oidcLogoutReplayBucket)
		return err
	}); err != nil {
		return nil, fmt.Errorf("create OIDC logout bucket: %w", err)
	}
	return &RevocationStore{db: db}, nil
}

// Apply records the logout token ID for replay protection and the cutoff for
// the affected IdP subject or session. Subject cutoffs expire after app tokens
// derived from older IdP assertions can no longer be valid. Session IDs remain
// revoked permanently because an IdP session identifier must not be reused.
func (s *RevocationStore) Apply(logout OIDCLogout) error {
	if s == nil || s.db == nil {
		return errors.New("OIDC logout store is unavailable")
	}
	if logout.Issuer == "" || logout.TokenID == "" || logout.IssuedAt.IsZero() || logout.ExpiresAt.IsZero() ||
		(logout.Subject == "" && logout.SessionID == "") {
		return errors.New("incomplete OIDC logout event")
	}

	now := time.Now().Unix()
	issuedAt := logout.IssuedAt.Unix()
	retainUntil := time.Now().Add(Lifetime + 5*time.Minute).Unix()
	tokenExpiresAt := logout.ExpiresAt.Unix()

	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(oidcLogoutBucket)
		replayBucket := tx.Bucket(oidcLogoutReplayBucket)
		if bucket == nil || replayBucket == nil {
			return errors.New("OIDC logout bucket is unavailable")
		}

		if err := pruneExpiredLogoutTokens(replayBucket, now); err != nil {
			return err
		}
		replayID := revocationKey("id", logout.Issuer, logout.TokenID)
		if replayBucket.Get(replayID) != nil {
			return ErrLogoutReplay
		}
		replayExpiryKey := logoutReplayExpiryKey(tokenExpiresAt, logout.Issuer, logout.TokenID)
		if err := replayBucket.Put(replayID, encodeRevocation(0, tokenExpiresAt)); err != nil {
			return err
		}
		if err := replayBucket.Put(replayExpiryKey, replayID); err != nil {
			return err
		}

		if logout.SessionID == "" && logout.Subject != "" {
			if err := updateRevocation(bucket, revocationKey("sub", logout.Issuer, logout.Subject), issuedAt, retainUntil); err != nil {
				return err
			}
		}
		if logout.SessionID != "" {
			// A sid identifies one IdP browser session. Keep this tombstone so an
			// old refresh token cannot mint another app session for the same sid.
			if err := updateRevocation(bucket, revocationKey("sid", logout.Issuer, logout.SessionID), int64(^uint64(0)>>1), int64(^uint64(0)>>1)); err != nil {
				return err
			}
		}
		return nil
	})
}

func pruneExpiredLogoutTokens(bucket *bbolt.Bucket, now int64) error {
	cursor := bucket.Cursor()
	for key, idKey := cursor.First(); key != nil; key, idKey = cursor.Next() {
		if !strings.HasPrefix(string(key), "exp:") {
			continue
		}
		_, expiresAt, err := decodeRevocation(bucket.Get(idKey))
		if err != nil {
			return err
		}
		if expiresAt > now {
			// Expiry keys sort by time, so later entries are also still valid.
			return nil
		}
		if err := bucket.Delete(idKey); err != nil {
			return err
		}
		if err := cursor.Delete(); err != nil {
			return err
		}
	}
	return nil
}

func (s *RevocationStore) IsRevoked(principal Principal) (bool, error) {
	if s == nil || s.db == nil {
		return false, errors.New("OIDC logout store is unavailable")
	}
	if principal.AuthSource != AuthSourceOIDC || principal.IDPIssuer == "" || principal.IDPSubject == "" || principal.IDPTokenIAT <= 0 {
		return false, errors.New("invalid OIDC session identity")
	}
	var revoked bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(oidcLogoutBucket)
		if bucket == nil {
			return errors.New("OIDC logout bucket is unavailable")
		}
		cutoff, expiresAt, err := decodeRevocation(bucket.Get(revocationKey("sub", principal.IDPIssuer, principal.IDPSubject)))
		if err != nil {
			return err
		}
		if expiresAt > time.Now().Unix() && principal.IDPTokenIAT <= cutoff {
			revoked = true
			return nil
		}
		if principal.IDPSessionID != "" {
			_, expiresAt, err := decodeRevocation(bucket.Get(revocationKey("sid", principal.IDPIssuer, principal.IDPSessionID)))
			if err != nil {
				return err
			}
			if expiresAt > time.Now().Unix() {
				revoked = true
			}
		}
		return nil
	})
	return revoked, err
}

func updateRevocation(bucket *bbolt.Bucket, key []byte, cutoff, expiresAt int64) error {
	oldCutoff, oldExpiry, err := decodeRevocation(bucket.Get(key))
	if err != nil {
		return err
	}
	if cutoff > oldCutoff {
		oldCutoff = cutoff
	}
	if expiresAt > oldExpiry {
		oldExpiry = expiresAt
	}
	return bucket.Put(key, encodeRevocation(oldCutoff, oldExpiry))
}

func revocationKey(kind, issuer, identifier string) []byte {
	identity := sha256.Sum256([]byte(issuer + "\x00" + identifier))
	return []byte(kind + ":" + hex.EncodeToString(identity[:]))
}

func logoutReplayExpiryKey(expiresAt int64, issuer, tokenID string) []byte {
	identity := sha256.Sum256([]byte(issuer + "\x00" + tokenID))
	return []byte(fmt.Sprintf("exp:%016x:%s", expiresAt, hex.EncodeToString(identity[:])))
}

func encodeRevocation(cutoff, expiresAt int64) []byte {
	value := make([]byte, 16)
	binary.BigEndian.PutUint64(value[:8], uint64(cutoff))
	binary.BigEndian.PutUint64(value[8:], uint64(expiresAt))
	return value
}

func decodeRevocation(value []byte) (cutoff, expiresAt int64, err error) {
	if len(value) == 0 {
		return 0, 0, nil
	}
	if len(value) != 16 {
		return 0, 0, errors.New("invalid OIDC logout record")
	}
	return int64(binary.BigEndian.Uint64(value[:8])), int64(binary.BigEndian.Uint64(value[8:])), nil
}
