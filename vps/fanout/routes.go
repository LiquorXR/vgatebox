package main

// apiRoute 描述一条对外 HTTP 路由契约：路径、允许方法、处理函数与认证方式。
//
// testdata/api/routes.json 是它的金样本；TestAPIContract 校验三者一致：
//  1. allAPIRoutes() 与金样本完全一致（改路由必须同步改表和金样本）；
//  2. 源码中的 mux.HandleFunc 注册集合与金样本一致（HANDLES 全覆盖，
//     杜绝注册了却没有契约、或有契约却没有注册的漂移）；
//  3. routes 注册点不再出现匿名 HandleFunc（图谱 HANDLES 边可归因）。
//
// methods 取值：GET、POST、DELETE 的组合；认证方式：
//   - session：需要管理登录态（Auth.Wrap 未登录时 /api/* 回 401）
//   - token：免登录但验独立口令（/sub 由 handleSub 自验 token）
//   - mixed：页面入口，未登录跳登录页（/ 由 handleIndex + Auth.Wrap 决定）
type apiRoute struct {
	Path    string   `json:"path"`
	Methods []string `json:"methods"`
	Handler string   `json:"handler"`
	Auth    string   `json:"auth"`
}

// allAPIRoutes 返回全部后端路由契约，按 path 排序。
// main.go 的 36 条 + Console 的 13 条 + SstpManager 的 5 条 +
// ThirdPartyManager 的 5 条 = 59 条。
func allAPIRoutes() []apiRoute {
	return []apiRoute{
		{Path: "/", Methods: []string{"GET", "HEAD"}, Handler: "handleIndex", Auth: "mixed"},
		{Path: "/api/config/rollback", Methods: []string{"GET", "POST"}, Handler: "apiRollbackNative", Auth: "session"},
		{Path: "/api/console", Methods: []string{"GET"}, Handler: "Console.apiConsole", Auth: "session"},
		{Path: "/api/console/automation", Methods: []string{"GET", "POST"}, Handler: "Console.apiAutomation", Auth: "session"},
		{Path: "/api/console/check", Methods: []string{"POST"}, Handler: "Console.apiCheck", Auth: "session"},
		{Path: "/api/console/check/cancel", Methods: []string{"POST"}, Handler: "Console.apiCheckCancel", Auth: "session"},
		{Path: "/api/console/check/settings", Methods: []string{"GET", "POST"}, Handler: "Console.apiCheckSettings", Auth: "session"},
		{Path: "/api/console/direct", Methods: []string{"POST"}, Handler: "Console.apiHostDirect", Auth: "session"},
		{Path: "/api/console/ippure-cache", Methods: []string{"GET", "POST", "DELETE"}, Handler: "Console.apiIPPureCache", Auth: "session"},
		{Path: "/api/console/qr", Methods: []string{"GET"}, Handler: "Console.apiConsoleQR", Auth: "session"},
		{Path: "/api/console/refresh", Methods: []string{"POST"}, Handler: "Console.apiPoolRefresh", Auth: "session"},
		{Path: "/api/console/slot/delete", Methods: []string{"POST"}, Handler: "Console.apiDeleteSlot", Auth: "session"},
		{Path: "/api/console/socks", Methods: []string{"GET", "POST"}, Handler: "Console.apiPublicSocks", Auth: "session"},
		{Path: "/api/console/source-settings", Methods: []string{"GET", "POST"}, Handler: "Console.apiSourceSettings", Auth: "session"},
		{Path: "/api/console/target", Methods: []string{"POST"}, Handler: "Console.apiApplyTarget", Auth: "session"},
		{Path: "/api/cred", Methods: []string{"POST"}, Handler: "apiCred", Auth: "session"},
		{Path: "/api/exits", Methods: []string{"GET"}, Handler: "apiExits", Auth: "session"},
		{Path: "/api/export/xray", Methods: []string{"GET"}, Handler: "apiExportXray", Auth: "session"},
		{Path: "/api/jobs", Methods: []string{"GET"}, Handler: "apiJobs", Auth: "session"},
		{Path: "/api/jobs/dismiss", Methods: []string{"POST"}, Handler: "apiJobDismiss", Auth: "session"},
		{Path: "/api/nodes", Methods: []string{"GET"}, Handler: "apiNodes", Auth: "session"},
		{Path: "/api/panel/client/add", Methods: []string{"POST"}, Handler: "apiClientAdd", Auth: "session"},
		{Path: "/api/panel/client/del", Methods: []string{"POST"}, Handler: "apiClientDelete", Auth: "session"},
		{Path: "/api/panel/client/reset", Methods: []string{"POST"}, Handler: "apiClientReset", Auth: "session"},
		{Path: "/api/panel/inbound/new", Methods: []string{"POST"}, Handler: "apiInboundCreate", Auth: "session"},
		{Path: "/api/panel/inbound/update", Methods: []string{"POST"}, Handler: "apiInboundUpdate", Auth: "session"},
		{Path: "/api/provision", Methods: []string{"POST"}, Handler: "apiProvision", Auth: "session"},
		{Path: "/api/regions", Methods: []string{"GET"}, Handler: "apiRegions", Auth: "session"},
		{Path: "/api/resources/sstp", Methods: []string{"GET"}, Handler: "SstpManager.apiView", Auth: "session"},
		{Path: "/api/resources/sstp/cancel", Methods: []string{"POST"}, Handler: "SstpManager.apiCancelCheck", Auth: "session"},
		{Path: "/api/resources/sstp/check", Methods: []string{"POST"}, Handler: "SstpManager.apiStartCheck", Auth: "session"},
		{Path: "/api/resources/sstp/export", Methods: []string{"GET"}, Handler: "SstpManager.apiExport", Auth: "session"},
		{Path: "/api/resources/sstp/settings", Methods: []string{"POST"}, Handler: "SstpManager.apiSettings", Auth: "session"},
		{Path: "/api/resources/third-party", Methods: []string{"GET", "POST"}, Handler: "ThirdPartyManager.apiResources", Auth: "session"},
		{Path: "/api/resources/third-party/check", Methods: []string{"POST"}, Handler: "ThirdPartyManager.apiStartCheck", Auth: "session"},
		{Path: "/api/resources/third-party/delete", Methods: []string{"POST"}, Handler: "ThirdPartyManager.thirdPartyActionHandler", Auth: "session"},
		{Path: "/api/resources/third-party/subscribe", Methods: []string{"POST"}, Handler: "ThirdPartyManager.thirdPartyActionHandler", Auth: "session"},
		{Path: "/api/resources/third-party/toggle", Methods: []string{"POST"}, Handler: "ThirdPartyManager.thirdPartyActionHandler", Auth: "session"},
		{Path: "/api/settings", Methods: []string{"GET", "POST"}, Handler: "apiSettings", Auth: "session"},
		{Path: "/api/slot/delete", Methods: []string{"POST"}, Handler: "apiSlotLifecycle", Auth: "session"},
		{Path: "/api/slot/policy", Methods: []string{"GET", "POST"}, Handler: "apiSlotPolicy", Auth: "session"},
		{Path: "/api/slot/policy/rollback", Methods: []string{"POST"}, Handler: "apiRollbackSlotPolicy", Auth: "session"},
		{Path: "/api/slot/resume", Methods: []string{"POST"}, Handler: "apiSlotLifecycle", Auth: "session"},
		{Path: "/api/stop", Methods: []string{"POST"}, Handler: "apiStop", Auth: "session"},
		{Path: "/api/sub", Methods: []string{"GET"}, Handler: "apiSub", Auth: "session"},
		{Path: "/api/sub/reset", Methods: []string{"POST"}, Handler: "apiSubReset", Auth: "session"},
		{Path: "/api/swap", Methods: []string{"POST"}, Handler: "apiSwap", Auth: "session"},
		{Path: "/api/update/apply", Methods: []string{"POST"}, Handler: "UpdateManager.apply", Auth: "session"},
		{Path: "/api/update/check", Methods: []string{"GET"}, Handler: "UpdateManager.check", Auth: "session"},
		{Path: "/api/update/status", Methods: []string{"GET"}, Handler: "UpdateManager.status", Auth: "session"},
		{Path: "/api/xui/bind", Methods: []string{"POST"}, Handler: "apiXUIBind", Auth: "session"},
		{Path: "/api/xui/clone", Methods: []string{"POST"}, Handler: "apiXUIClone", Auth: "session"},
		{Path: "/api/xui/delete", Methods: []string{"POST"}, Handler: "apiXUIDelete", Auth: "session"},
		{Path: "/api/xui/detail", Methods: []string{"GET"}, Handler: "apiXUIDetail", Auth: "session"},
		{Path: "/api/xui/encryption/rotate", Methods: []string{"POST"}, Handler: "apiXUIRotateEncryption", Auth: "session"},
		{Path: "/api/xui/encryption/set", Methods: []string{"POST"}, Handler: "apiXUISetEncryption", Auth: "session"},
		{Path: "/api/xui/inbounds", Methods: []string{"GET"}, Handler: "apiXUIInbounds", Auth: "session"},
		{Path: "/api/xui/links", Methods: []string{"GET"}, Handler: "apiXUILinks", Auth: "session"},
		{Path: "/sub", Methods: []string{"GET"}, Handler: "handleSub", Auth: "token"},
	}
}
