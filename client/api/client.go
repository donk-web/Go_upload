package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"fyne-getinfo/config"
	"fyne-getinfo/model"
	"fyne-getinfo/session"
)

type Client struct{}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Data    model.LoginResult `json:"data"`
}

type sendPhoneCodeRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	CodeID   string `json:"code_id"`
	Captcha  string `json:"captcha"`
}

type refreshDoctorTokenRequest struct {
	HospitalCode string `json:"hospital_code"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	CodeID       string `json:"code_id"`
	Captcha      string `json:"captcha"`
	PhoneCode    string `json:"phone_code"`
}

type startYZYLoginRequest struct {
	HospitalCode string `json:"hospital_code"`
}

type refreshYZYLoginRequest struct {
	FlowID string `json:"flow_id"`
}

type CaptchaResult struct {
	CodeID      string `json:"code_id"`
	ImageBase64 string `json:"image_base64"`
	ContentType string `json:"content_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type captchaResponse struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    CaptchaResult `json:"data"`
}

type PhoneCodeResult struct {
	NextCaptcha *CaptchaResult `json:"next_captcha"`
	ExpiresIn   int            `json:"expires_in"`
}

type sendPhoneCodeResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    PhoneCodeResult `json:"data"`
}

type startYZYLoginResponse struct {
	Code    int                       `json:"code"`
	Message string                    `json:"message"`
	Data    model.YZYLoginStartResult `json:"data"`
}

type yzyLoginStatusResponse struct {
	Code    int                        `json:"code"`
	Message string                     `json:"message"`
	Data    model.YZYLoginStatusResult `json:"data"`
}

type rhrBasicInfoResponse struct {
	Data struct {
		List []rhrBasicInfo `json:"list"`
	} `json:"data"`
}

type rhrBasicInfo struct {
	ID           string `json:"id"`
	HealthFileNo string `json:"healthFileNo"`
	IDNumber     string `json:"idNumber"`
	Name         string `json:"name"`
}

type viewLogListResponse struct {
	Data []viewLogItem `json:"data"`
}

type viewLogItem struct {
	ViewTime      string `json:"viewTime"`
	ViewOrgName   string `json:"viewOrgName"`
	Department    string `json:"departmentName"`
	ViewUserName  string `json:"viewUserName"`
	AccessChannel string `json:"accessChannel"`
}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) GetDoctorCaptcha() (*CaptchaResult, error) {
	req, err := http.NewRequest(http.MethodGet, authURL("/api/doctor-token/captcha"), nil)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(config.Get().HTTPTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取图形验证码失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取图形验证码响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取图形验证码接口异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var result captchaResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("图形验证码响应解析失败: %w", err)
	}

	if result.Code != 0 {
		if result.Message == "" {
			result.Message = "获取图形验证码失败"
		}
		return nil, errors.New(result.Message)
	}

	if result.Data.CodeID == "" || result.Data.ImageBase64 == "" {
		return nil, errors.New("图形验证码响应不完整")
	}

	return &result.Data, nil
}

func (c *Client) SendDoctorPhoneCode(username, password, codeID, captcha string) (*PhoneCodeResult, error) {
	body, err := json.Marshal(sendPhoneCodeRequest{
		Username: username,
		Password: password,
		CodeID:   codeID,
		Captcha:  captcha,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, authURL("/api/doctor-token/send-phone-code"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	timeout := time.Duration(config.Get().HTTPTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送手机验证码失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取手机验证码响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("发送手机验证码接口异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var result sendPhoneCodeResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("手机验证码响应解析失败: %w", err)
	}

	if result.Code != 0 {
		if result.Message == "" {
			result.Message = "发送手机验证码失败"
		}
		return nil, errors.New(result.Message)
	}

	return &result.Data, nil
}

func (c *Client) RefreshDoctorToken(hospitalCode, username, password, codeID, captcha, phoneCode string) (*model.LoginResult, error) {
	body, err := json.Marshal(refreshDoctorTokenRequest{
		HospitalCode: hospitalCode,
		Username:     username,
		Password:     password,
		CodeID:       codeID,
		Captcha:      captcha,
		PhoneCode:    phoneCode,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, authURL("/api/doctor-token/refresh"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	timeout := time.Duration(config.Get().HTTPTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("更新医生token失败: %w", err)
	}
	defer resp.Body.Close()

	var result loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("更新医生token响应解析失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK || result.Code != 0 {
		if result.Message == "" {
			result.Message = "更新医生token失败"
		}
		return nil, errors.New(result.Message)
	}

	if result.Data.Token == "" {
		return nil, errors.New("更新成功但未返回token")
	}

	return &result.Data, nil
}

func (c *Client) StartYZYLogin(hospitalCode string) (*model.YZYLoginStartResult, error) {
	body, err := json.Marshal(startYZYLoginRequest{HospitalCode: hospitalCode})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, authURL("/api/doctor-token/yzy/start"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	timeout := time.Duration(config.Get().HTTPTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("创建粤政易扫码登录失败: %w", err)
	}
	defer resp.Body.Close()

	var result startYZYLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("粤政易扫码登录响应解析失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK || result.Code != 0 {
		if result.Message == "" {
			result.Message = "创建粤政易扫码登录失败"
		}
		return nil, errors.New(result.Message)
	}
	if result.Data.FlowID == "" || result.Data.QRImageBase64 == "" {
		return nil, errors.New("粤政易扫码登录响应不完整")
	}
	return &result.Data, nil
}

func (c *Client) RefreshYZYLogin(flowID string) (*model.YZYLoginStartResult, error) {
	body, err := json.Marshal(refreshYZYLoginRequest{FlowID: flowID})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, authURL("/api/doctor-token/yzy/refresh"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	timeout := time.Duration(config.Get().HTTPTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("刷新粤政易二维码失败: %w", err)
	}
	defer resp.Body.Close()

	var result startYZYLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("粤政易二维码刷新响应解析失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK || result.Code != 0 {
		if result.Message == "" {
			result.Message = "刷新粤政易二维码失败"
		}
		return nil, errors.New(result.Message)
	}
	if result.Data.FlowID == "" || result.Data.QRImageBase64 == "" {
		return nil, errors.New("粤政易二维码刷新响应不完整")
	}
	return &result.Data, nil
}

func (c *Client) GetYZYLoginStatus(flowID string) (*model.YZYLoginStatusResult, error) {
	req, err := http.NewRequest(http.MethodGet, authURL("/api/doctor-token/yzy/status/"+flowID), nil)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(config.Get().HTTPTimeout) * time.Second
	if timeout < 35*time.Second {
		timeout = 35 * time.Second
	}

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询粤政易扫码登录状态失败: %w", err)
	}
	defer resp.Body.Close()

	var result yzyLoginStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("粤政易扫码登录状态解析失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK || result.Code != 0 {
		if result.Message == "" {
			result.Message = "查询粤政易扫码登录状态失败"
		}
		return nil, errors.New(result.Message)
	}
	return &result.Data, nil
}

func (c *Client) Login(username, password string) (*model.LoginResult, error) {
	cfg := config.Get()

	body, err := json.Marshal(loginRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(cfg.HTTPTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	req, err := http.NewRequest(http.MethodPost, authURL(cfg.LoginEndpoint), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	var result loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("登录响应解析失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK || result.Code != 0 {
		if result.Message == "" {
			result.Message = "登录失败"
		}
		return nil, errors.New(result.Message)
	}

	return &result.Data, nil
}

func authURL(path string) string {
	cfg := config.Get()
	return strings.TrimRight(cfg.AuthBaseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

// QueryResidents 查询居民数据
func (c *Client) QueryResidents(req model.Request) (*model.Response, error) {
	// ========== 模拟模式 ==========
	if config.Current.MockMode {
		// 模拟网络延迟 800ms，体验更真实
		time.Sleep(800 * time.Millisecond)

		// 模拟"输入指定身份证才有数据"的场景
		if req.IDCard == "" {
			return nil, errors.New("身份证号不能为空")
		}

		// 返回假数据
		return &model.Response{
			Code:    0,
			Message: "success",
			Data: []model.ArchiveViewLog{
				{
					IDCard:        req.IDCard,
					Name:          defaultString(req.Name, "张三"),
					Index:         1,
					ViewTime:      "2026-06-12 11:44:18",
					ViewOrgName:   "钟村街社区卫生服务中心",
					Department:    "",
					ViewUserName:  "毛敏",
					AccessChannel: "社区通",
				},
				{
					IDCard:        req.IDCard,
					Name:          defaultString(req.Name, "张三"),
					Index:         2,
					ViewTime:      "2026-06-12 11:31:32",
					ViewOrgName:   "钟村街社区卫生服务中心",
					Department:    "",
					ViewUserName:  "毛敏",
					AccessChannel: "社区通",
				},
			},
		}, nil
	}

	// ========== 真实请求模式 ==========
	return c.queryReal(req)
}

func (c *Client) KeepBusinessTokenAlive() error {
	token := session.Token()
	if token == "" {
		return errors.New("请先登录")
	}

	cfg := config.Get()
	url := strings.TrimRight(cfg.APIBaseURL, "/") + "/apis/yqfk-sysmanage/sysUser/getLastOrgRole/pc"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	setBusinessHeaders(req, token)
	return c.doJSON(req, &struct{}{})
}

// 把原来的真实请求代码移到这个私有方法里
func (c *Client) queryReal(req model.Request) (*model.Response, error) {
	token := session.Token()
	if token == "" {
		return nil, errors.New("请先登录")
	}

	basicInfo, err := c.getRhrBasicInfo(req, token)
	if err != nil {
		return nil, err
	}

	viewLogs, err := c.getViewLogList(basicInfo.ID, token)
	if err != nil {
		return nil, err
	}

	records := make([]model.ArchiveViewLog, 0, len(viewLogs))
	for i, item := range viewLogs {
		records = append(records, model.ArchiveViewLog{
			IDCard:        basicInfo.IDNumber,
			Name:          basicInfo.Name,
			Index:         i + 1,
			ViewTime:      item.ViewTime,
			ViewOrgName:   item.ViewOrgName,
			Department:    item.Department,
			ViewUserName:  item.ViewUserName,
			AccessChannel: accessChannelName(item.AccessChannel),
		})
	}

	return &model.Response{
		Code:    0,
		Message: "success",
		Data:    records,
	}, nil
}

func (c *Client) getRhrBasicInfo(req model.Request, token string) (*rhrBasicInfo, error) {
	cfg := config.Get()
	url := strings.TrimRight(cfg.APIBaseURL, "/") + "/apis/yqfk-population/rhr/getRhrBasicInfoList"

	payload := map[string]any{
		"fileStatusCode":           "0",
		"desensitization":          "0",
		"name":                     req.Name,
		"idNumber":                 req.IDCard,
		"divisionsCodeOfResidence": "4401",
		"personType":               []string{},
		"pageNum":                  1,
		"pageSize":                 20,
	}

	var result rhrBasicInfoResponse
	if err := c.postBusinessJSON(url, token, payload, &result); err != nil {
		return nil, err
	}

	if len(result.Data.List) == 0 {
		return nil, errors.New("查无此人")
	}
	if len(result.Data.List) > 1 {
		return nil, errors.New("查到多人")
	}

	return &result.Data.List[0], nil
}

func (c *Client) getViewLogList(infoID, token string) ([]viewLogItem, error) {
	cfg := config.Get()
	url := strings.TrimRight(cfg.APIBaseURL, "/") + "/apis/yqfk-population/rhr/getViewLogList/" + infoID

	var result viewLogListResponse
	if err := c.getBusinessJSON(url, token, &result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

func (c *Client) postBusinessJSON(url, token string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	setBusinessHeaders(req, token)
	return c.doJSON(req, target)
}

func (c *Client) getBusinessJSON(url, token string, target any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	setBusinessHeaders(req, token)
	return c.doJSON(req, target)
}

func (c *Client) doJSON(req *http.Request, target any) error {
	cfg := config.Get()
	timeout := time.Duration(cfg.HTTPTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("业务接口请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取业务接口响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("业务接口返回异常: HTTP %d %s", resp.StatusCode, string(data))
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("解析业务接口响应失败: %w", err)
	}

	return nil
}

func IsBusinessAuthError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	keywords := []string{
		"http 401",
		"unauthorized",
		"未授权",
		"token",
		"登录",
		"过期",
	}
	for _, keyword := range keywords {
		if strings.Contains(text, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func setBusinessHeaders(req *http.Request, token string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("requestChannel", "PC")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:152.0) Gecko/20100101 Firefox/152.0")
}

func accessChannelName(code string) string {
	names := map[string]string{
		"1": "社区通",
		"2": "医院HIS系统",
		"3": "数字空间",
	}
	if name, ok := names[code]; ok {
		return name
	}
	return code
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
