package ui

import (
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"fyne-getinfo/component"
	"fyne-getinfo/config"
)

// 构建设置界面
func BuildSettingsView(window fyne.Window) fyne.CanvasObject {
	// 读取当前配置，填充到输入框
	cfg := config.Get()

	// 创建输入框
	authURLEntry := widget.NewEntry()
	authURLEntry.SetText(cfg.AuthBaseURL)
	authURLEntry.SetPlaceHolder("请输入登录后端地址,例如：http://localhost:8080")

	loginEndpointEntry := widget.NewEntry()
	loginEndpointEntry.SetText(cfg.LoginEndpoint)
	loginEndpointEntry.SetPlaceHolder("请输入登录接口路径,例如：/api/login")

	apiURLEntry := widget.NewEntry()
	apiURLEntry.SetText(cfg.APIBaseURL)
	apiURLEntry.SetPlaceHolder("请输入业务平台地址,例如：https://yqfk.wjw.gz.gov.cn")

	// HTTP超时时间输入框，要求整数
	timeoutEntry := widget.NewEntry()
	timeoutEntry.SetText(strconv.Itoa(cfg.HTTPTimeout))

	windowWidthEntry := widget.NewEntry()
	windowWidthEntry.SetText(strconv.Itoa(cfg.WindowWidth))

	windowHeightEntry := widget.NewEntry()
	windowHeightEntry.SetText(strconv.Itoa(cfg.WindowHeight))

	backgroundEntry := widget.NewEntry()
	backgroundEntry.SetText(cfg.BackgroundHex)
	backgroundEntry.SetPlaceHolder("例如：#F7F9FC")

	themeModeCheck := widget.NewCheck("跟随系统深浅色", nil)
	themeModeCheck.SetChecked(cfg.ThemeMode == "system")

	// 模拟模式开关
	mockCheck := widget.NewCheck("启用模拟模式（无需真实接口）", nil)
	mockCheck.SetChecked(cfg.MockMode)

	// 业务调试打印开关
	businessDebugCheck := widget.NewCheck("启用业务调试打印", nil)
	businessDebugCheck.SetChecked(cfg.BusinessDebug)

	// 保存按钮
	saveBtn := widget.NewButton("保存设置", func() {
		// 校验超时时间
		timeout, err := strconv.Atoi(timeoutEntry.Text)
		if err != nil || timeout <= 0 {
			component.ShowError(fmt.Errorf("超时时间必须是正整数"), window)
			return
		}

		windowWidth, err := strconv.Atoi(windowWidthEntry.Text)
		if err != nil || windowWidth <= 0 {
			component.ShowError(fmt.Errorf("窗口宽度必须是正整数"), window)
			return
		}

		windowHeight, err := strconv.Atoi(windowHeightEntry.Text)
		if err != nil || windowHeight <= 0 {
			component.ShowError(fmt.Errorf("窗口高度必须是正整数"), window)
			return
		}

		themeMode := "light"
		if themeModeCheck.Checked {
			themeMode = "system"
		}

		// 构建新配置
		newCfg := config.AppConfig{
			LoginPassword: cfg.LoginPassword,
			AuthBaseURL:   authURLEntry.Text,
			LoginEndpoint: loginEndpointEntry.Text,
			APIBaseURL:    apiURLEntry.Text,
			APIEndpoint:   cfg.APIEndpoint,
			HTTPTimeout:   timeout,
			WindowWidth:   windowWidth,
			WindowHeight:  windowHeight,
			ThemeMode:     themeMode,
			BackgroundHex: backgroundEntry.Text,
			MockMode:      mockCheck.Checked,
			BusinessDebug: businessDebugCheck.Checked,
		}

		config.Set(newCfg) // 更新全局配置

		if err := config.Save(); err != nil {
			component.ShowError(fmt.Errorf("保存配置失败: %v", err), window)
			return
		}

		component.ShowInformation("成功", "配置已保存到config.json，窗口大小和背景颜色将在下次启动时生效", window)

	})

	// 布局
	form := container.NewVBox(
		widget.NewLabelWithStyle("系统设置", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(""),
		widget.NewForm(
			widget.NewFormItem("登录后端:", authURLEntry),
			widget.NewFormItem("登录接口:", loginEndpointEntry),
			widget.NewFormItem("业务平台地址:", apiURLEntry),
			widget.NewFormItem("HTTP超时(秒):", timeoutEntry),
			widget.NewFormItem("窗口宽度:", windowWidthEntry),
			widget.NewFormItem("窗口高度:", windowHeightEntry),
			widget.NewFormItem("背景颜色:", backgroundEntry),
		),
		themeModeCheck,
		mockCheck,
		businessDebugCheck,
		widget.NewLabel(""),
		container.NewCenter(saveBtn),
	)

	return container.NewBorder(
		nil, nil, nil, nil,
		container.NewPadded(form),
	)
}
