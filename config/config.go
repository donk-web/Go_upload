package config

import (
	"encoding/json"
	"os"
	"sync"
)

// AppConfig 应用配置结构体
type AppConfig struct {
	LoginPassword string `json:"login_password"` //登录密码
	APIBaseURL	string `json:"api_base_url"`   //API基础URL
	APIEndpoint string `json:"api_endpoint"`   //接口路径
	HTTPTimeout int   `json:"http_timeout"`   //HTTP请求超时时间（秒）
	MockMode	bool  `json:"mock_mode"`      //是否启用模拟模式
}

// Current 全局配置实例，程序运行期间都通过它访问配置
var Current AppConfig

// 读写锁，防止多个goroutine同事读写配置导致死锁
var mu sync.RWMutex

func init() {
	// 默认值
	Current = AppConfig{
		LoginPassword: "0000",
		APIBaseURL: "http://localhost:8080",
		APIEndpoint: "/api/query",
		HTTPTimeout: 10,
		MockMode: true,
	}

	_ = Load() // 尝试加载配置文件，失败就用默认值
}

// 和exe同级
func configFile() string {
	return "config.json"
}

func Load() error {
	mu.Lock()
	defer mu.Unlock()

	data, err := os.ReadFile(configFile())
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在，使用默认配置
		}
		return err
	}

	return json.Unmarshal(data, &Current) //解析JSON到Current结构体
}

// Save 将当前配置保存到文件
func Save() error {
	mu.Lock()
	defer mu.Unlock()

	data, err := json.MarshalIndent(Current, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configFile(), data, 0644) // 0644权限，文件所有这可读写，其他人只读
}

// GetConfig 获取当前配置的副本，避免外部修改
func Get() AppConfig {
	mu.RLock()
	defer mu.RUnlock()
	return Current
}

// SetConfig 更新当前配置并保存到文件
func Set(cfg AppConfig) {
	mu.Lock()
	defer mu.Unlock()
	Current = cfg
	// _ = Save() // 保存失败不处理，继续使用新配置
}