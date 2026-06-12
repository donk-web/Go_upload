package main

import (
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"fyne-getinfo/auth"
	"fyne-getinfo/ui"
)

func main() {
	a := app.New()

	w := a.NewWindow("居民档案查询系统")
	w.Resize(fyne.NewSize(600, 500))
	w.CenterOnScreen()

	// 先显示登录界面
	loginView := auth.BuildLoginUI(w, func() {
			// 登录成功后，切换到带菜单的主界面
			showMainView(w)
	})

	w.SetContent(loginView)
	w.ShowAndRun()
}

// showMainView 显示带菜单的主界面
func showMainView(w fyne.Window) {
	// 内容容器
	content := container.NewMax()

	queryView := ui.BuildQueryView(w)
	settingsView := ui.BuildSettingsView(w)

	// 默认显示查询界面
	content.Objects = []fyne.CanvasObject{queryView}

	switchView := func(view fyne.CanvasObject) {
			content.Objects = []fyne.CanvasObject{view}
			content.Refresh()
	}

	// 菜单栏
	mainMenu := fyne.NewMainMenu(
			fyne.NewMenu("功能",
					fyne.NewMenuItem("首页查询", func() {
							switchView(queryView)
					}),
					fyne.NewMenuItem("系统配置", func() {
							switchView(settingsView)
					}),
			),
	)
	w.SetMainMenu(mainMenu)

	// 切换窗口内容
	w.SetContent(content)
	w.SetTitle("居民档案查询系统 - 首页查询")
}