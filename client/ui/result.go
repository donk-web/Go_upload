package ui

import (
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"fyne-getinfo/api"
	"fyne-getinfo/auth"
	"fyne-getinfo/component"
	"fyne-getinfo/model"
	"fyne-getinfo/session"
)

// BuildQueryView 构建查询主界面
func BuildQueryView(window fyne.Window) fyne.CanvasObject {
	// === 输入区域 ===
	idCardEntry := widget.NewEntry()
	idCardEntry.SetPlaceHolder("请输入身份证号")

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("请输入姓名（可选）")

	statusLabel := widget.NewLabel("查询结果将显示在这里")
	records := []model.ArchiveViewLog{}
	headers := []string{"证件号码", "姓名", "序号", "调阅时间", "调阅机构", "调阅科室", "调阅人", "调阅渠道"}

	resultTable := widget.NewTable(
		func() (int, int) {
			return len(records) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.Wrapping = fyne.TextTruncate
			return label
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			label := obj.(*widget.Label)
			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.SetText(headers[id.Col])
				return
			}

			label.TextStyle = fyne.TextStyle{}
			record := records[id.Row-1]
			label.SetText(viewLogCellText(record, id.Col))
		},
	)
	resultTable.SetColumnWidth(0, 180)
	resultTable.SetColumnWidth(1, 90)
	resultTable.SetColumnWidth(2, 60)
	resultTable.SetColumnWidth(3, 170)
	resultTable.SetColumnWidth(4, 220)
	resultTable.SetColumnWidth(5, 120)
	resultTable.SetColumnWidth(6, 100)
	resultTable.SetColumnWidth(7, 120)

	// === 查询按钮 ===
	var queryBtn *widget.Button
	queryBtn = widget.NewButton("查询", func() {
		idCard := idCardEntry.Text
		if idCard == "" {
			component.ShowInformation("提示", "请输入身份证号", window)
			return
		}

		req := model.Request{
			IDCard: idCard,
			Name:   nameEntry.Text,
		}

		queryBtn.Disable()
		statusLabel.SetText("正在查询...")

		go func() {
			client := api.NewClient()
			resp, err := client.QueryResidents(req)

			fyne.Do(func() {
				queryBtn.Enable()
				if err != nil {
					records = nil
					resultTable.Refresh()
					statusLabel.SetText("查询失败: " + err.Error())
					if api.IsBusinessAuthError(err) {
						dialog.ShowConfirm("社区通登录状态已失效", "是否现在登录并更新社区通登录状态？", func(ok bool) {
							if !ok {
								return
							}
							current := session.Get()
							auth.ShowDoctorTokenDialog(window, current.HospitalCode, func(result *model.LoginResult) {
								session.Set(session.Info{
									Token:        result.Token,
									HospitalCode: current.HospitalCode,
									Username:     current.Username,
									Role:         current.Role,
									Doctor:       current.Doctor,
								})
								statusLabel.SetText("社区通登录状态已更新，请重新查询")
							})
						}, window)
						return
					}
					component.ShowError(err, window)
					return
				}

				if resp.Code != 0 {
					records = nil
					resultTable.Refresh()
					statusLabel.SetText(fmt.Sprintf("查询失败: %s", resp.Message))
					component.ShowInformation("查询失败", resp.Message, window)
					return
				}

				records = resp.Data
				resultTable.Refresh()
				statusLabel.SetText(fmt.Sprintf("共查询到 %d 条查阅记录", len(records)))
			})
		}()
	})

	inputForm := container.NewVBox(
		widget.NewLabelWithStyle("居民档案查阅记录", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewForm(
			widget.NewFormItem("身份证号", idCardEntry),
			widget.NewFormItem("姓名", nameEntry),
		),
		queryBtn,
		statusLabel,
	)

	return container.NewBorder(
		inputForm,
		nil,
		nil,
		nil,
		container.NewPadded(resultTable),
	)
}

func viewLogCellText(record model.ArchiveViewLog, col int) string {
	switch col {
	case 0:
		return record.IDCard
	case 1:
		return record.Name
	case 2:
		return strconv.Itoa(record.Index)
	case 3:
		return record.ViewTime
	case 4:
		return record.ViewOrgName
	case 5:
		return record.Department
	case 6:
		return record.ViewUserName
	case 7:
		return record.AccessChannel
	default:
		return ""
	}
}
