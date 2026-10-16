import { computed, reactive, ref } from 'vue'
import { ApiError } from '../api/client'
import * as feedApi from '../api/feed'
import type { FeedVideoItem } from '../api/types'
import { useAuthStore } from '../stores/auth'

export type TabKey = 'recommend' | 'latest' | 'hot' | 'following'

export function useVideoFeed() {
  const auth = useAuthStore()
  const tab = ref<TabKey>('recommend')

  const recommend = reactive({
    items: [] as FeedVideoItem[],
    loading: false, error: '',
    hasMore: true,
    source: 'vector' as 'vector' | 'following' | 'hot',
    batchStart: 0,
    hotOffset: 0,
    followTime: 0,
    likedIds: [] as number[],
  })

  const hot = reactive({
    items: [] as FeedVideoItem[],
    loading: false, error: '',
    hasMore: false,
    nextLikesCountBefore: undefined as number | undefined,
    nextIdBefore: undefined as number | undefined,
  })

  const latest = reactive({
    items: [] as FeedVideoItem[],
    loading: false, error: '',
    hasMore: false, nextTime: 0,
  })

  const following = reactive({
    items: [] as FeedVideoItem[],
    loading: false, error: '',
    hasMore: false, nextTime: 0,
  })

  const currentState = computed(() => {
    if (tab.value === 'latest') return latest
    if (tab.value === 'hot') return hot
    if (tab.value === 'following') return following
    return recommend
  })

  const seenIds = new Set<number>()
  let recommendTicket = 0

  function remember(items: FeedVideoItem[]) {
    for (const item of items) seenIds.add(item.id)
  }

  function noteRecommendLike(id: number, liked: boolean) {
    if (tab.value !== 'recommend') return
    if (!liked) {
      recommend.likedIds = recommend.likedIds.filter((item) => item !== id)
      return
    }
    if (!recommend.likedIds.includes(id)) recommend.likedIds.push(id)
    const idx = recommend.items.findIndex((item) => item.id === id)
    if (idx >= 0 && idx < recommend.items.length - 1) {
      recommend.items = recommend.items.slice(0, idx + 1)
    }
    recommend.hasMore = true
    void loadRecommend(false, { force: true, source: 'vector' })
  }

  function nextRecommendSource(): 'vector' | 'following' | 'hot' {
    if (recommend.likedIds.length > 0) return 'vector'
    if (recommend.source === 'vector') return 'following'
    if (recommend.source === 'following') return 'hot'
    return 'following'
  }

  function recommendOrder(reset: boolean, forced?: 'vector' | 'following' | 'hot') {
    const first = forced ?? (reset ? 'vector' : nextRecommendSource())
    const order: Array<'vector' | 'following' | 'hot'> = [first]
    if (first === 'vector') order.push('following', 'hot')
    else if (first === 'following') order.push('hot')
    return order
  }

  async function fetchRecommend(source: 'vector' | 'following' | 'hot') {
    const exclude = source === 'vector' ? [...seenIds].join(',') : ''
    if (source === 'following') {
      return feedApi.listRecommend({ source, latest_time: recommend.followTime, exclude })
    }
    if (source === 'hot') {
      return feedApi.listRecommend({ source, offset: recommend.hotOffset, exclude })
    }
    return feedApi.listRecommend({ source: 'vector', exclude })
  }

  async function loadRecommend(reset: boolean, opts?: { force?: boolean; source?: 'vector' | 'following' | 'hot' }) {
    if (recommend.loading && !opts?.force) return
    if (!reset && !opts?.force && !recommend.hasMore) return
    const ticket = ++recommendTicket
    recommend.loading = true
    recommend.error = ''
    try {
      if (reset) seenIds.clear()
      const order = recommendOrder(reset, opts?.source)
      let source: 'vector' | 'following' | 'hot' = order[0] ?? 'vector'
      let res = await fetchRecommend(source)
      if (ticket !== recommendTicket) return
      let fresh = res.video_list.filter((item) => !seenIds.has(item.id))
      for (const fallback of order.slice(1)) {
        if (fresh.length > 0) break
        source = fallback
        res = await fetchRecommend(source)
        if (ticket !== recommendTicket) return
        fresh = res.video_list.filter((item) => !seenIds.has(item.id))
      }
      if (ticket !== recommendTicket) return
      if (source === 'hot') recommend.hotOffset = res.next_offset || recommend.hotOffset + fresh.length
      if (source === 'following' && res.next_time) recommend.followTime = res.next_time
      remember(fresh)
      recommend.batchStart = reset ? 0 : recommend.items.length
      recommend.source = source
      recommend.likedIds = []
      recommend.items = reset ? fresh : recommend.items.concat(fresh)
      recommend.hasMore = fresh.length > 0
    } catch (e) {
      if (ticket !== recommendTicket) return
      recommend.error = e instanceof ApiError ? e.message : String(e)
    } finally {
      if (ticket === recommendTicket) recommend.loading = false
    }
  }

  async function loadLatest(reset: boolean) {
    if (latest.loading) return
    latest.loading = true
    latest.error = ''
    try {
      const res = await feedApi.listLatest({ limit: 10, latest_time: reset ? 0 : latest.nextTime })
      latest.hasMore = res.has_more
      latest.nextTime = res.next_time
      latest.items = reset ? res.video_list : latest.items.concat(res.video_list)
    } catch (e) {
      latest.error = e instanceof ApiError ? e.message : String(e)
    } finally {
      latest.loading = false
    }
  }

  async function loadHot(reset: boolean) {
    if (hot.loading) return
    hot.loading = true
    hot.error = ''
    try {
      const res = await feedApi.listLikesCount({
        limit: 10,
        likes_count_before: reset ? undefined : hot.nextLikesCountBefore,
        id_before: reset ? undefined : hot.nextIdBefore,
      })
      hot.hasMore = res.has_more
      hot.nextLikesCountBefore = res.next_likes_count_before
      hot.nextIdBefore = res.next_id_before
      hot.items = reset ? res.video_list : hot.items.concat(res.video_list)
    } catch (e) {
      hot.error = e instanceof ApiError ? e.message : String(e)
    } finally {
      hot.loading = false
    }
  }

  async function loadFollowing(reset: boolean) {
    if (!auth.isLoggedIn) {
      following.error = '登录后才能查看关注流'
      return
    }
    if (following.loading) return
    following.loading = true
    following.error = ''
    try {
      const res = await feedApi.listByFollowing({ limit: 10, latest_time: reset ? 0 : following.nextTime })
      following.hasMore = res.has_more
      following.nextTime = res.next_time
      following.items = reset ? res.video_list : following.items.concat(res.video_list)
    } catch (e) {
      following.error = e instanceof ApiError ? e.message : String(e)
    } finally {
      following.loading = false
    }
  }

  async function ensureTabLoaded() {
    if (tab.value === 'recommend' && recommend.items.length === 0) await loadRecommend(true)
    if (tab.value === 'latest' && latest.items.length === 0) await loadLatest(true)
    if (tab.value === 'hot' && hot.items.length === 0) await loadHot(true)
    if (tab.value === 'following' && following.items.length === 0) await loadFollowing(true)
  }

  async function loadMoreIfNeeded(activeIndex: number) {
    const items = currentState.value.items
    if (items.length === 0) return
    if (activeIndex < items.length - 3) return
    if (tab.value === 'recommend') {
      if (activeIndex >= items.length - 1 && recommend.hasMore) await loadRecommend(false)
      return
    }
    if (tab.value === 'latest' && latest.hasMore) await loadLatest(false)
    if (tab.value === 'hot' && hot.hasMore) await loadHot(false)
    if (tab.value === 'following' && following.hasMore) await loadFollowing(false)
  }

  return { tab, recommend, latest, hot, following, currentState, loadRecommend, loadLatest, loadHot, loadFollowing, ensureTabLoaded, loadMoreIfNeeded, noteRecommendLike }
}
