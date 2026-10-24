package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"encoding/csv"
	"encoding/pem"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gavintan/gopkg/aes"
	"github.com/gavintan/gopkg/tools"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/patrickmn/go-cache"
	"github.com/robfig/cron/v3"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	gLogger "gorm.io/gorm/logger"
)

type ClientData struct {
	ID             string  `json:"id"`
	Rip            string  `json:"rip"`
	Vip            string  `json:"vip"`
	Vip6           string  `json:"vip6"`
	RecvBytes      float64 `json:"recvBytes"`
	SendBytes      float64 `json:"sendBytes"`
	ConnDate       string  `json:"connDate"`
	OnlineTime     string  `json:"onlineTime"`
	Username       string  `json:"username"`
	CommonName     string  `json:"commonName"`
	IsNftBlacklist bool    `json:"isNftBlacklist"`
}

type ServerData struct {
	RunDate    string
	Status     string
	StatusDesc string
	Address    string
	Nclients   string
	BytesIn    string
	BytesOut   string
	Mode       string
	Version    string
}

type ClientConfigData struct {
	Name     string `json:"name"`
	FullName string `json:"fullName"`
	File     string `json:"file"`
	Date     string `json:"date"`
}

type Params struct {
	Draw        int    `json:"draw" form:"draw"`
	Offset      int    `json:"offset" form:"offset"`
	Limit       int    `json:"limit" form:"limit"`
	OrderColumn string `json:"orderColumn" form:"orderColumn"`
	Order       string `json:"order" form:"order"`
	Search      string `json:"search" form:"search"`
	Qt          string `json:"qt" form:"qt"`
}

type CertData struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Subject   string `json:"subject"`
	Issuer    string `json:"issuer"`
	NotBefore string `json:"notBefore"`
	NotAfter  string `json:"notAfter"`
	ExpiresIn string `json:"expiresIn"`
	Status    string `json:"status"`
	SerialNo  string `json:"serialNo"`
}

type ovpn struct {
	address string
}

var (
	version = "1.0.0"
	//go:embed templates
	FS embed.FS

	cc     = cache.New(5*time.Minute, 10*time.Minute)
	db     *gorm.DB
	logger = gLogger.New(
		log.New(os.Stdout, "[OPENVPN-WEB] "+time.Now().Format("2006-01-02 15:04:05.000")+" MAIN ", 0),
		gLogger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  gLogger.Error,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
			ParameterizedQueries:      true,
		},
	)
	ovData = os.Getenv("OVPN_DATA")
	conf   config
)

func (ov *ovpn) sendCommand(command string) (string, error) {
	var data string
	var sb strings.Builder

	conn, err := net.DialTimeout("tcp", ov.address, time.Second*3)
	if err != nil {
		logger.Error(context.Background(), err.Error())
		return data, err
	}

	defer conn.Close()

	conn.SetDeadline(time.Now().Add(time.Second * 3))
	fmt.Fprintf(conn, "%s\n", command)

	for {
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)

		re := regexp.MustCompile(">INFO(.)*\r\n")
		if str := re.ReplaceAllString(string(buf[:n]), ""); str != "" {
			sb.Write([]byte(str))
		}

		if err != nil || strings.HasSuffix(sb.String(), "\r\nEND\r\n") || strings.HasPrefix(sb.String(), "SUCCESS:") {
			break
		}
	}

	data = strings.TrimPrefix(strings.TrimSuffix(strings.TrimSuffix(sb.String(), "\r\nEND\r\n"), "\r\n"), "SUCCESS: ")

	return data, nil
}

func (ov *ovpn) getClient() []ClientData {
	clients := make([]ClientData, 0)

	data, err := ov.sendCommand("status 3")
	if err != nil {
		return clients
	}

	for _, v := range strings.Split(data, "\r\n") {
		cdSlice := strings.Split(v, "\t")

		if cdSlice[0] == "CLIENT_LIST" {
			recv, _ := strconv.ParseFloat(cdSlice[5], 64)
			send, _ := strconv.ParseFloat(cdSlice[6], 64)
			connDate, _ := time.ParseInLocation("2006-01-02 15:04:05", cdSlice[7], time.Local)

			rip := cdSlice[2]
			if strings.Count(cdSlice[2], ":") == 1 {
				rip = cdSlice[2][:strings.IndexByte(cdSlice[2], ':')]
			}

			cd := ClientData{
				Rip:        rip,
				Vip:        cdSlice[3],
				Vip6:       cdSlice[4],
				RecvBytes:  recv,
				SendBytes:  send,
				ConnDate:   cdSlice[7],
				Username:   cdSlice[9],
				CommonName: cdSlice[1],
				ID:         cdSlice[10],
				OnlineTime: (time.Duration(time.Now().Unix()-connDate.Unix()) * time.Second).String(),
				IsNftBlacklist: func() bool {
					existV4, _ := getNftTableSetElement("blacklist", cdSlice[3])
					existV6, _ := getNftTableSetElement("blacklist", cdSlice[4])
					return existV4 || existV6
				}(),
			}

			clients = append(clients, cd)
		}
	}

	return clients

}

func (ov *ovpn) getServer() ServerData {
	var sd ServerData

	data, err := ov.sendCommand("state")
	if err != nil {
		return sd
	}

	sateSlice := strings.Split(data, ",")
	if len(sateSlice) >= 3 {
		runDate, _ := strconv.ParseInt(sateSlice[0], 10, 64)
		sd.RunDate = time.Unix(runDate, 0).Format("2006-01-02 15:04:05")
		sd.Status = sateSlice[1]
		sd.StatusDesc = sateSlice[2]
		sd.Address = sateSlice[3]
	}

	data, err = ov.sendCommand("load-stats")
	if err != nil {
		return sd
	}

	statsSlice := strings.Split(data, ",")
	for _, v := range statsSlice {
		statsKeySlice := strings.Split(v, "=")

		switch statsKeySlice[0] {
		case "nclients":
			sd.Nclients = statsKeySlice[1]
		case "bytesin":
			in, _ := strconv.ParseFloat(statsKeySlice[1], 64)
			sd.BytesIn = tools.FormatBytes(in)
		case "bytesout":
			out, _ := strconv.ParseFloat(statsKeySlice[1], 64)
			sd.BytesOut = tools.FormatBytes(out)
		}
	}

	data, err = ov.sendCommand("version")
	if err != nil {
		return sd
	}

	for _, v := range strings.Split(data, "\n") {
		if strings.HasPrefix(v, "OpenVPN Version: ") {
			sd.Version = strings.TrimPrefix(v, "OpenVPN Version: ")
		}
	}

	return sd

}

func (ov *ovpn) killClient(cid string) {
	ov.sendCommand(fmt.Sprintf("client-kill %s HALT", cid))
}

func parseCrl(crlPath string) (*CertData, error) {
	crlData, err := os.ReadFile(crlPath)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(crlData)
	if block == nil {
		return nil, fmt.Errorf("无法解析证书文件")
	}

	crl, err := x509.ParseRevocationList(block.Bytes)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	expiresIn := crl.NextUpdate.Sub(now)

	var status string
	var expiresInStr string

	if now.After(crl.NextUpdate) {
		status = "已过期"
		expiresInStr = fmt.Sprintf("已过期 %d 天", int(now.Sub(crl.NextUpdate).Hours()/24))
	} else if expiresIn < 30*24*time.Hour {
		status = "即将过期"
		expiresInStr = fmt.Sprintf("%d 天后过期", int(expiresIn.Hours()/24))
	} else {
		status = "正常"
		expiresInStr = fmt.Sprintf("%d 天后过期", int(expiresIn.Hours()/24))
	}

	return &CertData{
		Name:      strings.TrimSuffix(filepath.Base(crlPath), filepath.Ext(crlPath)),
		Type:      "CRL证书",
		Subject:   "",
		Issuer:    crl.Issuer.String(),
		NotBefore: crl.ThisUpdate.Local().Format("2006-01-02 15:04:05"),
		NotAfter:  crl.NextUpdate.Local().Format("2006-01-02 15:04:05"),
		ExpiresIn: expiresInStr,
		Status:    status,
		SerialNo:  "",
	}, nil
}

func parseCert(certPath string) (*CertData, error) {
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return nil, fmt.Errorf("无法解析证书文件")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	expiresIn := cert.NotAfter.Sub(now)

	var status string
	var expiresInStr string

	if now.After(cert.NotAfter) {
		status = "已过期"
		expiresInStr = fmt.Sprintf("已过期 %d 天", int(now.Sub(cert.NotAfter).Hours()/24))
	} else if expiresIn < 30*24*time.Hour {
		status = "即将过期"
		expiresInStr = fmt.Sprintf("%d 天后过期", int(expiresIn.Hours()/24))
	} else {
		status = "正常"
		expiresInStr = fmt.Sprintf("%d 天后过期", int(expiresIn.Hours()/24))
	}

	certType := "客户端证书"
	if cert.IsCA {
		certType = "CA证书"
	} else if strings.Contains(cert.Subject.CommonName, "server") {
		certType = "服务器证书"
	}

	return &CertData{
		Name:      strings.TrimSuffix(filepath.Base(certPath), filepath.Ext(certPath)),
		Type:      certType,
		Subject:   cert.Subject.String(),
		Issuer:    cert.Issuer.String(),
		NotBefore: cert.NotBefore.Local().Format("2006-01-02 15:04:05"),
		NotAfter:  cert.NotAfter.Local().Format("2006-01-02 15:04:05"),
		ExpiresIn: expiresInStr,
		Status:    status,
		SerialNo:  cert.SerialNumber.String(),
	}, nil
}

func getCerts(ovData string) []CertData {
	cers := make([]CertData, 0)
	pkiDir := filepath.Join(ovData, "pki")

	caPath := filepath.Join(pkiDir, "ca.crt")
	if cert, err := parseCert(caPath); err == nil {
		cers = append(cers, *cert)
	} else {
		logger.Error(context.Background(), err.Error())
	}

	crlPath := filepath.Join(pkiDir, "crl.pem")
	if cert, err := parseCrl(crlPath); err == nil {
		cers = append(cers, *cert)
	} else {
		logger.Error(context.Background(), err.Error())
	}

	issuedDir := filepath.Join(pkiDir, "issued")
	if files, err := os.ReadDir(issuedDir); err == nil {
		for _, file := range files {
			if strings.HasSuffix(file.Name(), ".crt") {
				certPath := filepath.Join(issuedDir, file.Name())
				if cert, err := parseCert(certPath); err == nil {
					cers = append(cers, *cert)
				} else {
					logger.Error(context.Background(), err.Error())
				}
			}
		}
	} else {
		logger.Error(context.Background(), err.Error())
	}

	return cers
}

func isValidPassword(pw string) bool {
	lower := regexp.MustCompile(`[a-z]`)
	upper := regexp.MustCompile(`[A-Z]`)
	digit := regexp.MustCompile(`[0-9]`)
	special := regexp.MustCompile(`[!@#\$%\^&\*()_+\-=\[\]{};':"\\|,.<>\/?]`)

	count := 0
	if len(pw) >= 12 {
		count++
	}
	if lower.MatchString(pw) {
		count++
	}
	if upper.MatchString(pw) {
		count++
	}
	if digit.MatchString(pw) {
		count++
	}
	if special.MatchString(pw) {
		count++
	}

	return count == 5
}

func genRandomString(length int) string {
	if length < 1 {
		return ""
	}
	result := make([]byte, length)
	if _, err := rand.Read(result); err != nil {
		panic("secure random unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(result)[:length]
}

func IsLocalRequest(c *gin.Context) bool {
	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return false
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	return parsedIP.IsLoopback()
}

func AuthMiddleWare() gin.HandlerFunc { return authMiddleware() }

func init() {
	if strings.HasSuffix(os.Args[0], ".test") {
		return
	}
	initConfig()
	loadConfig()
}

func main() {
	ov := ovpn{
		address: ovManage,
	}

	var err error
	db, err = gorm.Open(sqlite.Open(filepath.Join(ovData, "ovpn.db")+"?_pragma=foreign_keys(1)"), &gorm.Config{
		Logger: logger,
	})

	if err != nil {
		panic(err)
	}

	c := cron.New()
	c.AddFunc("@daily", func() {
		configRequestMu.RLock()
		defer configRequestMu.RUnlock()
		db.Exec("DELETE FROM sessions WHERE expires_at <= ?", time.Now())
		err := History{}.Clear()
		if err != nil {
			logger.Error(context.Background(), err.Error())
		}
	})
	c.Start()

	if err := db.AutoMigrate(&Group{}); err != nil {
		panic("group schema migration failed")
	}
	if err := db.FirstOrCreate(&Group{Name: "Default", ParentID: nil}).Error; err != nil {
		panic("default group initialization failed")
	}
	if err := db.AutoMigrate(&User{}, &History{}, &Firewall{}); err != nil {
		panic("security schema migration failed")
	}

	r := setupRouter(ov)
	if err := r.Run(fmt.Sprintf(":%s", webPort)); err != nil {
		panic(err)
	}
}

func setupRouter(ov ovpn) *gin.Engine {
	store := newSessionStore(db)
	r := gin.New()
	r.SetTrustedProxies(nil)
	r.Use(gin.Recovery())
	r.Use(configRequestGuard())
	r.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {

		var statusColor, methodColor, resetColor string
		if param.IsOutputColor() {
			statusColor = param.StatusCodeColor()
			methodColor = param.MethodColor()
			resetColor = param.ResetColor()
		}

		if param.Latency > time.Minute {
			param.Latency = param.Latency.Truncate(time.Second)
		}
		return fmt.Sprintf("[OPENVPN-WEB] %v GIN |%s %3d %s| %13v | %15s |%s %-7s %s %#v\n%s",
			param.TimeStamp.Format("2006-01-02 15:04:05.000"),
			statusColor, param.StatusCode, resetColor,
			param.Latency,
			param.ClientIP,
			methodColor, param.Method, resetColor,
			param.Path,
			param.ErrorMessage,
		)
	}))

	store.Options(cookieOptions(7 * 24 * 3600))
	r.Use(sessions.Sessions("user_session", store))
	r.Use(CSRFMiddleware())

	// r.Use(gin.Recovery())

	templ := template.Must(template.New("").ParseFS(FS, "templates/*.html"))
	r.SetHTMLTemplate(templ)
	f, _ := fs.Sub(FS, "templates/static")
	r.StaticFS("/static", http.FS(f))

	r.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", gin.H{})
	})

	registerAuthRoutes(r)

	r.GET("/captcha", func(c *gin.Context) {
		key, mBase64, tBase64, err := getCaptcha()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"key": key, "master": mBase64, "thumb": tBase64})
	})

	r.Use(AuthMiddleWare())
	registerAccountRoutes(r)

	r.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)
		if user, ok := session.Get("user").(string); ok {
			if user == adminUsername {
				c.Redirect(302, "/admin")
				return
			}

			u := User{Username: user}.Info()
			if u.IsFirstLogin != nil && *u.IsFirstLogin {
				c.Redirect(302, "/login")
				return
			}
		}

		c.HTML(http.StatusOK, "client.html", conf.Client)
	})

	r.GET("/admin", func(c *gin.Context) {
		session := sessions.Default(c)
		if user, ok := session.Get("user").(string); ok {
			if user != adminUsername {
				c.Redirect(302, "/")
				return
			}
		}

		c.HTML(http.StatusOK, "index.html", gin.H{
			"server":  ov.getServer(),
			"sysUser": adminUsername,
			"version": "v" + version,
		})
	})

	r.GET("/settings", func(c *gin.Context) {
		var conf config
		viper.Unmarshal(&conf)

		c.JSON(http.StatusOK, conf)
	})

	r.POST("/settings", func(c *gin.Context) {
		u, ok := ownAccount(c)
		if !ok || !requireCurrentPassword(c, &u) {
			return
		}
		if err := c.Request.ParseForm(); err != nil {
			securityError(c, 400, "invalid form")
			return
		}
		for k, values := range c.Request.PostForm {
			if len(values) != 1 {
				securityError(c, 400, "duplicate setting")
				return
			}
			if err := validateVPNSetting(k, values[0]); err != nil {
				securityError(c, 400, err.Error())
				return
			}
			if k != "currentPass" && !allowedSetting(k) {
				securityError(c, 400, "unknown or protected setting")
				return
			}
		}
		staged := viper.New()
		staged.SetConfigType("json")
		staged.SetConfigFile(viper.ConfigFileUsed())
		staged.SetConfigPermissions(0600)
		if err := staged.MergeConfigMap(viper.AllSettings()); err != nil {
			securityError(c, 500, "settings unavailable")
			return
		}
		for k, vs := range c.Request.PostForm {
			if k == "currentPass" {
				continue
			}
			val := vs[0]
			if val == "" && (k == "system.base.admin_password" || k == "system.email.password" || k == "system.ldap.ldap_bind_password") {
				continue
			}
			switch k {
			case "system.base.admin_username":
				if strings.TrimSpace(val) == "" {
					securityError(c, 400, "administrator username required")
					return
				}
				if val != viper.GetString(k) {
					staged.Set("system.base.admin_session_version", secureToken())
				}
			case "system.base.admin_password":
				staged.Set("system.base.admin_session_version", secureToken())
				if !isValidPassword(val) || len(val) > 72 {
					securityError(c, 400, "password does not meet policy")
					return
				}
				ep, err := bcrypt.GenerateFromPassword([]byte(val), 12)
				if err != nil {
					securityError(c, 500, "password update failed")
					return
				}
				staged.Set("system.base.admin_first_login", false)
				val = string(ep)
			case "system.email.password":
				val, _ = aes.AesEncrypt(val, secretKey)
			case "system.base.max_duplicate_login":
				n, err := strconv.Atoi(val)
				if err != nil {
					n = 0
				}

				if n > 0 {
					cfg, err := initOvpnConfig()
					if err != nil {
						logger.Error(context.Background(), err.Error())
						return
					}

					statusLogPath := filepath.Join(ovData, "openvpn-status.log")
					if cfg.Get("status-version") != "3" || cfg.Get("status") != statusLogPath+" 1" {
						cfg.Set("status", statusLogPath+" 1")
						cfg.Set("status-version", "3")
						cfg.Save()

						ov.sendCommand("signal SIGHUP")
					}
				}
			case "openvpn.ovpn_subnet", "openvpn.ovpn_subnet6":
				_, _, err := net.ParseCIDR(val)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
					return
				}
			case "openvpn.ovpn_push_dns1", "openvpn.ovpn_push_dns2":
				if net.ParseIP(val) == nil {
					c.JSON(http.StatusInternalServerError, gin.H{"message": "invalid IP address: " + val})
					return
				}
			}

			switch val {
			case "true":
				staged.Set(k, true)
			case "false":
				staged.Set(k, false)
			default:
				staged.Set(k, val)
			}
		}
		if err := staged.WriteConfig(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}

		for _, key := range staged.AllKeys() {
			viper.Set(key, staged.Get(key))
		}
		loadConfig()
		upadteOvpnConfig()
		c.JSON(http.StatusOK, gin.H{"message": "更新成功"})
	})

	r.POST("/email/send", func(c *gin.Context) {
		email := c.PostForm("email")
		subject := c.PostForm("subject")
		content := c.PostForm("content")

		err := sendEmail(email, subject, content)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		} else {
			c.JSON(http.StatusOK, gin.H{"message": "发送成功"})
		}
	})

	ovpn := r.Group("/ovpn")
	{
		ovpn.StaticFS("/download", http.Dir(filepath.Join(ovData, "clients")))

		ovpn.POST("/server", func(c *gin.Context) {
			a := c.PostForm("action")

			switch a {
			case "settings":
				k := c.PostForm("key")
				v := c.PostForm("value")

				if k == "auth-user" {
					msg := "停用"
					if v == "true" {
						msg = "启用"
					}
					cmd := exec.Command("docker-entrypoint.sh", "auth", v)
					if out, err := cmd.CombinedOutput(); err != nil {
						if len(out) == 0 {
							out = []byte(err.Error())
						}
						logger.Error(context.Background(), string(out))
						c.JSON(http.StatusInternalServerError, gin.H{"message": fmt.Sprintf("%s用户认证失败", msg)})
					} else {
						ov.sendCommand("signal SIGHUP")
						c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("%s用户认证成功", msg)})
					}
				}
			case "renewCert":
				day := c.PostForm("day")

				cmd := exec.Command("docker-entrypoint.sh", "renewcert", day)
				if out, err := cmd.CombinedOutput(); err != nil {
					if len(out) == 0 {
						out = []byte(err.Error())
					}
					logger.Error(context.Background(), string(out))
					c.JSON(http.StatusInternalServerError, gin.H{"message": "更新证书失败"})
					return
				}

				ov.sendCommand("signal SIGHUP")
				c.JSON(http.StatusOK, gin.H{"message": "更新证书成功"})
			case "restartSrv":
				_, err := ov.sendCommand("signal SIGHUP")
				if err != nil {
					logger.Error(context.Background(), err.Error())
					c.JSON(http.StatusInternalServerError, gin.H{"message": "重启服务失败"})
					return
				}

				c.JSON(http.StatusOK, gin.H{"message": "重启服务成功"})
			case "getConfig":
				data, err := os.ReadFile(filepath.Join(ovData, "server.conf"))
				if err != nil {
					logger.Error(context.Background(), err.Error())
					c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
					return
				}

				c.JSON(http.StatusOK, gin.H{"content": string(data)})
			case "updateConfig":
				content := c.PostForm("content")

				if err := updateServerConfig(content); err != nil {
					securityError(c, 400, "configuration rejected: "+err.Error())
					return
				}
				c.JSON(http.StatusOK, gin.H{"message": "配置更新成功"})
			default:
				c.JSON(http.StatusUnprocessableEntity, gin.H{"message": "未知操作"})
			}

		})

		ovpn.POST("/kill", func(c *gin.Context) {
			cid := c.PostForm("cid")
			ov.killClient(cid)
			c.JSON(http.StatusOK, gin.H{"code": http.StatusOK})
		})

		ovpn.GET("/firewall", FirewallHandler)
		ovpn.POST("/firewall", FirewallHandler)
		ovpn.PATCH("/firewall", FirewallHandler)
		ovpn.DELETE("/firewall/:id", FirewallHandler)

		ovpn.POST("/login", func(c *gin.Context) {
			var u User
			c.ShouldBind(&u)
			u.MfaSecret = ""
			u.OvpnConfig = c.PostForm("common_name")

			err := u.Login(true)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
			} else {
				c.JSON(http.StatusOK, gin.H{"message": "登录成功"})
			}
		})

		ovpn.GET("/online-client", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"server": ov.getServer(), "clients": ov.getClient()})
		})

		ovpn.GET("/group", func(c *gin.Context) {
			var g Group
			c.JSON(http.StatusOK, g.All())
		})

		ovpn.GET("/group/:id", func(c *gin.Context) {
			var g Group
			c.JSON(http.StatusOK, g.Get(c.Param("id")))
		})

		ovpn.GET("/group/:id/users", func(c *gin.Context) {
			var auth bool
			var g Group

			gid := c.Param("id")

			cmd := exec.Command("egrep", "^auth-user-pass-verify", filepath.Join(ovData, "server.conf"))
			if err := cmd.Run(); err != nil {
				auth = false
			} else {
				auth = true
			}

			c.JSON(http.StatusOK, gin.H{"users": g.GetUsers(gid), "authUser": auth})
		})

		ovpn.POST("/group", func(c *gin.Context) {
			var g Group
			if err := c.ShouldBind(&g); err != nil {
				securityError(c, 400, "invalid group")
				return
			}
			if err := g.BeforeSave(db); err != nil {
				securityError(c, 400, err.Error())
				return
			}

			err := g.Create()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"message": "添加成功"})
		})

		ovpn.PATCH("/group", func(c *gin.Context) {
			var g Group
			if err := c.ShouldBind(&g); err != nil {
				securityError(c, 400, "invalid group")
				return
			}
			if err := g.BeforeSave(db); err != nil {
				securityError(c, 400, err.Error())
				return
			}

			if config, ok := c.Request.PostForm["config"]; ok {
				if config[0] == "" {
					db.Model(&g).Update("config", nil)
				}
			}

			err := g.Update()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"message": "更新成功"})
		})

		ovpn.DELETE("/group/:id", func(c *gin.Context) {
			var g Group
			c.ShouldBind(&g)

			err := g.Delete(c.Param("id"))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
		})

		ovpn.GET("/user", func(c *gin.Context) {
			var u User

			username := c.Query("username")
			if username != "" {
				u.Username = username
			}

			c.JSON(http.StatusOK, u.Info())
		})

		ovpn.GET("/user/:id", func(c *gin.Context) {
			var u User
			c.JSON(http.StatusOK, u.Get(c.Param("id")))
		})

		r.GET("/user/template", func(c *gin.Context) {
			c.Header("Content-Type", "text/csv")
			c.Header("Content-Disposition", "attachment; filename=user_template.csv")

			c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

			writer := csv.NewWriter(c.Writer)
			defer writer.Flush()

			writer.Write([]string{"username", "password", "name", "email", "is_enable", "expire_date", "ip_addr", "ovpn_config"})
			writer.Write([]string{"zhangsan", "123456", "张三", "zhangsan@example.com", "1", "2025-12-01/00:00:00", "10.8.0.222", "tt-gz.ovpn"})
			writer.Write([]string{"lisi", "123456", "李四", "lisi@example.com", "0", "", "", "tt-sh.ovpn"})
		})

		ovpn.GET("/user/export", func(c *gin.Context) {
			gid := c.Query("gid")

			fileName := fmt.Sprintf("user_%s.csv", time.Now().Format("20060102150405"))

			c.Header("Content-Type", "text/csv; charset=utf-8")
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
			c.Header("Cache-Control", "no-cache")

			c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

			writer := csv.NewWriter(c.Writer)
			header := []string{"ID", "用户名", "密码", "姓名", "节点", "启用", "过期时间", "IP地址", "配置文件", "MFA", "创建时间"}
			if err := writer.Write(header); err != nil {
				logger.Error(context.Background(), err.Error())
				return
			}
			writer.Flush()

			gQuery := db.Model(&Group{}).
				Select("id").
				Where(`
				parent_id = ?
				OR EXISTS (
					SELECT 1 FROM `+"`group`"+`
					WHERE id = ? AND parent_id IS NULL
				)
				`, gid, gid)

			// Load group names before opening the streaming user cursor. A nested
			// query per row can deadlock a bounded SQLite connection pool.
			var exportGroups []Group
			if err := db.Select("id", "name").Find(&exportGroups).Error; err != nil {
				securityError(c, 500, "export failed")
				return
			}
			groupNames := map[uint]string{}
			for _, group := range exportGroups {
				groupNames[group.ID] = group.Name
			}
			rows, err := db.Model(&User{}).Where("gid = ? OR gid IN (?)", gid, gQuery).Rows()
			if err != nil {
				return
			}
			defer rows.Close()

			for rows.Next() {
				var u User

				db.ScanRows(rows, &u)

				enable := "0"
				if *u.IsEnable {
					enable = "1"
				}

				record := []string{
					strconv.Itoa(int(u.ID)),
					u.Username,
					"",
					u.Name,
					groupNames[u.Gid],
					enable,
					u.ExpireDate,
					u.IpAddr,
					u.OvpnConfig,
					strconv.FormatBool(u.MfaSecret != ""),
					u.CreatedAt.Format("2006-01-02 15:04:05"),
				}

				if err := writer.Write(record); err != nil {
					logger.Error(context.Background(), err.Error())
					return
				}
			}
			writer.Flush()

			if err := writer.Error(); err != nil {
				logger.Error(context.Background(), err.Error())
			}
		})

		ovpn.POST("/user", func(c *gin.Context) {
			var u User
			c.ShouldBind(&u)
			u.MfaSecret = ""

			file, err := c.FormFile("file")
			if err != nil {
				if strings.Contains(err.Error(), "no such file") {
					c.JSON(http.StatusInternalServerError, gin.H{"message": "没有上传文件"})
					return
				}
			} else {
				gid := c.PostForm("gid")
				f, _ := file.Open()

				defer f.Close()

				reader := csv.NewReader(f)

				header, err := reader.Read()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
					return
				}

				if len(header) != 8 {
					c.JSON(http.StatusInternalServerError, gin.H{"message": "导入文件格式错误"})
					return
				}

				for {
					record, err := reader.Read()
					if err == io.EOF {
						break
					}

					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
						return
					}

					enable := record[4] == "1"
					gid64, err := strconv.ParseUint(gid, 10, 64)
					u := User{
						Username:   record[0],
						Password:   record[1],
						Name:       record[2],
						Email:      record[3],
						IsEnable:   &enable,
						ExpireDate: strings.Replace(record[5], "/", " ", 1),
						IpAddr:     record[6],
						OvpnConfig: record[7],
						Gid:        uint(gid64),
					}

					err = u.Create()
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
						return
					}
				}

				c.JSON(http.StatusOK, gin.H{"message": "导入用户成功"})
				return
			}

			if isFirstLogin, ok := c.Request.PostForm["isFirstLogin"]; ok {
				val := isFirstLogin[0] == "true"
				u.IsFirstLogin = &val
			}

			err = u.Create()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			} else {
				sendNotifyEmail := c.PostForm("sendNotifyEmail")
				if sendNotifyEmail == "true" {
					func() {
						var tpl *template.Template
						var buf bytes.Buffer

						tpl, err = template.ParseFS(FS, "templates/email.html")
						if err == nil {
							err = tpl.Execute(&buf, struct {
								Type     string
								Name     string
								Username string
								Password string
								SiteUrl  string
							}{
								Type:     "addUser",
								Name:     u.Name,
								Username: u.Username,
								Password: c.PostForm("password"),
								SiteUrl:  viper.GetString("system.base.site_url"),
							})
						}

						if err != nil {
							logger.Error(context.Background(), err.Error())
							return
						}

						sendEmail(u.Email, "用户开通通知", buf.String())
					}()
				}

				c.JSON(http.StatusOK, gin.H{"message": "添加用户成功"})
			}
		})

		ovpn.PATCH("/user", func(c *gin.Context) {
			var u User
			c.ShouldBind(&u)
			u.MfaSecret = ""

			if ipAddr, ok := c.Request.PostForm["ipAddr"]; ok {
				if ipAddr[0] == "" {
					db.Model(&u).Update("ip_addr", nil)
				}
			}

			if expireDate, ok := c.Request.PostForm["expireDate"]; ok {
				if expireDate[0] == "" {
					db.Model(&u).Update("expire_date", nil)
				}
			}

			err := u.Update()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			} else {
				sendNotifyEmail := c.PostForm("sendNotifyEmail")
				if sendNotifyEmail == "true" {
					func() {
						var cu User
						db.First(&cu, u.ID)

						if cu.Email != "" {
							var tpl *template.Template
							var buf bytes.Buffer

							tpl, err = template.ParseFS(FS, "templates/email.html")
							if err == nil {
								err = tpl.Execute(&buf, struct {
									Type     string
									Name     string
									Username string
									Password string
									SiteUrl  string
								}{
									Type:     "resetPass",
									Name:     cu.Name,
									Username: cu.Username,
									Password: c.PostForm("password"),
									SiteUrl:  viper.GetString("system.base.site_url"),
								})
							}

							if err != nil {
								logger.Error(context.Background(), err.Error())
								return
							}

							sendEmail(cu.Email, "用户密码重置通知", buf.String())
						} else {
							logger.Error(context.Background(), "发送邮件通知失败，用户没有配置邮箱地址")
						}
					}()
				}

				c.JSON(http.StatusOK, gin.H{"message": "用户更新成功"})
			}
		})

		ovpn.DELETE("/user/:id", func(c *gin.Context) {
			var u User
			id := c.Param("id")

			err := u.Delete(id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			} else {
				c.JSON(http.StatusOK, gin.H{"message": "删除用户成功"})
			}
		})

		ovpn.GET("/client", func(c *gin.Context) {
			clients := make([]ClientConfigData, 0)

			files, _ := os.ReadDir(filepath.Join(ovData, "clients"))
			for _, file := range files {
				finfo, _ := file.Info()

				f := ClientConfigData{
					Name:     strings.TrimSuffix(file.Name(), filepath.Ext(file.Name())),
					FullName: file.Name(),
					File:     fmt.Sprintf("/ovpn/download/%s", file.Name()),
					Date:     finfo.ModTime().Local().Format("2006-01-02 15:04:05"),
				}
				clients = append(clients, f)
			}

			sort.Slice(clients, func(i, j int) bool {
				return clients[i].Date < clients[j].Date
			})

			c.JSON(http.StatusOK, clients)

		})

		ovpn.GET("/client/:name/ccd", func(c *gin.Context) {
			name := c.Param("name")
			ccdDir := filepath.Join(ovData, "ccd")

			os.MkdirAll(ccdDir, 0755)

			ccdRoot, err := os.OpenRoot(ccdDir)
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			defer ccdRoot.Close()

			data, err := ccdRoot.ReadFile(name)
			if err != nil {
				if os.IsNotExist(err) {
					c.JSON(http.StatusOK, gin.H{"content": ""})
				} else {
					c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				}
				return
			}

			c.JSON(http.StatusOK, gin.H{"content": string(data)})
		})

		ovpn.GET("/client/:name/config", func(c *gin.Context) {
			name := c.Param("name")
			clientsDir := filepath.Join(ovData, "clients")

			clientsRoot, err := os.OpenRoot(clientsDir)
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			defer clientsRoot.Close()

			data, err := clientsRoot.ReadFile(name + ".ovpn")
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"content": string(data)})
		})

		ovpn.PUT("/client/:name/ccd", func(c *gin.Context) {
			name := c.Param("name")
			content := c.PostForm("content")
			if !connectionIdentity.MatchString(name) {
				securityError(c, 400, "invalid client name")
				return
			}
			if err := validateClientDirectives(content); err != nil {
				securityError(c, 400, err.Error())
				return
			}
			msg := "客户端更新成功"
			ccdDir := filepath.Join(ovData, "ccd")

			os.MkdirAll(ccdDir, 0755)

			cfg, err := initOvpnConfig()
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}

			if cfg.Get("client-config-dir") == "" {
				cfg.Set("client-config-dir", ccdDir)
				cfg.Save()

				msg += "（未启用CCD需要重启服务生效）"
			}

			ccdRoot, err := os.OpenRoot(ccdDir)
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			defer ccdRoot.Close()

			err = ccdRoot.WriteFile(name, []byte(content), 0644)
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"message": msg})
		})

		ovpn.PUT("/client/:name/config", func(c *gin.Context) {
			name := c.Param("name")
			content := c.PostForm("content")
			clientsDir := filepath.Join(ovData, "clients")

			clientsRoot, err := os.OpenRoot(clientsDir)
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			defer clientsRoot.Close()

			if !connectionIdentity.MatchString(name) {
				securityError(c, 400, "invalid client name")
				return
			}
			original, err := clientsRoot.ReadFile(name + ".ovpn")
			if err != nil {
				securityError(c, 400, "client configuration unavailable")
				return
			}
			if err = validateVPNConfigUpdate(string(original), content, true); err != nil {
				securityError(c, 400, err.Error())
				return
			}
			err = atomicRootConfig(clientsRoot, name+".ovpn", []byte(content))
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			} else {
				c.JSON(http.StatusOK, gin.H{"message": "客户端配置更新成功"})
			}
		})

		ovpn.POST("/client", func(c *gin.Context) {
			name := c.PostForm("name")
			serverAddr := c.PostForm("serverAddr")
			serverPort := c.PostForm("serverPort")
			config := c.PostForm("config")
			ccdConfig := c.PostForm("ccdConfig")
			mfa := c.DefaultPostForm("mfa", "false")
			if !connectionIdentity.MatchString(name) || !validVPNPort(serverPort) || (mfa != "true" && mfa != "false") {
				securityError(c, 400, "invalid client parameters")
				return
			}
			if err := validateClientDirectives(config); err != nil {
				securityError(c, 400, err.Error())
				return
			}
			if err := validateClientDirectives(ccdConfig); err != nil {
				securityError(c, 400, err.Error())
				return
			}

			clientsDir := filepath.Join(ovData, "clients")

			os.MkdirAll(clientsDir, 0755)

			clientsRoot, err := os.OpenRoot(clientsDir)
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			defer clientsRoot.Close()

			_, err = clientsRoot.Stat(name + ".ovpn")
			if err != nil {
				if os.IsNotExist(err) {
					cmd := exec.Command("docker-entrypoint.sh", "genclient", name, serverAddr, serverPort, config, ccdConfig, mfa)
					if out, err := cmd.CombinedOutput(); err != nil {
						if len(out) == 0 {
							out = []byte(err.Error())
						}
						logger.Error(context.Background(), string(out))
						c.JSON(http.StatusInternalServerError, gin.H{"message": "客户端添加失败"})
						return
					}

					if err := writeClientCCD(name, ccdConfig); err != nil {
						securityError(c, 500, "client generated but CCD persistence failed")
						return
					}
					c.JSON(http.StatusOK, gin.H{"message": "客户端添加成功"})
					return
				}

				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": "非法客户端名称"})
				return
			}

			c.JSON(http.StatusUnprocessableEntity, gin.H{"message": "客户端已存在"})
		})

		ovpn.DELETE("/client/:name", func(c *gin.Context) {
			name := c.Param("name")

			cmd := exec.Command("easyrsa", "--batch", "revoke", name)
			out, err := cmd.CombinedOutput()
			if err == nil {
				cmd = exec.Command("easyrsa", "gen-crl")
				if out, err = cmd.CombinedOutput(); err != nil {
					logger.Error(context.Background(), string(out))
					c.JSON(http.StatusInternalServerError, gin.H{"message": "更新CRL证书失败"})
				}
			} else {
				if len(out) == 0 {
					out = []byte(err.Error())
				}
				logger.Error(context.Background(), string(out))
				c.JSON(http.StatusInternalServerError, gin.H{"message": "删除客户端失败"})
				return
			}

			dataRoot, err := os.OpenRoot(ovData)
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			defer dataRoot.Close()

			dataRoot.Remove(filepath.Join("clients", fmt.Sprintf("%s.ovpn", name)))
			dataRoot.Remove(filepath.Join("ccd", name))

			c.JSON(http.StatusOK, gin.H{"message": "删除客户端成功"})
		})

		ovpn.GET("/history", func(c *gin.Context) {
			var h History
			var p Params

			c.ShouldBindQuery(&p)

			c.JSON(http.StatusOK, h.Query(p))
		})

		ovpn.POST("/history", func(c *gin.Context) {
			var h History
			c.ShouldBind(&h)

			err := h.Create()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			} else {
				c.JSON(http.StatusOK, gin.H{"message": "添加记录成功"})
			}
		})

		ovpn.GET("/history/export", func(c *gin.Context) {
			var p Params
			c.ShouldBindQuery(&p)

			fileName := fmt.Sprintf("history_%s.csv", time.Now().Format("20060102150405"))

			c.Header("Content-Type", "text/csv; charset=utf-8")
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
			c.Header("Cache-Control", "no-cache")

			c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

			writer := csv.NewWriter(c.Writer)
			header := []string{"ID", "用户名", "客户端", "VPN IP", "用户 IP", "下载流量", "上传流量", "上线时间", "在线时长", "创建时间"}
			if err := writer.Write(header); err != nil {
				logger.Error(context.Background(), err.Error())
				return
			}
			writer.Flush()

			query := db.Model(&History{})
			if p.Qt != "" {
				qt := strings.Split(p.Qt, ",")
				if len(qt) == 2 {
					query = query.Where("time_unix BETWEEN ? AND ?", qt[0], qt[1])
				}
			}

			rows, err := query.Rows()
			if err != nil {
				return
			}
			defer rows.Close()

			for rows.Next() {
				var h History

				db.ScanRows(rows, &h)
				record := []string{
					strconv.Itoa(int(h.ID)),
					h.Username,
					h.CommonName,
					h.Vip,
					h.Rip,
					tools.FormatBytes(h.BytesReceived),
					tools.FormatBytes(h.BytesSent),
					time.Unix(h.TimeUnix, 0).Format("2006-01-02 15:04:05"),
					(time.Duration(h.TimeDuration) * time.Second).String(),
					h.CreatedAt.Format("2006-01-02 15:04:05"),
				}

				if err := writer.Write(record); err != nil {
					logger.Error(context.Background(), err.Error())
					return
				}
			}
			writer.Flush()

			if err := writer.Error(); err != nil {
				logger.Error(context.Background(), err.Error())
			}
		})

		ovpn.GET("/certs", func(c *gin.Context) {
			c.JSON(http.StatusOK, getCerts(ovData))
		})
	}

	client := r.Group("/client")
	{
		client.GET("/userConfig", func(c *gin.Context) {
			var u User
			session := sessions.Default(c)
			if user, ok := session.Get("user").(string); ok {
				u.Username = user
			}

			u = u.Info()
			configName := u.OvpnConfig

			if ldapAuth {
				l, err := InitLdap()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
					return
				}

				lu, err := l.Get(u.Username)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
					return
				}

				configName = lu.OvpnConfig
			}

			if configName == "" {
				c.JSON(http.StatusInternalServerError, gin.H{"message": "该账号未指定配置文件，请联系管理员"})
				return
			}

			clientsDir := filepath.Join(ovData, "clients")

			clientsRoot, err := os.OpenRoot(clientsDir)
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			defer clientsRoot.Close()

			data, err := clientsRoot.ReadFile(configName)
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": "读取配置文件失败"})
				return
			}

			challengeLine := `static-challenge "Enter MFA code" 1`
			content := string(data)

			if u.MfaSecret != "" {
				if !strings.Contains(content, challengeLine) {
					if !strings.HasSuffix(content, "\n") {
						content += "\n"
					}
					content += challengeLine + "\n"
				}
			} else {
				content = strings.ReplaceAll(content, challengeLine+"\n", "")
			}

			cfg, err := initOvpnConfig()
			if err != nil {
				logger.Error(context.Background(), err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}

			if cfg.Get("auth-user-pass-verify") != "" {
				if strings.Contains(content, "#auth-user-pass") {
					content = strings.ReplaceAll(content, "#auth-user-pass", "auth-user-pass")
				}
			} else {
				if !strings.Contains(content, "#auth-user-pass") {
					content = strings.ReplaceAll(content, "auth-user-pass", "#auth-user-pass")
				}
			}

			c.JSON(http.StatusOK, gin.H{"filename": configName, "content": content})
		})

	}

	return r
}
