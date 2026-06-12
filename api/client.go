package api

  import (
		"fmt"
        // "encoding/json"
        "errors"
        "time"

        "fyne-getinfo/config"
        "fyne-getinfo/model"
  )

  type Client struct{}

  func NewClient() *Client {
        return &Client{}
  }

  // QueryResidents 查询居民数据
  func (c *Client) QueryResidents(req model.Request) (*model.Response, error) {
        // ========== 模拟模式 ==========
        if config.Current.MockMode {
                // 模拟网络延迟 800ms，体验更真实
                time.Sleep(800 * time.Millisecond)

                // 模拟"输入指定身份证才有数据"的场景
                if req.IDCard == "" {
                        return nil, errors.New("身份证号不能为空")
                }

                // 返回假数据
                return &model.Response{
                        Code:    0,
                        Message: "success",
                        Data: []model.Resident{
                                {
                                        ID:        "10001",
                                        Name:      "张三",
                                        IDCard:    req.IDCard,
                                        Address:   "北京市朝阳区建国路88号",
                                        Status:    "在档",
                                        CreatedAt: "2023-05-12",
                                },
                                {
                                        ID:        "10002",
                                        Name:      "张三（迁入）",
                                        IDCard:    req.IDCard,
                                        Address:   "上海市浦东新区世纪大道1号",
                                        Status:    "迁入待审",
                                        CreatedAt: "2025-01-20",
                                },
                        },
                }, nil
        }

        // ========== 真实请求模式 ==========
        return c.queryReal(req)
  }

  // 把原来的真实请求代码移到这个私有方法里
  func (c *Client) queryReal(req model.Request) (*model.Response, error) {
        // 这里放你之前写的 HTTP POST 代码
        // ... json.Marshal、http.NewRequest、httpClient.Do 等 ...
        // 为了编译通过先返回个空
        return nil, fmt.Errorf("真实接口未配置")
  }