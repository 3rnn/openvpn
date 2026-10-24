package main

import (
	"errors"
	"github.com/pquerna/otp/totp"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestSessionTransitionsFailClosedOnDatastoreErrors(t *testing.T) {
	for _, transition := range []string{"logout", "password-to-MFA", "login-rotation", "MFA-completion"} {
		for _, fault := range []string{"read", "delete", "update", "create"} {
			if (transition == "logout" && (fault == "update" || fault == "create")) || (transition == "MFA-completion" && fault == "update") {
				continue
			}
			t.Run(transition+"/"+fault, func(t *testing.T) {
				authTestDB(t)
				seed := ""
				if transition == "password-to-MFA" || transition == "MFA-completion" {
					seed = "JBSWY3DPEHPK3PXP"
				}
				u := newAuthUser(t, "alice", seed)
				r := authRouter(t)
				b := newBrowser(r)
				b.csrf(t)
				if transition == "MFA-completion" {
					if w := b.request("POST", "/login", url.Values{"username": {"alice"}, "password": {"LongPassword!123"}}); w.Code != 200 {
						t.Fatal(w.Body)
					}
				}
				oldCookie := *b.cookies["user_session"]
				sentinel := errors.New("injected session datastore failure")
				reads := 0
				fail := func(tx *gorm.DB) {
					if tx.Statement.Table != "sessions" {
						return
					}
					if fault == "read" {
						reads++
						target := 3
						if transition == "logout" {
							target = 2
						}
						if reads != target {
							return
						}
					}
					tx.AddError(sentinel)
				}
				switch fault {
				case "read":
					db.Callback().Query().Before("gorm:query").Register("test:session-failure", fail)
					defer db.Callback().Query().Remove("test:session-failure")
				case "delete":
					db.Callback().Delete().Before("gorm:delete").Register("test:session-failure", fail)
					defer db.Callback().Delete().Remove("test:session-failure")
				case "update":
					db.Callback().Update().Before("gorm:update").Register("test:session-failure", fail)
					defer db.Callback().Update().Remove("test:session-failure")
				case "create":
					db.Callback().Create().Before("gorm:create").Register("test:session-failure", fail)
					defer db.Callback().Create().Remove("test:session-failure")
				}
				path := "/login"
				values := url.Values{"username": {"alice"}, "password": {"LongPassword!123"}}
				if transition == "MFA-completion" {
					code, err := totp.GenerateCode(u.MfaSecret, time.Now())
					if err != nil {
						t.Fatal(err)
					}
					values = url.Values{"passcode": {code}}
				}
				if transition == "logout" {
					path = "/logout"
					values = nil
				}
				w := b.request("POST", path, values)
				if w.Code != 500 {
					t.Fatalf("datastore %s failure reported success: %d %s", fault, w.Code, w.Body)
				}
				for _, cookie := range w.Result().Cookies() {
					if cookie.MaxAge >= 0 && cookie.Value != oldCookie.Value {
						t.Fatal("minted new session after datastore failure")
					}
				}
			})
		}
	}
}

func TestSessionDeletionReadErrorPreservesID(t *testing.T) {
	authTestDB(t)
	store := newSessionStore(db)
	req := httptest.NewRequest("GET", "/", nil)
	s, err := store.New(req, "user_session")
	if err != nil {
		t.Fatal(err)
	}
	s.Options.MaxAge = 3600
	w := httptest.NewRecorder()
	if err = store.Save(req, w, s); err != nil {
		t.Fatal(err)
	}
	oldID := s.ID
	req = httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(w.Result().Cookies()[0])
	s, err = store.New(req, "user_session")
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("injected read error")
	db.Callback().Query().Before("gorm:query").Register("test:read", func(tx *gorm.DB) {
		if tx.Statement.Table == "sessions" {
			tx.AddError(sentinel)
		}
	})
	s.Options.MaxAge = -1
	w = httptest.NewRecorder()
	err = store.Save(req, w, s)
	db.Callback().Query().Remove("test:read")
	if !errors.Is(err, sentinel) {
		t.Fatalf("lost read error: %v", err)
	}
	if s.ID != oldID || len(w.Result().Cookies()) != 0 {
		t.Fatal("failed deletion completed session invalidation")
	}
	var count int64
	if err := db.Table("sessions").Where("id = ?", oldID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("old row: %d %v", count, err)
	}
}

func TestSessionDeletionVerificationFailureRollsBack(t *testing.T) {
	authTestDB(t)
	store := newSessionStore(db)
	req := httptest.NewRequest("GET", "/", nil)
	s, _ := store.New(req, "user_session")
	s.Options.MaxAge = 3600
	if err := store.Save(req, httptest.NewRecorder(), s); err != nil {
		t.Fatal(err)
	}
	oldID := s.ID
	sentinel := errors.New("verification unavailable")
	reads := 0
	db.Callback().Query().Before("gorm:query").Register("test:verify", func(tx *gorm.DB) {
		if tx.Statement.Table == "sessions" {
			reads++
			if reads == 2 {
				tx.AddError(sentinel)
			}
		}
	})
	s.Options.MaxAge = -1
	w := httptest.NewRecorder()
	err := store.Save(req, w, s)
	db.Callback().Query().Remove("test:verify")
	if !errors.Is(err, sentinel) || s.ID != oldID || len(w.Result().Cookies()) != 0 {
		t.Fatalf("verification failure completed deletion: %v", err)
	}
	var count int64
	if err := db.Table("sessions").Where("id = ?", oldID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("deletion not rolled back: %d %v", count, err)
	}
	if err := store.Save(req, w, s); err != nil {
		t.Fatal(err)
	}
	if s.ID != "" {
		t.Fatal("successful deletion retained old ID")
	}
	s.Options.MaxAge = 3600
	if err := store.Save(req, httptest.NewRecorder(), s); err != nil {
		t.Fatal(err)
	}
	if s.ID == oldID || s.ID == "" {
		t.Fatal("rotation did not mint a fresh ID")
	}
}

func TestMissingSessionDeletionIsIdempotentButUpdateCannotResurrect(t *testing.T) {
	authTestDB(t)
	store := newSessionStore(db)
	req := httptest.NewRequest("GET", "/", nil)
	s, _ := store.New(req, "user_session")
	s.Options.MaxAge = 3600
	if err := store.Save(req, httptest.NewRecorder(), s); err != nil {
		t.Fatal(err)
	}
	id := s.ID
	if err := db.Table("sessions").Where("id = ?", id).Delete(&storedSession{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Save(req, httptest.NewRecorder(), s); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("revoked session resurrected: %v", err)
	}
	if s.ID != id {
		t.Fatal("failed update changed ID")
	}
	s.Options.MaxAge = -1
	if err := store.Save(req, httptest.NewRecorder(), s); err != nil {
		t.Fatal(err)
	}
	if s.ID != "" {
		t.Fatal("missing session deletion not completed")
	}
}

func TestSessionLoadFailureCannotBeClearedIntoNewSession(t *testing.T) {
	authTestDB(t)
	store := newSessionStore(db)
	req := httptest.NewRequest("GET", "/", nil)
	s, _ := store.New(req, "user_session")
	s.Options.MaxAge = 3600
	w := httptest.NewRecorder()
	if err := store.Save(req, w, s); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("GET", "/", nil)
	req.AddCookie(w.Result().Cookies()[0])
	sentinel := errors.New("load unavailable")
	db.Callback().Query().Before("gorm:query").Register("test:load", func(tx *gorm.DB) { tx.AddError(sentinel) })
	s, err := store.Get(req, "user_session")
	db.Callback().Query().Remove("test:load")
	if !errors.Is(err, sentinel) {
		t.Fatalf("load error: %v", err)
	}
	// Equivalent to gin's Clear: application values cannot erase the load error.
	for k := range s.Values {
		delete(s.Values, k)
	}
	for _, age := range []int{-1, 3600} {
		s.Options.MaxAge = age
		w = httptest.NewRecorder()
		if err := store.Save(req, w, s); !errors.Is(err, sentinel) {
			t.Fatalf("load error lost after clear: %v", err)
		}
		if s.ID != "" || len(w.Result().Cookies()) != 0 {
			t.Fatal("load failure minted/deleted session")
		}
	}
}
