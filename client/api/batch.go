package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne-getinfo/config"
	"fyne-getinfo/model"
	"fyne-getinfo/session"
)

type BatchJobCreateOptions struct {
	HospitalCode   string
	CreatedBy      string
	FileName       string
	File           io.Reader
	WorkerCount    int
	FetchBatchSize int
	WriteBatchSize int
	QueryMethod    string
}

type batchJobResponse struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    model.BatchJob `json:"data"`
}

type batchActionResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type batchJobListResponse struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    []model.BatchJob `json:"data"`
}

func (c *Client) CreateBatchJob(options BatchJobCreateOptions) (*model.BatchJob, error) {
	if options.File == nil {
		return nil, errors.New("请选择需要导入的文件")
	}
	if strings.TrimSpace(options.HospitalCode) == "" || strings.TrimSpace(options.CreatedBy) == "" {
		return nil, errors.New("当前登录账号信息不完整")
	}

	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	go func() {
		var writeErr error
		defer func() {
			if closeErr := multipartWriter.Close(); writeErr == nil {
				writeErr = closeErr
			}
			_ = writer.CloseWithError(writeErr)
		}()

		fields := map[string]string{
			"hospital_code":    options.HospitalCode,
			"created_by":       options.CreatedBy,
			"worker_count":     strconv.Itoa(options.WorkerCount),
			"fetch_batch_size": strconv.Itoa(options.FetchBatchSize),
			"write_batch_size": strconv.Itoa(options.WriteBatchSize),
			"query_method":     options.QueryMethod,
		}
		for name, value := range fields {
			if writeErr = multipartWriter.WriteField(name, value); writeErr != nil {
				return
			}
		}

		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeMultipartFileName(filepath.Base(options.FileName))))
		header.Set("Content-Type", "application/octet-stream")
		part, err := multipartWriter.CreatePart(header)
		if err != nil {
			writeErr = err
			return
		}
		_, writeErr = io.Copy(part, options.File)
	}()

	req, err := http.NewRequest(http.MethodPost, authURL("/api/batch-jobs"), reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		return nil, err
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	setBatchAuthHeader(req)

	var result batchJobResponse
	if err := doBatchJSON(req, 15*time.Minute, &result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(defaultString(result.Message, "创建批量查询任务失败"))
	}
	return &result.Data, nil
}

func (c *Client) GetBatchJob(jobID int64) (*model.BatchJob, error) {
	req, err := http.NewRequest(http.MethodGet, authURL(fmt.Sprintf("/api/batch-jobs/%d", jobID)), nil)
	if err != nil {
		return nil, err
	}
	setBatchAuthHeader(req)

	var result batchJobResponse
	if err := doBatchJSON(req, batchHTTPTimeout(), &result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(defaultString(result.Message, "读取批量查询任务失败"))
	}
	return &result.Data, nil
}

func (c *Client) ListBatchJobs(hospitalCode string, limit int) ([]model.BatchJob, error) {
	if limit < 1 {
		limit = 20
	}
	endpoint := fmt.Sprintf(
		"/api/batch-jobs?hospital_code=%s&limit=%d",
		url.QueryEscape(hospitalCode),
		limit,
	)
	req, err := http.NewRequest(http.MethodGet, authURL(endpoint), nil)
	if err != nil {
		return nil, err
	}
	setBatchAuthHeader(req)

	var result batchJobListResponse
	if err := doBatchJSON(req, batchHTTPTimeout(), &result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(defaultString(result.Message, "读取最近批量查询任务失败"))
	}
	return result.Data, nil
}

func (c *Client) StartBatchJob(jobID int64) error {
	return c.batchJobAction(jobID, "start")
}

func (c *Client) PauseBatchJob(jobID int64) error {
	return c.batchJobAction(jobID, "pause")
}

func (c *Client) ResumeBatchJob(jobID int64) error {
	return c.batchJobAction(jobID, "resume")
}

func (c *Client) StopBatchJob(jobID int64) error {
	return c.batchJobAction(jobID, "stop")
}

func (c *Client) RetryBatchJob(jobID int64) error {
	return c.batchJobAction(jobID, "retry")
}

func (c *Client) batchJobAction(jobID int64, action string) error {
	req, err := http.NewRequest(
		http.MethodPost,
		authURL(fmt.Sprintf("/api/batch-jobs/%d/%s", jobID, action)),
		nil,
	)
	if err != nil {
		return err
	}
	setBatchAuthHeader(req)

	var result batchActionResponse
	if err := doBatchJSON(req, batchHTTPTimeout(), &result); err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(defaultString(result.Message, "批量查询操作失败"))
	}
	return nil
}

func (c *Client) ExportBatchJob(jobID int64, target io.Writer) error {
	if target == nil {
		return errors.New("导出目标不能为空")
	}
	req, err := http.NewRequest(http.MethodGet, authURL(fmt.Sprintf("/api/batch-jobs/%d/export", jobID)), nil)
	if err != nil {
		return err
	}
	setBatchAuthHeader(req)

	httpClient := &http.Client{Timeout: 30 * time.Minute}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载批量查询结果失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var result batchActionResponse
		if json.Unmarshal(data, &result) == nil && result.Message != "" {
			return errors.New(result.Message)
		}
		return fmt.Errorf("下载批量查询结果失败: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if _, err := io.Copy(target, resp.Body); err != nil {
		return fmt.Errorf("保存批量查询结果失败: %w", err)
	}
	return nil
}

func doBatchJSON(req *http.Request, timeout time.Duration, target any) error {
	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("批量查询接口请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("读取批量查询接口响应失败: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("解析批量查询接口响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if response, ok := target.(*batchJobResponse); ok && response.Message != "" {
			return errors.New(response.Message)
		}
		if response, ok := target.(*batchActionResponse); ok && response.Message != "" {
			return errors.New(response.Message)
		}
		if response, ok := target.(*batchJobListResponse); ok && response.Message != "" {
			return errors.New(response.Message)
		}
		return fmt.Errorf("批量查询接口返回异常: HTTP %d", resp.StatusCode)
	}
	return nil
}

func setBatchAuthHeader(req *http.Request) {
	if token := session.Token(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func batchHTTPTimeout() time.Duration {
	timeout := time.Duration(config.Get().HTTPTimeout) * time.Second
	if timeout < 10*time.Second {
		return 10 * time.Second
	}
	return timeout
}

func escapeMultipartFileName(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}
