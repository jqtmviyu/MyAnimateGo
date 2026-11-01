package app

import (
	"context"
	"encoding/json"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/discovery"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/mcpapi"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

func mcpOperations(api *server.Server, catalog *discovery.Client, engine *pipeline.Engine) mcpapi.Operations {
	resolve := func(ctx context.Context, input mcpapi.RssToAniInput) (map[string]any, error) {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		var dto map[string]any
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return catalog.ResolveSubscription(ctx, dto, engine)
	}
	return mcpapi.Operations{
		SearchMikan: func(ctx context.Context, input mcpapi.SearchMikanInput) (any, error) {
			var season *discovery.MikanSeason
			if input.Season != nil {
				season = &discovery.MikanSeason{}
				if input.Season.Year != nil {
					season.Year = *input.Season.Year
				}
				if input.Season.Season != nil {
					season.Season = *input.Season.Season
				}
				if input.Season.SeasonLabel != nil {
					season.SeasonLabel = *input.Season.SeasonLabel
				}
				if input.Season.Select != nil {
					season.Select = *input.Season.Select
				}
			}
			return catalog.SearchMikan(ctx, input.Text, season)
		},
		SearchAniBT:          func(ctx context.Context) (any, error) { return catalog.SearchAniBT(ctx, discovery.AniBTSearchInput{}) },
		SearchAnimeGarden:    func(ctx context.Context) (any, error) { return catalog.SearchAnimeGarden(ctx, "") },
		GetMikanGroups:       catalog.GetMikanGroups,
		GetAniBTGroups:       catalog.GetAniBTGroups,
		GetAnimeGardenGroups: catalog.GetAnimeGardenGroups,
		PreviewSubscriptionItems: func(ctx context.Context, input mcpapi.RssToAniInput) (any, error) {
			ani, err := resolve(ctx, input)
			if err != nil {
				return nil, err
			}
			preview, err := engine.Preview(ctx, ani)
			if err != nil {
				return nil, err
			}
			return map[string]any{"subscription": ani, "items": preview["items"]}, nil
		},
		AddSubscription: func(ctx context.Context, input mcpapi.RssToAniInput) (any, error) {
			ani, err := resolve(ctx, input)
			if err != nil {
				return nil, err
			}
			return api.AddSubscription(ctx, ani, false)
		},
	}
}
