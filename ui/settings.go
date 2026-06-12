package ui

import (
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"fyne-getinfo/config"
)

// 构建设置界面
func BuildSettingsView(window fyne.Window) fyne.CanvasObject {
	// 读取当前配置，填充到输入框
	cfg := config.Get()

	// 创建输入框
	apiURLEntry := widget.NewEntry()
	apiURLEntry.SetText(cfg.APIBaseURL)
	apiURLEntry.SetPlaceHolder("请输入API URL,例如：http://localhost:8080")

	apiEndpointEntry := widget.NewEntry()
	apiEndpointEntry.SetText(cfg.APIEndpoint)
	apiEndpointEntry.SetPlaceHolder("请输入API接口路径,例如：/api/query")

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetText(cfg.LoginPassword)

	// HTTP超时时间输入框，要求整数
	timeoutEntry := widget.NewEntry()
	timeoutEntry.SetText(strconv.Itoa(cfg.HTTPTimeout))

	// 模拟模式开关
	mockCheck := widget.NewCheck("启用模拟模式（无需真实接口）", nil)
	mockCheck.SetChecked(cfg.MockMode)

	// 保存按钮
	saveBtn := widget.NewButton("保存设置", func() {
		// 校验超时时间
		timeout, err := strconv.Atoi(timeoutEntry.Text)
		if err != nil || timeout <= 0 {
			dialog.ShowError(fmt.Errorf("超时时间必须是正整数"), window)
			return
		}

		// 构建新配置
		newCfg := config.AppConfig{
			APIBaseURL:	apiURLEntry.Text,
			APIEndpoint:	apiEndpointEntry.Text,
			LoginPassword:	passwordEntry.Text,
			HTTPTimeout:	timeout,
			MockMode:	mockCheck.Checked,
		}

		config.Set(newCfg) // 更新全局配置

		if err := config.Save(); err != nil{
			dialog.ShowError(fmt.Errorf("保存配置失败: %v", err), window)
			return
		}

		dialog.ShowInformation("成功", "配置已保存到config.json", window)

	})


	// 布局
	form := container.NewVBox(
		widget.NewLabelWithStyle("系统设置", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(""),
		widget.NewForm(
			widget.NewFormItem("API地址:", apiURLEntry),
			widget.NewFormItem("接口路径:", apiEndpointEntry),
			widget.NewFormItem("登录密码:", passwordEntry),
			widget.NewFormItem("HTTP超时(秒):", timeoutEntry),
		),
		mockCheck,
		widget.NewLabel(""),
		container.NewCenter(saveBtn),
	)

	return container.NewBorder(
		nil,nil, nil, nil,
		container.NewPadded(form),
	)
}