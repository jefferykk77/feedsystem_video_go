package feed

import "testing"

func TestSplitRecommendQuota(t *testing.T) {
	hot, following, vector := splitRecommendQuota(10)
	if hot != 2 || following != 3 || vector != 5 {
		t.Fatalf("quota = %d %d %d", hot, following, vector)
	}
	hot, following, vector = splitRecommendQuota(1)
	if hot+following+vector != 1 || vector != 1 {
		t.Fatalf("quota = %d %d %d", hot, following, vector)
	}
}

func TestMixRecommendKeepsRatioAndSkipsDuplicates(t *testing.T) {
	item := func(id uint) FeedVideoItem { return FeedVideoItem{ID: id} }
	hot := []FeedVideoItem{item(1), item(2), item(3)}
	following := []FeedVideoItem{item(1), item(4), item(5), item(6)}
	vector := []FeedVideoItem{item(7), item(8), item(9), item(10), item(2), item(11)}
	got := mixRecommend(hot, following, vector, 10)
	if len(got) != 10 {
		t.Fatalf("len=%d", len(got))
	}
	seen := map[uint]struct{}{}
	for _, video := range got {
		if _, ok := seen[video.ID]; ok {
			t.Fatalf("duplicate %d", video.ID)
		}
		seen[video.ID] = struct{}{}
	}
}
