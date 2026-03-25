//go:build oem_qianxin

package brand

func init() {
	currentBrand = BrandConfig{
		ID:            "qianxin",
		DisplayName:   "TigerClaw",
		DisplayNameCN: "奇爪",
		WindowTitle:   "TigerClaw",
		TrayTooltip:   "TigerClaw Dashboard",
		IconPath:      "assets/qianxin.png",
		IcnsPath:      "assets/qianxin.icns",
		IcoPath:       "assets/qianxin.ico",
		MobileAppName: "TigerClaw",
		ExtraTools: []ExtraToolDef{
			{
				Name:        "tigerclaw",
				DisplayName: "TigerClaw Code",
				ConfigKey:   "tigerclaw",
			},
		},
	}
}
