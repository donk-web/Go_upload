  package ui

  import (
        "fmt"

        "fyne.io/fyne/v2"
        "fyne.io/fyne/v2/container"
        "fyne.io/fyne/v2/dialog"
        "fyne.io/fyne/v2/widget"

        "fyne-getinfo/api"
        "fyne-getinfo/model"
  )

  // BuildQueryView 构建查询主界面
  func BuildQueryView(window fyne.Window) fyne.CanvasObject {
        // === 输入区域 ===
        idCardEntry := widget.NewEntry()
        idCardEntry.SetPlaceHolder("请输入身份证号")

        nameEntry := widget.NewEntry()
        nameEntry.SetPlaceHolder("请输入姓名（可选）")

        // === 结果展示区域 ===
		// 用Label显示结果，文字颜色正常；Scroll容器支持内容多时滚动
		resultLabel := widget.NewLabel("查询结果将显示在这里...")
		resultLabel.TextStyle = fyne.TextStyle{Monospace: true} // 等宽字体，排版整齐
		resultLabel.Wrapping = fyne.TextWrapBreak	//自动换行
		resultScroll := container.NewScroll(resultLabel)

        // === 查询按钮 ===
        queryBtn := widget.NewButton("查询", func() {
                idCard := idCardEntry.Text
                if idCard == "" {
                        dialog.ShowInformation("提示", "请输入身份证号", window)
                        return
                }

                // 1. 构造请求参数
                req := model.Request{
                        IDCard: idCard,
                        Name:   nameEntry.Text,
                }

                // 2. 调用 API（注意：网络请求可能耗时，需要异步）
                // 使用 goroutine 避免阻塞 UI 主线程！！！
                go func() {
                        client := api.NewClient()
                        resp, err := client.QueryResidents(req)

                        // 3. 回到主线程更新 UI
                        // Fyne 的 UI 操作必须在主线程执行，用 fyne.Do
                        if err != nil {
                                fyne.Do(func() {
                                        dialog.ShowError(err, window)
                                        resultLabel.SetText("查询失败: " + err.Error())
                                })
                                return
                        }

                        // 4. 处理响应
                        if resp.Code != 0 {
                                fyne.Do(func() {
                                        dialog.ShowInformation("查询失败", resp.Message, window)
                                        resultLabel.SetText(fmt.Sprintf("错误: %s", resp.Message))
                                })
                                return
                        }

                        // 5. 格式化展示结果
                        resultText := formatResult(resp.Data)
                        fyne.Do(func() {
                                resultLabel.SetText(resultText)
                                dialog.ShowInformation("成功", fmt.Sprintf("共查询到 %d 条记录", len(resp.Data)), window)                        })
                }()
        })

        // === 布局 ===
        // 使用 Border 布局：上-输入表单，中-结果，下-按钮
        inputForm := container.NewVBox(
                widget.NewLabelWithStyle("居民信息查询", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
                widget.NewForm(
                        widget.NewFormItem("身份证号", idCardEntry),
                        widget.NewFormItem("姓名", nameEntry),
                ),
                queryBtn,
        )

        return container.NewBorder(
                inputForm,   // 顶部
                nil,         // 底部
                nil,         // 左边
                nil,         // 右边
                container.NewPadded(resultScroll), // 中间
        )
  }

  // formatResult 把居民数据格式化成可读文本
  func formatResult(residents []model.Resident) string {
        if len(residents) == 0 {
                return "未查询到相关居民数据"
        }

        var text string
        for i, r := range residents {
                text += fmt.Sprintf(
                        "【%d】\n姓名: %s\n身份证: %s\n地址: %s\n状态: %s\n建档时间: %s\n\n",
                        i+1, r.Name, r.IDCard, r.Address, r.Status, r.CreatedAt,
                )
        }
        return text
  }