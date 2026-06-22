package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// TestBatchStress 使用真实MySQL和模拟业务接口测试完整批量调度链路。
// 默认跳过；通过 RUN_BATCH_STRESS=1 显式启用。
func TestBatchStress(t *testing.T) {
	if os.Getenv("RUN_BATCH_STRESS") != "1" {
		t.Skip("set RUN_BATCH_STRESS=1 to run integration stress test")
	}
	dsn := strings.TrimSpace(os.Getenv("MYSQL_DSN"))
	if dsn == "" {
		t.Fatal("MYSQL_DSN is required")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(20)
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	if err := ensureBatchSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	delay := stressEnvInt("BATCH_STRESS_DELAY_MS", 10)
	count := stressEnvInt("BATCH_STRESS_COUNT", 1000)
	workerProfiles := stressWorkerProfiles(os.Getenv("BATCH_STRESS_WORKERS"))
	var requests atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/getRhrBasicInfoList") {
			var payload struct {
				IDNumber string `json:"idNumber"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_, _ = fmt.Fprintf(
				w,
				`{"code":0,"data":{"list":[{"id":"resident-%s","idNumber":"%s","name":"压测居民"}]}}`,
				payload.IDNumber,
				payload.IDNumber,
			)
			return
		}
		if strings.Contains(r.URL.Path, "/getViewLogList/") {
			_, _ = w.Write([]byte(`{"code":0,"data":[{"viewTime":"2026-06-18 10:00:00","viewOrgName":"压测医院","departmentName":"全科","viewUserName":"压测医生","accessChannel":"1"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	for _, workers := range workerProfiles {
		t.Run(fmt.Sprintf("workers_%d", workers), func(t *testing.T) {
			jobID := insertStressJob(t, db, count, workers)
			t.Cleanup(func() {
				_, _ = db.Exec(`DELETE FROM batch_query_jobs WHERE id = ?`, jobID)
			})

			manager := newBatchManager(db)
			manager.business = &batchBusinessClient{
				baseURL: server.URL,
				client: &http.Client{
					Transport: &http.Transport{
						MaxIdleConns:        workers * 4,
						MaxIdleConnsPerHost: workers * 2,
						IdleConnTimeout:     30 * time.Second,
					},
					Timeout: 10 * time.Second,
				},
			}

			startRequests := requests.Load()
			started := time.Now()
			if err := manager.Start(jobID); err != nil {
				t.Fatal(err)
			}
			job := waitStressJob(t, db, jobID, 10*time.Minute)
			elapsed := time.Since(started)
			requestCount := requests.Load() - startRequests
			throughput := float64(job.ProcessedCount) / elapsed.Seconds()
			requestThroughput := float64(requestCount) / elapsed.Seconds()

			t.Logf(
				"records=%d workers=%d delay_ms=%d elapsed=%s throughput=%.2f records/s requests=%d request_rate=%.2f req/s success=%d failed=%d",
				count,
				workers,
				delay,
				elapsed.Round(time.Millisecond),
				throughput,
				requestCount,
				requestThroughput,
				job.SuccessCount,
				job.FailedCount,
			)
			if job.Status != batchJobCompleted || job.SuccessCount != count || job.FailedCount != 0 {
				t.Fatalf("unexpected final job: %#v", job)
			}
		})
	}
}

func insertStressJob(t *testing.T, db *sql.DB, count, workers int) int64 {
	t.Helper()
	hospitalCode := fmt.Sprintf("__stress_%d_%d", time.Now().UnixNano(), workers)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO hospital_business_tokens (
			hospital_code, business_token, enabled, expires_at, remark
		) VALUES (?, 'stress-token', 1, NULL, '自动压力测试，任务删除后保留标记')
	`, hospitalCode); err != nil {
		t.Fatal(err)
	}
	result, err := tx.Exec(`
		INSERT INTO batch_query_jobs (
			hospital_code, created_by, file_name, status,
			total_count, pending_count, worker_count, fetch_batch_size, write_batch_size
		) VALUES (?, 'stress-test', 'stress.csv', 'pending', ?, ?, ?, 500, 200)
	`, hospitalCode, count, count, workers)
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	const batchSize = 500
	for start := 0; start < count; start += batchSize {
		end := start + batchSize
		if end > count {
			end = count
		}
		placeholders := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*3)
		for index := start; index < end; index++ {
			placeholders = append(placeholders, "(?, ?, ?, 'pending')")
			args = append(args, jobID, index+2, fmt.Sprintf("440101199001%06d", index))
		}
		if _, err := tx.Exec(`
			INSERT INTO batch_query_items (job_id, source_row, id_card, status)
			VALUES `+strings.Join(placeholders, ","), args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM hospital_business_tokens WHERE hospital_code = ?`, hospitalCode)
	})
	return jobID
}

func waitStressJob(t *testing.T, db *sql.DB, jobID int64, timeout time.Duration) *batchJob {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := getBatchJob(context.Background(), db, jobID)
		if err != nil {
			t.Fatal(err)
		}
		switch job.Status {
		case batchJobCompleted, batchJobFailed, batchJobStopped:
			return job
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("stress job %d timed out", jobID)
	return nil
}

func stressWorkerProfiles(value string) []int {
	if strings.TrimSpace(value) == "" {
		return []int{1, 5, 10, 20}
	}
	var profiles []int
	for _, part := range strings.Split(value, ",") {
		number, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && number > 0 && number <= 50 {
			profiles = append(profiles, number)
		}
	}
	if len(profiles) == 0 {
		return []int{5}
	}
	return profiles
}

func stressEnvInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
