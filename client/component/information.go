package component

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ShowInformation shows an information dialog without Fyne's default info icon.
func ShowInformation(title, message string, parent fyne.Window) {
	ShowInformationWithCallback(title, message, parent, nil)
}

func ShowError(err error, parent fyne.Window) {
	if err == nil {
		return
	}
	ShowInformationWithCallback("错误", err.Error(), parent, nil)
}

func ShowInformationWithCallback(title, message string, parent fyne.Window, callback func()) {
	label := widget.NewLabel(message)
	label.Alignment = fyne.TextAlignCenter
	label.Wrapping = fyne.TextWrapWord

	var dlg *dialog.CustomDialog
	okBtn := widget.NewButton("确定", func() {
		if dlg != nil {
			dlg.Hide()
		}
		if callback != nil {
			callback()
		}
	})
	content := container.NewVBox(
		container.NewPadded(label),
		container.NewCenter(okBtn),
	)

	dlg = dialog.NewCustomWithoutButtons(title, content, parent)
	dlg.Resize(dialogSize(message))
	dlg.Show()
}

func dialogSize(message string) fyne.Size {
	messageLen := len([]rune(message))
	if messageLen > 120 {
		return fyne.NewSize(760, 280)
	}
	if messageLen > 18 {
		return fyne.NewSize(420, 180)
	}
	return fyne.NewSize(260, 160)
}
