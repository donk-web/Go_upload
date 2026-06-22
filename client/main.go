package main

import (
	"crypto/rand"
	"math/big"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"fyne-getinfo/api"
	"fyne-getinfo/auth"
	"fyne-getinfo/config"
	"fyne-getinfo/model"
	"fyne-getinfo/session"
	"fyne-getinfo/ui"
)

func main() {
	a := app.New()
	cfg := config.Get()
	if cfg.ThemeMode != "system" {
		a.Settings().SetTheme(ui.NewFixedTheme(cfg.BackgroundHex))
	}

	w := a.NewWindow("居民档案查询系统")
	w.Resize(fyne.NewSize(float32(cfg.WindowWidth), float32(cfg.WindowHeight)))
	w.CenterOnScreen() // 窗口居中

	showLoginView(w)
	w.ShowAndRun()
}

func showLoginView(w fyne.Window) {
	session.Clear()
	w.SetMainMenu(nil)
	w.SetTitle("居民档案查询系统")

	loginView := auth.BuildLoginUI(w, func() {
		// 登录成功后，切换到带菜单的主界面
		showMainView(w)
	})

	w.SetContent(loginView)
}

// showMainView 显示带菜单的主界面
func showMainView(w fyne.Window) {
	stopKeepAlive := make(chan struct{})
	var logoutOnce sync.Once

	current := session.Get()
	isSuperAdmin := current.IsSuperAdmin()
	loginInfoLabel := widget.NewLabel(formatLoginInfo(current))
	loginInfoLabel.Alignment = fyne.TextAlignTrailing

	refreshLoginInfo := func() {
		if session.Token() == "" {
			loginInfoLabel.SetText(formatLoginInfo(session.Get()))
			return
		}
		go func() {
			doctor, err := api.NewClient().GetCurrentDoctorInfo()
			if err != nil {
				return
			}
			session.SetDoctor(*doctor)
			fyne.Do(func() {
				loginInfoLabel.SetText(formatLoginInfo(session.Get()))
			})
		}()
	}
	startTokenKeepAlive(w, stopKeepAlive, refreshLoginInfo)

	// 内容容器
	content := container.NewMax()

	queryView := ui.BuildQueryView(w)
	batchQueryView := ui.BuildBatchQueryView(w)

	// 默认显示查询界面
	content.Objects = []fyne.CanvasObject{queryView}

	switchView := func(title string, view fyne.CanvasObject) {
		content.Objects = []fyne.CanvasObject{view}
		content.Refresh()
		w.SetTitle("居民档案查询系统 - " + title)
	}

	homeBtn := widget.NewButton("首页查询", func() {
		switchView("首页查询", queryView)
	})
	batchQueryBtn := widget.NewButton("批量查询", func() {
		switchView("批量查询", batchQueryView)
	})
	logout := func() {
		logoutOnce.Do(func() {
			close(stopKeepAlive)
			showLoginView(w)
		})
	}
	refreshTokenBtn := widget.NewButton("更新社区通登录状态", func() {
		current := session.Get()
		auth.ShowDoctorTokenDialog(w, current.HospitalCode, func(result *model.LoginResult) {
			session.Set(session.Info{
				Token:        result.Token,
				HospitalCode: current.HospitalCode,
				Username:     current.Username,
				Role:         current.Role,
				Doctor:       doctorFallback(result.Username),
			})
			loginInfoLabel.SetText(formatLoginInfo(session.Get()))
			refreshLoginInfo()
		})
	})
	logoutBtn := widget.NewButton("退出登录", logout)

	navItems := []fyne.CanvasObject{homeBtn, batchQueryBtn}
	menuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("首页查询", func() {
			switchView("首页查询", queryView)
		}),
		fyne.NewMenuItem("批量查询", func() {
			switchView("批量查询", batchQueryView)
		}),
	}

	if isSuperAdmin {
		settingsView := ui.BuildSettingsView(w)
		settingsBtn := widget.NewButton("系统配置", func() {
			switchView("系统配置", settingsView)
		})
		navItems = append(navItems, settingsBtn)
		menuItems = append(menuItems, fyne.NewMenuItem("系统配置", func() {
			switchView("系统配置", settingsView)
		}))
	}

	navItems = append(navItems, refreshTokenBtn)
	navItems = append(navItems, logoutBtn)
	nav := container.NewHBox(navItems...)

	menuItems = append(menuItems, fyne.NewMenuItem("社区通状态更新", func() {
		current := session.Get()
		auth.ShowDoctorTokenDialog(w, current.HospitalCode, func(result *model.LoginResult) {
			session.Set(session.Info{
				Token:        result.Token,
				HospitalCode: current.HospitalCode,
				Username:     current.Username,
				Role:         current.Role,
				Doctor:       doctorFallback(result.Username),
			})
			loginInfoLabel.SetText(formatLoginInfo(session.Get()))
			refreshLoginInfo()
		})
	}))
	menuItems = append(menuItems, fyne.NewMenuItem("退出登录", logout))

	// 菜单栏
	mainMenu := fyne.NewMainMenu(
		fyne.NewMenu("功能", menuItems...),
	)
	w.SetMainMenu(mainMenu)

	// 切换窗口内容
	topBar := container.NewBorder(nil, nil, nav, loginInfoLabel)
	w.SetContent(container.NewBorder(topBar, nil, nil, nil, content))
	w.SetTitle("居民档案查询系统 - 首页查询")
	refreshLoginInfo()
}

func startTokenKeepAlive(w fyne.Window, stop <-chan struct{}, onTokenUpdated func()) {
	var mu sync.Mutex
	refreshing := false

	isStopped := func() bool {
		select {
		case <-stop:
			return true
		default:
			return false
		}
	}

	checkToken := func() {
		if isStopped() || session.Token() == "" {
			return
		}

		err := api.NewClient().KeepBusinessTokenAlive()
		if isStopped() {
			return
		}
		if err == nil || !api.IsBusinessAuthError(err) {
			return
		}

		mu.Lock()
		if refreshing {
			mu.Unlock()
			return
		}
		refreshing = true
		mu.Unlock()

		fyne.Do(func() {
			if isStopped() {
				return
			}
			dialog.ShowConfirm("社区通登录状态已失效", "社区通登录状态已失效，是否现在登录并更新社区通登录状态？", func(ok bool) {
				if isStopped() {
					return
				}
				if !ok {
					mu.Lock()
					refreshing = false
					mu.Unlock()
					return
				}

				current := session.Get()
				auth.ShowDoctorTokenDialog(w, current.HospitalCode, func(result *model.LoginResult) {
					if isStopped() {
						return
					}
					session.Set(session.Info{
						Token:        result.Token,
						HospitalCode: current.HospitalCode,
						Username:     current.Username,
						Role:         current.Role,
						Doctor:       doctorFallback(result.Username),
					})
					if onTokenUpdated != nil {
						onTokenUpdated()
					}
				})
				mu.Lock()
				refreshing = false
				mu.Unlock()
			}, w)
		})
	}

	go func() {
		checkToken()

		for {
			timer := time.NewTimer(randomKeepAliveInterval())
			select {
			case <-stop:
				timer.Stop()
				return
			case <-timer.C:
			}
			checkToken()
		}
	}()
}

func formatLoginInfo(info session.Info) string {
	parts := make([]string, 0, 4)
	if info.Doctor.Name != "" {
		parts = append(parts, "当前医生："+info.Doctor.Name)
	} else if info.Doctor.Account != "" {
		parts = append(parts, "医生账号："+info.Doctor.Account)
	} else {
		parts = append(parts, "当前账号："+info.Username)
	}

	hospital := info.Doctor.Hospital
	if hospital == "" {
		hospital = info.HospitalCode
	}
	if hospital != "" {
		parts = append(parts, "机构："+hospital)
	}
	if info.Doctor.Department != "" {
		parts = append(parts, "科室："+info.Doctor.Department)
	}
	if info.Doctor.Role != "" {
		parts = append(parts, "角色："+info.Doctor.Role)
	}
	return strings.Join(parts, "  |  ")
}

func doctorFallback(account string) model.DoctorInfo {
	account = strings.TrimSpace(account)
	if account == "" {
		return model.DoctorInfo{}
	}
	return model.DoctorInfo{Account: account}
}

func randomKeepAliveInterval() time.Duration {
	const (
		minMinutes = 8
		maxMinutes = 13
	)

	n, err := rand.Int(rand.Reader, big.NewInt(maxMinutes-minMinutes+1))
	if err != nil {
		return 10 * time.Minute
	}
	return time.Duration(minMinutes+int(n.Int64())) * time.Minute
}
