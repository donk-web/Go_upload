package auth

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"fyne-getinfo/api"
	"fyne-getinfo/component"
	"fyne-getinfo/model"
)

// ShowDoctorTokenDialog 仅用于业务 token 失效后，让医生完成真实 SSO 登录并刷新 token。
func ShowDoctorTokenDialog(window fyne.Window, hospitalCode string, onSuccess func(*model.LoginResult)) {
	usernameEntry := widget.NewEntry()
	usernameEntry.SetPlaceHolder("请输入医生账号")

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("请输入医生登录密码")

	captchaEntry := widget.NewEntry()
	captchaEntry.SetPlaceHolder("请输入图形验证码")

	phoneCodeEntry := widget.NewEntry()
	phoneCodeEntry.SetPlaceHolder("请输入手机验证码")

	phoneStatusLabel := widget.NewLabel("正在获取真实图形验证码...")
	phoneStatusLabel.Wrapping = fyne.TextWrapWord
	qrStatusLabel := widget.NewLabel("点击下方按钮创建粤政易扫码登录")
	qrStatusLabel.Wrapping = fyne.TextWrapWord

	codeID := ""
	captchaImage := canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, 120, 45)))
	captchaImage.FillMode = canvas.ImageFillContain

	refreshCaptcha := func() {
		phoneStatusLabel.SetText("正在获取真实图形验证码...")
		go func() {
			result, err := api.NewClient().GetDoctorCaptcha()
			fyne.Do(func() {
				if err != nil {
					codeID = ""
					captchaImage.Image = image.NewRGBA(image.Rect(0, 0, 120, 45))
					captchaImage.Refresh()
					phoneStatusLabel.SetText("图形验证码获取失败")
					component.ShowError(err, window)
					return
				}

				img, err := decodeCaptchaImage(result.ImageBase64)
				if err != nil {
					codeID = ""
					phoneStatusLabel.SetText("图形验证码图片解析失败")
					component.ShowError(err, window)
					return
				}

				codeID = result.CodeID
				captchaImage.Image = img
				captchaImage.Refresh()
				captchaEntry.SetText("")
				phoneCodeEntry.SetText("")
				phoneStatusLabel.SetText("请输入医生账号、密码和图形验证码，然后发送手机验证码")
			})
		}()
	}

	refreshBtn := widget.NewButton("刷新", refreshCaptcha)
	var tokenDialog dialog.Dialog

	var sendCodeBtn *widget.Button
	sendCodeBtn = widget.NewButton("发送验证码", func() {
		username := strings.TrimSpace(usernameEntry.Text)
		password := passwordEntry.Text
		captcha := strings.TrimSpace(captchaEntry.Text)

		if username == "" {
			component.ShowInformation("提示", "医生账号不能为空", window)
			return
		}
		if password == "" {
			component.ShowInformation("提示", "医生密码不能为空", window)
			return
		}
		if codeID == "" {
			component.ShowInformation("提示", "图形验证码还未加载成功", window)
			return
		}
		if captcha == "" {
			component.ShowInformation("提示", "图形验证码不能为空", window)
			return
		}

		sendCodeBtn.Disable()
		phoneStatusLabel.SetText("正在发送手机验证码...")
		go func() {
			result, err := api.NewClient().SendDoctorPhoneCode(username, password, codeID, captcha)
			fyne.Do(func() {
				if err != nil {
					sendCodeBtn.Enable()
					phoneStatusLabel.SetText("手机验证码发送失败")
					refreshCaptcha()
					component.ShowError(err, window)
					return
				}

				if result.NextCaptcha != nil {
					img, err := decodeCaptchaImage(result.NextCaptcha.ImageBase64)
					if err != nil {
						sendCodeBtn.Enable()
						phoneStatusLabel.SetText("下一张图形验证码图片解析失败")
						component.ShowError(err, window)
						return
					}
					codeID = result.NextCaptcha.CodeID
					captchaImage.Image = img
					captchaImage.Refresh()
					captchaEntry.SetText("")
				}

				phoneStatusLabel.SetText("手机验证码已发送，请输入新的图形验证码和手机验证码后更新 token")
				go countdownSendButton(sendCodeBtn, phoneStatusLabel)
			})
		}()
	})

	var refreshTokenBtn *widget.Button
	refreshTokenBtn = widget.NewButton("更新社区通登录状态", func() {
		username := strings.TrimSpace(usernameEntry.Text)
		password := passwordEntry.Text
		captcha := strings.TrimSpace(captchaEntry.Text)
		phoneCode := strings.TrimSpace(phoneCodeEntry.Text)

		if hospitalCode == "" {
			component.ShowInformation("提示", "当前本地账号未绑定医院编号", window)
			return
		}
		if username == "" || password == "" || codeID == "" || captcha == "" || phoneCode == "" {
			component.ShowInformation("提示", "医生账号、密码、图形验证码和手机验证码都不能为空", window)
			return
		}

		refreshTokenBtn.Disable()
		phoneStatusLabel.SetText("正在更新社区通登录状态...")
		go func() {
			result, err := api.NewClient().RefreshDoctorToken(hospitalCode, username, password, codeID, captcha, phoneCode)
			fyne.Do(func() {
				refreshTokenBtn.Enable()
				if err != nil {
					phoneStatusLabel.SetText("社区通登录状态更新失败")
					component.ShowError(err, window)
					return
				}

				phoneStatusLabel.SetText("社区通登录状态已更新")
				if onSuccess != nil {
					onSuccess(result)
				}
				component.ShowInformationWithCallback("成功", "社区通登录状态已更新", window, func() {
					if tokenDialog != nil {
						tokenDialog.Hide()
					}
				})
			})
		}()
	})

	qrImage := canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, 260, 260)))
	qrImage.FillMode = canvas.ImageFillContain
	qrImage.Hide()
	qrMask := canvas.NewRectangle(color.NRGBA{R: 0, G: 0, B: 0, A: 110})
	qrMask.Hide()

	currentYZYFlowID := ""
	var qrWrap *fyne.Container
	decodeYZYQRCode := func(result *model.YZYLoginStartResult) (image.Image, error) {
		return decodeCaptchaImage(result.QRImageBase64)
	}

	var scanLoginBtn *widget.Button
	var refreshQRBtn *widget.Button
	scanLoginBtn = widget.NewButton("粤政易扫码登录", func() {
		if hospitalCode == "" {
			component.ShowInformation("提示", "当前本地账号未绑定医院编号", window)
			return
		}

		scanLoginBtn.Disable()
		if refreshQRBtn != nil {
			refreshQRBtn.Disable()
		}
		qrStatusLabel.SetText("正在创建粤政易扫码登录...")
		go func() {
			start, err := api.NewClient().StartYZYLogin(hospitalCode)
			if err != nil {
				fyne.Do(func() {
					scanLoginBtn.Enable()
					if refreshQRBtn != nil {
						refreshQRBtn.Enable()
					}
					qrStatusLabel.SetText("粤政易扫码登录创建失败")
					component.ShowError(err, window)
				})
				return
			}

			img, err := decodeYZYQRCode(start)
			if err != nil {
				fyne.Do(func() {
					scanLoginBtn.Enable()
					if refreshQRBtn != nil {
						refreshQRBtn.Enable()
					}
					qrStatusLabel.SetText("粤政易二维码解析失败")
					component.ShowError(err, window)
				})
				return
			}

			fyne.Do(func() {
				currentYZYFlowID = start.FlowID
				qrMask.Hide()
				refreshQRBtn.Hide()
				refreshQRBtn.Disable()
				qrImage.Image = img
				qrImage.Show()
				qrImage.Refresh()
				qrWrap.Show()
				qrStatusLabel.SetText("请使用粤政易 APP 扫码登录")
			})

			pollYZYLoginStatus(window, start.FlowID, scanLoginBtn, refreshQRBtn, qrMask, qrStatusLabel, tokenDialog, onSuccess)
		}()
	})
	refreshQRBtn = widget.NewButton("刷新二维码", func() {
		if currentYZYFlowID == "" {
			component.ShowInformation("提示", "请先创建粤政易扫码登录", window)
			return
		}

		scanLoginBtn.Disable()
		refreshQRBtn.Disable()
		qrStatusLabel.SetText("正在刷新粤政易二维码...")
		go func() {
			refreshed, err := api.NewClient().RefreshYZYLogin(currentYZYFlowID)
			if err != nil {
				fyne.Do(func() {
					scanLoginBtn.Enable()
					refreshQRBtn.Enable()
					qrStatusLabel.SetText("粤政易二维码刷新失败")
					component.ShowError(err, window)
				})
				return
			}

			img, err := decodeYZYQRCode(refreshed)
			if err != nil {
				fyne.Do(func() {
					scanLoginBtn.Enable()
					refreshQRBtn.Enable()
					qrStatusLabel.SetText("粤政易二维码解析失败")
					component.ShowError(err, window)
				})
				return
			}

			fyne.Do(func() {
				currentYZYFlowID = refreshed.FlowID
				qrMask.Hide()
				refreshQRBtn.Hide()
				refreshQRBtn.Disable()
				qrImage.Image = img
				qrImage.Show()
				qrImage.Refresh()
				qrWrap.Show()
				qrStatusLabel.SetText("请使用粤政易 APP 扫码登录")
			})
			pollYZYLoginStatus(window, refreshed.FlowID, scanLoginBtn, refreshQRBtn, qrMask, qrStatusLabel, tokenDialog, onSuccess)
		}()
	})
	refreshQRBtn.Hide()
	qrWrap = container.NewStack(
		qrImage,
		qrMask,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(140, 44), refreshQRBtn)),
	)
	qrWrap.Hide()

	captchaBox := container.NewGridWrap(fyne.NewSize(120, 45), captchaImage)
	phoneContent := container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("医生账号", usernameEntry),
			widget.NewFormItem("医生密码", passwordEntry),
			widget.NewFormItem("图形验证码", container.NewBorder(nil, nil, nil, container.NewHBox(captchaBox, refreshBtn), captchaEntry)),
			widget.NewFormItem("手机验证码", container.NewBorder(nil, nil, nil, sendCodeBtn, phoneCodeEntry)),
		),
		phoneStatusLabel,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(180, 44), refreshTokenBtn)),
	)

	qrContent := container.NewVBox(
		qrStatusLabel,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(260, 260), qrWrap)),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(180, 44), scanLoginBtn)),
	)

	content := container.NewAppTabs(
		container.NewTabItem("手机验证登录", phoneContent),
		container.NewTabItem("粤政易登录", qrContent),
	)
	content.SetTabLocation(container.TabLocationTop)

	tokenDialog = dialog.NewCustom("社区通状态更新", "关闭", content, window)
	tokenDialog.Resize(fyne.NewSize(600, 620))
	tokenDialog.Show()
	refreshCaptcha()
}

func pollYZYLoginStatus(window fyne.Window, flowID string, scanLoginBtn *widget.Button, refreshQRBtn *widget.Button, qrMask *canvas.Rectangle, statusLabel *widget.Label, tokenDialog dialog.Dialog, onSuccess func(*model.LoginResult)) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)
	for {
		select {
		case <-timeout:
			fyne.Do(func() {
				scanLoginBtn.Enable()
				qrMask.Show()
				qrMask.Refresh()
				refreshQRBtn.Show()
				refreshQRBtn.Enable()
				statusLabel.SetText("粤政易扫码登录已超时")
			})
			return
		case <-ticker.C:
			result, err := api.NewClient().GetYZYLoginStatus(flowID)
			if err != nil {
				fyne.Do(func() {
					scanLoginBtn.Enable()
					qrMask.Show()
					qrMask.Refresh()
					refreshQRBtn.Show()
					refreshQRBtn.Enable()
					statusLabel.SetText("粤政易扫码登录状态查询失败")
					component.ShowError(err, window)
				})
				return
			}

			switch result.Status {
			case "success":
				fyne.Do(func() {
					scanLoginBtn.Enable()
					qrMask.Hide()
					refreshQRBtn.Hide()
					statusLabel.SetText("社区通登录状态已更新")
					if result.Result != nil && onSuccess != nil {
						onSuccess(result.Result)
					}
					component.ShowInformationWithCallback("成功", "社区通登录状态已更新", window, func() {
						if tokenDialog != nil {
							tokenDialog.Hide()
						}
					})
				})
				return
			case "failed":
				fyne.Do(func() {
					scanLoginBtn.Enable()
					qrMask.Show()
					qrMask.Refresh()
					refreshQRBtn.Show()
					refreshQRBtn.Enable()
					statusLabel.SetText("粤政易扫码登录失败")
					if result.Message == "" {
						result.Message = "粤政易扫码登录失败"
					}
					component.ShowError(fmt.Errorf("%s", result.Message), window)
				})
				return
			case "expired":
				fyne.Do(func() {
					scanLoginBtn.Enable()
					qrMask.Show()
					qrMask.Refresh()
					refreshQRBtn.Show()
					refreshQRBtn.Enable()
				})
				return
			default:
				if result.Message != "" {
					fyne.Do(func() {
						statusLabel.SetText(result.Message)
					})
				}
			}
		}
	}
}

func decodeCaptchaImage(encoded string) (image.Image, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return img, nil
}

func countdownSendButton(sendCodeBtn *widget.Button, statusLabel *widget.Label) {
	for second := 60; second > 0; second-- {
		current := second
		fyne.Do(func() {
			sendCodeBtn.SetText(fmt.Sprintf("重发(%ds)", current))
		})
		time.Sleep(time.Second)
	}

	fyne.Do(func() {
		sendCodeBtn.SetText("发送验证码")
		sendCodeBtn.Enable()
		statusLabel.SetText("如未收到验证码，可重新发送")
	})
}
