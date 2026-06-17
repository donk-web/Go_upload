package model

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
