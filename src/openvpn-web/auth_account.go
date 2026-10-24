package main

import (
	"strconv"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func registerAccountRoutes(r *gin.Engine) {
	r.GET("/client/userinfo", func(c *gin.Context) {
		u, ok := ownAccount(c)
		if !ok {
			return
		}
		admin, _ := c.Get("auth_admin")
		if admin == true {
			first := viper.GetBool("system.base.admin_first_login")
			u.IsFirstLogin = &first
		}
		if ldapAuth && admin != true {
			l, err := InitLdap()
			if err != nil {
				securityError(c, 503, "directory unavailable")
				return
			}
			lu, err := l.Get(u.Username)
			if err != nil {
				securityError(c, 503, "directory unavailable")
				return
			}
			c.JSON(200, lu)
			return
		}
		c.JSON(200, u)
	})
	r.POST("/client/modifyPass", func(c *gin.Context) {
		u, ok := ownAccount(c)
		if !ok || !requireCurrentPassword(c, &u) {
			return
		}
		password := c.PostForm("password")
		if !isValidPassword(password) || len(password) > 72 {
			securityError(c, 400, "密码不满足要求（12-72字节，包含大小写字母、数字、特殊字符）")
			return
		}
		admin, _ := c.Get("auth_admin")
		if admin == true {
			hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
			if err != nil {
				securityError(c, 500, "password update failed")
				return
			}
			old := viper.GetString("system.base.admin_password")
			oldFirst := viper.GetBool("system.base.admin_first_login")
			oldVersion := viper.GetString("system.base.admin_session_version")
			viper.Set("system.base.admin_session_version", secureToken())
			viper.Set("system.base.admin_password", string(hash))
			viper.Set("system.base.admin_first_login", false)
			if err = persistConfig(viper.GetViper()); err != nil {
				viper.Set("system.base.admin_password", old)
				viper.Set("system.base.admin_first_login", oldFirst)
				viper.Set("system.base.admin_session_version", oldVersion)
				securityError(c, 500, "password update failed")
				return
			}
		} else {
			hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
			if err != nil {
				securityError(c, 500, "password update failed")
				return
			}
			result := db.Model(&User{}).Where("id = ? AND password = ?", u.ID, u.Password).Updates(map[string]interface{}{"password": string(hash), "is_first_login": false, "session_version": gorm.Expr("session_version + 1")})
			if result.Error != nil || result.RowsAffected != 1 {
				securityError(c, 500, "password update failed")
				return
			}
		}
		if err := destroySession(c); err != nil {
			securityError(c, 500, "session unavailable")
			return
		}
		c.JSON(200, gin.H{"message": "密码修改成功", "redirect": "/login"})
	})
	r.GET("/client/mfa", func(c *gin.Context) {
		u, ok := ownAccount(c)
		if !ok {
			return
		}
		c.JSON(200, gin.H{"mfaEnable": u.MfaSecret != "", "user": u})
	})
	r.POST("/client/mfa/enroll", func(c *gin.Context) {
		u, ok := ownAccount(c)
		if !ok || !requireCurrentPassword(c, &u) {
			return
		}
		if u.ID == 0 || ldapAuth {
			securityError(c, 400, "MFA enrollment unavailable")
			return
		}
		if u.MfaSecret != "" {
			securityError(c, 409, "MFA already enabled; disable before enrolling a new seed")
			return
		}
		secret, err := GenMfa(u.Username)
		if err != nil {
			securityError(c, 500, "MFA enrollment failed")
			return
		}
		s := sessions.Default(c)
		s.Set("mfa_enroll_id", int(u.ID))
		s.Set("mfa_enroll_secret", secret)
		s.Set("mfa_enroll_expiry", time.Now().Add(mfaPendingLifetime).Unix())
		s.Set("mfa_enroll_attempts", 0)
		s.Set("mfa_enroll_version", accountVersion(u))
		if err = s.Save(); err != nil {
			securityError(c, 500, "session unavailable")
			return
		}
		// The only endpoint that returns a seed: freshly generated, pending and not active.
		c.JSON(200, gin.H{"mfaSecret": secret, "mfaEnable": false})
	})
	r.POST("/client/mfa", func(c *gin.Context) {
		u, ok := ownAccount(c)
		if !ok || !requireCurrentPassword(c, &u) {
			return
		}
		s := sessions.Default(c)
		id, _ := s.Get("mfa_enroll_id").(int)
		secret, _ := s.Get("mfa_enroll_secret").(string)
		expiry, _ := s.Get("mfa_enroll_expiry").(int64)
		attempts, _ := s.Get("mfa_enroll_attempts").(int)
		clear := func() {
			for _, key := range []string{"mfa_enroll_id", "mfa_enroll_secret", "mfa_enroll_expiry", "mfa_enroll_attempts", "mfa_enroll_version"} {
				s.Delete(key)
			}
		}
		if id != int(u.ID) || id == 0 || secret == "" || expiry <= time.Now().Unix() || attempts >= mfaMaxAttempts || s.Get("mfa_enroll_version") != accountVersion(u) {
			clear()
			if err := s.Save(); err != nil {
				securityError(c, 500, "session unavailable")
				return
			}
			securityError(c, 401, "MFA enrollment expired")
			return
		}
		if !ValidateMfa(c.PostForm("passcode"), secret) {
			s.Set("mfa_enroll_attempts", attempts+1)
			if attempts+1 >= mfaMaxAttempts {
				clear()
			}
			if err := s.Save(); err != nil {
				securityError(c, 500, "session unavailable")
				return
			}
			securityError(c, 401, "验证码错误")
			return
		}
		result := db.Model(&User{}).Where("id = ? AND (mfa_secret = '' OR mfa_secret IS NULL)", u.ID).Updates(map[string]interface{}{"mfa_secret": secret, "session_version": gorm.Expr("session_version + 1")})
		if result.Error != nil || result.RowsAffected != 1 {
			securityError(c, 500, "MFA update failed")
			return
		}
		clear()
		if err := destroySession(c); err != nil {
			securityError(c, 500, "session unavailable")
			return
		}
		c.JSON(200, gin.H{"message": "MFA已启用", "redirect": "/login"})
	})
	r.DELETE("/client/mfa/:id", func(c *gin.Context) {
		u, ok := ownAccount(c)
		if !ok || !requireCurrentPassword(c, &u) {
			return
		}
		id := u.ID
		admin, _ := c.Get("auth_admin")
		if admin == true {
			parsed, err := strconv.ParseUint(c.Param("id"), 10, 64)
			if err != nil || parsed == 0 {
				securityError(c, 400, "invalid user")
				return
			}
			id = uint(parsed)
		}
		if id == 0 || ldapAuth {
			securityError(c, 400, "MFA update unavailable")
			return
		}
		result := db.Model(&User{}).Where("id = ?", id).Updates(map[string]interface{}{"mfa_secret": "", "session_version": gorm.Expr("session_version + 1")})
		if result.Error != nil || result.RowsAffected != 1 {
			securityError(c, 500, "MFA update failed")
			return
		}
		if admin != true {
			if err := destroySession(c); err != nil {
				securityError(c, 500, "session unavailable")
				return
			}
		}
		c.JSON(200, gin.H{"message": "MFA已停用", "redirect": "/login"})
	})
}
