package auth

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	

	"fyne-getinfo/config"
)

// BuildLoginUI构建并返回登录界面
// onSuccess: 登录成功后的回调函数，用于切换到主界面
func BuildLoginUI(window fyne.Window, onSuccess func()) fyne.CanvasObject {
	// 创建输入框
	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("请输入登录密码")

	// 创建登录按钮
	loginBtn := widget.NewButton("登录", func() {
		input := passwordEntry.Text
		
		// 1.校验密码
		if input == "" {
			dialog.ShowInformation("提示","密码不能为空", window)
			return
		}

		if input != config.Current.LoginPassword {
			dialog.ShowError(fmt.Errorf("密码错误"), window)
			return
		}

		dialog.ShowInformation("成功","登录成功", window)
		onSuccess()
	})

	passwordRow := container.NewBorder(
		nil, nil,
		widget.NewLabel("密码："),
		nil,
		passwordEntry,
	)

	// 布局
	form := container.NewVBox(
		widget.NewLabelWithStyle("居民档案查询系统", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(""),
		passwordRow,
		widget.NewLabel(""),
		container.NewCenter(loginBtn),
	)


	// 整体居中 + 四周留白
	return container.NewBorder(
		nil, nil, nil, nil,                // 上、下、左、右都空着，中间占满
		container.NewPadded(form),         // 中间放 form，四周有内边距
	)
}