//go:build darwin

package main

import (
	"context"
	"log"
	"time"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func setupTray(app *App, appOptions *options.App) {
	// We still use a basic Application Menu for macOS to support standard shortcuts
	appMenu := menu.NewMenu()
	appMenu.Append(menu.AppMenu())
	appMenu.Append(menu.EditMenu())
	appOptions.Menu = appMenu

	// On macOS we keep the original OnStartup (app.startup) as-is and only
	// override OnDomReady to defer systray initialization until the WebView
	// and window are fully created.  On macOS 26 (Tahoe) with Liquid Glass,
	// initializing systray during OnStartup races with first-frame rendering
	// and can cause a crash.
	origDomReady := appOptions.OnDomReady
	appOptions.OnDomReady = func(ctx context.Context) {
		if origDomReady != nil {
			origDomReady(ctx)
		}

		// Use RunWithExternalLoop since Wails already owns the NSApplication event loop.
		start, _ := systray.RunWithExternalLoop(func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[tray] panic in onReady: %v", r)
				}
			}()

			systray.SetIcon(icon)
			systray.SetTooltip("MaClaw Dashboard")
			systray.CreateMenu()

			mShow := systray.AddMenuItem("Show Main Window", "Show Main Window")
			systray.AddSeparator()
			mQuit := systray.AddMenuItem("Quit", "Quit Application")

			UpdateTrayMenu = func(lang string) {
				t, ok := trayTranslations[lang]
				if !ok {
					t = trayTranslations["en"]
				}
				systray.SetTooltip(t["title"])
				mShow.SetTitle(t["show"])
				mQuit.SetTitle(t["quit"])
			}

			OnConfigChanged = func(cfg AppConfig) {
				runtime.EventsEmit(app.ctx, "config-changed", cfg)
			}

			ShowNotification = func(title, message string, iconFlag uint32) {
				_ = systray.ShowBalloonNotification(title, message, iconFlag)
			}

			FlashAndBeep = func() {
				systray.FlashAndBeep()
			}

			mShow.Click(func() {
				go runtime.WindowShow(app.ctx)
			})

			mQuit.Click(func() {
				go func() {
					systray.Quit()
					time.Sleep(100 * time.Millisecond)
					runtime.Quit(app.ctx)
				}()
			})

			if app.CurrentLanguage != "" {
				go func() {
					time.Sleep(500 * time.Millisecond)
					UpdateTrayMenu(app.CurrentLanguage)
				}()
			}
		}, func() {})

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[tray] panic in nativeStart: %v", r)
				}
			}()
			start()
		}()
	}
}
