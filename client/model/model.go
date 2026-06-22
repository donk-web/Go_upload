package model

import "time"

type Request struct {
	IDCard string `json:"id_card"`
	Name   string `json:"name,omitempty"`
}

type ArchiveViewLog struct {
	IDCard        string `json:"id_card"`
	Name          string `json:"name"`
	Index         int    `json:"index"`
	ViewTime      string `json:"view_time"`
	ViewOrgName   string `json:"view_org_name"`
	Department    string `json:"department"`
	ViewUserName  string `json:"view_user_name"`
	AccessChannel string `json:"access_channel"`
}

type Response struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    []ArchiveViewLog `json:"data"`
}

type LoginResult struct {
	Token        string `json:"token"`
	HospitalCode string `json:"hospital_code"`
	Username     string `json:"username"`
	Role         string `json:"role"`
}

// DoctorInfo 是业务系统中当前 token 对应的医生信息。
type DoctorInfo struct {
	Name       string `json:"name"`
	Account    string `json:"account"`
	Hospital   string `json:"hospital"`
	Department string `json:"department"`
	Role       string `json:"role"`
}

type YZYLoginStartResult struct {
	FlowID        string `json:"flow_id"`
	PageURL       string `json:"page_url"`
	QRImageBase64 string `json:"qr_image_base64"`
	ContentType   string `json:"content_type"`
	ExpiresIn     int    `json:"expires_in"`
}

type YZYLoginStatusResult struct {
	Status  string       `json:"status"`
	Message string       `json:"message"`
	Result  *LoginResult `json:"result"`
}

type BatchJob struct {
	ID             int64      `json:"id"`
	HospitalCode   string     `json:"hospital_code"`
	CreatedBy      string     `json:"created_by"`
	FileName       string     `json:"file_name"`
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
	ErrorMessage   string     `json:"error_message"`
	StartedAt      *time.Time `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
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
