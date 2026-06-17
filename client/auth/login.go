package auth

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"fyne-getinfo/api"
	"fyne-getinfo/component"
	"fyne-getinfo/session"
)

// BuildLoginUI 构建 exe 启动时的本地账号登录界面。
func BuildLoginUI(window fyne.Window, onSuccess func()) fyne.CanvasObject {
	usernameEntry := widget.NewEntry()
	usernameEntry.SetPlaceHolder("请输入账号")

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("请输入登录密码")

	var loginBtn *widget.Button
	loginBtn = widget.NewButton("登录", func() {
		username := strings.TrimSpace(usernameEntry.Text)
		password := passwordEntry.Text

		if username == "" {
			component.ShowInformation("提示", "账号不能为空", window)
			return
		}

		if password == "" {
			component.ShowInformation("提示", "密码不能为空", window)
			return
		}

		loginBtn.Disable()
		go func() {
			result, err := api.NewClient().Login(username, password)
			fyne.Do(func() {
				loginBtn.Enable()
				if err != nil {
					component.ShowError(err, window)
					return
				}

				session.Set(session.Info{
					Token:        result.Token,
					HospitalCode: result.HospitalCode,
					Username:     result.Username,
					Role:         result.Role,
				})
				if result.Token == "" {
					component.ShowInformation("登录成功", "登录成功，当前登录信息为空，请进入系统后社区通更新登录状态", window)
				} else {
					component.ShowInformation("成功", "登录成功", window)
				}
				onSuccess()
			})
		}()
	})

	usernameRow := container.NewBorder(
		nil, nil,
		widget.NewLabel("账号："),
		nil,
		usernameEntry,
	)

	passwordRow := container.NewBorder(
		nil, nil,
		widget.NewLabel("密码："),
		nil,
		passwordEntry,
	)

	form := container.NewVBox(
		widget.NewLabelWithStyle("居民档案查询系统", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(""),
		usernameRow,
		widget.NewLabel(""),
		passwordRow,
		widget.NewLabel(""),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(180, 48), loginBtn)),
	)

	return container.NewBorder(
		nil, nil, nil, nil,
		container.NewPadded(form),
	)
}
