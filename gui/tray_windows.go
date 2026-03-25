//go:build windows

package main

import (
	"os"
	stdruntime "runtime"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func setupTray(app *App, appOptions *options.App) {
	// Start the systray immediately (before wails.Run) so the tray icon
	// appears as soon as the process launches, instead of waiting for the
	// Wails WebView to finish initialising.
	go func() {
		// Lock the OS thread for the systray message loop on Windows
		stdruntime.LockOSThread()

		systray.Run(func() {
			systray.SetIcon(icon)
			systray.SetTitle(brand.Current().DisplayName)
			systray.SetTooltip(brand.Current().TrayTooltip)
			systray.SetOnDClick(func(menu systray.IMenu) {
				go func() {
					if app.ctx == nil {
						return
					}
					runtime.WindowShow(app.ctx)
					runtime.WindowSetAlwaysOnTop(app.ctx, true)
					runtime.WindowSetAlwaysOnTop(app.ctx, false)
				}()
			})

			mShow := systray.AddMenuItem("Show", "Show Main Window")
			systray.AddSeparator()
			mQuit := systray.AddMenuItem("Quit", "Quit Application")

			isVisible := !app.IsAutoStart

			// Register update function
			UpdateTrayMenu = func(lang string) {
				tr := trayTranslations()
				t, ok := tr[lang]
				if !ok {
					t = tr["en"]
				}
				systray.SetTitle(t["title"])
				systray.SetTooltip(t["title"])
				if isVisible {
					mShow.SetTitle(t["hide"])
				} else {
					mShow.SetTitle(t["show"])
				}
				mQuit.SetTitle(t["quit"])
			}

			UpdateTrayVisibility = func(visible bool) {
				isVisible = visible
				UpdateTrayMenu(app.CurrentLanguage)
			}

			// Register config change listener
			OnConfigChanged = func(cfg AppConfig) {
				if app.ctx == nil {
					return
				}
				runtime.EventsEmit(app.ctx, "config-changed", cfg)
			}

			// System notification (not available in upstream energye/systray)
			ShowNotification = func(title, message string, iconFlag uint32) {
				_, _, _ = title, message, iconFlag
			}

			// Flash + beep (not available in upstream energye/systray)
			FlashAndBeep = func() {
			}

			// Handle menu clicks
			mShow.Click(func() {
				go func() {
					if app.ctx == nil {
						return
					}
					if isVisible {
						runtime.WindowHide(app.ctx)
						isVisible = false
					} else {
						runtime.WindowShow(app.ctx)
						runtime.WindowSetAlwaysOnTop(app.ctx, true)
						runtime.WindowSetAlwaysOnTop(app.ctx, false)
						isVisible = true
					}
					UpdateTrayMenu(app.CurrentLanguage)
				}()
			})

			mQuit.Click(func() {
				go func() {
					if app.ctx == nil {
						// Wails hasn't started yet; just exit.
						os.Exit(0)
						return
					}
					runtime.Quit(app.ctx)
					// Give wails a moment to run OnShutdown cleanup
					time.Sleep(500 * time.Millisecond)
					systray.Quit()
				}()
			})

			if app.CurrentLanguage != "" {
				UpdateTrayMenu(app.CurrentLanguage)
			}
		}, func() {
			// systray message loop exited; force-kill the process as a safety net
			os.Exit(0)
		})
	}()
}
