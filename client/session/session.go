package session

import (
	"sync"

	"fyne-getinfo/model"
)

type Info struct {
	Token        string
	HospitalCode string
	Username     string
	Role         string
	Doctor       model.DoctorInfo
}

var (
	mu      sync.RWMutex
	current Info
)

func Set(info Info) {
	mu.Lock()
	defer mu.Unlock()
	current = info
}

func Get() Info {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

func Token() string {
	mu.RLock()
	defer mu.RUnlock()
	return current.Token
}

func SetDoctor(doctor model.DoctorInfo) {
	mu.Lock()
	defer mu.Unlock()
	current.Doctor = doctor
}

func Clear() {
	mu.Lock()
	defer mu.Unlock()
	current = Info{}
}

func (i Info) IsSuperAdmin() bool {
	return i.Role == "super_admin"
}
