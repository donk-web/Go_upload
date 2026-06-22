package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sync"
	"time"
)

type batchManager struct {
	db          *sql.DB
	business    *batchBusinessClient
	mu          sync.Mutex
	controllers map[int64]*batchController
}

type batchController struct {
	cancel context.CancelFunc
}

func newBatchManager(db *sql.DB) *batchManager {
	return &batchManager{
		db:          db,
		business:    newBatchBusinessClient(),
		controllers: make(map[int64]*batchController),
	}
}

func (m *batchManager) RecoverInterruptedJobs(ctx context.Context) error {
	if _, err := m.db.ExecContext(ctx, `
		UPDATE batch_query_items i
		INNER JOIN batch_query_jobs j ON j.id = i.job_id
		SET i.status = 'pending', i.claimed_at = NULL, i.started_at = NULL
		WHERE j.status = 'running' AND i.status = 'running'
	`); err != nil {
		return err
	}
	if _, err := m.db.ExecContext(ctx, `
		UPDATE batch_query_jobs
		SET status = 'paused', error_message = '服务重启，任务已自动暂停'
		WHERE status = 'running'
	`); err != nil {
		return err
	}
	return nil
}

func (m *batchManager) Start(jobID int64) error {
	m.mu.Lock()
	if _, exists := m.controllers[jobID]; exists {
		m.mu.Unlock()
		return errors.New("任务正在运行或停止中")
	}
	m.mu.Unlock()

	job, err := getBatchJob(context.Background(), m.db, jobID)
	if err != nil {
		return err
	}
	if job.Status != batchJobPending && job.Status != batchJobPaused {
		return errors.New("当前任务状态不允许开始")
	}
	if err := setBatchJobRunning(context.Background(), m.db, jobID); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	controller := &batchController{cancel: cancel}
	m.mu.Lock()
	m.controllers[jobID] = controller
	m.mu.Unlock()

	go m.run(ctx, controller, job)
	return nil
}

func (m *batchManager) Pause(jobID int64) error {
	job, err := getBatchJob(context.Background(), m.db, jobID)
	if err != nil {
		return err
	}
	if job.Status != batchJobRunning {
		return errors.New("只有查询中的任务可以暂停")
	}
	if err := setBatchJobStatus(context.Background(), m.db, jobID, batchJobPaused, "任务已手动暂停"); err != nil {
		return err
	}
	m.cancel(jobID)
	return nil
}

func (m *batchManager) Stop(jobID int64) error {
	job, err := getBatchJob(context.Background(), m.db, jobID)
	if err != nil {
		return err
	}
	if job.Status != batchJobPending && job.Status != batchJobRunning && job.Status != batchJobPaused {
		return errors.New("当前任务状态不允许停止")
	}
	if err := setBatchJobStatus(context.Background(), m.db, jobID, batchJobStopped, "任务已手动停止"); err != nil {
		return err
	}
	m.cancel(jobID)
	return nil
}

func (m *batchManager) cancel(jobID int64) {
	m.mu.Lock()
	controller := m.controllers[jobID]
	m.mu.Unlock()
	if controller != nil {
		controller.cancel()
	}
}

func (m *batchManager) run(ctx context.Context, controller *batchController, job *batchJob) {
	defer func() {
		if err := resetRunningBatchItems(context.Background(), m.db, job.ID); err != nil {
			log.Printf("恢复批量任务 %d 的运行中明细失败: %v", job.ID, err)
		}
		if err := syncBatchJobCounters(context.Background(), m.db, job.ID); err != nil {
			log.Printf("同步批量任务 %d 统计失败: %v", job.ID, err)
		}
		m.mu.Lock()
		if m.controllers[job.ID] == controller {
			delete(m.controllers, job.ID)
		}
		m.mu.Unlock()
	}()

	token, err := loadActiveBusinessToken(ctx, m.db, job.HospitalCode)
	if err != nil {
		_ = setBatchJobStatus(context.Background(), m.db, job.ID, batchJobPaused, err.Error())
		return
	}

	workerCount := clampInt(job.WorkerCount, 1, 50)
	fetchSize := clampInt(job.FetchBatchSize, workerCount, 5000)
	writeSize := clampInt(job.WriteBatchSize, 1, 1000)

	for {
		if ctx.Err() != nil {
			return
		}
		status, err := currentBatchJobStatus(ctx, m.db, job.ID)
		if err != nil {
			m.failJob(job.ID, err)
			return
		}
		if status != batchJobRunning {
			return
		}

		items, err := claimBatchItems(ctx, m.db, job.ID, fetchSize)
		if err != nil {
			if ctx.Err() == nil {
				m.failJob(job.ID, err)
			}
			return
		}
		if len(items) == 0 {
			_ = syncBatchJobCounters(context.Background(), m.db, job.ID)
			status, _ = currentBatchJobStatus(context.Background(), m.db, job.ID)
			if status == batchJobRunning {
				_ = setBatchJobStatus(context.Background(), m.db, job.ID, batchJobCompleted, "")
			}
			return
		}

		authError, err := m.processBatch(ctx, controller, token, items, workerCount, writeSize)
		if err != nil {
			if ctx.Err() == nil {
				m.failJob(job.ID, err)
			}
			return
		}
		if authError != nil {
			_ = setBatchJobStatus(context.Background(), m.db, job.ID, batchJobPaused, "社区通登录状态已失效："+authError.Error())
			controller.cancel()
			return
		}
		if err := syncBatchJobCounters(context.Background(), m.db, job.ID); err != nil {
			m.failJob(job.ID, err)
			return
		}
	}
}

func (m *batchManager) processBatch(ctx context.Context, controller *batchController, token string, items []batchItem, workerCount, writeSize int) (error, error) {
	taskCh := make(chan batchItem, workerCount*2)
	outcomeCh := make(chan batchQueryOutcome, workerCount*2)
	var workers sync.WaitGroup

	for index := 0; index < workerCount; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case item, ok := <-taskCh:
					if !ok {
						return
					}
					outcome := m.queryItem(ctx, token, item)
					select {
					case outcomeCh <- outcome:
					case <-ctx.Done():
						if outcome.AuthError {
							select {
							case outcomeCh <- outcome:
							default:
							}
						}
						return
					}
				}
			}
		}()
	}

	go func() {
		defer close(taskCh)
		for _, item := range items {
			select {
			case taskCh <- item:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(outcomeCh)
	}()

	buffer := make([]batchQueryOutcome, 0, writeSize)
	var authError error
	flush := func() error {
		if len(buffer) == 0 {
			return nil
		}
		if err := saveBatchOutcomes(context.Background(), m.db, buffer); err != nil {
			return err
		}
		buffer = buffer[:0]
		return nil
	}

	for outcome := range outcomeCh {
		if outcome.AuthError {
			if authError == nil {
				authError = outcome.Error
				controller.cancel()
			}
			continue
		}
		if outcome.Status == "" {
			continue
		}
		buffer = append(buffer, outcome)
		if len(buffer) >= writeSize {
			if err := flush(); err != nil {
				return authError, err
			}
		}
	}
	if err := flush(); err != nil {
		return authError, err
	}
	return authError, nil
}

func (m *batchManager) queryItem(ctx context.Context, token string, item batchItem) batchQueryOutcome {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		records, err := m.business.QueryResident(ctx, token, item.IDCard)
		if err == nil {
			return batchQueryOutcome{Item: item, Status: batchItemSuccess, Records: records}
		}
		if errors.Is(err, errBatchResidentNotFound) {
			return batchQueryOutcome{Item: item, Status: batchItemNotFound, Error: err}
		}
		if errors.Is(err, errBatchMultipleResidents) {
			return batchQueryOutcome{Item: item, Status: batchItemFailed, Error: err}
		}
		if isBatchBusinessAuthError(err) {
			return batchQueryOutcome{Item: item, Error: err, AuthError: true}
		}
		if ctx.Err() != nil {
			return batchQueryOutcome{Item: item}
		}
		lastErr = err
		if attempt < 2 {
			timer := time.NewTimer(time.Duration(attempt+1) * 500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return batchQueryOutcome{Item: item}
			case <-timer.C:
			}
		}
	}
	return batchQueryOutcome{Item: item, Status: batchItemFailed, Error: lastErr}
}

func (m *batchManager) failJob(jobID int64, err error) {
	message := "批量查询执行失败"
	if err != nil {
		message = err.Error()
	}
	if updateErr := setBatchJobStatus(context.Background(), m.db, jobID, batchJobFailed, message); updateErr != nil {
		log.Printf("更新批量任务 %d 失败状态失败: %v", jobID, updateErr)
	}
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func (m *batchManager) Resume(jobID int64) error {
	return m.Start(jobID)
}

func (m *batchManager) IsRunning(jobID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.controllers[jobID]
	return ok
}
