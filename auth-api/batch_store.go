package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func createBatchJob(ctx context.Context, db *sql.DB, hospitalCode, createdBy, fileName string, workerCount, fetchBatchSize, writeBatchSize int, filePath string) (int64, int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO batch_query_jobs (
			hospital_code, created_by, file_name, status,
			worker_count, fetch_batch_size, write_batch_size
		) VALUES (?, ?, ?, 'pending', ?, ?, ?)
	`, hospitalCode, createdBy, fileName, workerCount, fetchBatchSize, writeBatchSize)
	if err != nil {
		return 0, 0, err
	}
	jobID, err := result.LastInsertId()
	if err != nil {
		return 0, 0, err
	}

	inserter := &batchItemInserter{ctx: ctx, tx: tx, jobID: jobID, batchSize: 500}
	total, err := importBatchFile(filePath, inserter.Add)
	if err != nil {
		return 0, 0, err
	}
	if err := inserter.Flush(); err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE batch_query_jobs
		SET total_count = ?, pending_count = ?
		WHERE id = ?
	`, total, total, jobID); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return jobID, total, nil
}

type batchItemInserter struct {
	ctx       context.Context
	tx        *sql.Tx
	jobID     int64
	batchSize int
	rows      []batchImportRow
}

type batchImportRow struct {
	sourceRow int
	idCard    string
}

func (i *batchItemInserter) Add(sourceRow int, idCard string) error {
	i.rows = append(i.rows, batchImportRow{sourceRow: sourceRow, idCard: idCard})
	if len(i.rows) >= i.batchSize {
		return i.Flush()
	}
	return nil
}

func (i *batchItemInserter) Flush() error {
	if len(i.rows) == 0 {
		return nil
	}
	placeholders := make([]string, 0, len(i.rows))
	args := make([]any, 0, len(i.rows)*3)
	for _, row := range i.rows {
		placeholders = append(placeholders, "(?, ?, ?, 'pending')")
		args = append(args, i.jobID, row.sourceRow, row.idCard)
	}
	query := `
		INSERT INTO batch_query_items (job_id, source_row, id_card, status)
		VALUES ` + strings.Join(placeholders, ",")
	if _, err := i.tx.ExecContext(i.ctx, query, args...); err != nil {
		return err
	}
	i.rows = i.rows[:0]
	return nil
}

func getBatchJob(ctx context.Context, db *sql.DB, jobID int64) (*batchJob, error) {
	var job batchJob
	var startedAt, completedAt sql.NullTime
	var errorMessage sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT
			j.id, j.hospital_code, j.created_by, j.file_name, j.status,
			(SELECT COUNT(*) FROM batch_query_items i WHERE i.job_id = j.id) AS total_count,
			(SELECT COUNT(*) FROM batch_query_items i WHERE i.job_id = j.id AND i.status = 'pending') AS pending_count,
			(SELECT COUNT(*) FROM batch_query_items i WHERE i.job_id = j.id AND i.status = 'running') AS running_count,
			(SELECT COUNT(*) FROM batch_query_items i WHERE i.job_id = j.id AND i.status = 'success') AS success_count,
			(SELECT COUNT(*) FROM batch_query_items i WHERE i.job_id = j.id AND i.status = 'not_found') AS not_found_count,
			(SELECT COUNT(*) FROM batch_query_items i WHERE i.job_id = j.id AND i.status = 'failed') AS failed_count,
			j.worker_count, j.fetch_batch_size, j.write_batch_size,
			j.error_message, j.started_at, j.completed_at, j.created_at, j.updated_at
		FROM batch_query_jobs j
		WHERE j.id = ?
	`, jobID).Scan(
		&job.ID, &job.HospitalCode, &job.CreatedBy, &job.FileName, &job.Status,
		&job.TotalCount, &job.PendingCount, &job.RunningCount, &job.SuccessCount,
		&job.NotFoundCount, &job.FailedCount, &job.WorkerCount, &job.FetchBatchSize,
		&job.WriteBatchSize, &errorMessage, &startedAt, &completedAt,
		&job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if errorMessage.Valid {
		job.ErrorMessage = errorMessage.String
	}
	if startedAt.Valid {
		value := startedAt.Time
		job.StartedAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time
		job.CompletedAt = &value
	}
	job.ProcessedCount = job.SuccessCount + job.NotFoundCount + job.FailedCount
	if job.TotalCount > 0 {
		job.Progress = float64(job.ProcessedCount) / float64(job.TotalCount)
	}
	job.CanStart = job.Status == batchJobPending
	job.CanPause = job.Status == batchJobRunning
	job.CanResume = job.Status == batchJobPaused
	job.CanStop = job.Status == batchJobPending || job.Status == batchJobRunning || job.Status == batchJobPaused
	job.CanRetry = job.FailedCount > 0 && job.Status != batchJobRunning
	job.CanExport = job.ProcessedCount > 0
	return &job, nil
}

func listBatchJobs(ctx context.Context, db *sql.DB, hospitalCode string, limit int) ([]batchJob, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id
		FROM batch_query_jobs
		WHERE hospital_code = ?
		ORDER BY id DESC
		LIMIT ?
	`, hospitalCode, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	jobs := make([]batchJob, 0, len(ids))
	for _, id := range ids {
		job, err := getBatchJob(ctx, db, id)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	return jobs, nil
}

func setBatchJobRunning(ctx context.Context, db *sql.DB, jobID int64) error {
	result, err := db.ExecContext(ctx, `
		UPDATE batch_query_jobs
		SET status = 'running',
			error_message = NULL,
			started_at = COALESCE(started_at, NOW()),
			completed_at = NULL
		WHERE id = ? AND status IN ('pending', 'paused')
	`, jobID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return errors.New("当前任务状态不允许开始")
	}
	return nil
}

func setBatchJobStatus(ctx context.Context, db *sql.DB, jobID int64, status, message string) error {
	completed := status == batchJobCompleted || status == batchJobStopped || status == batchJobFailed
	message = truncateBatchText(message, 1000)
	_, err := db.ExecContext(ctx, `
		UPDATE batch_query_jobs
		SET status = ?,
			error_message = NULLIF(?, ''),
			completed_at = CASE WHEN ? THEN NOW() ELSE completed_at END
		WHERE id = ?
	`, status, message, completed, jobID)
	return err
}

func currentBatchJobStatus(ctx context.Context, db *sql.DB, jobID int64) (string, error) {
	var status string
	err := db.QueryRowContext(ctx, `SELECT status FROM batch_query_jobs WHERE id = ?`, jobID).Scan(&status)
	return status, err
}

func claimBatchItems(ctx context.Context, db *sql.DB, jobID int64, limit int) ([]batchItem, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, job_id, source_row, id_card
		FROM batch_query_items
		WHERE job_id = ? AND status = 'pending'
		ORDER BY id
		LIMIT ?
		FOR UPDATE
	`, jobID, limit)
	if err != nil {
		return nil, err
	}
	var items []batchItem
	for rows.Next() {
		var item batchItem
		if err := rows.Scan(&item.ID, &item.JobID, &item.SourceRow, &item.IDCard); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	ids := make([]string, len(items))
	args := make([]any, 0, len(items)+1)
	for index, item := range items {
		ids[index] = "?"
		args = append(args, item.ID)
	}
	args = append(args, jobID)
	query := `
		UPDATE batch_query_items
		SET status = 'running', claimed_at = NOW(), started_at = NOW()
		WHERE id IN (` + strings.Join(ids, ",") + `) AND job_id = ?`
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func saveBatchOutcomes(ctx context.Context, db *sql.DB, outcomes []batchQueryOutcome) error {
	if len(outcomes) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	deleteResults, err := tx.PrepareContext(ctx, `DELETE FROM batch_query_results WHERE item_id = ?`)
	if err != nil {
		return err
	}
	defer deleteResults.Close()
	insertResult, err := tx.PrepareContext(ctx, `
		INSERT INTO batch_query_results (
			job_id, item_id, id_card, person_name, record_index,
			view_time, view_org_name, department, view_user_name, access_channel
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer insertResult.Close()
	updateItem, err := tx.PrepareContext(ctx, `
		UPDATE batch_query_items
		SET status = ?,
			error_message = NULLIF(?, ''),
			retry_count = retry_count + ?,
			completed_at = NOW()
		WHERE id = ?
	`)
	if err != nil {
		return err
	}
	defer updateItem.Close()

	for _, outcome := range outcomes {
		if outcome.Status == "" {
			continue
		}
		if _, err := deleteResults.ExecContext(ctx, outcome.Item.ID); err != nil {
			return err
		}
		for _, record := range outcome.Records {
			if _, err := insertResult.ExecContext(
				ctx, outcome.Item.JobID, outcome.Item.ID, record.IDCard, record.Name, record.Index,
				record.ViewTime, record.ViewOrgName, record.Department, record.ViewUserName, record.AccessChannel,
			); err != nil {
				return err
			}
		}
		errorMessage := ""
		retryIncrement := 0
		if outcome.Error != nil {
			errorMessage = truncateBatchText(outcome.Error.Error(), 1000)
			if outcome.Status == batchItemFailed {
				retryIncrement = 1
			}
		}
		if _, err := updateItem.ExecContext(ctx, outcome.Status, errorMessage, retryIncrement, outcome.Item.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func resetRunningBatchItems(ctx context.Context, db *sql.DB, jobID int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE batch_query_items
		SET status = 'pending', claimed_at = NULL, started_at = NULL
		WHERE job_id = ? AND status = 'running'
	`, jobID)
	return err
}

func retryFailedBatchItems(ctx context.Context, db *sql.DB, jobID int64) (int64, error) {
	result, err := db.ExecContext(ctx, `
		UPDATE batch_query_items
		SET status = 'pending', error_message = NULL, claimed_at = NULL,
			started_at = NULL, completed_at = NULL
		WHERE job_id = ? AND status = 'failed'
	`, jobID)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if count > 0 {
		if _, err := db.ExecContext(ctx, `
			UPDATE batch_query_jobs
			SET status = 'pending', error_message = NULL, completed_at = NULL
			WHERE id = ? AND status <> 'running'
		`, jobID); err != nil {
			return 0, err
		}
	}
	return count, nil
}

func loadActiveBusinessToken(ctx context.Context, db *sql.DB, hospitalCode string) (string, error) {
	var token string
	var enabled bool
	var expiresAt sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(business_token, ''), enabled, expires_at
		FROM hospital_business_tokens
		WHERE hospital_code = ?
		LIMIT 1
	`, hospitalCode).Scan(&token, &enabled, &expiresAt)
	if err != nil {
		return "", err
	}
	if !enabled {
		return "", errors.New("业务token已禁用")
	}
	if expiresAt.Valid && time.Now().After(expiresAt.Time) {
		return "", errors.New("业务token已过期")
	}
	if strings.TrimSpace(token) == "" {
		return "", errors.New("业务token为空")
	}
	return token, nil
}

func validateBatchOwner(ctx context.Context, db *sql.DB, hospitalCode, username string) error {
	var exists int
	err := db.QueryRowContext(ctx, `
		SELECT 1
		FROM hospital_clients
		WHERE hospital_code = ? AND username = ? AND enabled = 1
		LIMIT 1
	`, hospitalCode, username).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("当前账号与医院不匹配或已禁用")
	}
	return err
}

func authorizeBatchHospital(ctx context.Context, db *sql.DB, hospitalCode, token string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("缺少批量查询访问凭证")
	}
	var exists int
	err := db.QueryRowContext(ctx, `
		SELECT 1
		FROM hospital_business_tokens
		WHERE hospital_code = ?
			AND business_token = ?
			AND enabled = 1
			AND (expires_at IS NULL OR expires_at > NOW())
		LIMIT 1
	`, hospitalCode, token).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("批量查询访问凭证无效或已过期")
	}
	return err
}

func authorizeBatchJob(ctx context.Context, db *sql.DB, jobID int64, token string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("缺少批量查询访问凭证")
	}
	var exists int
	err := db.QueryRowContext(ctx, `
		SELECT 1
		FROM batch_query_jobs j
		INNER JOIN hospital_business_tokens t ON t.hospital_code = j.hospital_code
		WHERE j.id = ?
			AND t.business_token = ?
			AND t.enabled = 1
			AND (t.expires_at IS NULL OR t.expires_at > NOW())
		LIMIT 1
	`, jobID, token).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("批量查询任务不存在，或访问凭证无效")
	}
	return err
}

func syncBatchJobCounters(ctx context.Context, db *sql.DB, jobID int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE batch_query_jobs j
		SET
			total_count = (SELECT COUNT(*) FROM batch_query_items i WHERE i.job_id = j.id),
			pending_count = (SELECT COUNT(*) FROM batch_query_items i WHERE i.job_id = j.id AND i.status IN ('pending', 'running')),
			success_count = (SELECT COUNT(*) FROM batch_query_items i WHERE i.job_id = j.id AND i.status = 'success'),
			not_found_count = (SELECT COUNT(*) FROM batch_query_items i WHERE i.job_id = j.id AND i.status = 'not_found'),
			failed_count = (SELECT COUNT(*) FROM batch_query_items i WHERE i.job_id = j.id AND i.status = 'failed')
		WHERE j.id = ?
	`, jobID)
	if err != nil {
		return fmt.Errorf("同步任务统计失败: %w", err)
	}
	return nil
}

func truncateBatchText(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}
