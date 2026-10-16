package feed

import (
	"context"
	"feedsystem_video_go/internal/video"
	"strconv"
	"strings"
	"time"
)

type TagSearcher interface {
	SearchVideos(ctx context.Context, tags []string, limit int) ([]uint, error)
}

func splitRecommendQuota(limit int) (hot, following, vector int) {
	if limit <= 0 {
		return 0, 0, 0
	}
	weights := []int{2, 3, 5}
	counts := make([]int, 3)
	remainders := make([]int, 3)
	used := 0
	for i, weight := range weights {
		counts[i] = limit * weight / 10
		remainders[i] = limit * weight % 10
		used += counts[i]
	}
	left := limit - used
	for left > 0 {
		best := 0
		for i := 1; i < len(weights); i++ {
			if remainders[i] > remainders[best] || (remainders[i] == remainders[best] && weights[i] > weights[best]) {
				best = i
			}
		}
		counts[best]++
		remainders[best] = -1
		left--
	}
	return counts[0], counts[1], counts[2]
}

func mixRecommend(hot, following, vector []FeedVideoItem, limit int) []FeedVideoItem {
	if limit <= 0 {
		return nil
	}
	queues := map[string][]FeedVideoItem{
		"hot":       hot,
		"following": following,
		"vector":    vector,
	}
	pattern := []string{"vector", "following", "vector", "hot", "vector", "following", "vector", "following", "vector", "hot"}
	seen := map[uint]struct{}{}
	out := make([]FeedVideoItem, 0, limit)
	for len(out) < limit {
		moved := false
		for _, source := range pattern {
			if len(out) >= limit {
				break
			}
			if takeNext(queues, source, seen, &out) {
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	for _, source := range []string{"vector", "following", "hot"} {
		for len(out) < limit && takeNext(queues, source, seen, &out) {
		}
	}
	return out
}

func takeNext(queues map[string][]FeedVideoItem, source string, seen map[uint]struct{}, out *[]FeedVideoItem) bool {
	queue := queues[source]
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		queues[source] = queue
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		*out = append(*out, item)
		return true
	}
	queues[source] = queue
	return false
}

func parseExcludeIDs(raw string) map[uint]struct{} {
	out := map[uint]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil || n == 0 {
			continue
		}
		out[uint(n)] = struct{}{}
	}
	return out
}

func (f *FeedService) ListRecommend(ctx context.Context, source string, offset int, latestTime int64, viewerAccountID uint, exclude map[uint]struct{}) (ListRecommendResponse, error) {
	switch source {
	case "following":
		return f.recommendFollowing(ctx, viewerAccountID, latestTime)
	case "hot":
		return f.recommendHotPage(ctx, viewerAccountID, offset)
	default:
		return f.recommendVectorPage(ctx, viewerAccountID, exclude)
	}
}

func (f *FeedService) recommendVectorPage(ctx context.Context, viewerAccountID uint, exclude map[uint]struct{}) (ListRecommendResponse, error) {
	items, err := f.recommendVector(ctx, viewerAccountID, 5, f.tagSearcher, exclude)
	if err != nil {
		return ListRecommendResponse{}, err
	}
	if items == nil {
		items = []FeedVideoItem{}
	}
	return ListRecommendResponse{VideoList: items, Source: "vector", HasMore: len(items) > 0}, nil
}

func (f *FeedService) recommendFollowing(ctx context.Context, viewerAccountID uint, latestTime int64) (ListRecommendResponse, error) {
	if viewerAccountID == 0 {
		return ListRecommendResponse{VideoList: []FeedVideoItem{}, Source: "following"}, nil
	}
	var before time.Time
	if latestTime > 0 {
		before = time.Unix(latestTime, 0)
	}
	resp, err := f.ListByFollowing(ctx, 3, before, viewerAccountID)
	if err != nil {
		return ListRecommendResponse{}, err
	}
	if resp.VideoList == nil {
		resp.VideoList = []FeedVideoItem{}
	}
	return ListRecommendResponse{
		VideoList: resp.VideoList,
		Source:    "following",
		NextTime:  resp.NextTime,
		HasMore:   len(resp.VideoList) > 0,
	}, nil
}

func (f *FeedService) recommendHotPage(ctx context.Context, viewerAccountID uint, offset int) (ListRecommendResponse, error) {
	items, err := f.recommendHot(ctx, viewerAccountID, 2, offset)
	if err != nil {
		return ListRecommendResponse{}, err
	}
	if items == nil {
		items = []FeedVideoItem{}
	}
	return ListRecommendResponse{
		VideoList:  items,
		Source:     "hot",
		NextOffset: offset + len(items),
		HasMore:    len(items) > 0,
	}, nil
}

func (f *FeedService) recommendHot(ctx context.Context, viewerAccountID uint, limit, offset int) ([]FeedVideoItem, error) {
	if limit <= 0 {
		return nil, nil
	}
	resp, err := f.ListByPopularity(ctx, limit, 0, offset, viewerAccountID, 0, time.Time{}, 0)
	if err != nil {
		return nil, err
	}
	items := resp.VideoList
	if items == nil {
		items = []FeedVideoItem{}
	}
	if len(items) >= limit {
		return items[:limit], nil
	}
	videos, err := f.repo.ListByPopularity(ctx, limit+len(items)+8, 0, time.Time{}, 0)
	if err != nil {
		return items, nil
	}
	seen := map[uint]struct{}{}
	for _, item := range items {
		seen[item.ID] = struct{}{}
	}
	extra := make([]*video.Video, 0, limit-len(items))
	for _, item := range videos {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		extra = append(extra, item)
		if len(items)+len(extra) >= limit {
			break
		}
	}
	built, err := f.buildFeedVideos(ctx, extra, viewerAccountID)
	if err != nil || len(built) == 0 {
		return items, nil
	}
	return append(items, built...), nil
}

func (f *FeedService) recommendVector(ctx context.Context, viewerAccountID uint, limit int, searcher TagSearcher, exclude map[uint]struct{}) ([]FeedVideoItem, error) {
	if viewerAccountID == 0 || searcher == nil {
		return nil, nil
	}
	like, err := f.likeRepo.LatestLiked(ctx, viewerAccountID)
	if err != nil || like == nil || like.VideoID == 0 {
		return nil, err
	}
	tags, err := f.repo.ListTagNames(ctx, like.VideoID)
	if err != nil || len(tags) == 0 {
		return nil, err
	}
	ids, err := searcher.SearchVideos(ctx, tags, 50)
	if err != nil {
		return nil, err
	}
	filtered := make([]uint, 0, limit)
	for _, id := range ids {
		if id == like.VideoID {
			continue
		}
		if _, skip := exclude[id]; skip {
			continue
		}
		filtered = append(filtered, id)
		if len(filtered) == limit {
			break
		}
	}
	videos, err := f.repo.GetByIDs(ctx, filtered)
	if err != nil {
		return nil, err
	}
	byID := map[uint]*video.Video{}
	for _, item := range videos {
		byID[item.ID] = item
	}
	ordered := make([]*video.Video, 0, len(filtered))
	for _, id := range filtered {
		if item := byID[id]; item != nil {
			ordered = append(ordered, item)
		}
	}
	return f.buildFeedVideos(ctx, ordered, viewerAccountID)
}
