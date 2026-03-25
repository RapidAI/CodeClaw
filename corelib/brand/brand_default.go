//go:build !oem_qianxin

package brand

func init() {
	currentBrand = BrandConfig{
		ID:            "maclaw",
		DisplayName:   "MaClaw",
		DisplayNameCN: "码卡龙",
		WindowTitle:   "MaClaw",
		TrayTooltip:   "MaClaw Dashboard",
		IconPath:      "build/appicon.png",
		IcnsPath:      "build/AppIcon.icns",
		IcoPath:       "build/windows/icon.ico",
		MobileAppName: "MaClaw Chat",
		ExtraTools:    []ExtraToolDef{},
	}
}
