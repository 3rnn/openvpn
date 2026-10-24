package main

import (
	"encoding/json"
	"fmt"
	"github.com/gavintan/gopkg/aes"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/pquerna/otp/totp"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	gLogger "gorm.io/gorm/logger"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func authTestDB(t *testing.T) {
	resetLoginFail("192.0.2.1")
	if secretKey == "" {
		secretKey = "0123456789abcdef0123456789abcdef"
	}
	if adminUsername == "" {
		adminUsername = "admin"
	}
	t.Helper()
	old := db
	oldLDAP := ldapAuth
	ldapAuth = false
	var err error
	db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gLogger.Default.LogMode(gLogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err = db.AutoMigrate(&User{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s, _ := db.DB(); s.Close(); db = old; ldapAuth = oldLDAP })
}
func TestPasswordSingleHashAndLegacyMigration(t *testing.T) {
	authTestDB(t)
	u := User{Username: "alice", Password: "LongPassword!123"}
	if err := u.Create(); err != nil {
		t.Fatal(err)
	}
	var saved User
	if err := db.First(&saved, u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(saved.Password), []byte("LongPassword!123")) != nil {
		t.Fatal("password is not a bcrypt hash")
	}
	before := saved.Password
	if err := db.Model(&saved).Update("name", "Alice").Error; err != nil {
		t.Fatal(err)
	}
	db.First(&saved, u.ID)
	if saved.Password != before {
		t.Fatal("unrelated update rehashed password")
	}
	legacy, err := aes.AesEncrypt("OldPassword!123", secretKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&User{}).Where("id = ?", u.ID).UpdateColumn("password", legacy).Error; err != nil {
		t.Fatal(err)
	}
	login := User{Username: "alice", Password: "OldPassword!123"}
	if err = login.Login(false); err != nil {
		t.Fatal(err)
	}
	db.First(&saved, u.ID)
	if bcrypt.CompareHashAndPassword([]byte(saved.Password), []byte("OldPassword!123")) != nil {
		t.Fatal("legacy password not migrated")
	}
}

func TestLoginIgnoresClientIDAndFailsClosed(t *testing.T) {
	authTestDB(t)
	u := User{Username: "alice", Password: "LongPassword!123"}
	if err := u.Create(); err != nil {
		t.Fatal(err)
	}
	login := User{ID: 9999, Username: "alice", Password: "LongPassword!123"}
	if err := login.Login(false); err != nil {
		t.Fatalf("client ID controlled lookup: %v", err)
	}
	if login.ID != u.ID {
		t.Fatal("identity not loaded from database")
	}
	if err := db.Migrator().DropTable(&User{}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("database failure panicked: %v", r)
		}
	}()
	if err := (&User{Username: "alice", Password: "LongPassword!123"}).Login(false); err == nil {
		t.Fatal("database failure accepted")
	}
}

func authRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(configRequestGuard())
	r.Use(sessions.Sessions("user_session", newSessionStore(db)))
	r.Use(CSRFMiddleware())
	registerAuthRoutes(r)
	r.GET("/test/expire", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("mfa_pending_expiry", time.Now().Add(-time.Minute).Unix())
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		c.Status(200)
	})
	r.Use(AuthMiddleWare())
	registerAccountRoutes(r)
	r.GET("/admin", func(c *gin.Context) { c.Status(200) })
	r.GET("/client/test", func(c *gin.Context) { c.Status(200) })
	return r
}

type browser struct {
	r       http.Handler
	cookies map[string]*http.Cookie
	token   string
}

func newBrowser(r http.Handler) *browser { return &browser{r: r, cookies: map[string]*http.Cookie{}} }
func (b *browser) request(method, path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range b.cookies {
		req.AddCookie(c)
	}
	if b.token != "" {
		req.Header.Set("X-CSRF-Token", b.token)
	}
	w := httptest.NewRecorder()
	b.r.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return w
}
func (b *browser) csrf(t *testing.T) {
	t.Helper()
	w := b.request("GET", "/csrf", nil)
	var obj map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &obj); err != nil {
		t.Fatal(err)
	}
	b.token = obj["csrfToken"]
	if b.token == "" {
		t.Fatal("no CSRF token")
	}
}
func newAuthUser(t *testing.T, name, seed string) User {
	t.Helper()
	first := false
	u := User{Username: name, Password: "LongPassword!123", MfaSecret: seed, IsFirstLogin: &first}
	if err := u.Create(); err != nil {
		t.Fatal(err)
	}
	return u
}
func loginBrowser(t *testing.T, b *browser, name string) *httptest.ResponseRecorder {
	t.Helper()
	b.csrf(t)
	return b.request("POST", "/login", url.Values{"username": {name}, "password": {"LongPassword!123"}})
}
func TestCSRFLoginLogoutAndServiceBoundary(t *testing.T) {
	authTestDB(t)
	r := authRouter(t)
	b := newBrowser(r)
	if w := b.request("POST", "/login", nil); w.Code != 403 {
		t.Fatalf("login without CSRF status %d", w.Code)
	}
	b.csrf(t)
	if w := b.request("POST", "/logout", nil); w.Code != 200 {
		t.Fatalf("logout with CSRF: %d %s", w.Code, w.Body)
	}
	if w := b.request("GET", "/logout", nil); w.Code != 405 {
		t.Fatalf("GET logout mutates: %d", w.Code)
	}
}
func TestMFAPendingBrowserIdentityExpiryRetriesAndDBFailure(t *testing.T) {
	authTestDB(t)
	a := newAuthUser(t, "alice", "JBSWY3DPEHPK3PXP")
	newAuthUser(t, "bob", "GEZDGNBVGY3TQOJQ")
	r := authRouter(t)
	alice, bob, other := newBrowser(r), newBrowser(r), newBrowser(r)
	if w := loginBrowser(t, alice, "alice"); w.Code != 200 || !strings.Contains(w.Body.String(), "mfaPending") {
		t.Fatalf("no pending: %d %s", w.Code, w.Body)
	}
	loginBrowser(t, bob, "bob")
	other.csrf(t)
	code, _ := totp.GenerateCode(a.MfaSecret, time.Now())
	spoof := url.Values{"passcode": {code}, "username": {"alice"}, "id": {fmt.Sprint(a.ID)}, "mfaSecret": {a.MfaSecret}}
	if w := other.request("POST", "/login", spoof); w.Code != 401 {
		t.Fatal("other browser reused pending")
	}
	if w := bob.request("POST", "/login", spoof); w.Code != 401 {
		t.Fatal("bob completed alice pending")
	}
	if w := alice.request("POST", "/login", url.Values{"passcode": {code}, "username": {"bob"}, "id": {"9999"}, "mfaSecret": {"fake"}}); w.Code != 200 || !strings.Contains(w.Body.String(), "登录成功") {
		t.Fatalf("server identity not authoritative: %d %s", w.Code, w.Body)
	}
	if w := alice.request("POST", "/login", spoof); w.Code != 403 {
		t.Fatal("old CSRF token survived authentication rotation")
	}
	alice.csrf(t)
	if w := alice.request("GET", "/client/test", nil); w.Code != 200 {
		t.Fatalf("authenticated user denied %d", w.Code)
	}
	fail := newBrowser(r)
	loginBrowser(t, fail, "alice")
	for i := 0; i < 5; i++ {
		fail.request("POST", "/login", url.Values{"passcode": {"invalid"}})
	}
	if w := fail.request("POST", "/login", spoof); w.Code != 401 && w.Code != 403 {
		t.Fatal("retry limit not enforced")
	}
	resetLoginFail("192.0.2.1")
	pending := newBrowser(r)
	loginBrowser(t, pending, "alice")
	if err := db.Migrator().DropTable(&User{}); err != nil {
		t.Fatal(err)
	}
	if w := pending.request("POST", "/login", spoof); w.Code != 401 {
		t.Fatalf("DB failure status %d", w.Code)
	}
}

func TestSessionRevocationAndRedactedRoutes(t *testing.T) {
	for _, change := range []string{"password", "is_enable", "mfa_secret", "gid", "username", "deleted"} {
		t.Run(change, func(t *testing.T) {
			authTestDB(t)
			u := newAuthUser(t, "alice", "")
			r := authRouter(t)
			b := newBrowser(r)
			if w := loginBrowser(t, b, "alice"); w.Code != 200 {
				t.Fatal(w.Body)
			}
			b.csrf(t)
			if w := b.request("GET", "/client/userinfo", nil); w.Code != 200 || strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "mfaSecret") {
				t.Fatalf("profile: %d %s", w.Code, w.Body)
			}
			var err error
			switch change {
			case "password":
				err = db.Model(&User{}).Where("id = ?", u.ID).Update("password", "OtherPassword!123").Error
			case "is_enable":
				err = db.Model(&User{}).Where("id = ?", u.ID).Update("is_enable", false).Error
			case "mfa_secret":
				err = db.Model(&User{}).Where("id = ?", u.ID).Update("mfa_secret", "seed").Error
			case "gid":
				err = db.Model(&User{}).Where("id = ?", u.ID).Update("gid", 2).Error
			case "username":
				err = db.Model(&User{}).Where("id = ?", u.ID).Update("username", "newalice").Error
			case "deleted":
				err = db.Delete(&User{}, u.ID).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if w := b.request("GET", "/client/test", nil); w.Code != 401 {
				t.Fatalf("stale %s session accepted: %d", change, w.Code)
			}
		})
	}
}

func TestAdministratorBootstrapAndMigrationNoReset(t *testing.T) {
	for _, provided := range []bool{false, true} {
		t.Run(fmt.Sprint(provided), func(t *testing.T) {
			dir := t.TempDir()
			v := viper.New()
			v.SetConfigFile(filepath.Join(dir, "config.json"))
			v.SetConfigType("json")
			v.Set("system.base.admin_username", "admin")
			if err := v.WriteConfig(); err != nil {
				t.Fatal(err)
			}
			if provided {
				t.Setenv("OPENVPN_UI_ADMIN_PASSWORD", "ConfiguredPassword!123")
			} else {
				t.Setenv("OPENVPN_UI_ADMIN_PASSWORD", "")
			}
			if err := initializeAdministrator(v, dir, true); err != nil {
				t.Fatal(err)
			}
			password := "ConfiguredPassword!123"
			if !provided {
				b, err := os.ReadFile(filepath.Join(dir, "bootstrap-admin-password"))
				if err != nil {
					t.Fatal(err)
				}
				password = strings.TrimSpace(string(b))
				info, err := os.Stat(filepath.Join(dir, "bootstrap-admin-password"))
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("bootstrap permissions")
				}
			}
			if password == "admin" || bcrypt.CompareHashAndPassword([]byte(v.GetString("system.base.admin_password")), []byte(password)) != nil {
				t.Fatal("insecure bootstrap")
			}
			if !v.GetBool("system.base.admin_first_login") {
				t.Fatal("initial admin not forced to change")
			}
			original := v.GetString("system.base.admin_password")
			t.Setenv("OPENVPN_UI_ADMIN_PASSWORD", "ReplacementPassword!123")
			if err := initializeAdministrator(v, dir, false); err != nil {
				t.Fatal(err)
			}
			if v.GetString("system.base.admin_password") != original {
				t.Fatal("existing credentials reset by env")
			}
		})
	}
	v := viper.New()
	if err := initializeAdministrator(v, t.TempDir(), false); err == nil {
		t.Fatal("missing migrated credential silently initialized")
	}
}

func configureTestAdmin(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(filepath.Join(t.TempDir(), "config.json"))
	viper.SetConfigType("json")
	hash, err := bcrypt.GenerateFromPassword([]byte("LongPassword!123"), 12)
	if err != nil {
		t.Fatal(err)
	}
	viper.Set("system.base.admin_username", "admin")
	viper.Set("system.base.admin_password", string(hash))
	viper.Set("system.base.admin_first_login", false)
	viper.Set("system.base.token", "service-test-token")
	viper.Set("system.base.secret_key", secretKey)
	if err = viper.WriteConfig(); err != nil {
		t.Fatal(err)
	}
}
func TestPendingMFAExpiresAndRotatesCookie(t *testing.T) {
	authTestDB(t)
	u := newAuthUser(t, "alice", "JBSWY3DPEHPK3PXP")
	r := authRouter(t)
	b := newBrowser(r)
	b.csrf(t)
	anonymous := *b.cookies["user_session"]
	w := b.request("POST", "/login", url.Values{"username": {"alice"}, "password": {"LongPassword!123"}})
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	if b.cookies["user_session"].Value == anonymous.Value {
		t.Fatal("password-stage session not rotated")
	}
	stolen := newBrowser(r)
	stolen.cookies["user_session"] = &anonymous
	stolen.token = b.token
	if w := stolen.request("POST", "/login", url.Values{"passcode": {"000000"}}); w.Code != 403 {
		t.Fatal("old anonymous session still usable")
	}
	b.request("GET", "/test/expire", nil)
	code, _ := totp.GenerateCode(u.MfaSecret, time.Now())
	if w := b.request("POST", "/login", url.Values{"passcode": {code}}); w.Code != 401 {
		t.Fatal("expired MFA pending accepted")
	}
}
func TestOwnPasswordChangeRequiresCurrentAndRevokesOtherBrowser(t *testing.T) {
	authTestDB(t)
	u := newAuthUser(t, "alice", "")
	other := newAuthUser(t, "bob", "")
	r := authRouter(t)
	a, b := newBrowser(r), newBrowser(r)
	loginBrowser(t, a, "alice")
	loginBrowser(t, b, "alice")
	a.csrf(t)
	b.csrf(t)
	if w := a.request("POST", "/client/modifyPass", url.Values{"password": {"ChangedPassword!123"}, "id": {fmt.Sprint(other.ID)}}); w.Code != 401 {
		t.Fatal("password change missing currentPass accepted")
	}
	if w := a.request("POST", "/client/modifyPass", url.Values{"password": {"ChangedPassword!123"}, "currentPass": {"LongPassword!123"}, "id": {fmt.Sprint(other.ID)}, "isFirstLogin": {"true"}}); w.Code != 200 {
		t.Fatalf("password change: %d %s", w.Code, w.Body)
	}
	var alice, bob User
	db.First(&alice, u.ID)
	db.First(&bob, other.ID)
	if !alice.verifyPassword("ChangedPassword!123") || !bob.verifyPassword("LongPassword!123") {
		t.Fatal("client ID controlled target")
	}
	if alice.IsFirstLogin == nil || *alice.IsFirstLogin {
		t.Fatal("first-login flag controlled by client")
	}
	if w := b.request("GET", "/client/test", nil); w.Code != 401 {
		t.Fatal("other session survived password change")
	}
}
func TestEnrollmentServerSecretOnlyAndNoStoredSeedDisclosure(t *testing.T) {
	authTestDB(t)
	u := newAuthUser(t, "alice", "")
	bob := newAuthUser(t, "bob", "")
	r := authRouter(t)
	b := newBrowser(r)
	loginBrowser(t, b, "alice")
	b.csrf(t)
	if w := b.request("POST", "/client/mfa/enroll", nil); w.Code != 401 {
		t.Fatal("missing currentPass accepted")
	}
	w := b.request("POST", "/client/mfa/enroll", url.Values{"currentPass": {"LongPassword!123"}})
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	seed, _ := response["mfaSecret"].(string)
	if seed == "" {
		t.Fatal("no one-time enrollment seed")
	}
	if w := b.request("GET", "/client/mfa", nil); strings.Contains(w.Body.String(), seed) || strings.Contains(w.Body.String(), "mfaSecret") {
		t.Fatal("pending seed disclosed by GET")
	}
	code, _ := totp.GenerateCode(seed, time.Now())
	w = b.request("POST", "/client/mfa", url.Values{"currentPass": {"LongPassword!123"}, "passcode": {code}, "id": {fmt.Sprint(bob.ID)}, "mfaSecret": {"client-chosen-secret"}})
	if w.Code != 200 {
		t.Fatalf("enrollment %d %s", w.Code, w.Body)
	}
	var saved User
	db.First(&saved, u.ID)
	if saved.MfaSecret != seed {
		t.Fatal("client secret persisted")
	}
	db.First(&saved, bob.ID) // explicit fresh variable below avoids composite identity
	var untouched User
	db.First(&untouched, bob.ID)
	if untouched.MfaSecret != "" {
		t.Fatal("client id selected enrollment account")
	}
	loginBrowser(t, b, "alice")
	code, _ = totp.GenerateCode(seed, time.Now())
	if w := b.request("POST", "/login", url.Values{"passcode": {code}}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	b.csrf(t)
	for _, path := range []string{"/client/mfa", "/client/userinfo"} {
		w = b.request("GET", path, nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), seed) || strings.Contains(w.Body.String(), "mfaSecret") {
			t.Fatalf("stored seed disclosed: %s", w.Body)
		}
	}
	if w := b.request("DELETE", "/client/mfa/"+fmt.Sprint(bob.ID), nil); w.Code != 401 {
		t.Fatal("disable without currentPass accepted")
	}
}
func TestRealRouterAuthorizationCSRFDisabledAndAPISecrets(t *testing.T) {
	authTestDB(t)
	configureTestAdmin(t)
	u := newAuthUser(t, "alice", "")
	if err := db.AutoMigrate(&Group{}); err != nil {
		t.Fatal(err)
	}
	viper.Set("system.ldap.ldap_bind_password", "ldap-secret-marker")
	viper.Set("system.email.password", "email-secret-marker")
	r := setupRouter(ovpn{})
	user := newBrowser(r)
	loginBrowser(t, user, "alice")
	user.csrf(t)
	for _, path := range []string{"/settings", "/ovpn/user", "/ovpn/group/1/users", "/admin"} {
		if w := user.request("GET", path, nil); w.Code != 403 {
			t.Fatalf("nonadmin %s status %d", path, w.Code)
		}
	}
	if err := db.Model(&User{}).Where("id = ?", u.ID).Update("is_enable", false).Error; err != nil {
		t.Fatal(err)
	}
	if w := user.request("GET", "/client/userinfo", nil); w.Code != 401 {
		t.Fatalf("real route disabled session %d", w.Code)
	}
	admin := newBrowser(r)
	loginBrowser(t, admin, "admin")
	admin.csrf(t)
	for _, path := range []string{"/settings", "/ovpn/user/" + fmt.Sprint(u.ID), "/ovpn/group/1/users", "/ovpn/user/export?gid=1"} {
		w := admin.request("GET", path, nil)
		if w.Code != 200 {
			t.Fatalf("API %s %d %s", path, w.Code, w.Body)
		}
		for _, secret := range []string{"LongPassword!123", "ldap-secret-marker", "email-secret-marker", "service-test-token", secretKey} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatalf("API %s leaked secret", path)
			}
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		admin.token = ""
		if w := admin.request(method, "/ovpn/user", nil); w.Code != 403 {
			t.Fatalf("mutation %s without CSRF %d", method, w.Code)
		}
	}
	admin.csrf(t)
	if w := admin.request("POST", "/settings", url.Values{"system.base.site_url": {"https://test.example"}}); w.Code != 401 {
		t.Fatal("settings missing currentPass accepted")
	}
}
func TestAdminForcedFirstPasswordChangeAndLegacyNoReset(t *testing.T) {
	authTestDB(t)
	configureTestAdmin(t)
	viper.Set("system.base.admin_first_login", true)
	legacy, err := aes.AesEncrypt("LongPassword!123", secretKey)
	if err != nil {
		t.Fatal(err)
	}
	viper.Set("system.base.admin_password", legacy)
	if err = viper.WriteConfig(); err != nil {
		t.Fatal(err)
	}
	r := authRouter(t)
	b := newBrowser(r)
	w := loginBrowser(t, b, "admin")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"isFirstLogin":true`) {
		t.Fatalf("admin login %d %s", w.Code, w.Body)
	}
	if bcrypt.CompareHashAndPassword([]byte(viper.GetString("system.base.admin_password")), []byte("LongPassword!123")) != nil {
		t.Fatal("legacy admin password reset instead of migrated")
	}
	b.csrf(t)
	if w := b.request("GET", "/client/test", nil); w.Code != 403 {
		t.Fatal("first-login admin accessed functionality")
	}
	if w := b.request("POST", "/client/modifyPass", url.Values{"currentPass": {"LongPassword!123"}, "password": {"ChangedPassword!123"}}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if viper.GetBool("system.base.admin_first_login") {
		t.Fatal("forced change remains")
	}
}

func TestPasswordUpdatesHashExactlyOncePreserveLegacyUnchanged(t *testing.T) {
	authTestDB(t)
	u := newAuthUser(t, "alice", "")
	reset := User{ID: u.ID, Password: "ResetPassword!123"}
	if err := reset.Update(); err != nil {
		t.Fatal(err)
	}
	var saved User
	if err := db.First(&saved, u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !saved.verifyPassword("ResetPassword!123") {
		t.Fatal("administrator reset not single hashed")
	}
	legacy, err := aes.AesEncrypt("LegacyPassword!123", secretKey)
	if err != nil {
		t.Fatal(err)
	}
	db.Model(&User{}).Where("id = ?", u.ID).UpdateColumn("password", legacy)
	if err := db.First(&saved, u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&saved).Update("name", "unchanged credential").Error; err != nil {
		t.Fatal(err)
	}
	var reloaded User
	db.First(&reloaded, u.ID)
	if reloaded.Password != legacy {
		t.Fatal("unrelated update changed legacy credential")
	}
	if err := db.Model(&User{}).Where("id = ?", u.ID).Update("password", "MapPassword!123").Error; err != nil {
		t.Fatal(err)
	}
	db.First(&reloaded, u.ID)
	if bcrypt.CompareHashAndPassword([]byte(reloaded.Password), []byte("MapPassword!123")) != nil {
		t.Fatal("map update persisted non-hash password")
	}
}

func TestConcurrentBrowserSessionIsolation(t *testing.T) {
	authTestDB(t)
	r := authRouter(t)
	var wg sync.WaitGroup
	failures := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := newBrowser(r)
			for j := 0; j < 8; j++ {
				w := b.request("GET", "/csrf", nil)
				if w.Code != 200 {
					failures <- fmt.Sprint(w.Code)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
}

func TestCookiePolicyAndStrictServiceExemption(t *testing.T) {
	authTestDB(t)
	configureTestAdmin(t)
	t.Setenv("OPENVPN_UI_COOKIE_SECURE", "true")
	r := authRouter(t)
	b := newBrowser(r)
	b.csrf(t)
	cookie := b.cookies["user_session"]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatal("unsafe cookie policy")
	}
	cases := []struct {
		method, path, peer, token, forwarded string
		want                                 bool
	}{
		{"POST", "/ovpn/login", "127.0.0.1:1234", "service-test-token", "", true},
		{"POST", "/ovpn/history", "[::1]:1234", "service-test-token", "", true},
		{"GET", "/ovpn/firewall", "127.0.0.1:1234", "service-test-token", "", true},
		{"POST", "/ovpn/login", "192.0.2.1:1234", "service-test-token", "127.0.0.1", false},
		{"POST", "/ovpn/login", "127.0.0.1:1234", "service-test-token", "192.0.2.1", false},
		{"POST", "/ovpn/login", "127.0.0.1:1234", "", "", false},
		{"POST", "/settings", "127.0.0.1:1234", "service-test-token", "", false},
		{"DELETE", "/ovpn/firewall", "127.0.0.1:1234", "service-test-token", "", false},
		{"PATCH", "/ovpn/history", "127.0.0.1:1234", "service-test-token", "", false},
	}
	for _, test := range cases {
		req := httptest.NewRequest(test.method, test.path, nil)
		req.RemoteAddr = test.peer
		req.Header.Set("O-Token", test.token)
		req.Header.Set("X-Forwarded-For", test.forwarded)
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = req
		if got := serviceRequest(ctx); got != test.want {
			t.Fatalf("service exemption %s %s peer %s=%t", test.method, test.path, test.peer, got)
		}
	}
	viper.Set("system.base.token", "")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/ovpn/login", nil)
	ctx.Request.RemoteAddr = "127.0.0.1:1234"
	if serviceRequest(ctx) {
		t.Fatal("empty token exemption")
	}
}

func TestConfigurationChangesSerializeWithReads(t *testing.T) {
	r := gin.New()
	r.Use(configRequestGuard())
	entered, release, readDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r.POST("/settings", func(c *gin.Context) { close(entered); <-release; c.Status(200) })
	r.GET("/read", func(c *gin.Context) { close(readDone); c.Status(200) })
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/settings", nil))
	}()
	<-entered
	go r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/read", nil))
	select {
	case <-readDone:
		close(release)
		t.Fatal("configuration read overlapped update")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-writeDone
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("read did not resume")
	}
}

func TestDisableEnableDoesNotResurrectStolenSession(t *testing.T) {
	authTestDB(t)
	u := newAuthUser(t, "alice", "")
	r := authRouter(t)
	b := newBrowser(r)
	loginBrowser(t, b, "alice")
	b.csrf(t)
	if err := db.Model(&User{}).Where("id = ?", u.ID).Update("is_enable", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&User{}).Where("id = ?", u.ID).Update("is_enable", true).Error; err != nil {
		t.Fatal(err)
	}
	if w := b.request("GET", "/client/test", nil); w.Code != 401 {
		t.Fatal("disabled/re-enabled account resurrected old session")
	}
}

func TestFailedSettingsPersistenceDoesNotChangeCredential(t *testing.T) {
	authTestDB(t)
	configureTestAdmin(t)
	r := setupRouter(ovpn{})
	b := newBrowser(r)
	loginBrowser(t, b, "admin")
	b.csrf(t)
	original := viper.GetString("system.base.admin_password")
	viper.SetConfigFile(filepath.Join(t.TempDir(), "missing", "config.json"))
	if w := b.request("POST", "/settings", url.Values{"currentPass": {"LongPassword!123"}, "system.base.admin_password": {"ChangedPassword!123"}}); w.Code != 500 {
		t.Fatalf("expected persistence failure: %d", w.Code)
	}
	if viper.GetString("system.base.admin_password") != original {
		t.Fatal("failed settings write changed live administrator credential")
	}
}
func TestFailedAdminMigrationDoesNotResetPassword(t *testing.T) {
	authTestDB(t)
	configureTestAdmin(t)
	legacy, err := aes.AesEncrypt("LongPassword!123", secretKey)
	if err != nil {
		t.Fatal(err)
	}
	viper.Set("system.base.admin_password", legacy)
	viper.SetConfigFile(filepath.Join(t.TempDir(), "missing", "config.json"))
	if verifyAdminPassword("LongPassword!123") {
		t.Fatal("migration persistence failure accepted")
	}
	if viper.GetString("system.base.admin_password") != legacy {
		t.Fatal("migration failure reset or changed administrator password")
	}
	if verifyAdminPassword("admin") {
		t.Fatal("default admin password accepted")
	}
}

func TestAPISecretsRedacted(t *testing.T) {
	u := User{Password: "password-marker", MfaSecret: "seed-marker"}
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "password-marker") || strings.Contains(string(b), "seed-marker") {
		t.Fatalf("user leaks secrets: %s", b)
	}
	var cfg config
	cfg.System.Base.AdminPassword = "admin-marker"
	cfg.System.Ldap.LdapBindPassword = "ldap-marker"
	cfg.System.Email.Password = "mail-marker"
	b, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"admin-marker", "ldap-marker", "mail-marker"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("settings leaks %s", secret)
		}
	}
}
