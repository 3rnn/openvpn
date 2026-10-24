package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/gavintan/gopkg/aes"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
)

const mfaPendingLifetime = 3 * time.Minute
const mfaMaxAttempts = 5

var authMutationMu sync.Mutex

func constantEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func secureToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(b)
}
func cookieOptions(age int) sessions.Options {
	secure := viper.GetBool("system.base.cookie_secure") || strings.EqualFold(os.Getenv("OPENVPN_UI_COOKIE_SECURE"), "true") || strings.HasPrefix(strings.ToLower(viper.GetString("system.base.site_url")), "https://")
	return sessions.Options{Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: age}
}

// A service exemption requires all three: exact route+method, direct loopback
// peer (never forwarded headers), and a nonempty constant-time checked token.
func serviceRequest(c *gin.Context) bool {
	p, m := c.Request.URL.Path, c.Request.Method
	allowed := (p == "/ovpn/login" && m == "POST") || (p == "/ovpn/history" && m == "POST") || (p == "/ovpn/firewall" && (m == "GET" || m == "POST"))
	token := viper.GetString("system.base.token")
	return allowed && token != "" && IsLocalRequest(c) && c.GetHeader("Forwarded") == "" && c.GetHeader("X-Forwarded-For") == "" && c.GetHeader("X-Real-IP") == "" && constantEqual(c.GetHeader("O-Token"), token)
}
func securityError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"message": message})
}
func CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" || serviceRequest(c) {
			c.Next()
			return
		}
		// Serializing browser mutations avoids lost pending-attempt updates and OTP
		// completion replay from simultaneous requests using the same session.
		authMutationMu.Lock()
		defer authMutationMu.Unlock()
		s := sessions.Default(c)
		token, ok := s.Get("csrf_token").(string)
		if !ok || token == "" || !constantEqual(token, c.GetHeader("X-CSRF-Token")) {
			securityError(c, 403, "CSRF validation failed")
			return
		}
		if origin := c.GetHeader("Origin"); origin != "" {
			scheme := "http"
			if c.Request.TLS != nil || cookieOptions(0).Secure {
				scheme = "https"
			}
			if origin != scheme+"://"+c.Request.Host {
				securityError(c, 403, "CSRF origin mismatch")
				return
			}
		}
		c.Next()
	}
}

// Delete before saving: Clear alone does not rotate the backing gorm session ID.
func rotateSession(c *gin.Context, age int) (sessions.Session, error) {
	s := sessions.Default(c)
	s.Clear()
	s.Options(cookieOptions(-1))
	if err := s.Save(); err != nil {
		return nil, err
	}
	s.Options(cookieOptions(age))
	s.Set("csrf_token", secureToken())
	return s, nil
}
func destroySession(c *gin.Context) error {
	s := sessions.Default(c)
	s.Clear()
	s.Options(cookieOptions(-1))
	return s.Save()
}
func sessionExists(s sessions.Session) bool {
	var n int64
	return s.ID() != "" && db.Table("sessions").Where("id = ? AND expires_at > ?", s.ID(), time.Now()).Count(&n).Error == nil && n == 1
}
func userAvailable(u User) bool {
	if u.ID == 0 || u.IsEnable == nil || !*u.IsEnable {
		return false
	}
	if u.ExpireDate != "" {
		for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02", time.RFC3339} {
			if expiry, err := time.ParseInLocation(layout, u.ExpireDate, time.Local); err == nil {
				return time.Now().Before(expiry)
			}
		}
		return false
	}
	return true
}
func accountVersion(u User) string {
	enabled := u.IsEnable != nil && *u.IsEnable
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s|%s|%s|%t|%d|%s|%t", u.ID, u.SessionVersion, u.Username, u.Password, u.MfaSecret, enabled, u.Gid, u.ExpireDate, ldapAuth)))
	return hex.EncodeToString(sum[:])
}
func ldapVersion() string {
	sum := sha256.Sum256([]byte(fmt.Sprint(ldapAuth) + "|" + fmt.Sprint(viper.GetStringMap("system.ldap")) + "|" + viper.GetString("system.base.admin_session_version")))
	return hex.EncodeToString(sum[:])
}
func adminVersion() string {
	sum := sha256.Sum256([]byte(viper.GetString("system.base.admin_username") + "\x00" + viper.GetString("system.base.admin_password") + "\x00" + viper.GetString("system.base.admin_session_version")))
	return hex.EncodeToString(sum[:])
}
func verifyAdminPassword(password string) bool {
	hash := viper.GetString("system.base.admin_password")
	if strings.HasPrefix(hash, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	plain, err := aes.AesDecrypt(hash, secretKey)
	if err != nil || !constantEqual(plain, password) {
		return false
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return false
	}
	viper.Set("system.base.admin_password", string(b))
	if err = viper.WriteConfig(); err != nil {
		viper.Set("system.base.admin_password", hash)
		return false
	}
	return true
}
func completeLogin(c *gin.Context, u User, admin, ldap bool, age int) error {
	s, err := rotateSession(c, age)
	if err != nil {
		return err
	}
	s.Set("user", u.Username)
	s.Set("auth_kind", "local")
	s.Set("authenticated_user_id", int(u.ID))
	s.Set("session_version", accountVersion(u))
	if admin {
		s.Set("auth_kind", "admin")
		s.Set("session_version", adminVersion())
	} else if ldap {
		s.Set("auth_kind", "ldap")
		s.Set("session_version", ldapVersion())
	}
	if err = s.Save(); err != nil {
		return err
	}
	resetLoginFail(c.ClientIP())
	redirect := "/"
	first := u.IsFirstLogin != nil && *u.IsFirstLogin
	if admin {
		redirect = "/admin"
		first = viper.GetBool("system.base.admin_first_login")
	}
	c.JSON(200, gin.H{"message": "登录成功", "redirect": redirect, "user": gin.H{"id": u.ID, "isFirstLogin": first}})
	return nil
}
func registerAuthRoutes(r *gin.Engine) {
	r.GET("/csrf", func(c *gin.Context) {
		s := sessions.Default(c)
		token, _ := s.Get("csrf_token").(string)
		if token == "" {
			token = secureToken()
			s.Options(cookieOptions(3600))
			s.Set("csrf_token", token)
			if err := s.Save(); err != nil {
				securityError(c, 500, "session unavailable")
				return
			}
		}
		c.JSON(200, gin.H{"csrfToken": token})
	})
	r.POST("/login", loginHandler)
	r.GET("/logout", func(c *gin.Context) { securityError(c, 405, "Use POST /logout with a CSRF token") })
	r.POST("/logout", func(c *gin.Context) {
		if err := destroySession(c); err != nil {
			securityError(c, 500, "session unavailable")
			return
		}
		c.JSON(200, gin.H{"message": "已退出", "redirect": "/login"})
	})
}
func loginHandler(c *gin.Context) {
	s := sessions.Default(c)
	code := c.PostForm("passcode")
	fail := func() { setLoginFail(c.ClientIP()); securityError(c, 401, "authentication failed") }
	if code != "" {
		id, ok := s.Get("authenticated_user_id").(int)
		pending, _ := s.Get("mfa_pending").(bool)
		expiry, _ := s.Get("mfa_pending_expiry").(int64)
		attempts, _ := s.Get("mfa_attempts").(int)
		if !ok || !pending || expiry <= time.Now().Unix() || attempts >= mfaMaxAttempts || !sessionExists(s) {
			destroySession(c)
			fail()
			return
		}
		var u User
		if err := db.First(&u, id).Error; err != nil || !userAvailable(u) || u.MfaSecret == "" || s.Get("pending_version") != accountVersion(u) {
			destroySession(c)
			fail()
			return
		}
		if !ValidateMfa(code, u.MfaSecret) {
			s.Set("mfa_attempts", attempts+1)
			if attempts+1 >= mfaMaxAttempts {
				destroySession(c)
			} else if err := s.Save(); err != nil {
				securityError(c, 500, "session unavailable")
				return
			}
			fail()
			return
		}
		age, _ := s.Get("login_max_age").(int)
		if age == 0 {
			age = 3600
		}
		if err := completeLogin(c, u, false, false, age); err != nil {
			securityError(c, 500, "session unavailable")
		}
		return
	}
	if getLoginFail(c.ClientIP()) >= loginCaptchaThreshold && !checkCaptcha(c.PostForm("c_key"), c.PostForm("c_dots")) {
		c.AbortWithStatusJSON(401, gin.H{"message": "需要进行人机验证", "needCaptcha": true})
		return
	}
	username, password := c.PostForm("username"), c.PostForm("password")
	age := 3600
	if c.PostForm("remember7d") == "on" {
		age = 7 * 24 * 3600
	}
	// A new password stage always invalidates any prior authenticated/pending state.
	csrf := s.Get("csrf_token")
	s.Clear()
	s.Set("csrf_token", csrf)
	if err := s.Save(); err != nil {
		securityError(c, 500, "session unavailable")
		return
	}
	if username == viper.GetString("system.base.admin_username") && username != "" {
		if !verifyAdminPassword(password) {
			fail()
			return
		}
		if err := completeLogin(c, User{Username: username}, true, false, age); err != nil {
			securityError(c, 500, "session unavailable")
		}
		return
	}
	u := User{Username: username, Password: password}
	if err := u.Login(false); err != nil {
		fail()
		return
	}
	if ldapAuth {
		if err := completeLogin(c, User{Username: username}, false, true, age); err != nil {
			securityError(c, 500, "session unavailable")
		}
		return
	}
	if !userAvailable(u) {
		fail()
		return
	}
	if u.MfaSecret != "" {
		// Rotate between anonymous and password-verified, keeping only the CSRF token
		// usable by the next MFA form; the old session ID is immediately invalid.
		newSession, err := rotateSession(c, 180)
		if err != nil {
			securityError(c, 500, "session unavailable")
			return
		}
		s = newSession
		s.Set("csrf_token", csrf)
		s.Set("authenticated_user_id", int(u.ID))
		s.Set("mfa_pending", true)
		s.Set("mfa_pending_expiry", time.Now().Add(mfaPendingLifetime).Unix())
		s.Set("mfa_attempts", 0)
		s.Set("pending_version", accountVersion(u))
		s.Set("login_max_age", age)
		if err = s.Save(); err != nil {
			securityError(c, 500, "session unavailable")
			return
		}
		c.JSON(200, gin.H{"message": "需要MFA验证", "mfaPending": true})
		return
	}
	if err := completeLogin(c, u, false, false, age); err != nil {
		securityError(c, 500, "session unavailable")
	}
}
func currentAccount(c *gin.Context) (User, bool, error) {
	s := sessions.Default(c)
	name, _ := s.Get("user").(string)
	kind, _ := s.Get("auth_kind").(string)
	if !sessionExists(s) || name == "" {
		return User{}, false, errors.New("unauthorized")
	}
	if kind == "admin" {
		if name != viper.GetString("system.base.admin_username") || s.Get("session_version") != adminVersion() {
			return User{}, false, errors.New("revoked")
		}
		return User{Username: name}, true, nil
	}
	if kind == "ldap" {
		if !ldapAuth || s.Get("session_version") != ldapVersion() {
			return User{}, false, errors.New("revoked")
		}
		l, err := InitLdap()
		if err != nil {
			return User{}, false, err
		}
		_, err = l.Get(name)
		return User{Username: name}, false, err
	}
	id, ok := s.Get("authenticated_user_id").(int)
	if !ok || kind != "local" || ldapAuth {
		return User{}, false, errors.New("unauthorized")
	}
	var u User
	if err := db.First(&u, id).Error; err != nil {
		return User{}, false, err
	}
	if !userAvailable(u) || name != u.Username || s.Get("session_version") != accountVersion(u) {
		return User{}, false, errors.New("revoked")
	}
	return u, false, nil
}
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if serviceRequest(c) {
			c.Next()
			return
		}
		u, admin, err := currentAccount(c)
		if err != nil {
			destroySession(c)
			securityError(c, 401, "authentication required")
			return
		}
		c.Set("auth_user", u)
		c.Set("auth_admin", admin)
		path := c.Request.URL.Path
		if !admin && path != "/" && !strings.HasPrefix(path, "/client/") {
			securityError(c, 403, "administrator required")
			return
		}
		first := u.IsFirstLogin != nil && *u.IsFirstLogin
		if admin {
			first = viper.GetBool("system.base.admin_first_login")
		}
		if first && path != "/" && path != "/admin" && path != "/client/userinfo" && path != "/client/modifyPass" {
			securityError(c, 403, "password change required")
			return
		}
		c.Next()
	}
}
func ownAccount(c *gin.Context) (User, bool) {
	value, ok := c.Get("auth_user")
	if !ok {
		securityError(c, 401, "authentication required")
		return User{}, false
	}
	u, ok := value.(User)
	if !ok {
		securityError(c, 401, "authentication required")
	}
	return u, ok
}

// The settings endpoint accepts only fields explicitly modeled as UI settings.
// Internal keys (token, secret_key, first-login/session version) are not writable.
func allowedSetting(key string) bool {
	parts := strings.Split(key, ".")
	typ := reflect.TypeOf(config{})
	for _, part := range parts {
		found := false
		if typ.Kind() != reflect.Struct {
			return false
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.Tag.Get("mapstructure") == part {
				typ = f.Type
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return typ.Kind() != reflect.Struct
}

func requireCurrentPassword(c *gin.Context, u *User) bool {
	password := c.PostForm("currentPass")
	admin, _ := c.Get("auth_admin")
	valid := false
	if password != "" {
		if admin == true {
			valid = verifyAdminPassword(password)
		} else if !ldapAuth {
			valid = u.verifyPassword(password)
		}
	}
	if !valid {
		securityError(c, 401, "current password required")
		return false
	}
	return true
}
