package model

type Request struct {
	IDCard string `json:"id_card"`
	Name   string `json:"name,omitempty"`
}

type Resident struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IDCard    string `json:"id_card"`
	Address   string `json:"address"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    []Resident  `json:"data"`
}

type LoginResult struct {

}