package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"fyne-getinfo/api"
	"fyne-getinfo/auth"
	"fyne-getinfo/component"
	"fyne-getinfo/model"
	"fyne-getinfo/session"
)

// BuildBatchQueryView 构建批量查询任务的完整操作界面。
func BuildBatchQueryView(window fyne.Window) fyne.CanvasObject {
	var selectedURI fyne.URI
	var currentJob *model.BatchJob
	var pollCancel context.CancelFunc
	var promptedTokenJobID int64
	busy := false

	fileLabel := widget.NewLabel("尚未选择文件")
	fileLabel.Wrapping = fyne.TextTruncate
	fileTip := widget.NewLabel("支持 .xlsx、.csv；输入列只需包含身份证号，身份证列必须设置为文本格式。")
	fileTip.Wrapping = fyne.TextWrapWord

	workerSelect := widget.NewSelect([]string{"3", "5", "10", "20"}, nil)
	workerSelect.SetSelected("5")
	fetchBatchSelect := widget.NewSelect([]string{"200", "500", "1000", "2000"}, nil)
	fetchBatchSelect.SetSelected("500")
	writeBatchSelect := widget.NewSelect([]string{"100", "200", "500"}, nil)
	writeBatchSelect.SetSelected("200")

	statusLabel := widget.NewLabel("等待选择批量查询文件")
	statusLabel.Wrapping = fyne.TextWrapWord
	progress := widget.NewProgressBar()
	progress.Min = 0
	progress.Max = 1

	totalValue := batchStatisticValue()
	pendingValue := batchStatisticValue()
	successValue := batchStatisticValue()
	notFoundValue := batchStatisticValue()
	failedValue := batchStatisticValue()
	speedValue := batchStatisticValue()
	remainingValue := batchStatisticValue()
	speedValue.SetText("0 人/秒")
	remainingValue.SetText("--")

	var selectFileBtn *widget.Button
	var recentJobsBtn *widget.Button
	var startBtn *widget.Button
	var pauseBtn *widget.Button
	var continueBtn *widget.Button
	var stopBtn *widget.Button
	var retryBtn *widget.Button
	var exportBtn *widget.Button
	var applyJob func(*model.BatchJob, float64)
	var startPolling func(int64)
	var promptTokenRefresh func(*model.BatchJob)

	setSelectEnabled := func(selectWidget *widget.Select, enabled bool) {
		if enabled {
			selectWidget.Enable()
		} else {
			selectWidget.Disable()
		}
	}

	refreshControls := func() {
		if busy {
			selectFileBtn.Disable()
			recentJobsBtn.Disable()
			startBtn.Disable()
			pauseBtn.Disable()
			continueBtn.Disable()
			stopBtn.Disable()
			retryBtn.Disable()
			exportBtn.Disable()
			setSelectEnabled(workerSelect, false)
			setSelectEnabled(fetchBatchSelect, false)
			setSelectEnabled(writeBatchSelect, false)
			return
		}

		running := currentJob != nil && currentJob.Status == "running"
		if running {
			selectFileBtn.Disable()
			recentJobsBtn.Disable()
		} else {
			selectFileBtn.Enable()
			recentJobsBtn.Enable()
		}
		setSelectEnabled(workerSelect, !running)
		setSelectEnabled(fetchBatchSelect, !running)
		setSelectEnabled(writeBatchSelect, !running)

		canCreate := selectedURI != nil && (currentJob == nil ||
			currentJob.Status == "completed" || currentJob.Status == "stopped" || currentJob.Status == "failed")
		canStartExisting := currentJob != nil && currentJob.CanStart
		if canCreate || canStartExisting {
			startBtn.Enable()
		} else {
			startBtn.Disable()
		}
		setButtonEnabled(pauseBtn, currentJob != nil && currentJob.CanPause)
		setButtonEnabled(continueBtn, currentJob != nil && currentJob.CanResume)
		setButtonEnabled(stopBtn, currentJob != nil && currentJob.CanStop)
		setButtonEnabled(retryBtn, currentJob != nil && currentJob.CanRetry)
		setButtonEnabled(exportBtn, currentJob != nil && currentJob.CanExport)
	}

	setBusy := func(value bool, message string) {
		busy = value
		if message != "" {
			statusLabel.SetText(message)
		}
		refreshControls()
	}

	applyJob = func(job *model.BatchJob, speed float64) {
		if job == nil {
			return
		}
		currentJob = job
		totalValue.SetText(strconv.Itoa(job.TotalCount))
		pendingValue.SetText(strconv.Itoa(job.PendingCount + job.RunningCount))
		successValue.SetText(strconv.Itoa(job.SuccessCount))
		notFoundValue.SetText(strconv.Itoa(job.NotFoundCount))
		failedValue.SetText(strconv.Itoa(job.FailedCount))
		progress.SetValue(clampProgress(job.Progress))
		speedValue.SetText(fmt.Sprintf("%.1f 人/秒", speed))

		remaining := job.PendingCount + job.RunningCount
		if remaining == 0 {
			remainingValue.SetText("0秒")
		} else if speed > 0 {
			remainingValue.SetText(formatBatchDuration(time.Duration(float64(remaining)/speed) * time.Second))
		} else {
			remainingValue.SetText("--")
		}
		statusLabel.SetText(batchJobStatusText(job))
		refreshControls()

		if job.Status == "paused" && isBatchTokenMessage(job.ErrorMessage) {
			promptTokenRefresh(job)
		}
	}

	promptTokenRefresh = func(job *model.BatchJob) {
		if job == nil || promptedTokenJobID == job.ID {
			return
		}
		promptedTokenJobID = job.ID
		dialog.ShowConfirm(
			"社区通登录状态已失效",
			"批量查询已自动暂停，是否现在更新社区通登录状态并继续任务？",
			func(ok bool) {
				if !ok {
					return
				}
				current := session.Get()
				auth.ShowDoctorTokenDialog(window, current.HospitalCode, func(result *model.LoginResult) {
					updated := session.Get()
					doctor := updated.Doctor
					if strings.TrimSpace(result.Username) != "" {
						doctor.Account = strings.TrimSpace(result.Username)
					}
					session.Set(session.Info{
						Token:        result.Token,
						HospitalCode: updated.HospitalCode,
						Username:     updated.Username,
						Role:         updated.Role,
						Doctor:       doctor,
					})
					setBusy(true, "登录状态已更新，正在继续批量查询...")
					go func(jobID int64) {
						client := api.NewClient()
						err := client.ResumeBatchJob(jobID)
						if err == nil {
							var refreshed *model.BatchJob
							refreshed, err = client.GetBatchJob(jobID)
							fyne.Do(func() {
								setBusy(false, "")
								if err != nil {
									component.ShowError(err, window)
									return
								}
								applyJob(refreshed, 0)
								startPolling(jobID)
							})
							return
						}
						fyne.Do(func() {
							setBusy(false, "")
							component.ShowError(err, window)
						})
					}(job.ID)
				})
			},
			window,
		)
	}

	startPolling = func(jobID int64) {
		if pollCancel != nil {
			pollCancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		pollCancel = cancel
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			lastProcessed := -1
			lastTime := time.Now()

			for {
				job, err := api.NewClient().GetBatchJob(jobID)
				if err != nil {
					fyne.Do(func() {
						statusLabel.SetText("刷新任务进度失败：" + err.Error())
					})
				} else {
					now := time.Now()
					speed := 0.0
					if lastProcessed >= 0 {
						elapsed := now.Sub(lastTime).Seconds()
						if elapsed > 0 {
							speed = float64(job.ProcessedCount-lastProcessed) / elapsed
							if speed < 0 {
								speed = 0
							}
						}
					}
					lastProcessed = job.ProcessedCount
					lastTime = now
					fyne.Do(func() {
						applyJob(job, speed)
					})
					if job.Status != "running" {
						return
					}
				}

				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}

	runJobAction := func(message string, action func(*api.Client, int64) error, restartPolling bool) {
		if currentJob == nil {
			return
		}
		jobID := currentJob.ID
		setBusy(true, message)
		go func() {
			client := api.NewClient()
			err := action(client, jobID)
			var job *model.BatchJob
			if err == nil {
				job, err = client.GetBatchJob(jobID)
			}
			fyne.Do(func() {
				setBusy(false, "")
				if err != nil {
					component.ShowError(err, window)
					return
				}
				applyJob(job, 0)
				if restartPolling && job.Status == "running" {
					startPolling(jobID)
				}
			})
		}()
	}

	selectFileBtn = widget.NewButton("选择 Excel / CSV", func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				component.ShowError(err, window)
				return
			}
			if reader == nil {
				return
			}
			selectedURI = reader.URI()
			_ = reader.Close()
			if pollCancel != nil {
				pollCancel()
			}
			currentJob = nil
			promptedTokenJobID = 0
			fileLabel.SetText(filepath.Base(selectedURI.Name()))
			statusLabel.SetText("文件已选择，等待开始查询")
			progress.SetValue(0)
			totalValue.SetText("0")
			pendingValue.SetText("0")
			successValue.SetText("0")
			notFoundValue.SetText("0")
			failedValue.SetText("0")
			speedValue.SetText("0 人/秒")
			remainingValue.SetText("--")
			refreshControls()
		}, window)
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".xlsx", ".csv"}))
		fileDialog.Resize(fyne.NewSize(900, 650))
		fileDialog.Show()
	})

	recentJobsBtn = widget.NewButton("最近任务", func() {
		current := session.Get()
		if current.Token == "" {
			component.ShowInformation("提示", "当前社区通登录状态为空", window)
			return
		}
		setBusy(true, "正在读取最近批量查询任务...")
		go func() {
			jobs, err := api.NewClient().ListBatchJobs(current.HospitalCode, 20)
			fyne.Do(func() {
				setBusy(false, "")
				if err != nil {
					component.ShowError(err, window)
					return
				}
				if len(jobs) == 0 {
					component.ShowInformation("最近任务", "当前医院暂无批量查询任务", window)
					return
				}

				options := make([]string, len(jobs))
				byOption := make(map[string]*model.BatchJob, len(jobs))
				for index := range jobs {
					job := jobs[index]
					option := fmt.Sprintf(
						"#%d · %s · %s · %d/%d",
						job.ID,
						batchStatusName(job.Status),
						job.FileName,
						job.ProcessedCount,
						job.TotalCount,
					)
					options[index] = option
					jobCopy := job
					byOption[option] = &jobCopy
				}
				selector := widget.NewSelect(options, nil)
				selector.SetSelected(options[0])
				picker := dialog.NewCustomConfirm(
					"选择最近任务",
					"加载",
					"取消",
					container.NewPadded(selector),
					func(ok bool) {
						if !ok {
							return
						}
						job := byOption[selector.Selected]
						if job == nil {
							return
						}
						if pollCancel != nil {
							pollCancel()
						}
						selectedURI = nil
						fileLabel.SetText("已加载任务：" + job.FileName)
						applyJob(job, 0)
						if job.Status == "running" {
							startPolling(job.ID)
						}
					},
					window,
				)
				picker.Resize(fyne.NewSize(700, 240))
				picker.Show()
			})
		}()
	})

	startBtn = widget.NewButton("开始查询", func() {
		if currentJob != nil && currentJob.CanStart {
			runJobAction("正在启动批量查询...", func(client *api.Client, jobID int64) error {
				return client.StartBatchJob(jobID)
			}, true)
			return
		}
		if selectedURI == nil {
			component.ShowInformation("提示", "请先选择 Excel 或 CSV 文件", window)
			return
		}
		current := session.Get()
		if current.Token == "" {
			component.ShowInformation("提示", "当前社区通登录状态为空，请先更新登录状态", window)
			return
		}

		workerCount, _ := strconv.Atoi(workerSelect.Selected)
		fetchBatchSize, _ := strconv.Atoi(fetchBatchSelect.Selected)
		writeBatchSize, _ := strconv.Atoi(writeBatchSelect.Selected)
		uri := selectedURI
		setBusy(true, "正在流式上传并导入身份证数据，请稍候...")
		go func() {
			reader, err := storage.Reader(uri)
			if err != nil {
				fyne.Do(func() {
					setBusy(false, "")
					component.ShowError(err, window)
				})
				return
			}
			job, err := api.NewClient().CreateBatchJob(api.BatchJobCreateOptions{
				HospitalCode:   current.HospitalCode,
				CreatedBy:      current.Username,
				FileName:       uri.Name(),
				File:           reader,
				WorkerCount:    workerCount,
				FetchBatchSize: fetchBatchSize,
				WriteBatchSize: writeBatchSize,
			})
			_ = reader.Close()
			if err == nil {
				err = api.NewClient().StartBatchJob(job.ID)
			}
			if err == nil {
				job, err = api.NewClient().GetBatchJob(job.ID)
			}
			fyne.Do(func() {
				setBusy(false, "")
				if err != nil {
					component.ShowError(err, window)
					return
				}
				applyJob(job, 0)
				startPolling(job.ID)
			})
		}()
	})

	pauseBtn = widget.NewButton("暂停", func() {
		runJobAction("正在暂停批量查询...", func(client *api.Client, jobID int64) error {
			return client.PauseBatchJob(jobID)
		}, false)
	})
	continueBtn = widget.NewButton("继续", func() {
		runJobAction("正在继续批量查询...", func(client *api.Client, jobID int64) error {
			return client.ResumeBatchJob(jobID)
		}, true)
	})
	stopBtn = widget.NewButton("停止", func() {
		if currentJob == nil {
			return
		}
		dialog.ShowConfirm("停止批量查询", "停止后已完成的数据会保留，是否确认停止？", func(ok bool) {
			if !ok {
				return
			}
			runJobAction("正在停止批量查询...", func(client *api.Client, jobID int64) error {
				return client.StopBatchJob(jobID)
			}, false)
		}, window)
	})
	retryBtn = widget.NewButton("重试失败数据", func() {
		runJobAction("正在重置失败数据并重新查询...", func(client *api.Client, jobID int64) error {
			if err := client.RetryBatchJob(jobID); err != nil {
				return err
			}
			return client.StartBatchJob(jobID)
		}, true)
	})
	exportBtn = widget.NewButton("导出结果", func() {
		if currentJob == nil {
			return
		}
		jobID := currentJob.ID
		saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				component.ShowError(err, window)
				return
			}
			if writer == nil {
				return
			}
			setBusy(true, "正在流式导出查询结果...")
			go func() {
				exportErr := api.NewClient().ExportBatchJob(jobID, writer)
				closeErr := writer.Close()
				if exportErr == nil {
					exportErr = closeErr
				}
				fyne.Do(func() {
					setBusy(false, "")
					if exportErr != nil {
						component.ShowError(exportErr, window)
						return
					}
					component.ShowInformation("导出成功", "批量查询结果已保存", window)
				})
			}()
		}, window)
		saveDialog.SetFileName(fmt.Sprintf("批量查询结果_%d_%s.csv", jobID, time.Now().Format("20060102_150405")))
		saveDialog.SetFilter(storage.NewExtensionFileFilter([]string{".csv"}))
		saveDialog.Resize(fyne.NewSize(900, 650))
		saveDialog.Show()
	})

	startBtn.Disable()
	pauseBtn.Disable()
	continueBtn.Disable()
	stopBtn.Disable()
	retryBtn.Disable()
	exportBtn.Disable()

	fileCard := widget.NewCard(
		"1. 选择数据文件",
		"文件将边读边上传，服务端按行导入 MySQL，不会一次性加载到内存。",
		container.NewVBox(
			container.NewBorder(nil, nil, container.NewHBox(selectFileBtn, recentJobsBtn), nil, fileLabel),
			fileTip,
		),
	)

	parameterCard := widget.NewCard(
		"2. 查询参数",
		"默认配置适合普通电脑。并发数决定接口速度，领取和写入批次主要影响数据库效率。",
		widget.NewForm(
			widget.NewFormItem("查询并发数", workerSelect),
			widget.NewFormItem("数据库领取批次", fetchBatchSelect),
			widget.NewFormItem("结果写入批次", writeBatchSelect),
		),
	)

	statistics := container.NewGridWithColumns(
		7,
		batchStatisticCard("总数据", totalValue),
		batchStatisticCard("待查询", pendingValue),
		batchStatisticCard("成功", successValue),
		batchStatisticCard("查无此人", notFoundValue),
		batchStatisticCard("失败", failedValue),
		batchStatisticCard("当前速度", speedValue),
		batchStatisticCard("预计剩余", remainingValue),
	)

	operationCard := widget.NewCard(
		"3. 任务操作",
		"任务进度每秒刷新；暂停、停止或程序关闭后，已完成结果都会保留在数据库。",
		container.NewVBox(
			container.NewGridWithColumns(6, startBtn, pauseBtn, continueBtn, stopBtn, retryBtn, exportBtn),
			statusLabel,
			progress,
			statistics,
		),
	)

	columnTip := widget.NewLabel(
		"支持表头：身份证号、身份证号码、证件号码、证件号、id_card、idCard。输出包含查询状态、姓名、调阅时间、调阅机构、调阅科室、调阅人、调阅渠道和失败原因。",
	)
	columnTip.Wrapping = fyne.TextWrapWord

	content := container.NewVBox(
		widget.NewLabelWithStyle("居民档案批量查询", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		fileCard,
		parameterCard,
		operationCard,
		widget.NewCard("数据格式说明", "", columnTip),
	)

	refreshControls()
	return container.NewVScroll(container.NewPadded(content))
}

func batchStatisticValue() *widget.Label {
	return widget.NewLabelWithStyle("0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
}

func batchStatisticCard(title string, value fyne.CanvasObject) fyne.CanvasObject {
	return widget.NewCard("", title, container.NewCenter(value))
}

func setButtonEnabled(button *widget.Button, enabled bool) {
	if enabled {
		button.Enable()
	} else {
		button.Disable()
	}
}

func clampProgress(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func batchJobStatusText(job *model.BatchJob) string {
	status := map[string]string{
		"pending":   "等待开始",
		"running":   "查询中",
		"paused":    "已暂停",
		"completed": "已完成",
		"stopped":   "已停止",
		"failed":    "执行失败",
	}[job.Status]
	if status == "" {
		status = job.Status
	}
	text := fmt.Sprintf(
		"任务 #%d · %s · 已处理 %d/%d",
		job.ID, status, job.ProcessedCount, job.TotalCount,
	)
	if job.ErrorMessage != "" {
		text += " · " + job.ErrorMessage
	}
	return text
}

func batchStatusName(status string) string {
	names := map[string]string{
		"pending":   "等待开始",
		"running":   "查询中",
		"paused":    "已暂停",
		"completed": "已完成",
		"stopped":   "已停止",
		"failed":    "执行失败",
	}
	if name := names[status]; name != "" {
		return name
	}
	return status
}

func isBatchTokenMessage(message string) bool {
	message = strings.ToLower(message)
	for _, keyword := range []string{"token", "登录状态", "登录", "未授权", "过期"} {
		if strings.Contains(message, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func formatBatchDuration(duration time.Duration) string {
	if duration < time.Minute {
		seconds := int(duration.Seconds())
		if seconds < 1 {
			seconds = 1
		}
		return fmt.Sprintf("%d秒", seconds)
	}
	if duration < time.Hour {
		return fmt.Sprintf("%d分钟", int(duration.Minutes()+0.5))
	}
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%d小时", hours)
	}
	return fmt.Sprintf("%d小时%d分钟", hours, minutes)
}
