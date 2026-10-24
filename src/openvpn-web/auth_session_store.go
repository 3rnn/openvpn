package main

import (
	"net/http"
	"sync"

	"github.com/gin-contrib/sessions"
	gormsessions "github.com/gin-contrib/sessions/gorm"
	gorillasessions "github.com/gorilla/sessions"
	"gorm.io/gorm"
)

// gormstore.New mutates shared cookie-codec MaxAge on every read. Protect the
// backend's read/write operations without serializing whole HTTP requests.
// Rebind returned sessions to this wrapper: merely locking Get does not protect
// Session.Save, which otherwise invokes the original, unsynchronized store.
type synchronizedSessionStore struct {
	mu    sync.Mutex
	inner sessions.Store
}

func newSessionStore(database *gorm.DB) sessions.Store {
	return &synchronizedSessionStore{inner: gormsessions.NewStore(database, false, []byte(secretKey))}
}
func (s *synchronizedSessionStore) Get(r *http.Request, name string) (*gorillasessions.Session, error) {
	return gorillasessions.GetRegistry(r).Get(s, name)
}
func (s *synchronizedSessionStore) New(r *http.Request, name string) (*gorillasessions.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.inner.New(r, name)
	if old == nil {
		return nil, err
	}
	fresh := gorillasessions.NewSession(s, name)
	fresh.ID = old.ID
	fresh.Values = old.Values
	fresh.Options = old.Options
	fresh.IsNew = old.IsNew
	return fresh, err
}
func (s *synchronizedSessionStore) Save(r *http.Request, w http.ResponseWriter, session *gorillasessions.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Save(r, w, session)
}
func (s *synchronizedSessionStore) Options(options sessions.Options) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.Options(options)
}
