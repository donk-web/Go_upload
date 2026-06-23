package main

import (
	"time"
)

const (
	batchQueryMethodLegacy = "legacy"
	batchQueryMethodNew    = "new"

	batchJobPending   = "pending"
	batchJobRunning   = "running"
	batchJobPaused    = "paused"
	batchJobCompleted = "completed"
	batchJobStopped   = "stopped"
	batchJobFailed    = "failed"

	batchItemPending  = "pending"
	batchItemRunning  = "running"
	batchItemSuccess  = "success"
	batchItemNotFound = "not_found"
	batchItemFailed   = "failed"
)

type batchJob struct {
	ID             int64      `json:"id"`
	HospitalCode   string     `json:"hospital_code"`
	CreatedBy      string     `json:"created_by"`
	FileName       string     `json:"file_name"`
	QueryMethod    string     `json:"query_method"`
	Status         string     `json:"status"`
	TotalCount     int        `json:"total_count"`
	PendingCount   int        `json:"pending_count"`
	RunningCount   int        `json:"running_count"`
	SuccessCount   int        `json:"success_count"`
	NotFoundCount  int        `json:"not_found_count"`
	FailedCount    int        `json:"failed_count"`
	WorkerCount    int        `json:"worker_count"`
	FetchBatchSize int        `json:"fetch_batch_size"`
	WriteBatchSize int        `json:"write_batch_size"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Progress       float64    `json:"progress"`
	ProcessedCount int        `json:"processed_count"`
	CanStart       bool       `json:"can_start"`
	CanPause       bool       `json:"can_pause"`
	CanResume      bool       `json:"can_resume"`
	CanStop        bool       `json:"can_stop"`
	CanRetry       bool       `json:"can_retry"`
	CanExport      bool       `json:"can_export"`
}

type batchItem struct {
	ID        int64
	JobID     int64
	SourceRow int
	IDCard    string
}

type batchArchiveRecord struct {
	IDCard        string
	Name          string
	Index         int
	ViewTime      string
	ViewOrgName   string
	Department    string
	ViewUserName  string
	AccessChannel string
}

type batchQueryOutcome struct {
	Item      batchItem
	Status    string
	Records   []batchArchiveRecord
	Error     error
	AuthError bool
}
