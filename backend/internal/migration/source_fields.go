package migration

import "strings"

// historicalFields 列出可导入的数据字段，用于识别导入报告中的未知项。
var historicalFields = map[string]map[string]map[string]bool{
	"1.1.50": {
		"Ani": set(
			"backRss", "bangumiId", "cover", "currentEpisodeNumber",
			"customDownloadPath", "downloadPath", "enable", "exclude",
			"globalExclude", "id", "match", "month",
			"offset", "ova", "pinyin", "season",
			"subgroup", "themoviedbName", "title", "totalEpisodeNumber",
			"type", "url", "year",
		),
		"Config": set(
			"acronym", "autoDisabled", "backRss", "debug",
			"delete", "download", "downloadCount", "downloadPath",
			"exclude", "fileExist", "host", "ipWhitelist",
			"ipWhitelistStr", "login", "mail", "mailAccount",
			"mailAddressee", "mikanHost", "offset", "ovaDownloadPath",
			"password", "proxy", "proxyHost", "proxyPort",
			"qbRenameTitle", "qbUseDownloadPath", "quarter", "rename",
			"renameSleep", "rss", "seasonName", "skip5",
			"sleep", "telegram", "telegramApiHost", "telegramBotToken",
			"telegramChatId", "titleYear", "tmdb", "username",
			"watchErrorTorrent", "webHook", "webHookBody", "webHookMethod",
			"webHookUrl",
		),
	},
	"1.2.82": {
		"Ani": set(
			"backRss", "backRssList", "bgmUrl", "cover",
			"currentEpisodeNumber", "customDownloadPath", "customEpisode", "customEpisodeGroupIndex",
			"customEpisodeStr", "customRenameTemplate", "customRenameTemplateEnable", "date",
			"downloadNew", "downloadPath", "enable", "exclude",
			"exists", "globalExclude", "id", "image",
			"jpTitle", "lastDownloadTime", "match", "month",
			"notDownload", "offset", "omit", "ova",
			"pinyin", "procrastinating", "score", "season",
			"subgroup", "themoviedbName", "title", "tmdb",
			"totalEpisodeNumber", "type", "upload", "url",
			"week", "year",
		),
		"Config": set(
			"acronym", "alist", "alistDownloadRetryNumber", "alistDownloadTimeout",
			"alistHost", "alistOvaPath", "alistPath", "alistRefresh",
			"alistRefreshDelayed", "alistRetry", "alistTask", "alistToken",
			"apiKey", "autoDisabled", "autoTrackersUpdate", "autoUpdate",
			"awaitStalledUP", "backRss", "bgmImage", "bgmJpName",
			"bgmToken", "coexist", "configBackup", "configBackupDay",
			"customCss", "customEpisode", "customEpisodeGroupIndex", "customEpisodeStr",
			"customGithub", "customGithubUrl", "customJs", "debug",
			"delayedDownload", "delete", "deleteBackRSSOnly", "deleteFiles",
			"dlLimit", "download", "downloadCount", "downloadNew",
			"downloadPath", "downloadRetry", "embyApiKey", "embyDelayed",
			"embyHost", "embyRefresh", "embyRefreshViewIds", "enabledExclude",
			"exclude", "expirationTime", "fileExist", "gcSleep",
			"github", "githubToken", "host", "importExclude",
			"inactiveSeedingTimeLimit", "innerIP", "ipWhitelist", "ipWhitelistStr",
			"login", "loginEffectiveHours", "logsMax", "mail",
			"mailAccount", "mailAddressee", "mailImage", "messageList",
			"messageTemplate", "mikanHost", "multiLoginForbidden", "offset",
			"omit", "outTradeNo", "ovaDownloadPath", "password",
			"procrastinating", "procrastinatingDay", "provider", "proxy",
			"proxyHost", "proxyPassword", "proxyPort", "proxyUsername",
			"qbUseDownloadPath", "quarter", "quarterMerge", "ratioLimit",
			"rename", "renameDelTmdbId", "renameDelYear", "renameSleepSeconds",
			"renameTemplate", "rss", "rssTimeout", "scoreShow",
			"seasonName", "seedingTimeLimit", "serverChan", "serverChan3ApiUrl",
			"serverChanSendKey", "serverChanTitleAction", "serverChanType", "showLastDownloadTime",
			"showPlaylist", "skip5", "sleep", "systemMsg",
			"telegram", "telegramApiHost", "telegramBotToken", "telegramChatId",
			"telegramFormat", "telegramImage", "telegramTopicId", "titleYear",
			"tmdb", "tmdbAnime", "tmdbApi", "tmdbApiKey",
			"tmdbId", "tmdbLanguage", "trackersUpdateUrls", "tryOut",
			"tvShowNfo", "upLimit", "updateTotalEpisodeNumber", "upload",
			"username", "verifyExpirationTime", "verifyLoginIp", "version",
			"watchErrorTorrent", "webHook", "webHookBody", "webHookMethod",
			"webHookUrl", "weekShow", "yearStorage",
		),
	},
	"3.2.52": {
		"Ani": set(
			"bgmUrl", "completed", "cover", "currentEpisodeNumber",
			"customCompleted", "customCompletedPathTemplate", "customDownloadPath", "customDownloadPathTemplate",
			"customEpisode", "customEpisodeGroupIndex", "customEpisodeStr", "customPriorityKeywords",
			"customPriorityKeywordsEnable", "customRenameTemplate", "customRenameTemplateEnable", "customTags",
			"customTagsEnable", "customUploadEnable", "customUploadPathTarget", "date",
			"downloadNew", "enable", "exclude", "exists",
			"globalExclude", "id", "image", "jpTitle",
			"lastDownloadTime", "match", "message", "mikanTitle",
			"month", "notDownload", "offset", "omit",
			"ova", "pinyin", "pinyinInitials", "procrastinating",
			"releaseDate", "score", "season", "sort",
			"standbyRssList", "subgroup", "themoviedbName", "title",
			"tmdb", "totalEpisodeNumber", "type", "upload",
			"url", "weekLabel", "year",
		),
		"Config": set(
			"allowCors", "apiKey", "autoDisabled", "autoStart",
			"autoTrackersUpdate", "autoUpdate", "awaitStalledUP", "bangumiIniEnabled",
			"bgmApi", "bgmAppID", "bgmAppSecret", "bgmImageSize",
			"bgmJpName", "bgmRedirectUri", "bgmRefreshToken", "bgmToken",
			"bgmTokenType", "coexist", "completed", "completedPathTemplate",
			"configBackup", "configBackupDay", "customCss", "customEpisode",
			"customEpisodeGroupIndex", "customEpisodeStr", "customJs", "customTags",
			"debug", "delayedDownload", "delete", "deleteStandbyRSSOnly",
			"dlLimit", "downloadCount", "downloadNew", "downloadPathTemplate",
			"downloadRetry", "downloadToolHost", "downloadToolPassword", "downloadToolType",
			"downloadToolUsername", "enabledExclude", "exclude", "expirationTime",
			"fileExist", "followDay", "forceUpdateTotalEpisodeNumber", "gitInfo",
			"githubToken", "importExclude", "inactiveSeedingTimeLimit", "innerIP",
			"ipWhitelist", "ipWhitelistStr", "jwtKey", "limitLoginAttempts",
			"login", "loginEffectiveHours", "logsMax", "maxFileNameLength",
			"mikanHost", "multiLoginForbidden", "notificationConfigList", "notificationTemplate",
			"offset", "omit", "outTradeNo", "ovaDownloadPathTemplate",
			"priorityKeywords", "priorityKeywordsEnable", "procrastinating", "procrastinatingDay",
			"procrastinatingMasterOnly", "proxy", "proxyHost", "proxyList",
			"proxyPassword", "proxyPort", "proxyUsername", "qbContentLayout",
			"qbUseDownloadPath", "ratioLimit", "rename", "renameDelTmdbId",
			"renameDelYear", "renameSleepSeconds", "renameTemplate", "reverseProxyTrustIpList",
			"reverseProxyTrustIpListEnabled", "rss", "rssSleepMinutes", "rssTimeout",
			"scrape", "seedingTimeLimit", "skip5", "sortType",
			"standbyRss", "subtitleIndependentFolderEnabled", "subtitleIndependentFolderName", "titleYear",
			"tmdb", "tmdbAnime", "tmdbApi", "tmdbApiKey",
			"tmdbId", "tmdbIdPlexMode", "tmdbImage", "tmdbLanguage",
			"tmdbOriginalName", "tmdbRomaji", "tokenId", "trackersUpdateUrls",
			"tryOut", "upLimit", "updateTotalEpisodeNumber", "upload",
			"uuid", "verifyExpirationTime", "verifyLoginIp", "version",
		),
	},
}

// 1.x 升级会保留旧字段；使用两个固定样本的字段并集识别，转换仍按实际字段执行。
func fieldsForFormat(version, model string) map[string]bool {
	result := map[string]bool{}
	versions := []string{strings.TrimPrefix(version, "v")}
	if strings.HasPrefix(version, "v1.") {
		versions = []string{"1.1.50", "1.2.82"}
	}
	for _, source := range versions {
		for name := range historicalFields[source][model] {
			result[name] = true
		}
	}
	return result
}

func configFields(version string) map[string]bool       { return fieldsForFormat(version, "Config") }
func subscriptionFields(version string) map[string]bool { return fieldsForFormat(version, "Ani") }
