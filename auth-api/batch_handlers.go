package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const maxBatchUploadSize = 100 << 20

func createBatchJobHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBatchUploadSize)
		hospitalCode := strings.TrimSpace(c.PostForm("hospital_code"))
		createdBy := strings.TrimSpace(c.PostForm("created_by"))
		if hospitalCode == "" || createdBy == "" {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "医院编号和创建人不能为空"})
			return
		}
		if err := authorizeBatchHospital(c.Request.Context(), db, hospitalCode, batchBearerToken(c)); err != nil {
			c.JSON(http.StatusUnauthorized, apiResponse{Code: 401, Message: err.Error()})
			return
		}
		if err := validateBatchOwner(c.Request.Context(), db, hospitalCode, createdBy); err != nil {
			c.JSON(http.StatusForbidden, apiResponse{Code: 403, Message: err.Error()})
			return
		}
		if _, err := loadActiveBusinessToken(c.Request.Context(), db, hospitalCode); err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "当前医院社区通登录状态不可用：" + err.Error()})
			return
		}

		workerCount, err := batchFormInt(c, "worker_count", 5, 1, 50)
		if err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: err.Error()})
			return
		}
		fetchBatchSize, err := batchFormInt(c, "fetch_batch_size", 500, workerCount, 5000)
		if err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: err.Error()})
			return
		}
		writeBatchSize, err := batchFormInt(c, "write_batch_size", 200, 1, 1000)
		if err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: err.Error()})
			return
		}
		queryMethod := strings.TrimSpace(c.PostForm("query_method"))
		if queryMethod == "" {
			queryMethod = batchQueryMethodLegacy
		}
		if queryMethod != batchQueryMethodLegacy && queryMethod != batchQueryMethodNew {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "查询方式不正确"})
			return
		}

		header, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "请选择需要导入的Excel或CSV文件"})
			return
		}
		extension := strings.ToLower(filepath.Ext(header.Filename))
		if extension != ".xlsx" && extension != ".csv" {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "仅支持 .xlsx 和 .csv 文件"})
			return
		}
		source, err := header.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "打开上传文件失败"})
			return
		}
		defer source.Close()

		temp, err := os.CreateTemp("", "go-upload-batch-*"+extension)
		if err != nil {
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "创建导入临时文件失败"})
			return
		}
		tempPath := temp.Name()
		defer os.Remove(tempPath)
		if _, err := io.Copy(temp, source); err != nil {
			temp.Close()
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "保存上传文件失败"})
			return
		}
		if err := temp.Close(); err != nil {
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "关闭导入临时文件失败"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
		defer cancel()
		jobID, total, err := createBatchJob(
			ctx, db, hospitalCode, createdBy, filepath.Base(header.Filename),
			queryMethod, workerCount, fetchBatchSize, writeBatchSize, tempPath,
		)
		if err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: err.Error()})
			return
		}
		job, err := getBatchJob(c.Request.Context(), db, jobID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "任务已创建，但读取任务信息失败"})
			return
		}
		c.JSON(http.StatusOK, apiResponse{
			Code:    0,
			Message: fmt.Sprintf("成功导入%d条身份证数据", total),
			Data:    job,
		})
	}
}

func getBatchJobHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID, ok := batchPathID(c)
		if !ok {
			return
		}
		if err := authorizeBatchJob(c.Request.Context(), db, jobID, batchBearerToken(c)); err != nil {
			c.JSON(http.StatusUnauthorized, apiResponse{Code: 401, Message: err.Error()})
			return
		}
		job, err := getBatchJob(c.Request.Context(), db, jobID)
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, apiResponse{Code: 404, Message: "批量查询任务不存在"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "读取批量查询任务失败"})
			return
		}
		c.JSON(http.StatusOK, apiResponse{Code: 0, Message: "ok", Data: job})
	}
}

func listBatchJobsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		hospitalCode := strings.TrimSpace(c.Query("hospital_code"))
		if hospitalCode == "" {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "医院编号不能为空"})
			return
		}
		if err := authorizeBatchHospital(c.Request.Context(), db, hospitalCode, batchBearerToken(c)); err != nil {
			c.JSON(http.StatusUnauthorized, apiResponse{Code: 401, Message: err.Error()})
			return
		}
		limit := 20
		if value := strings.TrimSpace(c.Query("limit")); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 || parsed > 100 {
				c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "limit必须在1到100之间"})
				return
			}
			limit = parsed
		}
		jobs, err := listBatchJobs(c.Request.Context(), db, hospitalCode, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "读取最近批量查询任务失败"})
			return
		}
		c.JSON(http.StatusOK, apiResponse{Code: 0, Message: "ok", Data: jobs})
	}
}

func startBatchJobHandler(manager *batchManager) gin.HandlerFunc {
	return batchManagerActionHandler("开始任务", manager, manager.Start)
}

func pauseBatchJobHandler(manager *batchManager) gin.HandlerFunc {
	return batchManagerActionHandler("暂停任务", manager, manager.Pause)
}

func resumeBatchJobHandler(manager *batchManager) gin.HandlerFunc {
	return batchManagerActionHandler("继续任务", manager, manager.Resume)
}

func stopBatchJobHandler(manager *batchManager) gin.HandlerFunc {
	return batchManagerActionHandler("停止任务", manager, manager.Stop)
}

func batchManagerActionHandler(action string, manager *batchManager, fn func(int64) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID, ok := batchPathID(c)
		if !ok {
			return
		}
		if err := authorizeBatchJob(c.Request.Context(), manager.db, jobID, batchBearerToken(c)); err != nil {
			c.JSON(http.StatusUnauthorized, apiResponse{Code: 401, Message: err.Error()})
			return
		}
		if err := fn(jobID); err != nil {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: action + "失败：" + err.Error()})
			return
		}
		c.JSON(http.StatusOK, apiResponse{Code: 0, Message: action + "成功"})
	}
}

func retryBatchJobHandler(db *sql.DB, manager *batchManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID, ok := batchPathID(c)
		if !ok {
			return
		}
		if err := authorizeBatchJob(c.Request.Context(), db, jobID, batchBearerToken(c)); err != nil {
			c.JSON(http.StatusUnauthorized, apiResponse{Code: 401, Message: err.Error()})
			return
		}
		if manager.IsRunning(jobID) {
			c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "任务运行中，不能重试失败数据"})
			return
		}
		count, err := retryFailedBatchItems(c.Request.Context(), db, jobID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "重置失败数据失败"})
			return
		}
		_ = syncBatchJobCounters(c.Request.Context(), db, jobID)
		c.JSON(http.StatusOK, apiResponse{
			Code:    0,
			Message: fmt.Sprintf("已将%d条失败数据恢复为待查询", count),
			Data:    gin.H{"retry_count": count},
		})
	}
}

func exportBatchJobHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID, ok := batchPathID(c)
		if !ok {
			return
		}
		if err := authorizeBatchJob(c.Request.Context(), db, jobID, batchBearerToken(c)); err != nil {
			c.JSON(http.StatusUnauthorized, apiResponse{Code: 401, Message: err.Error()})
			return
		}
		job, err := getBatchJob(c.Request.Context(), db, jobID)
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, apiResponse{Code: 404, Message: "批量查询任务不存在"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "读取批量查询任务失败"})
			return
		}

		rows, err := db.QueryContext(c.Request.Context(), `
			SELECT
				i.source_row, i.id_card, i.status, COALESCE(i.error_message, ''),
				COALESCE(r.person_name, ''), COALESCE(r.record_index, 0),
				COALESCE(r.view_time, ''), COALESCE(r.view_org_name, ''),
				COALESCE(r.department, ''), COALESCE(r.view_user_name, ''),
				COALESCE(r.access_channel, '')
			FROM batch_query_items i
			LEFT JOIN batch_query_results r ON r.item_id = i.id
			WHERE i.job_id = ?
			ORDER BY i.source_row, r.record_index
		`, jobID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, apiResponse{Code: 500, Message: "读取导出结果失败"})
			return
		}
		defer rows.Close()

		fileName := fmt.Sprintf("批量查询结果_%d_%s.csv", job.ID, time.Now().Format("20060102_150405"))
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename*=UTF-8''`+strings.ReplaceAll(url.QueryEscape(fileName), "+", "%20"))
		c.Status(http.StatusOK)
		_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
		writer := csv.NewWriter(c.Writer)
		_ = writer.Write([]string{
			"Excel原始行号", "身份证号", "查询状态", "失败原因", "姓名", "记录序号",
			"调阅时间", "调阅机构", "调阅科室", "调阅人", "调阅渠道",
		})
		for rows.Next() {
			var sourceRow, recordIndex int
			var idCard, status, errorMessage, name, viewTime, orgName, department, viewUser, channel string
			if err := rows.Scan(
				&sourceRow, &idCard, &status, &errorMessage, &name, &recordIndex,
				&viewTime, &orgName, &department, &viewUser, &channel,
			); err != nil {
				return
			}
			_ = writer.Write([]string{
				strconv.Itoa(sourceRow), excelIDCardText(idCard), batchStatusName(status), errorMessage, name,
				zeroBlank(recordIndex), viewTime, orgName, department, viewUser, channel,
			})
		}
		writer.Flush()
	}
}

func batchPathID(c *gin.Context) (int64, bool) {
	jobID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || jobID <= 0 {
		c.JSON(http.StatusBadRequest, apiResponse{Code: 400, Message: "批量查询任务ID不正确"})
		return 0, false
	}
	return jobID, true
}

func batchFormInt(c *gin.Context, name string, defaultValue, minValue, maxValue int) (int, error) {
	value := strings.TrimSpace(c.PostForm(name))
	if value == "" {
		return defaultValue, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < minValue || number > maxValue {
		return 0, fmt.Errorf("%s必须在%d到%d之间", name, minValue, maxValue)
	}
	return number, nil
}

func batchBearerToken(c *gin.Context) string {
	value := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(value) < 7 || !strings.EqualFold(value[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(value[7:])
}

func batchStatusName(status string) string {
	switch status {
	case batchItemPending:
		return "待查询"
	case batchItemRunning:
		return "查询中"
	case batchItemSuccess:
		return "查询成功"
	case batchItemNotFound:
		return "查无此人"
	case batchItemFailed:
		return "查询失败"
	default:
		return status
	}
}

func zeroBlank(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func excelIDCardText(value string) string {
	if value == "" {
		return ""
	}
	return `="` + value + `"`
}
