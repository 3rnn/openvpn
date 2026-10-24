package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"
	"time"

	"github.com/gavintan/gopkg/aes"
	"github.com/microcosm-cc/bluemonday"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type User struct {
	SessionVersion uint64     `gorm:"not null;default:1" json:"-" form:"-"`
	ID             uint       `gorm:"primarykey" json:"id" form:"id" uri:"id"`
	Username       string     `gorm:"uniqueIndex;column:username" json:"username" form:"username"`
	Password       string     `form:"password" json:"password"`
	IsEnable       *bool      `gorm:"default:true" form:"isEnable" json:"isEnable"`
	Name           string     `json:"name" form:"name"`
	Email          string     `json:"email" form:"email"`
	Gid            uint       `gorm:"default:1" json:"gid" form:"gid"`
	ExpireDate     string     `gorm:"default:NULL" json:"expireDate" form:"expireDate"`
	IpAddr         string     `gorm:"uniqueIndex;default:NULL" json:"ipAddr" form:"ipAddr"`
	OvpnConfig     string     `json:"ovpnConfig" form:"ovpnConfig"`
	MfaSecret      string     `json:"mfaSecret" form:"mfaSecret"`
	IsFirstLogin   *bool      `gorm:"default:true" form:"isFirstLogin" json:"isFirstLogin"`
	LastLoginAt    *time.Time `json:"lastLoginAt,omitempty" form:"lastLoginAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt,omitempty" form:"createdAt,omitempty"`
	UpdatedAt      time.Time  `json:"updatedAt,omitempty" form:"updatedAt,omitempty"`
}

// MarshalJSON is the single API serialization boundary, including nested group users.
func (u User) MarshalJSON() ([]byte, error) {
	type publicUser User
	b, err := json.Marshal(publicUser(u))
	if err != nil {
		return nil, err
	}
	var fields map[string]interface{}
	if err = json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	delete(fields, "password")
	delete(fields, "mfaSecret")
	fields["mfaEnabled"] = u.MfaSecret != ""
	return json.Marshal(fields)
}

func (u *User) BeforeSave(tx *gorm.DB) (err error) {
	p := bluemonday.UGCPolicy()

	val := reflect.ValueOf(u).Elem()
	for i := 0; i < val.NumField(); i++ {
		if val.Type().Field(i).Name == "Password" {
			continue
		}

		fieldVal := val.Field(i)
		if fieldVal.Kind() == reflect.String && fieldVal.CanSet() {
			rawStr := val.Field(i).String()
			val.Field(i).SetString(p.Sanitize(rawStr))
		}
	}

	// Hash only an explicitly supplied password, not the model receiver's
	// credential during unrelated map updates. This also preserves unmigrated
	// legacy ciphertext until the user proves possession of its plaintext.
	password := ""
	switch data := tx.Statement.Dest.(type) {
	case map[string]interface{}:
		if value, ok := data["password"].(string); ok {
			password = value
		}
		if value, ok := data["Password"].(string); ok {
			password = value
		}
	case User:
		password = data.Password
	case *User:
		password = data.Password
	default:
		if tx.Statement.ReflectValue.Kind() == reflect.Struct {
			field := tx.Statement.ReflectValue.FieldByName("Password")
			if field.IsValid() && field.Kind() == reflect.String {
				password = field.String()
			}
		}
	}
	unchanged := false
	if password != "" && u.ID != 0 {
		var original User
		result := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Select("id", "password").First(&original, u.ID)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		}
		unchanged = result.Error == nil && original.Password == password
	}
	if password != "" && !unchanged && !strings.HasPrefix(password, "$2") {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
		if err != nil {
			return err
		}
		tx.Statement.SetColumn("password", string(hash))
	}

	return nil
}

// Map updates are commonly used for explicit false/empty values. Bump even
// when the old value is restored later, so disable/enable and MFA-reset cycles
// cannot resurrect an otherwise identical stolen session. Struct-based public
// updates go through Update(), which increments within the same transaction.
func (u *User) BeforeUpdate(tx *gorm.DB) error {
	if data, ok := tx.Statement.Dest.(map[string]interface{}); ok {
		for _, key := range []string{"password", "Password", "mfa_secret", "MfaSecret", "username", "Username", "is_enable", "IsEnable", "gid", "Gid", "expire_date", "ExpireDate"} {
			if _, present := data[key]; present {
				if _, explicit := data["session_version"]; !explicit {
					data["session_version"] = gorm.Expr("session_version + 1")
				}
				break
			}
		}
	}
	return nil
}

// Passwords are never decrypted by read hooks. Legacy records migrate only
// after successful authentication, using an atomic compare-and-swap.
func (u *User) verifyPassword(password string) bool {
	if strings.HasPrefix(u.Password, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)) == nil
	}
	plain, err := aes.AesDecrypt(u.Password, secretKey)
	if err != nil || !constantEqual(plain, password) {
		return false
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return false
	}
	result := db.Model(&User{}).Where("id = ? AND password = ?", u.ID, u.Password).UpdateColumn("password", string(hash))
	if result.Error != nil || result.RowsAffected != 1 {
		return false
	}
	u.Password = string(hash)
	return true
}

func (u *User) All() []User {
	var users []User

	result := db.WithContext(context.Background()).Find(&users)
	if result.Error != nil {
		logger.Error(context.Background(), result.Error.Error())
		return []User{}
	}

	return users
}

func (u *User) Get(id string) User {
	result := db.First(&u, id)
	if result.Error != nil {
		logger.Error(context.Background(), result.Error.Error())
		return User{}
	}

	return *u
}

func (u *User) Create() error {
	if u.Username == "" || u.Password == "" {
		return fmt.Errorf("非法请求")
	}

	if u.Username == adminUsername {
		return fmt.Errorf("用户名与系统账户冲突")
	}

	result := db.Create(&u)
	return result.Error
}

func (u *User) Update() error {
	if u.ID == 0 {
		return errors.New("user ID required")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(u).Updates(u).Error; err != nil {
			return err
		}
		return tx.Model(&User{}).Where("id = ?", u.ID).UpdateColumn("session_version", gorm.Expr("session_version + 1")).Error
	})
}

func (u *User) Delete(id string) error {
	result := db.Unscoped().Delete(&User{}, id)
	return result.Error
}

func (u *User) UpdatePassword() error { return u.Update() }

func (u *User) Login(clogin bool) error {
	user := u.Username
	pass := u.Password
	commonName := u.OvpnConfig

	if clogin {
		if viper.GetInt("system.base.max_duplicate_login") > 0 {
			data, err := os.ReadFile(path.Join(ovData, "openvpn-status.log"))
			if err != nil {
				logger.Error(context.Background(), err.Error())
			}

			loginCount := 0
			for _, v := range strings.Split(string(data), "\n") {
				cdSlice := strings.Split(v, "\t")

				if cdSlice[0] == "CLIENT_LIST" {
					if cdSlice[9] == user {
						loginCount++
					}
				}
			}

			if loginCount >= viper.GetInt("system.base.max_duplicate_login") {
				return fmt.Errorf("用户已登录数量超过限制")
			}
		}
	}

	if ldapAuth {
		l, err := InitLdap()
		if err != nil {
			return err
		}

		return l.Auth(clogin, user, pass, commonName)
	} else {
		*u = User{}
		result := db.First(u, "username = ?", user)
		if result.Error != nil {
			return errors.New("authentication failed")
		}

		if !userAvailable(*u) {
			return errors.New("account disabled or expired")
		}

		if clogin {
			if u.MfaSecret != "" && !strings.HasPrefix(pass, "SCRV1:") {
				return fmt.Errorf("未获取到MFA验证码")
			}

			var passcode string
			if strings.HasPrefix(pass, "SCRV1:") {
				parts := strings.Split(pass, ":")
				if len(parts) == 3 {
					p, err := base64.StdEncoding.DecodeString(parts[1])
					if err != nil {
						return fmt.Errorf("passwd解码错误：%w", err)
					}

					pass = string(p)

					k, err := base64.StdEncoding.DecodeString(parts[2])
					if err != nil {
						return fmt.Errorf("key解码错误：%w", err)
					}

					passcode = string(k)
				}
			}

			if u.MfaSecret != "" {
				vaild := ValidateMfa(passcode, u.MfaSecret)
				if !vaild {
					return fmt.Errorf("MFA验证失败")
				}
			}
		}

		if !u.verifyPassword(pass) {
			return fmt.Errorf("密码错误")
		}

		if clogin {
			if viper.GetBool("system.base.validate_client_config") {
				if commonName != strings.TrimSuffix(u.OvpnConfig, ".ovpn") {
					return fmt.Errorf("使用非法配置文件登录")
				}
			}

			var ovconfig sql.NullString
			result := db.Raw(`
				WITH RECURSIVE group_up AS (
					SELECT
						id,
						parent_id,
						config,
						0 AS level
					FROM "group"
					WHERE id = ?
			
					UNION ALL
			
					SELECT
						g.id,
						g.parent_id,
						g.config,
						gu.level + 1
					FROM "group" g
					JOIN group_up gu ON g.id = gu.parent_id
				)
				SELECT GROUP_CONCAT(REPLACE(config, '\n', CHAR(10)), CHAR(10)) AS configs
				FROM group_up
				WHERE config IS NOT NULL
			`, u.Gid).Scan(&ovconfig)

			if result.Error != nil {
				return errors.New("connection policy lookup failed")
			}
			// Always replace the snapshot, including an empty policy, so a
			// prior connection's static address/options cannot survive removal.
			if err := writeClientConnectionConfig(user, commonName, u.IpAddr, ovconfig.String); err != nil {
				return err
			}
		}

		db.Model(&u).Update("last_login_at", time.Now())

		return nil
	}
}

func (u User) Info() User {
	var found User
	if u.Username != "" {
		if db.First(&found, "username = ?", u.Username).Error != nil {
			return User{}
		}
	} else if u.ID != 0 {
		if db.First(&found, u.ID).Error != nil {
			return User{}
		}
	} else {
		return User{}
	}
	u = found

	return u
}

func (u *User) GetGroups() []Group {
	var groups []Group

	db.Raw(`
		WITH RECURSIVE group_tree AS (
			SELECT g.*
			FROM "group" g
			INNER JOIN user u ON u.gid = g.id
			WHERE u.username = ?

			UNION ALL

			SELECT g.*
			FROM "group" g
			INNER JOIN group_tree gt ON g.id = gt.parent_id
		)
		SELECT DISTINCT id FROM group_tree
	`, u.Username).Scan(&groups)

	return groups
}

func (User) TableName() string {
	return "user"
}
