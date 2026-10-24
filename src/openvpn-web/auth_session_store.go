package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gorilla/securecookie"
	gorillasessions "github.com/gorilla/sessions"
	gormstore "github.com/wader/gormstore/v2"
	"gorm.io/gorm"
)

// Keep the existing gormstore schema and signed-cookie format, but own the
// persistence path: gormstore treats database read errors as missing sessions.
// Serialize codec/options access without serializing whole HTTP requests.
type synchronizedSessionStore struct {
	mu       sync.Mutex
	inner    *gormstore.Store
	database *gorm.DB
}

type storedSession struct {
	ID        string `gorm:"primaryKey"`
	Data      string
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time
}

// A failed load must remain failed even if the next database operation recovers.
// gin's session adapter logs New errors but still exposes the returned session.
type sessionLoadError struct {
	store *synchronizedSessionStore
	name  string
}

func newSessionStore(database *gorm.DB) sessions.Store {
	return &synchronizedSessionStore{inner: gormstore.New(database, []byte(secretKey)), database: database}
}
func (s *synchronizedSessionStore) Get(r *http.Request, name string) (*gorillasessions.Session, error) {
	return gorillasessions.GetRegistry(r).Get(s, name)
}
func (s *synchronizedSessionStore) New(r *http.Request, name string) (*gorillasessions.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh := gorillasessions.NewSession(s, name)
	options := *s.inner.SessionOpts
	fresh.Options = &options
	fresh.IsNew = true
	cookie, err := r.Cookie(name)
	if err != nil {
		return fresh, nil
	}
	var id string
	if err = securecookie.DecodeMulti(name, cookie.Value, &id, s.inner.Codecs...); err != nil || id == "" {
		return fresh, nil
	}
	var row storedSession
	err = s.database.WithContext(r.Context()).Table("sessions").Where("id = ? AND expires_at > ?", id, time.Now()).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fresh, nil
	}
	if err != nil {
		*r = *r.WithContext(context.WithValue(r.Context(), sessionLoadError{s, name}, err))
		return fresh, err
	}
	if err = securecookie.DecodeMulti(name, row.Data, &fresh.Values, s.inner.Codecs...); err != nil {
		fresh.Values = make(map[interface{}]interface{})
		*r = *r.WithContext(context.WithValue(r.Context(), sessionLoadError{s, name}, err))
		return fresh, err
	}
	fresh.ID, fresh.IsNew = id, false
	return fresh, nil
}
func (s *synchronizedSessionStore) Save(r *http.Request, w http.ResponseWriter, session *gorillasessions.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := r.Context().Value(sessionLoadError{s, session.Name()}).(error); ok {
		return err
	}
	database := s.database.WithContext(r.Context())
	if session.Options.MaxAge < 0 {
		if session.ID != "" {
			// Do not clear the ID or expire the browser cookie until deletion and its
			// verification have committed. Missing rows are idempotent, read errors not.
			err := database.Transaction(func(tx *gorm.DB) error {
				var row storedSession
				err := tx.Table("sessions").Where("id = ?", session.ID).Take(&row).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				if err != nil {
					return err
				}
				if err = tx.Table("sessions").Where("id = ?", session.ID).Delete(&storedSession{}).Error; err != nil {
					return err
				}
				var count int64
				if err = tx.Table("sessions").Where("id = ?", session.ID).Count(&count).Error; err != nil {
					return err
				}
				if count != 0 {
					return errors.New("session deletion not completed")
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		session.ID, session.IsNew = "", true
		http.SetCookie(w, gorillasessions.NewCookie(session.Name(), "", session.Options))
		return nil
	}
	data, err := securecookie.EncodeMulti(session.Name(), session.Values, s.inner.Codecs...)
	if err != nil {
		return err
	}
	id := session.ID
	if id == "" {
		id = secureToken()
	}
	cookie, err := securecookie.EncodeMulti(session.Name(), id, s.inner.Codecs...)
	if err != nil {
		return err
	}
	now := time.Now()
	row := storedSession{ID: id, Data: data, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Duration(session.Options.MaxAge) * time.Second)}
	if session.ID == "" {
		err = database.Table("sessions").Create(&row).Error
	} else {
		// Never upsert a disappeared/revoked session or mint a new one after a
		// failed lookup. Only an explicitly new (or successfully deleted) session
		// can create a backing row.
		err = database.Transaction(func(tx *gorm.DB) error {
			var existing storedSession
			if err := tx.Table("sessions").Where("id = ?", id).Take(&existing).Error; err != nil {
				return err
			}
			result := tx.Table("sessions").Where("id = ?", id).Updates(map[string]interface{}{"data": data, "updated_at": now, "expires_at": row.ExpiresAt})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("session update not completed")
			}
			return nil
		})
	}
	if err != nil {
		return err
	}
	session.ID, session.IsNew = id, false
	http.SetCookie(w, gorillasessions.NewCookie(session.Name(), cookie, session.Options))
	return nil
}
func (s *synchronizedSessionStore) Options(options sessions.Options) {
	s.mu.Lock()
	defer s.mu.Unlock()
	converted := options.ToGorillaOptions()
	s.inner.SessionOpts = converted
}
