package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
)

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type refreshDoctorTokenRequest struct {
	HospitalCode string `json:"hospital_code" binding:"required"`
	Username     string `json:"username" binding:"required"`
	Password     string `json:"password" binding:"required"`
	CodeID       string `json:"code_id" binding:"required"`
	Captcha      string `json:"captcha" binding:"required"`
	PhoneCode    string `json:"phone_code" binding:"required"`
}

type sendPhoneCodeRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	CodeID   string `json:"code_id" binding:"required"`
	Captcha  string `json:"captcha" binding:"required"`
}

type startYZYLoginRequest struct {
	HospitalCode string `json:"hospital_code" binding:"required"`
}

type refreshYZYLoginRequest struct {
	FlowID string `json:"flow_id" binding:"required"`
}

type captchaData struct {
	CodeID      string `json:"code_id"`
	ImageBase64 string `json:"image_base64"`
	ContentType string `json:"content_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type yzyLoginData struct {
	FlowID        string `json:"flow_id"`
	PageURL       string `json:"page_url,omitempty"`
	QRImageBase64 string `json:"qr_image_base64"`
	ContentType   string `json:"content_type"`
	ExpiresIn     int    `json:"expires_in"`
}

type yzyLoginStatusData struct {
	Status  string     `json:"status"`
	Message string     `json:"message"`
	Result  *loginData `json:"result,omitempty"`
}

type phoneCodeData struct {
	NextCaptcha *captchaData `json:"next_captcha,omitempty"`
	ExpiresIn   int          `json:"expires_in"`
}

type loginData struct {
	Token        string `json:"token"`
	HospitalCode string `json:"hospital_code"`
	Username     string `json:"username"`
	Role         string `json:"role"`
}

type apiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type clientAccount struct {
	HospitalCode           string
	Username               string
	Role                   string
	PasswordHash           string
	BusinessToken          sql.NullString
	Enabled                bool
	ExpiresAt              sql.NullTime
	BusinessTokenEnabled   bool
	BusinessTokenExpiresAt sql.NullTime
}

type verificationCode struct {
	Code      string
	ExpiresAt time.Time
}

type verificationStore struct {
	mu         sync.Mutex
	captchas   map[string]verificationCode
	phoneCodes map[string]verificationCode
	ssoFlows   map[string]*ssoFlow
	yzyFlows   map[string]*yzyFlow
}

type ssoFlow struct {
	CodeID      string
	Phone       string
	Client      *http.Client
	LastTouched time.Time
}

type yzyFlow struct {
	ID           string
	HospitalCode string
	Client       *http.Client
	QRKey        string
	LastStatus   string
	Status       string
	Message      string
	Result       *loginData
	ExpiresAt    time.Time
}

type ssoResponse struct {
	Code int             `json:"code"`
	Desc string          `json:"desc"`
	Data json.RawMessage `json:"data"`
}

type provinceClientInfoResponse struct {
	Code int `json:"code"`
	Data struct {
		ClientID string `json:"clientId"`
		URL      string `json:"url"`
	} `json:"data"`
	Desc string `json:"desc"`
}

type gdPlatformMobileResponse struct {
	Code int `json:"code"`
	Data struct {
		LoginInfo struct {
			AccessToken string `json:"access_token"`
		} `json:"loginInfo"`
	} `json:"data"`
	Desc string `json:"desc"`
}

type yzyQRCodeKeyResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	QRCodeKey string `json:"qrcode_key"`
}

type yzyQRCodePollResponse struct {
	Status   string `json:"status"`
	AuthCode string `json:"auth_code"`
	Message  string `json:"message"`
}

type yzyVerifyResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Mobile      string `json:"mobile"`
		StoreCode   string `json:"storeCode"`
		NextProcess string `json:"nextProcess"`
	} `json:"data"`
}

type rzLoginByCodeResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Data    struct {
		RedirectURI string `json:"redirect_uri"`
	} `json:"data"`
}

const (
	captchaExpiresIn   = 5 * time.Minute
	phoneCodeExpiresIn = 5 * time.Minute
	yzyLoginExpiresIn  = 15 * time.Minute
)

func main() {
	loadEnvFile(".env")

	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		log.Fatal("本地登录数据库信息缺失")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}
	if err := ensureBatchSchema(context.Background(), db); err != nil {
		log.Fatal("初始化批量查询数据表失败:", err)
	}

	store := newVerificationStore()
	batchManager := newBatchManager(db)
	if err := batchManager.RecoverInterruptedJobs(context.Background()); err != nil {
		log.Println("恢复中断的批量查询任务失败:", err)
	}

	// Gin框架
	router := gin.New()
	router.Use(gin.Recovery())
	if businessDebugEnabled() {
		router.Use(gin.Logger())
	}
	if err := router.SetTrustedProxies(nil); err != nil {
		log.Fatal(err)
	}
	api := router.Group("/api")
	{
		api.GET("/health", healthHandler)
		api.POST("/login", localLoginHandler(db))

		batchJobs := api.Group("/batch-jobs")
		{
			batchJobs.POST("", createBatchJobHandler(db))
			batchJobs.GET("", listBatchJobsHandler(db))
			batchJobs.GET("/:id", getBatchJobHandler(db))
			batchJobs.POST("/:id/start", startBatchJobHandler(batchManager))
			batchJobs.POST("/:id/pause", pauseBatchJobHandler(batchManager))
			batchJobs.POST("/:id/resume", resumeBatchJobHandler(batchManager))
			batchJobs.POST("/:id/stop", stopBatchJobHandler(batchManager))
			batchJobs.POST("/:id/retry", retryBatchJobHandler(db, batchManager))
			batchJobs.GET("/:id/export", exportBatchJobHandler(db))
		}

		doctorToken := api.Group("/doctor-token")
		{
			doctorToken.GET("/captcha", captchaHandler(store))
			doctorToken.POST("/send-phone-code", sendPhoneCodeHandler(db, store))
			doctorToken.POST("/refresh", refreshDoctorTokenHandler(db, store))
			doctorToken.POST("/yzy/start", startYZYLoginHandler(db, store))
			doctorToken.POST("/yzy/refresh", refreshYZYLoginHandler(store))
			doctorToken.GET("/yzy/page", yzyLoginPageHandler(store))
			doctorToken.GET("/yzy/style.css", yzyLoginStyleHandler)
			doctorToken.POST("/yzy/complete", completeYZYLoginHandler(db, store))
			doctorToken.GET("/yzy/status/:flow_id", yzyLoginStatusHandler(db, store))
		}
	}

	addr := os.Getenv("SERVER_ADDR")
	if addr == "" {
		addr = os.Getenv("SEVER_ADDR")
	}
	if addr == "" {
		addr = ":8080"
	}

	log.Println("后端接口跑在", addr)
	if err := router.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, apiResponse{Code: 0, Message: "ok"})
}

func localLoginHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "账号和密码不能为空"})
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "账号和密码不能为空"})
			return
		}

		account, status, message, err := authenticateLocalLogin(c.Request.Context(), db, req.Username, req.Password)
		if err != nil {
			log.Println("登录查询失败:", err)
			c.JSON(status, apiResponse{Code: status, Message: message})
			return
		}
		if status != 0 {
			c.JSON(status, apiResponse{Code: status, Message: message})
			return
		}

		c.JSON(http.StatusOK, apiResponse{
			Code:    0,
			Message: "登录成功",
			Data: &loginData{
				Token:        account.BusinessToken.String,
				HospitalCode: account.HospitalCode,
				Username:     account.Username,
				Role:         account.Role,
			},
		})
	}
}

func captchaHandler(store *verificationStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		captcha, err := store.NewSSOCaptcha()
		if err != nil {
			log.Println("获取真实图形验证码失败:", err)
			c.JSON(http.StatusBadGateway, apiResponse{Code: 502, Message: "获取真实图形验证码失败"})
			return
		}
		c.JSON(http.StatusOK, apiResponse{
			Code:    0,
			Message: "ok",
			Data:    captcha,
		})
	}
}

func sendPhoneCodeHandler(db *sql.DB, store *verificationStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req sendPhoneCodeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "账号、密码和图形验证码不能为空"})
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		req.CodeID = strings.TrimSpace(req.CodeID)
		req.Captcha = strings.TrimSpace(req.Captcha)
		if req.Username == "" || req.Password == "" || req.CodeID == "" || req.Captcha == "" {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "账号、密码和图形验证码不能为空"})
			return
		}

		nextCaptcha, err := store.SendSSOPhoneCode(req.Username, req.Password, req.CodeID, req.Captcha)
		if err != nil {
			log.Println("真实手机验证码发送失败:", err)
			c.JSON(http.StatusBadGateway, apiResponse{Code: 502, Message: err.Error()})
			return
		}

		c.JSON(http.StatusOK, apiResponse{
			Code:    0,
			Message: "手机验证码已发送",
			Data: phoneCodeData{
				NextCaptcha: nextCaptcha,
				ExpiresIn:   int(phoneCodeExpiresIn.Seconds()),
			},
		})
	}
}

func refreshDoctorTokenHandler(db *sql.DB, store *verificationStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req refreshDoctorTokenRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "医院编号、医生账号、密码和验证码不能为空"})
			return
		}

		req.HospitalCode = strings.TrimSpace(req.HospitalCode)
		req.Username = strings.TrimSpace(req.Username)
		req.CodeID = strings.TrimSpace(req.CodeID)
		req.Captcha = strings.TrimSpace(req.Captcha)
		req.PhoneCode = strings.TrimSpace(req.PhoneCode)
		if req.HospitalCode == "" || req.Username == "" || req.Password == "" || req.CodeID == "" || req.Captcha == "" || req.PhoneCode == "" {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "医院编号、医生账号、密码、图形验证码和手机验证码不能为空"})
			return
		}

		result, err := store.LoginSSO(c.Request.Context(), req.Username, req.Password, req.CodeID, req.Captcha, req.PhoneCode)
		if err != nil {
			log.Println("真实登录失败:", err)
			c.JSON(http.StatusBadGateway, apiResponse{Code: 502, Message: err.Error()})
			return
		}

		if err := updateBusinessToken(c.Request.Context(), db, req.HospitalCode, result.Token); err != nil {
			log.Println("业务token写库失败:", err)
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "医生token已获取，但写入数据库失败"})
			return
		}

		result.HospitalCode = req.HospitalCode

		c.JSON(http.StatusOK, apiResponse{
			Code:    0,
			Message: "医生token已更新",
			Data:    result,
		})
	}
}

func startYZYLoginHandler(db *sql.DB, store *verificationStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req startYZYLoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "医院编号不能为空"})
			return
		}

		req.HospitalCode = strings.TrimSpace(req.HospitalCode)
		if req.HospitalCode == "" {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "医院编号不能为空"})
			return
		}
		if _, err := findBusinessTokenByHospital(c.Request.Context(), db, req.HospitalCode); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				c.JSON(http.StatusNotFound, apiResponse{Code: 404, Message: "医院编号未配置"})
				return
			}
			log.Println("医院编号查询失败:", err)
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "服务器错误"})
			return
		}

		if strings.TrimSpace(os.Getenv("YZY_APPID")) == "" {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "后端未配置YZY_APPID，无法生成粤政易扫码页"})
			return
		}

		flow, err := store.newYZYFlow(req.HospitalCode)
		if err != nil {
			log.Println("创建粤政易扫码会话失败:", err)
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "创建扫码会话失败"})
			return
		}

		qrKey, err := getYZYQRCodeKey(c.Request.Context(), flow.Client)
		if err != nil {
			store.failYZYFlow(flow.ID, err.Error())
			c.JSON(http.StatusBadGateway, apiResponse{Code: 502, Message: err.Error()})
			return
		}
		flow.QRKey = qrKey

		qrImage, contentType, err := getYZYQRCodeImage(c.Request.Context(), flow.Client, qrKey)
		if err != nil {
			store.failYZYFlow(flow.ID, err.Error())
			c.JSON(http.StatusBadGateway, apiResponse{Code: 502, Message: err.Error()})
			return
		}

		pageURL := requestBaseURL(c.Request) + "/api/doctor-token/yzy/page?flow_id=" + url.QueryEscape(flow.ID)
		c.JSON(http.StatusOK, apiResponse{
			Code:    0,
			Message: "ok",
			Data: yzyLoginData{
				FlowID:        flow.ID,
				PageURL:       pageURL,
				QRImageBase64: base64.StdEncoding.EncodeToString(qrImage),
				ContentType:   contentType,
				ExpiresIn:     int(yzyLoginExpiresIn.Seconds()),
			},
		})
	}
}

func yzyLoginPageHandler(store *verificationStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		flowID := strings.TrimSpace(c.Query("flow_id"))
		flow := store.getYZYFlow(flowID)
		if flow == nil {
			c.Data(http.StatusNotFound, "text/html; charset=utf-8", []byte("<!doctype html><meta charset=\"utf-8\"><body>扫码会话已失效，请回到客户端重新打开。</body>"))
			return
		}

		qrKey, err := getYZYQRCodeKey(c.Request.Context(), flow.Client)
		if err != nil {
			store.failYZYFlow(flowID, err.Error())
			c.Data(http.StatusBadGateway, "text/html; charset=utf-8", []byte("<!doctype html><meta charset=\"utf-8\"><body>获取粤政易二维码失败，请回到客户端重试。</body>"))
			return
		}

		qrHost := yzyQRHost()
		qrURL := yzyQRCodeImageURL(qrKey)
		page := fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>粤政易扫码登录</title>
<style>
body{margin:0;background:#eef2f7;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#303640}
.wrap{min-height:100vh;display:flex;align-items:center;justify-content:center}
.panel{width:420px;background:white;border-radius:12px;box-shadow:0 12px 35px rgba(23,31,45,.12);padding:28px;text-align:center}
h1{font-size:22px;margin:0 0 22px}
.qrcode{width:260px;height:260px;object-fit:contain}
.tip{margin-top:16px;color:#697386;font-size:15px}
.state{margin-top:12px;color:#1784ff;font-size:14px}
</style>
</head>
<body>
<div class="wrap">
  <div class="panel">
    <h1>粤政易扫码登录</h1>
    <img class="qrcode" src="%s" alt="粤政易登录二维码">
    <div class="tip">请使用粤政易 APP 扫码登录</div>
    <div id="state" class="state">等待扫码...</div>
  </div>
</div>
<script>
const flowID = %q;
const qrHost = %q;
const qrKey = %q;
const state = document.getElementById("state");
let completed = false;
let pollFailures = 0;

function jsonp(url, callbackName, timeoutMs) {
  return new Promise(function(resolve, reject) {
    const script = document.createElement("script");
    const timer = setTimeout(function() {
      cleanup();
      reject(new Error("timeout"));
    }, timeoutMs || 15000);

    function cleanup() {
      clearTimeout(timer);
      delete window[callbackName];
      if (script.parentNode) script.parentNode.removeChild(script);
    }

    window[callbackName] = function(payload) {
      cleanup();
      resolve(payload || {});
    };
    script.onerror = function() {
      cleanup();
      reject(new Error("load error"));
    };
    script.src = url;
    document.body.appendChild(script);
  });
}

async function completeLogin(code) {
  completed = true;
  state.textContent = "扫码成功，正在登录...";
  try {
    const resp = await fetch("/api/doctor-token/yzy/complete", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({flow_id: flowID, code})
    });
    const result = await resp.json();
    if (result.code === 0) {
      state.textContent = "登录成功，请回到客户端。";
    } else {
      state.textContent = result.message || "登录失败，请回到客户端重试。";
    }
  } catch (err) {
    state.textContent = "登录失败，请回到客户端重试。";
  }
}

async function poll(lastStatus) {
  if (completed) return;
  const callbackName = "jsonpCallback" + Date.now() + Math.floor(Math.random() * 1000);
  let url = qrHost.replace(/\/$/, "") + "/wwopen/sso/l/qrConnect?key=" + encodeURIComponent(qrKey);
  if (lastStatus) url += "&lastStatus=" + encodeURIComponent(lastStatus);
  url += "&callback=" + callbackName + "&_=" + Date.now();

  try {
    const result = await jsonp(url, callbackName, 15000);
    pollFailures = 0;
    switch (result.status) {
    case "QRCODE_SCAN_SUCC":
      if (result.auth_code) {
        completeLogin(result.auth_code);
      } else {
        state.textContent = "扫码成功但未返回授权码，请重新打开扫码页。";
      }
      break;
    case "QRCODE_SCAN_ING":
      state.textContent = "扫描成功，请在粤政易中确认登录。";
      setTimeout(function() { poll(result.status); }, 2000);
      break;
    case "QRCODE_SCAN_FAIL":
      state.textContent = "你已取消此次登录，可以重新打开扫码页再试。";
      setTimeout(function() { poll(result.status); }, 2000);
      break;
    case "QRCODE_SCAN_ERR":
      state.textContent = "二维码已过期，请回到客户端重新打开。";
      break;
    default:
      state.textContent = "等待扫码...";
      setTimeout(function() { poll(result.status); }, 2000);
    }
  } catch (err) {
    pollFailures++;
    if (pollFailures >= 10) {
      state.textContent = "扫码状态查询失败，请回到客户端重试。";
      return;
    }
    setTimeout(function() { poll(lastStatus); }, 6000);
  }
}

setTimeout(function() { poll(""); }, 2000);
</script>
</body>
</html>`, htmlEscape(qrURL), flowID, qrHost, qrKey)
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
	}
}

func refreshYZYLoginHandler(store *verificationStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req refreshYZYLoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "扫码会话不能为空"})
			return
		}

		flowID := strings.TrimSpace(req.FlowID)
		flow := store.getYZYFlow(flowID)
		if flow == nil {
			c.JSON(http.StatusGone, apiResponse{Code: 410, Message: "扫码会话已失效，请重新打开扫码登录"})
			return
		}
		if flow.Status == "success" {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "扫码登录已完成，无需刷新二维码"})
			return
		}

		qrKey, err := getYZYQRCodeKey(c.Request.Context(), flow.Client)
		if err != nil {
			store.failYZYFlow(flowID, err.Error())
			c.JSON(http.StatusBadGateway, apiResponse{Code: 502, Message: err.Error()})
			return
		}

		qrImage, contentType, err := getYZYQRCodeImage(c.Request.Context(), flow.Client, qrKey)
		if err != nil {
			store.failYZYFlow(flowID, err.Error())
			c.JSON(http.StatusBadGateway, apiResponse{Code: 502, Message: err.Error()})
			return
		}

		store.resetYZYFlow(flowID, qrKey)
		c.JSON(http.StatusOK, apiResponse{
			Code:    0,
			Message: "ok",
			Data: yzyLoginData{
				FlowID:        flowID,
				QRImageBase64: base64.StdEncoding.EncodeToString(qrImage),
				ContentType:   contentType,
				ExpiresIn:     int(yzyLoginExpiresIn.Seconds()),
			},
		})
	}
}

func completeYZYLoginHandler(db *sql.DB, store *verificationStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			FlowID string `json:"flow_id" binding:"required"`
			Code   string `json:"code" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "扫码参数不能为空"})
			return
		}

		flow := store.getYZYFlow(req.FlowID)
		if flow == nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "扫码会话已失效，请重新扫码"})
			return
		}

		result, err := loginByYZYCode(c.Request.Context(), flow.Client, strings.TrimSpace(req.Code))
		if err != nil {
			store.failYZYFlow(req.FlowID, err.Error())
			log.Println("粤政易扫码登录失败:", err)
			c.JSON(http.StatusBadGateway, apiResponse{Code: 502, Message: err.Error()})
			return
		}

		if err := updateBusinessToken(c.Request.Context(), db, flow.HospitalCode, result.Token); err != nil {
			store.failYZYFlow(req.FlowID, "医生token已获取，但写入数据库失败")
			log.Println("粤政易扫码token写库失败:", err)
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "医生token已获取，但写入数据库失败"})
			return
		}

		result.HospitalCode = flow.HospitalCode
		store.completeYZYFlow(req.FlowID, result)
		c.JSON(http.StatusOK, apiResponse{Code: 0, Message: "粤政易扫码登录成功", Data: result})
	}
}

func yzyLoginStatusHandler(db *sql.DB, store *verificationStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		flowID := strings.TrimSpace(c.Param("flow_id"))
		flow := store.getYZYFlow(flowID)
		if flow == nil {
			c.JSON(http.StatusGone, apiResponse{Code: 410, Message: "扫码会话已失效"})
			return
		}
		if flow.Status == "pending" && flow.QRKey != "" {
			poll, err := getYZYQRCodeStatus(c.Request.Context(), flow.Client, flow.QRKey, flow.LastStatus)
			if err != nil {
				log.Println("粤政易扫码状态查询失败:", err)
				flow.Message = "扫码状态查询失败"
			} else {
				flow.LastStatus = poll.Status
				switch poll.Status {
				case "QRCODE_SCAN_SUCC":
					if poll.AuthCode == "" {
						store.failYZYFlow(flowID, "粤政易扫码成功但未返回授权码")
						break
					}
					result, err := loginByYZYCode(c.Request.Context(), flow.Client, poll.AuthCode)
					if err != nil {
						store.failYZYFlow(flowID, err.Error())
						log.Println("粤政易扫码登录失败:", err)
						break
					}
					if err := updateBusinessToken(c.Request.Context(), db, flow.HospitalCode, result.Token); err != nil {
						store.failYZYFlow(flowID, "医生token已获取，但写入数据库失败")
						log.Println("粤政易扫码token写库失败:", err)
						break
					}
					result.HospitalCode = flow.HospitalCode
					store.completeYZYFlow(flowID, result)
				case "QRCODE_SCAN_ING":
					flow.Message = "扫描成功，请在粤政易中确认登录"
				case "QRCODE_SCAN_FAIL":
					flow.Message = "你已取消此次登录，可以重新扫码"
				case "QRCODE_SCAN_ERR":
					store.expireYZYFlow(flowID)
				default:
					flow.Message = "等待扫码"
				}
			}
		}

		c.JSON(http.StatusOK, apiResponse{
			Code:    0,
			Message: "ok",
			Data: yzyLoginStatusData{
				Status:  flow.Status,
				Message: flow.Message,
				Result:  flow.Result,
			},
		})
	}
}

func yzyLoginStyleHandler(c *gin.Context) {
	c.Data(http.StatusOK, "text/css; charset=utf-8", []byte(`
.impowerBox .qrcode {width: 180px;}
.impowerBox .title {display: none;}
.impowerBox .info {width: 180px;}
.status_icon {display: none !important}
.impowerBox .status {text-align: center;margin-top: 0;}
.impowerBox .status .status_txt {text-align: center;margin-left: -27px;}
.impowerBox .status .status_txt h4 {color: #4490d6;}
.impowerBox .status.status_browser {text-align: center;padding: 25px 0;}
`))
}

func authenticateLocalLogin(ctx context.Context, db *sql.DB, username, password string) (*clientAccount, int, string, error) {
	account, err := findAccount(ctx, db, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, http.StatusUnauthorized, "账号或密码错误", nil
		}
		return nil, http.StatusInternalServerError, "服务器错误", err
	}

	if !account.Enabled {
		return nil, http.StatusForbidden, "账号已禁用", nil
	}

	if account.ExpiresAt.Valid && time.Now().After(account.ExpiresAt.Time) {
		return nil, http.StatusForbidden, "授权已过期", nil
	}

	if sha256Hex(password) != account.PasswordHash {
		return nil, http.StatusUnauthorized, "账号或密码错误", nil
	}

	return account, 0, "", nil
}

func authenticateAccount(ctx context.Context, db *sql.DB, username, password string) (*clientAccount, int, string, error) {
	account, err := findAccount(ctx, db, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, http.StatusUnauthorized, "账号或密码错误", nil
		}
		return nil, http.StatusInternalServerError, "服务器错误", err
	}

	if !account.Enabled {
		return nil, http.StatusForbidden, "账号已禁用", nil
	}

	if account.ExpiresAt.Valid && time.Now().After(account.ExpiresAt.Time) {
		return nil, http.StatusForbidden, "授权已过期", nil
	}

	if !account.BusinessTokenEnabled {
		return nil, http.StatusForbidden, "业务token已禁用", nil
	}

	if account.BusinessTokenExpiresAt.Valid && time.Now().After(account.BusinessTokenExpiresAt.Time) {
		return nil, http.StatusForbidden, "业务token已过期", nil
	}

	if strings.TrimSpace(account.BusinessToken.String) == "" {
		return nil, http.StatusForbidden, "业务token为空", nil
	}

	if sha256Hex(password) != account.PasswordHash {
		return nil, http.StatusUnauthorized, "账号或密码错误", nil
	}

	return account, 0, "", nil
}

func findAccount(ctx context.Context, db *sql.DB, username string) (*clientAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var account clientAccount
	err := db.QueryRowContext(ctx, `
		SELECT
			c.hospital_code,
			c.username,
			c.password_hash,
			COALESCE(c.role, 'user'),
			COALESCE(t.business_token, ''),
			c.enabled,
			c.expires_at,
			t.enabled,
			t.expires_at
		FROM hospital_clients c
		INNER JOIN hospital_business_tokens t ON t.hospital_code = c.hospital_code
		WHERE c.username = ?
		LIMIT 1
	`, username).Scan(
		&account.HospitalCode,
		&account.Username,
		&account.PasswordHash,
		&account.Role,
		&account.BusinessToken,
		&account.Enabled,
		&account.ExpiresAt,
		&account.BusinessTokenEnabled,
		&account.BusinessTokenExpiresAt,
	)
	if err != nil {
		return nil, err
	}

	return &account, nil
}

func updateBusinessToken(ctx context.Context, db *sql.DB, hospitalCode, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	result, err := db.ExecContext(ctx, `
		UPDATE hospital_business_tokens
		SET business_token = ?,
			enabled = 1,
			expires_at = NULL,
			last_used_at = NOW(),
			updated_at = NOW()
		WHERE hospital_code = ?
	`, token, hospitalCode)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func findBusinessTokenByHospital(ctx context.Context, db *sql.DB, hospitalCode string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var token string
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(business_token, '')
		FROM hospital_business_tokens
		WHERE hospital_code = ?
		LIMIT 1
	`, hospitalCode).Scan(&token)
	return token, err
}

func (s *verificationStore) NewSSOCaptcha() (*captchaData, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	flow := &ssoFlow{
		Client: &http.Client{
			Timeout: 20 * time.Second,
			Jar:     jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		LastTouched: time.Now(),
	}

	return s.refreshSSOCaptcha(flow, "")
}

func (s *verificationStore) SendSSOPhoneCode(username, password, codeID, captcha string) (*captchaData, error) {
	flow := s.getSSOFlow(codeID)
	if flow == nil {
		return nil, errors.New("图形验证码已失效，请刷新")
	}

	apiURL := ssoURL("/sso/verifyUserNameAndPassword")
	params := apiURL.Query()
	params.Set("validateCode", captcha)
	params.Set("codeId", codeID)
	apiURL.RawQuery = params.Encode()

	payload := map[string]string{
		"account":   username,
		"pwd":       encryptPasswordForSSO(password),
		"loginType": "PC",
	}

	result, raw, err := postSSOJSON(flow.Client, apiURL.String(), payload)
	if err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(defaultString(result.Desc, strings.TrimSpace(string(raw)), "发送手机验证码失败"))
	}

	phone := parseJSONString(result.Data)
	if phone == "" {
		phone = strings.Trim(strings.TrimSpace(string(result.Data)), `"`)
	}
	flow.Phone = phone

	return s.refreshSSOCaptcha(flow, codeID)
}

func (s *verificationStore) LoginSSO(ctx context.Context, username, password, codeID, captcha, phoneCode string) (*loginData, error) {
	flow := s.getSSOFlow(codeID)
	if flow == nil {
		return nil, errors.New("登录会话已失效，请刷新图形验证码后重试")
	}

	if err := checkSSOCaptchaAndPassword(flow.Client, username, password, codeID, captcha); err != nil {
		return nil, err
	}

	if err := checkSSOLoginStatus(flow.Client, username); err != nil {
		return nil, err
	}

	token, err := submitSSOLogin(ctx, flow.Client, username, password, codeID, captcha, phoneCode, flow.Phone)
	if err != nil {
		return nil, err
	}

	s.deleteSSOFlow(codeID)
	return &loginData{
		Token:    token,
		Username: username,
	}, nil
}

func (s *verificationStore) newYZYFlow(hospitalCode string) (*yzyFlow, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	flow := &yzyFlow{
		ID:           randomID(),
		HospitalCode: hospitalCode,
		Client: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		Status:    "pending",
		Message:   "等待扫码",
		ExpiresAt: time.Now().Add(yzyLoginExpiresIn),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	s.yzyFlows[flow.ID] = flow
	return flow, nil
}

func (s *verificationStore) getYZYFlow(flowID string) *yzyFlow {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	flow := s.yzyFlows[flowID]
	if flow != nil && time.Now().After(flow.ExpiresAt) {
		delete(s.yzyFlows, flowID)
		return nil
	}
	return flow
}

func (s *verificationStore) completeYZYFlow(flowID string, result *loginData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if flow := s.yzyFlows[flowID]; flow != nil {
		flow.Status = "success"
		flow.Message = "粤政易扫码登录成功"
		flow.Result = result
	}
}

func (s *verificationStore) resetYZYFlow(flowID, qrKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if flow := s.yzyFlows[flowID]; flow != nil {
		flow.QRKey = qrKey
		flow.LastStatus = ""
		flow.Status = "pending"
		flow.Message = "等待扫码"
		flow.ExpiresAt = time.Now().Add(yzyLoginExpiresIn)
	}
}

func (s *verificationStore) expireYZYFlow(flowID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if flow := s.yzyFlows[flowID]; flow != nil {
		flow.Status = "expired"
		flow.Message = "二维码已过期"
	}
}

func (s *verificationStore) failYZYFlow(flowID, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if flow := s.yzyFlows[flowID]; flow != nil {
		flow.Status = "failed"
		flow.Message = message
	}
}

func (s *verificationStore) refreshSSOCaptcha(flow *ssoFlow, oldCodeID string) (*captchaData, error) {
	codeID := randomUUID()
	apiURL := ssoURL("/sso/getVerifyCode")
	params := apiURL.Query()
	params.Set("codeId", codeID)
	apiURL.RawQuery = params.Encode()

	req, err := http.NewRequest(http.MethodGet, apiURL.String(), nil)
	if err != nil {
		return nil, err
	}
	setSSOHeaders(req)

	resp, err := flow.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("真实验证码接口异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return nil, fmt.Errorf("真实验证码接口返回类型异常: %s", contentType)
	}

	flow.CodeID = codeID
	flow.LastTouched = time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	if oldCodeID != "" {
		delete(s.ssoFlows, oldCodeID)
	}
	s.ssoFlows[codeID] = flow

	return &captchaData{
		CodeID:      codeID,
		ImageBase64: base64.StdEncoding.EncodeToString(body),
		ContentType: contentType,
		ExpiresIn:   int(captchaExpiresIn.Seconds()),
	}, nil
}

func (s *verificationStore) getSSOFlow(codeID string) *ssoFlow {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	flow := s.ssoFlows[codeID]
	if flow != nil {
		flow.LastTouched = time.Now()
	}
	return flow
}

func (s *verificationStore) deleteSSOFlow(codeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ssoFlows, codeID)
}

func checkSSOCaptchaAndPassword(client *http.Client, username, password, codeID, captcha string) error {
	apiURL := ssoURL("/sso/checkValidateCodeAndPwd")
	params := apiURL.Query()
	params.Set("validateCode", captcha)
	params.Set("codeId", codeID)
	apiURL.RawQuery = params.Encode()

	payload := map[string]string{
		"account":   username,
		"pwd":       encryptPasswordForSSO(password),
		"loginType": "PC",
	}

	result, raw, err := postSSOJSON(client, apiURL.String(), payload)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(string(raw))
	if result.Code != 0 && text != "" && text != "false" && text != "null" {
		return errors.New(defaultString(result.Desc, text, "账号密码或图形验证码校验失败"))
	}
	return nil
}

func checkSSOLoginStatus(client *http.Client, username string) error {
	apiURL := ssoURL("/sso/checkLoginStatus")
	payload := map[string]string{
		"account":   username,
		"loginType": "PC",
	}
	_, _, err := postSSOJSON(client, apiURL.String(), payload)
	return err
}

func submitSSOLogin(ctx context.Context, client *http.Client, username, password, codeID, captcha, phoneCode, phone string) (string, error) {
	apiURL := ssoURL("/sso/login")
	params := apiURL.Query()
	params.Set("client_id", "gzyqfk")
	params.Set("redirect_uri", "https://yqfk.wjw.gz.gov.cn/VueForKhala")
	params.Set("response_type", "code")
	params.Set("scope", "all")
	params.Set("state", "o8kA4S")
	apiURL.RawQuery = params.Encode()

	form := url.Values{}
	form.Set("username", username)
	form.Set("password", md5Hex(password))
	form.Set("validateCode", captcha)
	form.Set("remember-me", "on")
	form.Set("codeId", codeID)
	form.Set("phoneCode", phoneCode)
	form.Set("phone", phone)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	setSSOHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return "", errors.New(ssoUnauthorizedMessage(strings.TrimSpace(string(body))))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("真实登录接口异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if token := findAccessToken(body); token != "" {
		return token, nil
	}

	contextPath := resp.Header.Get("CONTEXTPATH")
	if contextPath == "" {
		contextPath = resp.Header.Get("Location")
	}
	if contextPath == "" {
		return "", fmt.Errorf("登录成功但未返回跳转地址或token: %s", strings.TrimSpace(string(body)))
	}

	code, err := resolveOAuthCode(ctx, client, contextPath)
	if err != nil {
		return "", err
	}
	if code == "" {
		return "", fmt.Errorf("登录成功但跳转地址中未找到OAuth code: %s", contextPath)
	}

	return exchangeSSOCode(client, code)
}

func resolveOAuthCode(ctx context.Context, client *http.Client, contextPath string) (string, error) {
	if code := extractOAuthCode(contextPath); code != "" {
		return code, nil
	}

	parsed, err := normalizeSSOURL(contextPath)
	if err != nil {
		return "", err
	}

	if !strings.Contains(parsed.RawQuery, "loginSource=") {
		return "", nil
	}

	apiURL := ssoURL("/sso/oauth/authorize")
	params := apiURL.Query()
	params.Set("client_id", "gzyqfk")
	params.Set("redirect_uri", "https://yqfk.wjw.gz.gov.cn/VueForKhala")
	params.Set("response_type", "code")
	params.Set("scope", "all")
	params.Set("state", "o8kA4S")
	apiURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL.String(), nil)
	if err != nil {
		return "", err
	}
	setSSOHeaders(req)
	req.Header.Set("Referer", parsed.String())

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return "", errors.New(ssoUnauthorizedMessage(strings.TrimSpace(string(body))))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("OAuth授权跳转异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	target := resp.Header.Get("CONTEXTPATH")
	if target == "" {
		target = resp.Header.Get("Location")
	}
	if target == "" {
		target = strings.TrimSpace(string(body))
	}

	if code := extractOAuthCode(target); code != "" {
		return code, nil
	}
	return "", fmt.Errorf("OAuth授权跳转未返回code: %s", target)
}

func exchangeSSOCode(client *http.Client, code string) (string, error) {
	apiURL := ssoURL("/apis/yqfk-sysmanage/token/openid")
	params := apiURL.Query()
	params.Set("code", code)
	params.Set("redirect_uri", "https://yqfk.wjw.gz.gov.cn")
	apiURL.RawQuery = params.Encode()

	req, err := http.NewRequest(http.MethodGet, apiURL.String(), nil)
	if err != nil {
		return "", err
	}
	setSSOHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OAuth code换token异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	token := findAccessToken(body)
	if token == "" {
		return "", fmt.Errorf("OAuth响应未包含access_token: %s", strings.TrimSpace(string(body)))
	}
	return token, nil
}

func exchangeGdPlatformMobileCode(ctx context.Context, client *http.Client, code string) (string, error) {
	payload := map[string]string{
		"code": code,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://yqfk.wjw.gz.gov.cn/apis/yqfk-sysmanage/auth/oauth/pc/getGdPlatformMobile", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	setSSOHeaders(req)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Referer", "https://yqfk.wjw.gz.gov.cn/VueForKhala/login")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("省统一认证code换token异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var result gdPlatformMobileResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("省统一认证code换token响应解析失败: %w", err)
	}
	if result.Code != 0 {
		return "", errors.New(defaultString(result.Desc, strings.TrimSpace(string(raw)), "省统一认证code换token失败"))
	}
	if result.Data.LoginInfo.AccessToken == "" {
		return "", fmt.Errorf("省统一认证code换token未返回access_token: %s", strings.TrimSpace(string(raw)))
	}
	return result.Data.LoginInfo.AccessToken, nil
}

func fetchProvinceClientInfo(client *http.Client) (*provinceClientInfoResponse, error) {
	req, err := http.NewRequest(http.MethodGet, "https://yqfk.wjw.gz.gov.cn/apis/yqfk-sysmanage/auth/oauth/pc/getClientInfo", nil)
	if err != nil {
		return nil, err
	}
	setSSOHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取省统一认证入口异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result provinceClientInfoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("省统一认证入口解析失败: %w", err)
	}
	if result.Code != 0 || result.Data.ClientID == "" || result.Data.URL == "" {
		return nil, fmt.Errorf("省统一认证入口返回异常: %s", strings.TrimSpace(string(body)))
	}
	return &result, nil
}

func yzyQRHost() string {
	qrHost := strings.TrimSpace(os.Getenv("YZY_QR_HOST"))
	if qrHost != "" {
		return qrHost
	}
	return "https://zwwx.gdzwfw.gov.cn"
}

func yzyRedirectURI() string {
	redirectURI := strings.TrimSpace(os.Getenv("YZY_REDIRECT_URI"))
	if redirectURI != "" {
		return redirectURI
	}
	return "https://xtbg.gdzwfw.gov.cn"
}

func yzyQRCodeImageURL(qrKey string) string {
	params := url.Values{}
	params.Set("key", qrKey)
	params.Set("t", fmt.Sprintf("%d", time.Now().UnixMilli()))
	return strings.TrimRight(yzyQRHost(), "/") + "/wwopen/sso/qrImg?" + params.Encode()
}

func getYZYQRCodeImage(ctx context.Context, client *http.Client, qrKey string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, yzyQRCodeImageURL(qrKey), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://xtbg.gdzwfw.gov.cn/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("获取粤政易二维码图片异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "image/png"
	}
	return raw, contentType, nil
}

func getYZYQRCodeKey(ctx context.Context, client *http.Client) (string, error) {
	appID := strings.TrimSpace(os.Getenv("YZY_APPID"))
	agentID := strings.TrimSpace(os.Getenv("YZY_AGENTID"))

	params := url.Values{}
	params.Set("appid", appID)
	params.Set("agentid", agentID)
	params.Set("redirect_uri", yzyRedirectURI())
	params.Set("callback", "getQRCodeKeyCallback")
	params.Set("_", fmt.Sprintf("%d", time.Now().UnixMilli()))

	apiURL := strings.TrimRight(yzyQRHost(), "/") + "/wwopen/sso/getQRCodeKey?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://xtbg.gdzwfw.gov.cn/")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("获取粤政易二维码key异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var result yzyQRCodeKeyResponse
	if err := unmarshalJSONP(raw, &result); err != nil {
		return "", fmt.Errorf("粤政易二维码key响应解析失败: %w", err)
	}
	if result.Status != "SUCC" || result.QRCodeKey == "" {
		return "", fmt.Errorf("粤政易二维码key返回异常: %s", strings.TrimSpace(string(raw)))
	}
	return result.QRCodeKey, nil
}

func getYZYQRCodeStatus(ctx context.Context, client *http.Client, qrKey, lastStatus string) (*yzyQRCodePollResponse, error) {
	callbackName := "jsonpCallback"
	params := url.Values{}
	params.Set("key", qrKey)
	if lastStatus != "" {
		params.Set("lastStatus", lastStatus)
	}
	params.Set("callback", callbackName)
	params.Set("_", fmt.Sprintf("%d", time.Now().UnixMilli()))

	apiURL := strings.TrimRight(yzyQRHost(), "/") + "/wwopen/sso/l/qrConnect?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://xtbg.gdzwfw.gov.cn/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询粤政易扫码状态异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var result yzyQRCodePollResponse
	if err := unmarshalJSONP(raw, &result); err != nil {
		return nil, fmt.Errorf("粤政易扫码状态响应解析失败: %w", err)
	}
	return &result, nil
}

func loginByYZYCode(ctx context.Context, client *http.Client, code string) (*loginData, error) {
	clientInfo, err := fetchProvinceClientInfo(client)
	if err != nil {
		return nil, err
	}

	storeCode, err := verifyYZYCode(ctx, client, clientInfo.Data.ClientID, code)
	if err != nil {
		return nil, err
	}

	redirectURI, err := loginRZByStoreCode(ctx, client, clientInfo.Data.ClientID, storeCode)
	if err != nil {
		return nil, err
	}

	oauthCode, err := resolveProvinceOAuthCode(ctx, client, clientInfo.Data.ClientID, redirectURI)
	if err != nil {
		return nil, err
	}
	if oauthCode == "" {
		return nil, fmt.Errorf("粤政易扫码登录未返回OAuth code: %s", redirectURI)
	}

	token, err := exchangeGdPlatformMobileCode(ctx, client, oauthCode)
	if err != nil {
		return nil, err
	}
	return &loginData{Token: token}, nil
}

func verifyYZYCode(ctx context.Context, client *http.Client, clientID, code string) (string, error) {
	payload := map[string]string{
		"code":     code,
		"clientId": clientID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://xtbg.gdzwfw.gov.cn/zwrz/rz/oauthidentity/validyzycode", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	setProvinceHeaders(req, clientID)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("粤政易扫码校验异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var result yzyVerifyResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("粤政易扫码校验响应解析失败: %w", err)
	}
	if result.Status != 0 {
		return "", errors.New(defaultString(result.Message, strings.TrimSpace(string(raw)), "粤政易扫码校验失败"))
	}
	if result.Data.NextProcess != "" && result.Data.NextProcess != "login" {
		return "", fmt.Errorf("粤政易扫码还需要%s验证，当前暂不支持", result.Data.NextProcess)
	}
	if result.Data.StoreCode == "" {
		return "", fmt.Errorf("粤政易扫码未返回登录凭据: %s", strings.TrimSpace(string(raw)))
	}
	return result.Data.StoreCode, nil
}

func loginRZByStoreCode(ctx context.Context, client *http.Client, clientID, storeCode string) (string, error) {
	payload := map[string]any{
		"scope":         "all",
		"response_type": "code",
		"redirect_uri":  "https://yqfk.wjw.gz.gov.cn/VueForKhala/login",
		"client_id":     clientID,
		"state":         "o8kA4S",
		"store_code":    storeCode,
		"extend": map[string]string{
			"link_rz_yjz_host": "2",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://xtbg.gdzwfw.gov.cn/zwrz/rz/sso/rzloginbycode", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	setProvinceHeaders(req, clientID)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("省统一认证登录异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var result rzLoginByCodeResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("省统一认证登录响应解析失败: %w", err)
	}
	if result.Status != 0 {
		return "", errors.New(defaultString(result.Message, strings.TrimSpace(string(raw)), "省统一认证登录失败"))
	}
	if result.Data.RedirectURI == "" {
		return "", fmt.Errorf("省统一认证登录未返回跳转地址: %s", strings.TrimSpace(string(raw)))
	}
	return result.Data.RedirectURI, nil
}

func resolveProvinceOAuthCode(ctx context.Context, client *http.Client, clientID, rawURL string) (string, error) {
	if code := extractOAuthCode(rawURL); code != "" {
		return code, nil
	}

	parsed, err := normalizeSSOURL(rawURL)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	setProvinceHeaders(req, clientID)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Del("Content-Type")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", errors.New(ssoUnauthorizedMessage(strings.TrimSpace(string(raw))))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("省统一认证授权跳转异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	candidates := []string{
		resp.Header.Get("CONTEXTPATH"),
		resp.Header.Get("Location"),
		resp.Request.URL.String(),
		strings.TrimSpace(string(raw)),
	}
	for _, candidate := range candidates {
		if code := extractOAuthCodeFromText(candidate); code != "" {
			return code, nil
		}
	}
	return "", fmt.Errorf("省统一认证授权跳转未返回code: %s", firstNonEmpty(candidates...))
}

func postSSOJSON(client *http.Client, endpoint string, payload any) (*ssoResponse, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	setSSOHeaders(req)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, raw, errors.New(ssoUnauthorizedMessage(strings.TrimSpace(string(raw))))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, raw, fmt.Errorf("真实SSO接口异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var result ssoResponse
	if len(bytes.TrimSpace(raw)) > 0 && json.Unmarshal(raw, &result) != nil {
		result.Code = 0
	}
	return &result, raw, nil
}

func ssoURL(path string) *url.URL {
	u, _ := url.Parse("https://yqfk.wjw.gz.gov.cn")
	u.Path = path
	return u
}

func setSSOHeaders(req *http.Request) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://yqfk.wjw.gz.gov.cn")
	req.Header.Set("Referer", "https://yqfk.wjw.gz.gov.cn/sso/login")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:152.0) Gecko/20100101 Firefox/152.0")
}

func setProvinceHeaders(req *http.Request, clientID string) {
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://xtbg.gdzwfw.gov.cn")
	req.Header.Set("Referer", "https://xtbg.gdzwfw.gov.cn/zwrz/login?client_id="+url.QueryEscape(clientID)+"&scope=all&redirect_uri=https%3A%2F%2Fyqfk.wjw.gz.gov.cn%2FVueForKhala%2Flogin&response_type=code")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:152.0) Gecko/20100101 Firefox/152.0")
	req.Header.Set("clientId", clientID)
}

func encryptPasswordForSSO(password string) string {
	const keyText = "gzwjyqfkgzwjyqfk"
	block, err := aes.NewCipher([]byte(keyText))
	if err != nil {
		return ""
	}

	plain := pkcs7Pad([]byte(password), block.BlockSize())
	encrypted := make([]byte, len(plain))
	for start := 0; start < len(plain); start += block.BlockSize() {
		block.Encrypt(encrypted[start:start+block.BlockSize()], plain[start:start+block.BlockSize()])
	}
	return base64.StdEncoding.EncodeToString(encrypted)
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...)
}

func md5Hex(text string) string {
	sum := md5.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

func parseJSONString(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return ""
}

func findAccessToken(raw []byte) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return findAccessTokenValue(value)
}

func findAccessTokenValue(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key == "access_token" {
				if token, ok := item.(string); ok {
					return token
				}
			}
			if token := findAccessTokenValue(item); token != "" {
				return token
			}
		}
	case []any:
		for _, item := range typed {
			if token := findAccessTokenValue(item); token != "" {
				return token
			}
		}
	}
	return ""
}

func extractOAuthCode(rawURL string) string {
	parsed, err := normalizeSSOURL(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("code")
}

func extractOAuthCodeFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if code := extractOAuthCode(text); code != "" {
		return code
	}

	var value any
	if json.Unmarshal([]byte(text), &value) == nil {
		return findOAuthCodeValue(value)
	}

	for _, marker := range []string{"?code=", "&code=", "code="} {
		index := strings.Index(text, marker)
		if index < 0 {
			continue
		}
		start := index + len(marker)
		end := start
		for end < len(text) {
			char := text[end]
			if char == '&' || char == '"' || char == '\'' || char == '<' || char == ' ' || char == '\\' {
				break
			}
			end++
		}
		if end > start {
			code, err := url.QueryUnescape(text[start:end])
			if err == nil && strings.TrimSpace(code) != "" {
				return code
			}
			return text[start:end]
		}
	}
	return ""
}

func findOAuthCodeValue(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key == "code" {
				if code, ok := item.(string); ok {
					return code
				}
			}
			if text, ok := item.(string); ok {
				if code := extractOAuthCodeFromText(text); code != "" {
					return code
				}
			}
			if code := findOAuthCodeValue(item); code != "" {
				return code
			}
		}
	case []any:
		for _, item := range typed {
			if code := findOAuthCodeValue(item); code != "" {
				return code
			}
		}
	case string:
		return extractOAuthCodeFromText(typed)
	}
	return ""
}

func normalizeSSOURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(rawURL, "/") {
		rawURL = "https://yqfk.wjw.gz.gov.cn" + rawURL
	}
	return url.Parse(rawURL)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func requestBaseURL(req *http.Request) string {
	scheme := req.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if req.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := req.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = req.Host
	}
	return scheme + "://" + host
}

func htmlEscape(value string) string {
	return html.EscapeString(value)
}

func ssoUnauthorizedMessage(code string) string {
	messages := map[string]string{
		"VW": "登录失败，验证码错误",
		"UL": "登录失败次数过多，用户暂时锁定",
		"LF": "登录失败，请检查账号密码输入",
		"UB": "登录失败，账号未生效，请联系系统管理员",
		"US": "登录失败，账号已过期，请联系系统管理员",
		"PN": "首次登录，需要先在网页登录修改密码",
		"PO": "密码过期，需要先在网页登录修改密码",
		"PE": "密码太过简单，需要先在网页登录修改密码",
		"UA": "请先阅读并确认网页登录条款",
		"VE": "验证码填写错误",
	}
	if message, ok := messages[code]; ok {
		return message
	}
	return defaultString("", code, "真实登录失败")
}

func defaultString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func unmarshalJSONP(raw []byte, target any) error {
	text := strings.TrimSpace(string(raw))
	start := strings.Index(text, "(")
	end := strings.LastIndex(text, ")")
	if start < 0 || end <= start {
		return fmt.Errorf("不是有效JSONP: %s", text)
	}
	return json.Unmarshal([]byte(text[start+1:end]), target)
}

func newVerificationStore() *verificationStore {
	return &verificationStore{
		captchas:   make(map[string]verificationCode),
		phoneCodes: make(map[string]verificationCode),
		ssoFlows:   make(map[string]*ssoFlow),
		yzyFlows:   make(map[string]*yzyFlow),
	}
}

func (s *verificationStore) NewCaptcha() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked()
	codeID := randomID()
	code := randomTextCode(4)
	s.captchas[codeID] = verificationCode{
		Code:      code,
		ExpiresAt: time.Now().Add(captchaExpiresIn),
	}
	return codeID, code
}

func (s *verificationStore) VerifyCaptcha(codeID, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.captchas[codeID]
	if !ok || time.Now().After(item.ExpiresAt) {
		delete(s.captchas, codeID)
		return false
	}
	delete(s.captchas, codeID)
	return strings.EqualFold(item.Code, strings.TrimSpace(code))
}

func (s *verificationStore) NewPhoneCode(username string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked()
	code := randomDigitCode(6)
	s.phoneCodes[phoneCodeKey(username)] = verificationCode{
		Code:      code,
		ExpiresAt: time.Now().Add(phoneCodeExpiresIn),
	}
	return code
}

func (s *verificationStore) VerifyPhoneCode(username, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := phoneCodeKey(username)
	item, ok := s.phoneCodes[key]
	if !ok || time.Now().After(item.ExpiresAt) {
		delete(s.phoneCodes, key)
		return false
	}
	if item.Code != strings.TrimSpace(code) {
		return false
	}
	delete(s.phoneCodes, key)
	return true
}

func (s *verificationStore) cleanupLocked() {
	now := time.Now()
	for key, item := range s.captchas {
		if now.After(item.ExpiresAt) {
			delete(s.captchas, key)
		}
	}
	for key, item := range s.phoneCodes {
		if now.After(item.ExpiresAt) {
			delete(s.phoneCodes, key)
		}
	}
	for key, flow := range s.ssoFlows {
		if flow == nil || now.Sub(flow.LastTouched) > captchaExpiresIn {
			delete(s.ssoFlows, key)
		}
	}
	for key, flow := range s.yzyFlows {
		if flow == nil || now.After(flow.ExpiresAt) {
			delete(s.yzyFlows, key)
		}
	}
}

func phoneCodeKey(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func randomID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func randomUUID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:])
}

func randomTextCode(length int) string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	return randomFromChars(chars, length)
}

func randomDigitCode(length int) string {
	const chars = "0123456789"
	return randomFromChars(chars, length)
}

func randomFromChars(chars string, length int) string {
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			out[i] = chars[time.Now().UnixNano()%int64(len(chars))]
			continue
		}
		out[i] = chars[n.Int64()]
	}
	return string(out)
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func loadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}
