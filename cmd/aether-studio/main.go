package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"aether-rea/internal/systemproxy"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type studioState struct {
	bestIP      string
	sessionID   int
	lastRotate  time.Time
	proxyActive bool
}

func main() {
	studioApp := app.NewWithID("aether-rea.client-studio")
	win := studioApp.NewWindow("Aether-Realist Client Studio")
	win.Resize(fyne.NewSize(1100, 720))

	state := &studioState{sessionID: 1}
	content := buildUI(win, state)
	win.SetContent(content)

	if deskApp, ok := studioApp.(desktop.App); ok {
		setupTray(studioApp, deskApp, win)
		win.SetCloseIntercept(func() {
			win.Hide()
		})
	}

	win.ShowAndRun()
}

func buildUI(win fyne.Window, state *studioState) fyne.CanvasObject {
	domainEntry := widget.NewEntry()
	domainEntry.SetText("your-domain.com")
	pskEntry := widget.NewPasswordEntry()
	listenEntry := widget.NewEntry()
	listenEntry.SetText("127.0.0.1:1080")
	rotateEntry := widget.NewEntry()
	rotateEntry.SetText("20m")
	paddingEntry := widget.NewEntry()
	paddingEntry.SetText("128")

	commandStatus := widget.NewLabel("")
	copyButton := widget.NewButton("复制启动命令", func() {
		command := buildCommand(domainEntry.Text, pskEntry.Text, listenEntry.Text, rotateEntry.Text, paddingEntry.Text, state.bestIP)
		win.Clipboard().SetContent(command)
		commandStatus.SetText("命令已复制到剪贴板")
	})

	form := widget.NewForm(
		widget.NewFormItem("入口域名", domainEntry),
		widget.NewFormItem("PSK", pskEntry),
	)

	row := container.NewGridWithColumns(2,
		widget.NewForm(widget.NewFormItem("本地监听", listenEntry)),
		widget.NewForm(widget.NewFormItem("轮换周期", rotateEntry)),
	)

	paddingRow := widget.NewForm(widget.NewFormItem("Padding 上限", paddingEntry))

	configCard := widget.NewCard("连接配置", "", container.NewVBox(
		form,
		row,
		paddingRow,
		copyButton,
		commandStatus,
	))

	ipListLabel := widget.NewLabel("等待加载…")
	ipList := widget.NewList(
		func() int { return 0 },
		func() fyne.CanvasObject {
			ipLabel := widget.NewLabel("")
			tagLabel := widget.NewLabel("")
			return container.NewHBox(ipLabel, layout.NewSpacer(), tagLabel)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {},
	)
	ipList.Hide()

	bestIPLabel := widget.NewLabel("未选择")

	refreshButton := widget.NewButton("刷新列表", func() {
		fetchIPList(context.Background(), func(ips []string, err error) {
			if err != nil {
				ipListLabel.SetText("无法获取 IP 列表")
				ipList.Show()
				ipList.Refresh()
				return
			}
			if len(ips) == 0 {
				ipListLabel.SetText("未找到可用 IP")
				ipList.Show()
				ipList.Refresh()
				return
			}
			ipListLabel.SetText("")
			ipList.Show()

			ipList.Length = func() int { return len(ips) }
			ipList.UpdateItem = func(id widget.ListItemID, obj fyne.CanvasObject) {
				row := obj.(*fyne.Container)
				ipLabel := row.Objects[0].(*widget.Label)
				tagLabel := row.Objects[2].(*widget.Label)
				ipLabel.SetText(ips[id])
				if id == 0 {
					tagLabel.SetText("建议")
				} else {
					tagLabel.SetText("候选")
				}
			}

			ipList.OnSelected = func(id widget.ListItemID) {
				state.bestIP = ips[id]
				bestIPLabel.SetText(state.bestIP)
			}
			ipList.Refresh()
			ipList.Select(0)
		})
	})

	ipListCard := widget.NewCard("IP 优选", "从 https://ip.v2too.top/ 获取节点，并计算最优 UDP 443 对应 IP。", container.NewVBox(
		container.NewHBox(refreshButton, layout.NewSpacer()),
		ipListLabel,
		ipList,
	))

	sessionLabel := widget.NewLabel(fmt.Sprintf("#%d", state.sessionID))
	lastRotateLabel := widget.NewLabel("--")

	markRotateButton := widget.NewButton("记录轮换", func() {
		state.lastRotate = time.Now()
		lastRotateLabel.SetText(state.lastRotate.Format("15:04:05"))
	})

	monitorCard := widget.NewCard("运行监控", "", container.NewVBox(
		container.NewGridWithColumns(3,
			container.NewVBox(widget.NewLabel("当前会话"), sessionLabel),
			container.NewVBox(widget.NewLabel("上次轮换"), lastRotateLabel),
			container.NewVBox(widget.NewLabel("最佳 IP"), bestIPLabel),
		),
		markRotateButton,
		widget.NewLabel("建议将选中的 IP 设置为 --dial-addr，保持 TLS SNI 为入口域名。"),
	))

	proxyStatus := widget.NewLabel("系统代理未启用")
	proxyNote := widget.NewLabel("使用本地监听地址作为系统 SOCKS5 代理。")
	proxyToggle := widget.NewCheck("启用系统代理", nil)
	proxyToggle.OnChanged = func(enabled bool) {
		if enabled {
			if err := systemproxy.EnableSocksProxy(listenEntry.Text); err != nil {
				proxyStatus.SetText(fmt.Sprintf("启用失败: %v", err))
				proxyToggle.SetChecked(false)
				return
			}
			state.proxyActive = true
			proxyStatus.SetText("系统代理已启用")
			return
		}
		if err := systemproxy.DisableSocksProxy(); err != nil {
			proxyStatus.SetText(fmt.Sprintf("关闭失败: %v", err))
			proxyToggle.SetChecked(true)
			return
		}
		state.proxyActive = false
		proxyStatus.SetText("系统代理未启用")
	}

	proxyCard := widget.NewCard("系统代理", "", container.NewVBox(
		proxyStatus,
		proxyNote,
		proxyToggle,
	))

	leftColumn := container.NewVBox(configCard, monitorCard)
	rightColumn := container.NewVBox(ipListCard, proxyCard)

	layout := container.NewGridWithColumns(2, leftColumn, rightColumn)

	refreshButton.OnTapped()

	return container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("Aether-Realist Client Studio", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			widget.NewLabel("可视化配置、IP 优选、会话轮换与 WebTransport 代理管理。"),
		),
		nil,
		nil,
		nil,
		container.NewPadded(layout),
	)
}

func setupTray(app fyne.App, deskApp desktop.App, win fyne.Window) {
	showItem := fyne.NewMenuItem("显示窗口", func() {
		win.Show()
		win.RequestFocus()
	})
	quitItem := fyne.NewMenuItem("退出", func() {
		win.Close()
		app.Quit()
	})
	menu := fyne.NewMenu("Aether-Realist", showItem, quitItem)
	deskApp.SetSystemTrayMenu(menu)
}

func fetchIPList(ctx context.Context, callback func([]string, error)) {
	go func() {
		client := &http.Client{Timeout: 8 * time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ip.v2too.top/", nil)
		if err != nil {
			runOnMain(func() {
				callback(nil, err)
			})
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			runOnMain(func() {
				callback(nil, err)
			})
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			runOnMain(func() {
				callback(nil, err)
			})
			return
		}
		text := strings.TrimSpace(string(body))
		fields := strings.Fields(text)
		unique := map[string]struct{}{}
		for _, ip := range fields {
			unique[ip] = struct{}{}
		}
		ips := make([]string, 0, len(unique))
		for ip := range unique {
			ips = append(ips, ip)
		}
		sort.Strings(ips)
		if len(ips) > 12 {
			ips = ips[:12]
		}
		runOnMain(func() {
			callback(ips, nil)
		})
	}()
}

type mainRunner interface {
	RunOnMain(func())
}

func runOnMain(fn func()) {
	current := fyne.CurrentApp()
	if current == nil {
		fn()
		return
	}
	if runner, ok := current.Driver().(mainRunner); ok {
		runner.RunOnMain(fn)
		return
	}
	fn()
}

func buildCommand(domain, psk, listen, rotate, padding, bestIP string) string {
	if strings.TrimSpace(psk) == "" {
		psk = "<PSK>"
	}
	parts := []string{
		"./aether-client",
		fmt.Sprintf("--url https://%s/v1/api/sync", strings.TrimSpace(domain)),
		fmt.Sprintf("--psk \"%s\"", psk),
		fmt.Sprintf("--listen %s", strings.TrimSpace(listen)),
		fmt.Sprintf("--rotate %s", strings.TrimSpace(rotate)),
		fmt.Sprintf("--max-padding %s", strings.TrimSpace(padding)),
	}
	if bestIP != "" && bestIP != "未选择" {
		parts = append(parts, fmt.Sprintf("--dial-addr %s:443", bestIP))
	}
	return strings.Join(parts, " ")
}
