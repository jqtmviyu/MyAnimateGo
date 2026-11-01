package discovery

import (
	"encoding/json"
	"time"
)

type mikan struct {
	Seasons   []mikanSeason `json:"seasons"`
	Weeks     []mikanWeek   `json:"weeks"`
	TotalItem int           `json:"totalItem"`
}
type mikanSeason struct {
	Year        int    `json:"year,omitempty"`
	Season      string `json:"season,omitempty"`
	SeasonLabel string `json:"seasonLabel,omitempty"`
	Select      bool   `json:"select"`
}
type mikanWeek struct {
	WeekLabel string      `json:"weekLabel"`
	Items     []mikanInfo `json:"items"`
}
type mikanInfo struct {
	BgmID      string       `json:"bgmId,omitempty"`
	Cover      string       `json:"cover"`
	URL        string       `json:"url"`
	Exists     bool         `json:"exists"`
	Score      *float64     `json:"score,omitempty"`
	Title      string       `json:"title"`
	UpdateDate string       `json:"updateDate,omitempty"`
	BgmURL     string       `json:"bgmUrl,omitempty"`
	Groups     []mikanGroup `json:"groups,omitempty"`
}
type mikanGroup struct {
	SubgroupID string      `json:"subgroupId,omitempty"`
	Label      string      `json:"label"`
	RSS        string      `json:"rss"`
	BgmURL     string      `json:"bgmUrl,omitempty"`
	UpdateDay  string      `json:"updateDay,omitempty"`
	Items      []mikanItem `json:"items"`
	GroupRegex groupRegex  `json:"groupRegex"`
}
type mikanItem struct {
	Title      string    `json:"title"`
	Magnet     string    `json:"magnet,omitempty"`
	Size       *int64    `json:"size,omitempty"`
	FormatSize string    `json:"formatSize,omitempty"`
	CreatedAt  *javaDate `json:"createdAt,omitempty"`
	Torrent    string    `json:"torrent,omitempty"`
}
type groupRegex struct {
	RegexList [][]regexItem `json:"regexList"`
	Tags      []string      `json:"tags"`
}
type regexItem struct {
	Regex string `json:"regex"`
	Label string `json:"label"`
}

type aniBT struct {
	Total     int  `json:"total"`
	Limit     int  `json:"limit"`
	Truncated bool `json:"truncated"`
	Scope     struct {
		Kind   string `json:"kind"`
		Season string `json:"season,omitempty"`
	} `json:"scope"`
	CurrentSeason    string      `json:"currentSeason,omitempty"`
	RequestedSeason  string      `json:"requestedSeason,omitempty"`
	AvailableSeasons []string    `json:"availableSeasons"`
	ByWeekday        []aniBTWeek `json:"byWeekday"`
}
type aniBTWeek struct {
	Animes       []aniBTAnime `json:"animes"`
	Weekday      *int         `json:"weekday"`
	WeekdayLabel string       `json:"weekdayLabel"`
}
type aniBTAnime struct {
	Episodes          int        `json:"episodes,omitempty"`
	AiringAt          int64      `json:"airingAt,omitempty"`
	Weekday           int        `json:"weekday,omitempty"`
	ScheduleStatus    string     `json:"scheduleStatus,omitempty"`
	SeasonLabel       string     `json:"seasonLabel,omitempty"`
	PremiereDate      string     `json:"premiereDate,omitempty"`
	PremierePrecision string     `json:"premierePrecision,omitempty"`
	HasRelease        bool       `json:"hasRelease"`
	AnimeID           string     `json:"animeId,omitempty"`
	BgmID             string     `json:"bgmId,omitempty"`
	Cover             string     `json:"cover,omitempty"`
	Rating            float64    `json:"rating,omitempty"`
	Title             aniBTTitle `json:"title"`
	Format            string     `json:"format,omitempty"`
	Exists            bool       `json:"exists"`
	RSSReleaseCount   int        `json:"rssReleaseCount"`
}
type aniBTTitle struct {
	Japanese           string `json:"japanese,omitempty"`
	Chinese            string `json:"chinese,omitempty"`
	ChineseTraditional string `json:"chineseTraditional,omitempty"`
	English            string `json:"english,omitempty"`
	Primary            string `json:"primary,omitempty"`
	Romaji             string `json:"romaji,omitempty"`
}
type aniBTGroup struct {
	TotalReleases     int         `json:"totalReleases"`
	PublishedEpisodes []float64   `json:"publishedEpisodes,omitempty"`
	BgmID             string      `json:"bgmId"`
	GroupID           string      `json:"groupId,omitempty"`
	Slug              string      `json:"slug"`
	Name              string      `json:"name,omitempty"`
	Status            string      `json:"status,omitempty"`
	LastUpdatedAt     int64       `json:"lastUpdatedAt,omitempty"`
	Items             []aniBTItem `json:"items"`
	RSS               string      `json:"rss"`
	GroupRegex        groupRegex  `json:"groupRegex"`
}
type aniBTItem struct {
	Format      string   `json:"format,omitempty"`
	CustomTags  []string `json:"customTags,omitempty"`
	Torrent     string   `json:"torrent,omitempty"`
	EpisodeKey  string   `json:"episodeKey,omitempty"`
	Language    []string `json:"language,omitempty"`
	Magnet      string   `json:"magnet,omitempty"`
	PublishedAt int64    `json:"publishedAt,omitempty"`
	ReleaseID   string   `json:"releaseId,omitempty"`
	Resolution  string   `json:"resolution,omitempty"`
	Subtitle    string   `json:"subtitle,omitempty"`
	Title       string   `json:"title"`
	Size        int64    `json:"size,omitempty"`
	FormatSize  string   `json:"formatSize,omitempty"`
}

type animeGardenWeek struct {
	WeekLabel string               `json:"weekLabel"`
	Subjects  []animeGardenSubject `json:"subjects"`
}
type animeGardenSubject struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Keywords   []string  `json:"keywords,omitempty"`
	ActivedAt  *javaDate `json:"activedAt,omitempty"`
	IsArchived string    `json:"isArchived,omitempty"`
	WeekLabel  string    `json:"weekLabel,omitempty"`
	Exists     bool      `json:"exists"`
	Score      float64   `json:"score"`
	Cover      string    `json:"cover"`
}
type animeGardenGroup struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	LastUpdatedAt *javaDate         `json:"lastUpdatedAt,omitempty"`
	Items         []animeGardenItem `json:"items"`
	RSS           string            `json:"rss"`
	BgmID         string            `json:"bgmId"`
	GroupRegex    groupRegex        `json:"groupRegex"`
}
type animeGardenItem struct {
	ID         string                `json:"id,omitempty"`
	Provider   string                `json:"provider,omitempty"`
	ProviderID string                `json:"providerId,omitempty"`
	Title      string                `json:"title"`
	Href       string                `json:"href,omitempty"`
	Type       string                `json:"type,omitempty"`
	Magnet     string                `json:"magnet,omitempty"`
	Size       int64                 `json:"size,omitempty"`
	FormatSize string                `json:"formatSize,omitempty"`
	CreatedAt  *javaDate             `json:"createdAt,omitempty"`
	FetchedAt  *javaDate             `json:"fetchedAt,omitempty"`
	SubjectID  string                `json:"subjectId,omitempty"`
	Publisher  *animeGardenPublisher `json:"publisher,omitempty"`
	Fansub     *animeGardenFansub    `json:"fansub,omitempty"`
}
type animeGardenFansub struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type animeGardenPublisher struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}

type bgmInfo struct {
	ID       string           `json:"id,omitempty"`
	URL      string           `json:"url,omitempty"`
	Name     string           `json:"name,omitempty"`
	NameCN   string           `json:"nameCn,omitempty"`
	Eps      int              `json:"eps,omitempty"`
	Date     *javaDate        `json:"date,omitempty"`
	Images   bgmImages        `json:"images"`
	Season   int              `json:"season,omitempty"`
	Platform string           `json:"platform,omitempty"`
	Tags     []bgmTag         `json:"tags,omitempty"`
	Infobox  []map[string]any `json:"infobox,omitempty"`
	Rating   *bgmRating       `json:"rating,omitempty"`
}

func (info *bgmInfo) UnmarshalJSON(data []byte) error {
	type plain bgmInfo
	var value struct {
		plain
		IDRaw     json.RawMessage `json:"id"`
		NameCNAlt string          `json:"name_cn"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*info = bgmInfo(value.plain)
	if len(value.IDRaw) > 0 && string(value.IDRaw) != "null" {
		if value.IDRaw[0] == '"' {
			if err := json.Unmarshal(value.IDRaw, &info.ID); err != nil {
				return err
			}
		} else {
			var id json.Number
			if err := json.Unmarshal(value.IDRaw, &id); err != nil {
				return err
			}
			info.ID = id.String()
		}
	}
	if info.NameCN == "" {
		info.NameCN = value.NameCNAlt
	}
	return nil
}

type bgmImages struct {
	Small  string `json:"small,omitempty"`
	Grid   string `json:"grid,omitempty"`
	Large  string `json:"large,omitempty"`
	Medium string `json:"medium,omitempty"`
	Common string `json:"common,omitempty"`
}
type bgmTag struct {
	Name      string `json:"name"`
	Count     string `json:"count,omitempty"`
	TotalCont string `json:"totalCont,omitempty"`
}

func (tag *bgmTag) UnmarshalJSON(data []byte) error {
	var value struct {
		Name      string          `json:"name"`
		Count     json.RawMessage `json:"count"`
		TotalCont json.RawMessage `json:"totalCont"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	tag.Name = value.Name
	for _, field := range []struct {
		raw    json.RawMessage
		target *string
	}{{value.Count, &tag.Count}, {value.TotalCont, &tag.TotalCont}} {
		if len(field.raw) == 0 || string(field.raw) == "null" {
			continue
		}
		if field.raw[0] == '"' {
			if err := json.Unmarshal(field.raw, field.target); err != nil {
				return err
			}
		} else {
			var number json.Number
			if err := json.Unmarshal(field.raw, &number); err != nil {
				return err
			}
			*field.target = number.String()
		}
	}
	return nil
}

type bgmRating struct {
	Rank  int            `json:"rank,omitempty"`
	Score float64        `json:"score,omitempty"`
	Total int            `json:"total,omitempty"`
	Count map[string]int `json:"count,omitempty"`
}
type bgmEpisode struct {
	Airdate         *javaDate `json:"airdate,omitempty"`
	Name            string    `json:"name,omitempty"`
	NameCN          string    `json:"nameCn,omitempty"`
	Duration        string    `json:"duration,omitempty"`
	Desc            string    `json:"desc,omitempty"`
	Ep              float64   `json:"ep,omitempty"`
	Sort            float64   `json:"sort,omitempty"`
	ID              string    `json:"id,omitempty"`
	SubjectID       int       `json:"subjectId,omitempty"`
	Comment         int       `json:"comment,omitempty"`
	Type            int       `json:"type,omitempty"`
	Disc            int       `json:"disc,omitempty"`
	DurationSeconds int       `json:"durationSeconds,omitempty"`
}

func (episode *bgmEpisode) UnmarshalJSON(data []byte) error {
	type plain bgmEpisode
	var value struct {
		plain
		IDRaw              json.RawMessage `json:"id"`
		NameCNAlt          string          `json:"name_cn"`
		SubjectIDAlt       int             `json:"subject_id"`
		DurationSecondsAlt int             `json:"duration_seconds"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*episode = bgmEpisode(value.plain)
	if len(value.IDRaw) > 0 && string(value.IDRaw) != "null" {
		if value.IDRaw[0] == '"' {
			if err := json.Unmarshal(value.IDRaw, &episode.ID); err != nil {
				return err
			}
		} else {
			var id json.Number
			if err := json.Unmarshal(value.IDRaw, &id); err != nil {
				return err
			}
			episode.ID = id.String()
		}
	}
	if episode.NameCN == "" {
		episode.NameCN = value.NameCNAlt
	}
	if episode.SubjectID == 0 {
		episode.SubjectID = value.SubjectIDAlt
	}
	if episode.DurationSeconds == 0 {
		episode.DurationSeconds = value.DurationSecondsAlt
	}
	return nil
}

type tmdbVO struct {
	ThemoviedbName string         `json:"themoviedbName"`
	TMDB           map[string]any `json:"tmdb"`
}
type tmdbDTO struct {
	TMDBID string `json:"tmdbId"`
	Title  string `json:"title"`
	OVA    bool   `json:"ova"`
}

func subscribedBgmIDs(subscriptions []map[string]any) map[string]bool {
	ids := map[string]bool{}
	for _, subscription := range subscriptions {
		if id := subjectIDFromURL(text(subscription["bgmUrl"])); id != "" {
			ids[id] = true
		}
	}
	return ids
}

type javaDate struct{ time.Time }

func (d javaDate) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.Format("2006-01-02 15:04:05") + `"`), nil
}
func (d *javaDate) UnmarshalJSON(data []byte) error {
	value := string(data)
	if len(value) > 1 && value[0] == '"' {
		value = value[1 : len(value)-1]
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02", time.RFC3339, "2006"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			d.Time = parsed
			return nil
		}
	}
	return nil
}
