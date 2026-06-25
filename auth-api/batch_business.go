package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var (
	errBatchResidentNotFound  = errors.New("查无此人")
	errBatchMultipleResidents = errors.New("查到多人")
)

type batchBusinessClient struct {
	baseURL string
	client  *http.Client
}

type batchBasicInfoResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		List []struct {
			ID       string `json:"id"`
			IDNumber string `json:"idNumber"`
			Name     string `json:"name"`
		} `json:"list"`
	} `json:"data"`
}

type batchViewLogResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []struct {
		ViewTime      string `json:"viewTime"`
		ViewOrgName   string `json:"viewOrgName"`
		Department    string `json:"departmentName"`
		ViewUserName  string `json:"viewUserName"`
		AccessChannel string `json:"accessChannel"`
	} `json:"data"`
}

type batchComprehensiveSearchResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		List []struct {
			ID                 string `json:"id"`
			IdentityNum        string `json:"identityNum"`
			IdentityNumEncrypt string `json:"identityNumEncrypt"`
			RealIdentityNum    string `json:"realIdentityNum"`
			Name               string `json:"name"`
			RealName           string `json:"realName"`
		} `json:"list"`
	} `json:"data"`
}

type batchAppBasicInfoResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []struct {
		ID       string `json:"id"`
		IDNumber string `json:"idNumber"`
		Name     string `json:"name"`
	} `json:"data"`
}

func newBatchBusinessClient() *batchBusinessClient {
	baseURL := strings.TrimSpace(os.Getenv("BUSINESS_API_BASE"))
	if baseURL == "" {
		baseURL = "https://yqfk.wjw.gz.gov.cn"
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	return &batchBusinessClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}
}

func (c *batchBusinessClient) QueryResident(ctx context.Context, token, idCard string) ([]batchArchiveRecord, error) {
	basic, err := c.getBasicInfo(ctx, token, idCard)
	if err != nil {
		return nil, err
	}
	logs, err := c.getViewLogs(ctx, token, basic.ID)
	if err != nil {
		return nil, err
	}
	if len(logs.Data) == 0 {
		return []batchArchiveRecord{{
			IDCard: basic.IDNumber,
			Name:   basic.Name,
			Index:  0,
		}}, nil
	}
	records := make([]batchArchiveRecord, 0, len(logs.Data))
	for index, item := range logs.Data {
		records = append(records, batchArchiveRecord{
			IDCard:        basic.IDNumber,
			Name:          basic.Name,
			Index:         index + 1,
			ViewTime:      item.ViewTime,
			ViewOrgName:   item.ViewOrgName,
			Department:    item.Department,
			ViewUserName:  item.ViewUserName,
			AccessChannel: batchAccessChannelName(item.AccessChannel),
		})
	}
	return records, nil
}

func (c *batchBusinessClient) QueryResidentWithMethod(ctx context.Context, token, idCard, queryMethod string) ([]batchArchiveRecord, error) {
	if queryMethod == batchQueryMethodNew {
		return c.queryResidentNew(ctx, token, idCard)
	}
	return c.QueryResident(ctx, token, idCard)
}

func (c *batchBusinessClient) queryResidentNew(ctx context.Context, token, idCard string) ([]batchArchiveRecord, error) {
	searchItem, err := c.getComprehensiveSearchItem(ctx, token, idCard)
	if err != nil {
		return nil, err
	}
	archives, err := c.getBasicInfoListForApp(ctx, token, searchItem.IdentityNumEncrypt)
	if err != nil {
		return nil, err
	}

	name := firstBatchNonEmpty(searchItem.RealName, searchItem.Name)
	records := make([]batchArchiveRecord, 0)
	seen := make(map[string]struct{})
	var firstViewLogErr error
	successfulArchive := false
	successfulArchiveName := ""
	for _, archive := range archives {
		logs, err := c.getViewLogs(ctx, token, archive.ID)
		if err != nil {
			if firstViewLogErr == nil {
				firstViewLogErr = fmt.Errorf("档案%s查询调阅记录失败: %w", archive.ID, err)
			}
			continue
		}
		successfulArchive = true
		recordName := firstBatchNonEmpty(name, archive.Name)
		if successfulArchiveName == "" {
			successfulArchiveName = recordName
		}
		for _, item := range logs.Data {
			key := strings.Join([]string{
				item.ViewTime,
				item.ViewOrgName,
				item.Department,
				item.ViewUserName,
				item.AccessChannel,
			}, "\x00")
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			records = append(records, batchArchiveRecord{
				IDCard:        idCard,
				Name:          recordName,
				Index:         len(records) + 1,
				ViewTime:      item.ViewTime,
				ViewOrgName:   item.ViewOrgName,
				Department:    item.Department,
				ViewUserName:  item.ViewUserName,
				AccessChannel: batchAccessChannelName(item.AccessChannel),
			})
		}
	}
	if !successfulArchive {
		return nil, firstViewLogErr
	}
	if len(records) == 0 {
		return []batchArchiveRecord{{
			IDCard: idCard,
			Name:   firstBatchNonEmpty(successfulArchiveName, name, archives[0].Name),
			Index:  0,
		}}, nil
	}
	return records, nil
}

type batchComprehensiveSearchItem struct {
	IdentityNumEncrypt string
	Name               string
	RealName           string
}

type batchAppBasicInfoItem struct {
	ID   string
	Name string
}

func (c *batchBusinessClient) getComprehensiveSearchItem(ctx context.Context, token, idCard string) (*batchComprehensiveSearchItem, error) {
	payload := map[string]any{
		"comprehensiveQuery": idCard,
		"count":              true,
		"pageNum":            1,
		"pageSize":           10,
		"total":              0,
	}
	var result batchComprehensiveSearchResponse
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/apis/yqfk-population/basicHealthPop/getYqfkZhcxList", token, payload, &result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(firstBatchNonEmpty(result.Message, "综合查询居民失败"))
	}
	if len(result.Data.List) == 0 {
		return nil, errBatchResidentNotFound
	}

	selected := -1
	for index, item := range result.Data.List {
		if item.RealIdentityNum == idCard || item.IdentityNum == idCard {
			if selected >= 0 {
				return nil, errBatchMultipleResidents
			}
			selected = index
		}
	}
	if selected < 0 {
		if len(result.Data.List) != 1 {
			return nil, errBatchMultipleResidents
		}
		selected = 0
	}
	item := result.Data.List[selected]
	if strings.TrimSpace(item.IdentityNumEncrypt) == "" {
		return nil, errors.New("综合查询未返回加密身份证号")
	}
	return &batchComprehensiveSearchItem{
		IdentityNumEncrypt: item.IdentityNumEncrypt,
		Name:               item.Name,
		RealName:           item.RealName,
	}, nil
}

func (c *batchBusinessClient) getBasicInfoListForApp(ctx context.Context, token, idNumberEncrypt string) ([]batchAppBasicInfoItem, error) {
	payload := map[string]any{
		"channel":         "pc",
		"idNumberEncrypt": idNumberEncrypt,
	}
	var result batchAppBasicInfoResponse
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/apis/yqfk-population/rhr/getBasicInfoListForApp", token, payload, &result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(firstBatchNonEmpty(result.Message, "查询居民健康档案失败"))
	}
	archives := make([]batchAppBasicInfoItem, 0, len(result.Data))
	seen := make(map[string]struct{})
	for _, item := range result.Data {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			continue
		}
		if _, exists := seen[item.ID]; exists {
			continue
		}
		seen[item.ID] = struct{}{}
		archives = append(archives, batchAppBasicInfoItem{ID: item.ID, Name: item.Name})
	}
	if len(archives) == 0 {
		return nil, errBatchResidentNotFound
	}
	return archives, nil
}

func (c *batchBusinessClient) getBasicInfo(ctx context.Context, token, idCard string) (*struct {
	ID       string `json:"id"`
	IDNumber string `json:"idNumber"`
	Name     string `json:"name"`
}, error) {
	payload := map[string]any{
		"fileStatusCode":           "0",
		"desensitization":          "0",
		"name":                     "",
		"idNumber":                 idCard,
		"divisionsCodeOfResidence": "4401",
		"personType":               []string{},
		"pageNum":                  1,
		"pageSize":                 20,
	}
	var result batchBasicInfoResponse
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/apis/yqfk-population/rhr/getRhrBasicInfoList", token, payload, &result); err != nil {
		return nil, err
	}
	if result.Code != 0 && result.Message != "" {
		return nil, errors.New(result.Message)
	}
	if len(result.Data.List) == 0 {
		return nil, errBatchResidentNotFound
	}
	if len(result.Data.List) > 1 {
		return nil, errBatchMultipleResidents
	}
	return &result.Data.List[0], nil
}

func (c *batchBusinessClient) getViewLogs(ctx context.Context, token, infoID string) (*batchViewLogResponse, error) {
	var result batchViewLogResponse
	endpoint := c.baseURL + "/apis/yqfk-population/rhr/getViewLogList/" + url.PathEscape(infoID)
	if err := c.doJSON(ctx, http.MethodGet, endpoint, token, nil, &result); err != nil {
		return nil, err
	}
	if result.Code != 0 && result.Message != "" {
		return nil, errors.New(result.Message)
	}
	return &result, nil
}

func (c *batchBusinessClient) doJSON(ctx context.Context, method, endpoint, token string, payload any, target any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	setBatchBusinessHeaders(req, token)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("业务接口请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("读取业务接口响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("业务接口返回异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("解析业务接口响应失败: %w", err)
	}
	return nil
}

func setBatchBusinessHeaders(req *http.Request, token string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("requestChannel", "PC")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:152.0) Gecko/20100101 Firefox/152.0")
}

func isBatchBusinessAuthError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, keyword := range []string{"http 401", "http 403", "unauthorized", "未授权", "token", "登录", "过期"} {
		if strings.Contains(text, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func batchAccessChannelName(code string) string {
	switch code {
	case "1":
		return "社区通"
	case "2":
		return "医院HIS系统"
	case "3":
		return "数字空间"
	default:
		return code
	}
}

func firstBatchNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
